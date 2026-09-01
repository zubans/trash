// Package behavior evaluates the rules of a "special" service — one whose
// conditions do not fit the catalog's flags — as a script kept outside the Go
// code.
//
// Why a script at all. Every unusual property a service ever needed used to
// become a column plus an `if`: requires_verification (035), moderator_only
// (040), min_age. The verification service needs four rules at once — visible
// only to unverified customers, orderable once, free, and it pays the moderator
// who performed it — and none of them is reusable as a flag. The next such
// service would need four more.
//
// What a script may and may not do. A behaviour script is a pure function of
// the facts it is handed: it reads no database, opens no socket, keeps no state
// between calls, and never moves money itself. It answers questions ("may this
// user see this?", "what does it cost?") and, for events, returns a list of
// effects it wants applied. The core applies those effects, transactionally,
// through the same ledger every other payment goes through. That boundary is
// the whole design: a wrong script can produce a wrong decision, but it cannot
// produce a one-sided money movement or an unbalanced set of books.
package behavior

import (
	"fmt"
	"time"
)

// EffectKind names one thing a behaviour can ask the core to do. The set is
// deliberately closed: an effect is a Go function with its own guards, not an
// escape hatch into arbitrary state changes.
type EffectKind string

const (
	// EffectCompleteOrder closes an order and pays out exactly as a customer's
	// confirmation would.
	EffectCompleteOrder EffectKind = "complete_order"
	// EffectCancelOrder cancels an order and refunds whatever it still holds.
	EffectCancelOrder EffectKind = "cancel_order"
	// EffectPayBonus credits a user — the executor, the customer, or both in
	// turn — from the platform's BONUSES account. It must carry an idempotency
	// key, because unlike the others it has no state to check: paying twice
	// looks exactly like paying once.
	EffectPayBonus EffectKind = "pay_bonus"
	// EffectVerifyUser sets the manual verification flag. The core refuses it
	// unless the order it comes from was performed by a moderator (see
	// service/behavior_dispatch.go) — the script asks, the core decides whether
	// the asker was entitled to.
	EffectVerifyUser EffectKind = "verify_user"
	// EffectSystemMessage posts a system message into the order's chat. Applied
	// after the transaction commits, like every other chat notification.
	EffectSystemMessage EffectKind = "system_message"
)

// Effect is one requested change, as returned by a script.
type Effect struct {
	Kind EffectKind
	// OrderID and UserID are the subjects, empty when the effect does not use
	// them. They arrive as strings and are parsed by the applier: a script
	// cannot be trusted to hand back a well-formed id.
	OrderID string
	UserID  string
	// Amount is in rubles, as the script wrote it.
	Amount float64
	// Commission asks for the platform's share (order_commission_percent) to be
	// withheld from this payment. False by default and normally left so: a
	// reward is money the platform pays out, not money a customer paid, and a
	// commission on it would only move the platform's own money between its own
	// accounts. A behaviour whose rewards should be treated as ordinary
	// earnings sets it explicitly.
	Commission bool
	// Key is the idempotency key. Required for EffectPayBonus.
	Key    string
	Reason string
	Text   string
}

// Actor is a user as a script sees one.
type Actor struct {
	ID         string
	Role       string
	Roles      []string
	IsVerified bool
	Age        int
	Status     string
}

// HasRole mirrors repository.User.HasRole so the script and the Go gates agree
// on what holding a role means.
func (a *Actor) HasRole(role string) bool {
	if a == nil {
		return false
	}
	for _, r := range a.Roles {
		if r == role {
			return true
		}
	}
	return a.Role == role
}

// OrderFacts is an order as a script sees one.
type OrderFacts struct {
	ID         string
	Status     string
	CustomerID string
	ExecutorID string
	Amount     float64
	IsUrgent   bool
	IsAsap     bool
}

// VariantFacts is the service node the decision is about.
type VariantFacts struct {
	ID        string
	Code      string
	BasePrice float64
}

// Facts is everything a hook is allowed to know. The core fills it in before
// calling; the script cannot ask for anything that is not here, which is what
// keeps a hook a pure function and bounds what one costs to run.
type Facts struct {
	// Event is set for on_event only, e.g. "order.executed".
	Event string
	// Config is the node's behavior_config, merged over the script's defaults.
	Config map[string]interface{}
	// User is who the decision is about: the customer for visible/can_order.
	User *Actor
	// Viewer is the executor or moderator judging an order.
	Viewer *Actor
	// Customer is the order's customer, when there is an order.
	Customer *Actor
	Order    *OrderFacts
	Variant  *VariantFacts
	// Claims is how many times User has already ordered this variant.
	Claims int
	Now    time.Time
}

// Manifest is the static half of a behaviour: the properties the core needs to
// know without running anything — because they shape a database write (a claim
// row), a UI form, or which events are worth delivering.
type Manifest struct {
	Code string `json:"code"`
	Name string `json:"name"`
	// Description is shown in the admin panel next to the behaviour picker.
	Description string `json:"description"`
	// OncePerUser makes the core insert a claim row with the order, so a second
	// order for the same variant by the same user is refused by the database.
	OncePerUser bool `json:"once_per_user"`
	// ReleaseClaimOnCancel returns the claim when the order is cancelled. A
	// cancelled order must not lock a user out of a service for good.
	ReleaseClaimOnCancel bool `json:"release_claim_on_cancel"`
	// Events the script reacts to. An event outside this list is not delivered.
	Events []string `json:"events"`
	// Defaults are the config values a node inherits when it sets none.
	Defaults map[string]interface{} `json:"defaults"`
	// Hooks lists which functions the script actually defines, for the admin
	// panel and for the dry-run screen.
	Hooks []string `json:"hooks"`
}

// Handles reports whether the behaviour asked for this event.
func (m Manifest) Handles(event string) bool {
	for _, e := range m.Events {
		if e == event {
			return true
		}
	}
	return false
}

// DeniedError is a refusal produced by a script, carrying the message the user
// should see. It is a normal outcome, not a failure of the script.
type DeniedError struct {
	Code    string
	Message string
}

func (e *DeniedError) Error() string { return e.Message }

// Denied builds a refusal.
func Denied(code, message string) error {
	if message == "" {
		message = "услуга недоступна"
	}
	return &DeniedError{Code: code, Message: message}
}

// ErrUnknownBehavior reports a node naming a behaviour that is not loaded —
// a script that failed to compile, or a code left behind by a rollback.
type ErrUnknownBehavior struct{ Code string }

func (e *ErrUnknownBehavior) Error() string {
	return fmt.Sprintf("unknown service behavior %q", e.Code)
}
