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
