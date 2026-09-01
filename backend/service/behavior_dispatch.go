package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/behavior"
	"healthlogin/backend/metrics"
	"healthlogin/backend/money"
	"healthlogin/backend/repository"
)

// SettingBehaviorMaxBonus caps a single scripted payout, in rubles. The script
// decides the amount; this decides how wrong a script is allowed to be.
const SettingBehaviorMaxBonus = "behavior_max_bonus"

const defaultBehaviorMaxBonus = 5000.0

// ConfigVerifierRole is the configuration key naming the role that may perform a
// service. The core reads it for the verify_user guard; a behaviour that uses
// verify_user has to declare it (see behaviors/verification/config.star).
const ConfigVerifierRole = "verifier_role"

// BehaviorDispatcher delivers domain events to the behaviour scripts and
// applies the effects they ask for.
//
// The split it enforces is the point of the whole design. A script decides
// *what* should happen and says so in effects; this type decides whether the
// asker was entitled to it and performs it through the ordinary services — the
// ledger, the order lifecycle, the verification flag. A script can therefore be
// wrong about a decision, but it cannot pay a stranger, verify an arbitrary
// user, or write an unbalanced pair of ledger entries.
type BehaviorDispatcher struct {
	events      repository.EventRepository
	orders      repository.OrderRepository
	users       repository.UserRepository
	catalog     repository.ServiceCatalogRepository
	claims      repository.ServiceClaimRepository
	chat        repository.ChatRepository
	settings    repository.SettingsRepository
	submissions repository.SubmissionRepository
	ledger      *Ledger
	behaviors   *Behaviors
	orderSvc    *OrderService

	// batchSize bounds one tick; maxAttempts bounds one event's lifetime, so a
	// permanently failing event stops consuming the batch instead of blocking
	// every event behind it forever.
	batchSize   int
	maxAttempts int

	// Processed events are kept as history for this long, and swept no more
	// often than purgeEvery. The window is far longer than any redelivery, so
	// an idempotency key never disappears while its event could still come back.
	retention  time.Duration
	purgeEvery time.Duration
	mu         sync.Mutex
	lastPurge  time.Time
}

// NewBehaviorDispatcher wires the dispatcher. It needs the order service
// because completing and cancelling an order must go through exactly the same
// code a customer's own confirmation does.
func NewBehaviorDispatcher(
	events repository.EventRepository,
	orders repository.OrderRepository,
	users repository.UserRepository,
	catalog repository.ServiceCatalogRepository,
	claims repository.ServiceClaimRepository,
	chat repository.ChatRepository,
	settings repository.SettingsRepository,
	ledger *Ledger,
	behaviors *Behaviors,
	orderSvc *OrderService,
) *BehaviorDispatcher {
	return &BehaviorDispatcher{
		events: events, orders: orders, users: users, catalog: catalog,
		claims: claims, chat: chat, settings: settings, ledger: ledger,
		behaviors: behaviors, orderSvc: orderSvc,
		batchSize: 50, maxAttempts: 10,
		retention: 30 * 24 * time.Hour, purgeEvery: time.Hour,
	}
}

// WithSubmissions wires the store behind data checks and escalations. Without
// it a behaviour that declares check_fields simply takes no submissions.
func (d *BehaviorDispatcher) WithSubmissions(submissions repository.SubmissionRepository) *BehaviorDispatcher {
	d.submissions = submissions
	return d
}

// Tick processes one batch of pending events. It is called on a timer by the
// behaviour worker, under the leader guard.
func (d *BehaviorDispatcher) Tick(ctx context.Context) error {
	if d == nil || d.events == nil || d.behaviors == nil {
		return nil
	}
	events, err := d.events.ClaimPending(ctx, d.batchSize, d.maxAttempts)
	if err != nil {
		return err
	}
	for _, event := range events {
		if _, err := d.dispatch(ctx, event); err != nil {
			metrics.BehaviorEvent(event.Type, "failed")
			log.Printf("[behavior] event %s (%s) failed: %v", event.ID, event.Type, err)
			// Left unprocessed on purpose: the next tick retries it, up to
			// maxAttempts. The reason is stored so it can be read without
			// digging through logs.
			_ = d.events.MarkFailed(ctx, event.ID, err.Error())
			continue
		}
		metrics.BehaviorEvent(event.Type, "processed")
		if err := d.events.MarkProcessed(ctx, event.ID); err != nil {
			log.Printf("[behavior] event %s applied but not marked processed: %v", event.ID, err)
		}
	}
	if pending, err := d.events.CountPending(ctx); err == nil {
		metrics.SetBehaviorBacklog(pending)
	}
	d.purge(ctx)
	return nil
}

