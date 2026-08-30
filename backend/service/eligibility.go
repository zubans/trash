package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"healthlogin/backend/repository"
)

// Business defaults. Each one can be overridden through system_settings so the
// rules are not buried in the code as magic numbers.
const (
	defaultMaxActiveOrders        = 3
	defaultMaxExecutedUnconfirmed = 6
	defaultRejectPenaltyShare     = 0.5
	defaultMinBalanceLimit        = 0.0
)

// ErrExecutorNotEligible reports that an executor may not take a specific order.
var ErrExecutorNotEligible = errors.New("executor is not eligible for this order")

// ErrCustomerNotEligible reports that a customer may not order a specific service variant.
var ErrCustomerNotEligible = errors.New("customer is not eligible for this service")

// canCustomerOrderVariant is the single place that decides whether a customer
// may place an order for a service variant. A variant flagged
// requires_verification can only be ordered by a manually verified customer —
// the mirror of canExecutorTakeOrder on the executor side. It is used both when
// filtering the catalog and when the order is actually created, so the gate
// cannot be bypassed by posting a known variant id directly. Age (min_age) is
// deliberately an executor-side gate only: it restricts who may perform the job,
// not who may request it.
func canCustomerOrderVariant(customer *repository.User, variant *repository.ServiceNode) error {
	if customer == nil {
		return ErrCustomerNotEligible
	}
	if customer.Status == "BANNED" {
		return errors.New("аккаунт заблокирован")
	}
	if variant == nil {
		return nil
	}
	if variant.RequiresVerification && !customer.IsVerified() {
		return errors.New("для этой услуги требуется подтверждённый аккаунт")
	}
	return nil
}

// formatGeo renders a coordinate pair in the "lat,lon" form used by
// customer_profiles.last_geo. The column is parsed as coordinates by the
// matching worker, so nothing else may be written into it.
func formatGeo(lat, lon float64) string {
	return fmt.Sprintf("%f,%f", lat, lon)
}

// settingsGetter is the small slice of SettingsRepository used for tunables.
type settingsGetter interface {
	GetSettings(ctx context.Context) (map[string]string, error)
}

func settingFloat(ctx context.Context, repo settingsGetter, key string, defaultValue float64) float64 {
	if repo == nil {
		return defaultValue
	}
	settings, err := repo.GetSettings(ctx)
	if err != nil {
		return defaultValue
	}
	if v, ok := settings[key]; ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return defaultValue
}

func settingInt(ctx context.Context, repo settingsGetter, key string, defaultValue int) int {
	return int(settingFloat(ctx, repo, key, float64(defaultValue)))
}

// canExecutorTakeOrder is the single place that decides whether an executor is
// allowed to work on a given service variant. It is used both when filtering
// the order lists and when the executor actually acts on an order, so the
// restrictions cannot be bypassed by calling the endpoint directly with a
// known order id.
func canExecutorTakeOrder(executor *repository.User, variant *repository.ServiceNode) error {
	if executor == nil {
		return ErrExecutorNotEligible
	}
	if executor.Status == "BANNED" {
		return errors.New("аккаунт заблокирован")
	}
	if variant == nil {
		return nil
	}
	if variant.RequiresVerification && !executor.IsVerified() {
		return errors.New("для этого заказа требуется подтверждённый аккаунт")
	}
	if variant.MinAge > 0 && executor.GetAge() < variant.MinAge {
		return fmt.Errorf("для этого заказа требуется возраст не менее %d лет", variant.MinAge)
	}
	return nil
}

// canViewOrTakeOrder is the single predicate that decides whether a viewer (an
// executor and/or moderator) may both SEE and ACCEPT a given order. The order
// lists (map + table) and the accept path all go through it, so what an executor
// can act on never diverges from what they were shown.
//
// Rules:
//   - Moderator-only service: only a MODERATOR may see or take the order; the
//     normal executor gates do not apply (moderators are trusted staff).
//   - Normal service: the executor gates (ban, requires_verification, min_age)
//     apply, and on top of them a customer-verification segmentation —
//       * an unverified customer's order is visible to everyone (this is what
//         lets an unverified executor work the unverified pool);
//       * a verified customer's order is visible only to a verified executor or
//         to a moderator.
func canViewOrTakeOrder(viewer *repository.User, customer *repository.User, variant *repository.ServiceNode) error {
	if viewer == nil {
		return ErrExecutorNotEligible
	}
	if variant != nil && variant.ModeratorOnly {
		if !viewer.HasRole(repository.RoleModerator) {
			return ErrExecutorNotEligible
		}
		if viewer.Status == "BANNED" {
			return errors.New("аккаунт заблокирован")
		}
		return nil
	}
	if err := canExecutorTakeOrder(viewer, variant); err != nil {
		return err
	}
	if customer != nil && customer.IsVerified() {
		if !viewer.IsVerified() && !viewer.HasRole(repository.RoleModerator) {
			return ErrExecutorNotEligible
		}
	}
	return nil
}