// purge trims processed history, at most once per purgeEvery. A failure is
// logged and nothing else: the table growing slowly is not a reason to stop
// dispatching.
func (d *BehaviorDispatcher) purge(ctx context.Context) {
	d.mu.Lock()
	due := time.Since(d.lastPurge) >= d.purgeEvery
	if due {
		d.lastPurge = time.Now()
	}
	d.mu.Unlock()
	if !due {
		return
	}
	if removed, err := d.events.PurgeProcessed(ctx, d.retention); err != nil {
		log.Printf("[behavior] cannot trim processed events: %v", err)
	} else if removed > 0 {
		log.Printf("[behavior] trimmed %d processed events older than %s", removed, d.retention)
	}
}

// target is one order a behaviour may act on in response to an event.
type target struct {
	order   *repository.Order
	variant *repository.ServiceNode
}

// dispatch resolves who the event concerns and runs their behaviours. It
// returns the messages the behaviours posted, for the caller that is waiting on
// the outcome — an executor who has just submitted data for checking.
func (d *BehaviorDispatcher) dispatch(ctx context.Context, event *repository.DomainEvent) ([]string, error) {
	targets, err := d.targets(ctx, event)
	if err != nil {
		return nil, err
	}
	var messages []string
	for _, t := range targets {
		manifest, ok := d.behaviors.Manifest(t.variant)
		if !ok || !manifest.Handles(event.Type) {
			continue
		}
		facts, err := d.facts(ctx, event, t)
		if err != nil {
			return messages, err
		}
		effects, err := d.behaviors.Engine().OnEvent(d.behaviors.Code(t.variant), facts)
		if err != nil {
			metrics.BehaviorHookError(d.behaviors.Code(t.variant), behavior.HookOnEvent)
			return messages, err
		}
		if len(effects) == 0 {
			continue
		}
		posted, err := d.apply(ctx, event, t, effects)
		messages = append(messages, posted...)
		if err != nil {
			return messages, err
		}
	}
	return messages, nil
}

// targets answers "which running orders can this event change".
//
//   - An order event concerns its own order and nothing else.
//   - A user event concerns every order that user still has running. That is
//     what lets "this customer is now verified" close the verification order
//     they had open, without the admin action knowing anything about services.
func (d *BehaviorDispatcher) targets(ctx context.Context, event *repository.DomainEvent) ([]target, error) {
	var orders []*repository.Order
	switch event.SubjectType {
	case repository.EventSubjectOrder:
		order, err := d.orders.GetOrderByID(ctx, event.SubjectID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, nil
			}
			return nil, err
		}
		orders = []*repository.Order{order}
	case repository.EventSubjectUser:
		open, err := d.orders.FindOpenByCustomer(ctx, event.SubjectID)
		if err != nil {
			return nil, err
		}
		orders = open
	default:
		return nil, fmt.Errorf("unknown event subject %q", event.SubjectType)
	}

	targets := make([]target, 0, len(orders))
	for _, order := range orders {
		if order == nil {
			continue
		}
		variant, err := d.catalog.GetNodeByID(ctx, order.ServiceVariantID)
		if err != nil || variant == nil || !variant.HasBehavior() {
			continue
		}
		targets = append(targets, target{order: order, variant: variant})
	}
	return targets, nil
}

func (d *BehaviorDispatcher) facts(ctx context.Context, event *repository.DomainEvent, t target) (behavior.Facts, error) {
	facts := behavior.Facts{
		Event:   event.Type,
		Order:   orderFacts(t.order),
		Variant: variantFacts(t.variant),
		Config:  t.variant.BehaviorConfig,
	}
	if d.users != nil {
		if customer, err := d.users.FindByID(ctx, t.order.CustomerID); err == nil {
			facts.Customer = actorFacts(customer)
			facts.User = facts.Customer
		}
		if t.order.ExecutorID != nil {
			if executor, err := d.users.FindByID(ctx, *t.order.ExecutorID); err == nil {
				facts.Viewer = actorFacts(executor)
			}
		}
	}
	if d.claims != nil {
		if count, err := d.claims.CountForVariant(ctx, t.order.CustomerID, t.variant.ID); err == nil {
			facts.Claims = count
		}
	}
	if event.Type == repository.EventOrderSubmission {
		escalated := false
		if d.submissions != nil {
			if open, err := d.submissions.HasOpenEscalation(ctx, t.order.ID); err == nil {
				escalated = open
			}
		}
		facts.Submission = submissionFacts(event, escalated)
	}
	return facts, nil
}

// apply performs the effects of one behaviour in a single transaction: either
// the customer is verified, the order is closed and the reward is paid, or none
// of it happened and the event is retried.
func (d *BehaviorDispatcher) apply(ctx context.Context, event *repository.DomainEvent, t target, effects []behavior.Effect) ([]string, error) {
	maxBonus := money.FromRubles(settingFloat(ctx, d.settings, SettingBehaviorMaxBonus, defaultBehaviorMaxBonus))

	var messages []behavior.Effect
	err := d.ledger.RunInTx(ctx, func(tx *sql.Tx) error {
		for i, effect := range effects {
			if effect.Kind == behavior.EffectSystemMessage {
				// Chat is not part of the money transaction: a failed message
				// must not roll back a payment, and a rolled back payment must
				// not have announced itself.
				messages = append(messages, effect)
				continue
			}
			key := effect.Key
			if key == "" {
				key = fmt.Sprintf("%s:%d:%s", event.ID, i, effect.Kind)
			}
			// Claiming the key first is what makes redelivery safe: the second
			// attempt to pay the same reward finds the row taken and stops.
			err := d.events.RecordEffect(ctx, tx, key, event.ID, d.behaviors.Code(t.variant), string(effect.Kind), map[string]interface{}{
				"order_id": effect.OrderID,
				"user_id":  effect.UserID,
				"amount":   effect.Amount,
				"reason":   effect.Reason,
			})
			if errors.Is(err, repository.ErrEffectAlreadyApplied) {
				metrics.BehaviorEffect(string(effect.Kind), "duplicate")
				continue
			}
			if err != nil {
				return err
			}
			if err := d.applyOne(ctx, tx, t, effect, maxBonus); err != nil {
				metrics.BehaviorEffect(string(effect.Kind), "refused")
				return err
			}
			metrics.BehaviorEffect(string(effect.Kind), "applied")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	posted := make([]string, 0, len(messages))
	for _, message := range messages {
		d.postMessage(ctx, t, message)
		posted = append(posted, message.Text)
	}
	return posted, nil
}

// applyOne performs one effect, after checking that the behaviour was entitled
// to ask for it. Every guard here answers the same question: could this effect
// reach somebody or something outside the order the event was about?
func (d *BehaviorDispatcher) applyOne(ctx context.Context, tx *sql.Tx, t target, effect behavior.Effect, maxBonus money.Amount) error {
	switch effect.Kind {
	case behavior.EffectCompleteOrder:
		if err := d.requireOwnOrder(t, effect.OrderID); err != nil {
			return err
		}
		if err := d.orderSvc.confirmTx(ctx, tx, t.order.ID); err != nil {
			return err
		}
		// A closed order has nothing left for an administrator to decide.
		if d.submissions != nil {
			return d.submissions.ResolveByOrder(ctx, tx, t.order.ID, nil)
		}
		return nil

	case behavior.EffectCancelOrder:
		if err := d.requireOwnOrder(t, effect.OrderID); err != nil {
			return err
		}
		return d.orderSvc.cancelTx(ctx, tx, t.order.ID,
			repository.OrderStatusSearching, repository.OrderStatusAssigned, repository.OrderStatusExecuted)

	case behavior.EffectPayBonus:
		recipient, err := uuid.Parse(effect.UserID)
		if err != nil {
			return fmt.Errorf("pay_bonus: invalid recipient %q", effect.UserID)
		}
		// Only somebody involved in this order may be paid for it. Without this
		// a script could name any user id in the system.
		if recipient != t.order.CustomerID && (t.order.ExecutorID == nil || recipient != *t.order.ExecutorID) {
			return fmt.Errorf("pay_bonus: %s is not a party to order %s", recipient, t.order.ID)
		}
		amount := money.FromRubles(effect.Amount)
		if !amount.IsPositive() {
			return fmt.Errorf("pay_bonus: amount %s is not positive", amount)
		}
		if amount > maxBonus {
			return fmt.Errorf("pay_bonus: %s exceeds the %s ceiling (%s)", amount, maxBonus, SettingBehaviorMaxBonus)
		}
		return d.ledger.Bonus(ctx, tx, recipient, amount, d.commissionOnBonus(ctx, effect, amount), &t.order.ID)

	case behavior.EffectVerifyUser:
		subject, err := uuid.Parse(effect.UserID)
		if err != nil {
			return fmt.Errorf("verify_user: invalid user %q", effect.UserID)
		}
		// The script may ask to verify only the customer of the order it is
		// reacting to, and only when a moderator actually performed that order.
		// This is the guard that keeps a scripted verification as trustworthy as
		// the admin checkbox it stands in for.
		if subject != t.order.CustomerID {
			return fmt.Errorf("verify_user: %s is not the customer of order %s", subject, t.order.ID)
		}
		if err := d.requireModeratorExecutor(ctx, t); err != nil {
			return err
		}
		if err := d.users.UpdateVerifiedTx(ctx, tx, subject, true); err != nil {
			return err
		}
		log.Printf("[AUDIT] behavior %s verified user %s through order %s", d.behaviors.Code(t.variant), subject, t.order.ID)
		// Published like any other verification, so anything else that reacts to
		// a user becoming verified sees this one too.
		return d.events.Publish(ctx, tx, &repository.DomainEvent{
			Type:        repository.EventUserVerified,
			SubjectType: repository.EventSubjectUser,
			SubjectID:   subject,
			ActorID:     t.order.ExecutorID,
		})

	case behavior.EffectEscalate:
		if err := d.requireOwnOrder(t, effect.OrderID); err != nil {
			return err
		}
		if d.submissions == nil {
			return errors.New("escalations are not available on this server")
		}
		reason := strings.TrimSpace(effect.Reason)
		if reason == "" {
			reason = "передано администратору поведением услуги"
		}
		log.Printf("[AUDIT] behavior %s escalated order %s: %s", d.behaviors.Code(t.variant), t.order.ID, reason)
		return d.submissions.Escalate(ctx, tx, &repository.BehaviorEscalation{
			OrderID:      t.order.ID,
			BehaviorCode: d.behaviors.Code(t.variant),
			Reason:       reason,
		})

	default:
		return fmt.Errorf("unknown effect %q", effect.Kind)
	}
}

// commissionOnBonus works out the platform's share of a reward. Zero unless the
// behaviour asked for it: a reward is money the platform pays out, not money a
// customer paid, so the commission — which is a share of what a customer paid —
// does not apply to it by default. When a behaviour does opt in, the rate is the
// ordinary order_commission_percent, clamped by the same commissionOn used on
// the order path, so there is one definition of the rate in the service.
func (d *BehaviorDispatcher) commissionOnBonus(ctx context.Context, effect behavior.Effect, amount money.Amount) money.Amount {
	if !effect.Commission {
		return money.Zero
	}
	percent := settingFloat(ctx, d.settings, SettingOrderCommissionPercent, 0)
	return commissionOn(amount, map[string]float64{SettingOrderCommissionPercent: percent})
}

// requireOwnOrder refuses an effect aimed at any order other than the one the
// event was about.
func (d *BehaviorDispatcher) requireOwnOrder(t target, orderID string) error {
	if orderID != "" && !strings.EqualFold(orderID, t.order.ID.String()) {
		return fmt.Errorf("effect targets order %s, but the event was about %s", orderID, t.order.ID)
	}
	return nil
}

// requireModeratorExecutor checks that the order was performed by somebody the
// behaviour's configuration trusts to perform it.
func (d *BehaviorDispatcher) requireModeratorExecutor(ctx context.Context, t target) error {
	if t.order.ExecutorID == nil {
		return fmt.Errorf("order %s has no executor to vouch for it", t.order.ID)
	}
	if *t.order.ExecutorID == t.order.CustomerID {
		return fmt.Errorf("order %s was taken by its own customer", t.order.ID)
	}
	executor, err := d.users.FindByID(ctx, *t.order.ExecutorID)
	if err != nil {
		return err
	}
	// The role comes from the behaviour's own constants, overridden by the node:
	// exactly what the script's can_view_or_take reads, so what the core demands
	// of the verifier cannot drift from what the script let take the order.
	required := d.behaviors.ConfigString(t.variant, ConfigVerifierRole, repository.RoleModerator)
	if !executor.HasRole(required) {
		return fmt.Errorf("executor %s does not hold %s", executor.ID, required)
	}
	return nil
}

func (d *BehaviorDispatcher) postMessage(ctx context.Context, t target, effect behavior.Effect) {
	if d.chat == nil || strings.TrimSpace(effect.Text) == "" {
		return
	}
	if err := d.requireOwnOrder(t, effect.OrderID); err != nil {
		log.Printf("[behavior] refusing system_message: %v", err)
		return
	}
	chat, err := d.chat.GetChatByOrderID(ctx, t.order.ID)
	if err != nil || chat == nil {
		return
	}
	sender := t.order.CustomerID
	if t.order.ExecutorID != nil {
		sender = *t.order.ExecutorID
	}
	if _, err := d.chat.SaveMessage(ctx, chat.ID, sender, effect.Text); err != nil {
		log.Printf("[behavior] cannot post system message to order %s: %v", t.order.ID, err)
	}
}
