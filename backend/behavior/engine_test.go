package behavior

import (
	"os"
	"strings"
	"testing"
	"time"
)

// loadReal compiles the behaviour scripts that ship with the service, so these
// tests fail when a script is edited into something the core cannot use — the
// scripts live outside the Go code, and this is what keeps them covered by it.
func loadReal(t *testing.T) *Engine {
	t.Helper()
	e := New(DefaultLimits)
	if err := e.Load(os.DirFS("../behaviors"), "behaviors"); err != nil {
		t.Fatalf("load behaviors: %v", err)
	}
	return e
}

func customer(verified bool) *Actor {
	return &Actor{ID: "11111111-1111-1111-1111-111111111111", Role: "CUSTOMER", IsVerified: verified, Status: "ACTIVE"}
}

func moderator() *Actor {
	return &Actor{ID: "22222222-2222-2222-2222-222222222222", Role: "EXECUTOR", Roles: []string{"EXECUTOR", "MODERATOR"}, Status: "ACTIVE"}
}

func TestVerificationManifest(t *testing.T) {
	e := loadReal(t)
	m, ok := e.Manifest("verification")
	if !ok {
		t.Fatal("verification behaviour not loaded")
	}
	if !m.OncePerUser {
		t.Error("verification must be orderable once per user")
	}
	if !m.ReleaseClaimOnCancel {
		t.Error("a cancelled verification order must release the claim")
	}
	if !m.Handles("order.executed") || !m.Handles("user.verified") {
		t.Errorf("manifest does not declare the events it reacts to: %v", m.Events)
	}
	for _, key := range []string{"reward_executor", "reward_customer", "apply_commission", "verifier_role"} {
		if _, ok := m.Defaults[key]; !ok {
			t.Errorf("manifest declares no default for %q: %v", key, m.Defaults)
		}
	}
	// Commission on a reward is opt-in, and this behaviour does not opt in.
	if applies, _ := m.Defaults["apply_commission"].(bool); applies {
		t.Error("verification rewards must not be commissioned by default")
	}
}

func TestVerificationVisibility(t *testing.T) {
	e := loadReal(t)
	cases := []struct {
		name   string
		facts  Facts
		expect bool
	}{
		{"unverified customer sees it", Facts{User: customer(false)}, true},
		{"verified customer does not", Facts{User: customer(true)}, false},
		{"already ordered once", Facts{User: customer(false), Claims: 1}, false},
		{"anonymous visitor", Facts{}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := e.Visible("verification", c.facts)
			if err != nil {
				t.Fatalf("visible: %v", err)
			}
			if got != c.expect {
				t.Errorf("visible = %v, want %v", got, c.expect)
			}
		})
	}
}

func TestVerificationCanOrder(t *testing.T) {
	e := loadReal(t)
	if err := e.CanOrder("verification", Facts{User: customer(false)}); err != nil {
		t.Errorf("an unverified customer must be able to order: %v", err)
	}
	err := e.CanOrder("verification", Facts{User: customer(true)})
	if err == nil {
		t.Fatal("a verified customer must not be able to order verification")
	}
	var denied *DeniedError
	if !asDenied(err, &denied) {
		t.Fatalf("want a denial carrying a message, got %T: %v", err, err)
	}
	if denied.Message == "" {
		t.Error("denial must carry a message for the user")
	}
	if err := e.CanOrder("verification", Facts{User: customer(false), Claims: 1}); err == nil {
		t.Error("the service may be ordered only once")
	}
}

func TestVerificationOnlyModeratorsMayTakeIt(t *testing.T) {
	e := loadReal(t)
	plainExecutor := &Actor{ID: "33333333-3333-3333-3333-333333333333", Role: "EXECUTOR", Status: "ACTIVE"}

	if err := e.CanViewOrTake("verification", Facts{Viewer: moderator(), Customer: customer(false)}); err != nil {
		t.Errorf("a moderator must be able to take a verification order: %v", err)
	}
	if err := e.CanViewOrTake("verification", Facts{Viewer: plainExecutor, Customer: customer(false)}); err == nil {
		t.Error("a plain executor must not see or take a verification order")
	}
	self := moderator()
	if err := e.CanViewOrTake("verification", Facts{Viewer: self, Customer: &Actor{ID: self.ID}}); err == nil {
		t.Error("nobody may verify themselves")
	}
}

func TestVerificationIsFree(t *testing.T) {
	e := loadReal(t)
	price, ok, err := e.Price("verification", Facts{Variant: &VariantFacts{BasePrice: 500}})
	if err != nil {
		t.Fatalf("price: %v", err)
	}
	if !ok || price != 0 {
		t.Errorf("price = %v (set: %v), want a free service", price, ok)
	}
}

func TestVerificationCompletesAndPaysWhenTheDataMatches(t *testing.T) {
	e := loadReal(t)
	cust := customer(false)
	mod := moderator()
	order := &OrderFacts{ID: "44444444-4444-4444-4444-444444444444", Status: "ASSIGNED", CustomerID: cust.ID, ExecutorID: mod.ID}

	effects, err := e.OnEvent("verification", Facts{
		Event: "order.submission", Order: order, Customer: cust, Viewer: mod,
		Submission: &SubmissionFacts{Attempt: 1, AllMatch: true},
		Config:     map[string]interface{}{"reward_executor": 200.0},
	})
	if err != nil {
		t.Fatalf("on_event: %v", err)
	}

	var kinds []string
	var bonus *Effect
	for i := range effects {
		kinds = append(kinds, string(effects[i].Kind))
		if effects[i].Kind == EffectPayBonus {
			bonus = &effects[i]
		}
	}
	for _, want := range []EffectKind{EffectVerifyUser, EffectCompleteOrder, EffectPayBonus} {
		if !strings.Contains(strings.Join(kinds, ","), string(want)) {
			t.Errorf("missing effect %s, got %v", want, kinds)
		}
	}
	if bonus == nil {
		t.Fatal("no reward was requested")
	}
	if bonus.UserID != mod.ID {
		t.Errorf("reward paid to %s, want the verifier %s", bonus.UserID, mod.ID)
	}
	if bonus.Amount != 200 {
		t.Errorf("reward = %v, want the configured 200", bonus.Amount)
	}
	if bonus.Key == "" || !strings.Contains(bonus.Key, order.ID) {
		t.Errorf("reward key %q must tie the payment to the order", bonus.Key)
	}
	if bonus.Commission {
		t.Error("a reward the platform pays out must not be commissioned unless the behaviour says so")
	}
}

// Both rewards are constants of the behaviour: the customer's is zero by
// default and paid when a node configures one, and each has its own key so the
// two payments cannot collide.
func TestVerificationPaysBothSidesWhenConfigured(t *testing.T) {
	e := loadReal(t)
	cust := customer(false)
	mod := moderator()
	order := &OrderFacts{ID: "44444444-4444-4444-4444-444444444444", Status: "EXECUTED", CustomerID: cust.ID, ExecutorID: mod.ID}

	effects, err := e.OnEvent("verification", Facts{
		Event: "order.submission", Order: order, Customer: cust, Viewer: mod,
		Submission: &SubmissionFacts{Attempt: 1, AllMatch: true},
		Config: map[string]interface{}{
			"reward_executor":  150.0,
			"reward_customer":  50.0,
			"apply_commission": true,
		},
	})
	if err != nil {
		t.Fatalf("on_event: %v", err)
	}

	paid := map[string]Effect{}
	for _, effect := range effects {
		if effect.Kind == EffectPayBonus {
			paid[effect.UserID] = effect
		}
	}
	if len(paid) != 2 {
		t.Fatalf("got %d payments, want one per side: %v", len(paid), paid)
	}
	if got := paid[mod.ID].Amount; got != 150 {
		t.Errorf("executor reward = %v, want 150", got)
	}
	if got := paid[cust.ID].Amount; got != 50 {
		t.Errorf("customer reward = %v, want 50", got)
	}
	if paid[mod.ID].Key == paid[cust.ID].Key {
		t.Errorf("both rewards share the key %q, so only one of them would ever be paid", paid[mod.ID].Key)
	}
	// Turned on explicitly by the node, so it must reach the core.
	if !paid[mod.ID].Commission || !paid[cust.ID].Commission {
		t.Error("apply_commission was set but the effects do not carry it")
	}
}

// A reward of zero is not a payment: the effect must not be produced at all,
// or the applier would refuse a zero amount and fail the whole event.
func TestVerificationSkipsZeroRewards(t *testing.T) {
	e := loadReal(t)
	cust := customer(false)
	mod := moderator()
	effects, err := e.OnEvent("verification", Facts{
		Event:      "order.submission",
		Order:      &OrderFacts{ID: "44444444-4444-4444-4444-444444444444", Status: "ASSIGNED", CustomerID: cust.ID, ExecutorID: mod.ID},
		Customer:   cust,
		Submission: &SubmissionFacts{Attempt: 1, AllMatch: true},
		Config:     map[string]interface{}{"reward_executor": 0.0, "reward_customer": 0.0},
	})
	if err != nil {
		t.Fatalf("on_event: %v", err)
	}
	for _, effect := range effects {
		if effect.Kind == EffectPayBonus {
			t.Errorf("a zero reward produced a payment of %v", effect.Amount)
		}
	}
}

// The constants live in config.star; the logic reads them by name. A behaviour
// whose config file did not load would fail here rather than silently paying
// nothing.
func TestBehaviorConstantsComeFromTheConfigFile(t *testing.T) {
	e := loadReal(t)
	cust := customer(false)
	mod := moderator()
	effects, err := e.OnEvent("verification", Facts{
		Event:      "order.submission",
		Order:      &OrderFacts{ID: "44444444-4444-4444-4444-444444444444", Status: "ASSIGNED", CustomerID: cust.ID, ExecutorID: mod.ID},
		Customer:   cust,
		Submission: &SubmissionFacts{Attempt: 1, AllMatch: true},
	})
	if err != nil {
		t.Fatalf("on_event: %v", err)
	}
	for _, effect := range effects {
		if effect.Kind == EffectPayBonus && effect.Amount == 200 {
			return
		}
	}
	t.Errorf("no reward taken from config.star (REWARD_EXECUTOR = 200): %v", effects)
}

// A finished order must not be completed or paid for a second time, whichever
// event arrives afterwards.
func TestVerificationIgnoresFinishedOrders(t *testing.T) {
	e := loadReal(t)
	cust := customer(true)
	effects, err := e.OnEvent("verification", Facts{
		Event:    "user.verified",
		Customer: cust,
		Order:    &OrderFacts{ID: "44444444-4444-4444-4444-444444444444", Status: "COMPLETED", CustomerID: cust.ID},
	})
	if err != nil {
		t.Fatalf("on_event: %v", err)
	}
	if len(effects) != 0 {
		t.Errorf("a completed order produced %d effects", len(effects))
	}
}

// The admin-driven configuration keeps the flag an admin's decision: marking the
// visit done must not verify anybody by itself.
func TestVerificationAdminModeIgnoresExecution(t *testing.T) {
	e := loadReal(t)
	cust := customer(false)
	mod := moderator()
	effects, err := e.OnEvent("verification", Facts{
		Event:    "order.executed",
		Order:    &OrderFacts{ID: "44444444-4444-4444-4444-444444444444", Status: "EXECUTED", CustomerID: cust.ID, ExecutorID: mod.ID},
		Customer: cust,
		Config:   map[string]interface{}{"verified_by": "admin"},
	})
	if err != nil {
		t.Fatalf("on_event: %v", err)
	}
	if len(effects) != 0 {
		t.Errorf("verified_by=admin produced %d effects on execution", len(effects))
	}
}

// The identity check: the manifest declares which fields the moderator submits
// and that they see nothing else about the customer.
func TestVerificationDeclaresTheIdentityCheck(t *testing.T) {
	e := loadReal(t)
	m, _ := e.Manifest("verification")
	if !m.HideCustomerContacts {
		t.Error("the behaviour must state that the executor sees no customer contacts")
	}
	want := map[string]bool{"last_name": true, "first_name": true, "patronymic": true, "birth_date": true}
	if len(m.CheckFields) != len(want) {
		t.Fatalf("check_fields = %v", m.CheckFields)
	}
	for _, field := range m.CheckFields {
		if !want[field] {
			t.Errorf("unexpected checked field %q", field)
		}
	}
}

// A first mismatch is a typo until proven otherwise: warn, do not escalate, and
// do not say which field was wrong — the rest could then be found by trying.
func TestVerificationWarnsOnTheFirstMismatch(t *testing.T) {
	e := loadReal(t)
	cust := customer(false)
	mod := moderator()

	effects, err := e.OnEvent("verification", Facts{
		Event:    "order.submission",
		Order:    &OrderFacts{ID: "44444444-4444-4444-4444-444444444444", Status: "ASSIGNED", CustomerID: cust.ID, ExecutorID: mod.ID},
		Customer: cust,
		Viewer:   mod,
		Submission: &SubmissionFacts{
			Attempt:  1,
			AllMatch: false,
			Matches:  map[string]bool{"last_name": true, "first_name": false, "patronymic": true, "birth_date": true},
		},
	})
	if err != nil {
		t.Fatalf("on_event: %v", err)
	}
	if len(effects) != 1 || effects[0].Kind != EffectSystemMessage {
		t.Fatalf("expected one warning, got %v", effects)
	}
	if strings.Contains(effects[0].Text, "first_name") || strings.Contains(effects[0].Text, "Имя") {
		t.Errorf("the warning names the field that did not match: %q", effects[0].Text)
	}
	for _, effect := range effects {
		if effect.Kind == EffectVerifyUser || effect.Kind == EffectPayBonus {
			t.Errorf("a mismatch produced %s", effect.Kind)
		}
	}
}

// The last allowed attempt hands the case to an administrator.
func TestVerificationEscalatesAfterTheLastAttempt(t *testing.T) {
	e := loadReal(t)
	cust := customer(false)
	mod := moderator()

	effects, err := e.OnEvent("verification", Facts{
		Event:      "order.submission",
		Order:      &OrderFacts{ID: "44444444-4444-4444-4444-444444444444", Status: "ASSIGNED", CustomerID: cust.ID, ExecutorID: mod.ID},
		Customer:   cust,
		Viewer:     mod,
		Submission: &SubmissionFacts{Attempt: 2, AllMatch: false, Matches: map[string]bool{"birth_date": false}},
	})
	if err != nil {
		t.Fatalf("on_event: %v", err)
	}

	var escalated bool
	for _, effect := range effects {
		switch effect.Kind {
		case EffectEscalate:
			escalated = true
			if effect.Reason == "" {
				t.Error("the escalation carries no reason for the administrator")
			}
		case EffectVerifyUser, EffectCompleteOrder, EffectPayBonus:
			t.Errorf("a failed check produced %s", effect.Kind)
		}
	}
	if !escalated {
		t.Errorf("the case was not handed to an administrator: %v", effects)
	}
}

// Once a case is with an administrator the script stops acting on it.
func TestVerificationLeavesAnEscalatedOrderAlone(t *testing.T) {
	e := loadReal(t)
	cust := customer(false)
	effects, err := e.OnEvent("verification", Facts{
		Event:      "order.submission",
		Order:      &OrderFacts{ID: "44444444-4444-4444-4444-444444444444", Status: "ASSIGNED", CustomerID: cust.ID},
		Customer:   cust,
		Submission: &SubmissionFacts{Attempt: 3, AllMatch: true, Escalated: true},
	})
	if err != nil {
		t.Fatalf("on_event: %v", err)
	}
	if len(effects) != 0 {
		t.Errorf("the script acted on a case that is with an administrator: %v", effects)
	}
}

// Marking the visit done no longer verifies anybody by itself: the check does.
func TestVerificationDoesNotCompleteWithoutTheCheck(t *testing.T) {
	e := loadReal(t)
	cust := customer(false)
	mod := moderator()
	effects, err := e.OnEvent("verification", Facts{
		Event:    "order.executed",
		Order:    &OrderFacts{ID: "44444444-4444-4444-4444-444444444444", Status: "ASSIGNED", CustomerID: cust.ID, ExecutorID: mod.ID},
		Customer: cust,
		Viewer:   mod,
	})
	if err != nil {
		t.Fatalf("on_event: %v", err)
	}
	for _, effect := range effects {
		if effect.Kind == EffectVerifyUser || effect.Kind == EffectCompleteOrder || effect.Kind == EffectPayBonus {
			t.Errorf("marking the visit done produced %s without any data being checked", effect.Kind)
		}
	}
}

func TestPayBonusRequiresAnIdempotencyKey(t *testing.T) {
	e := New(DefaultLimits)
	src := []byte(`
MANIFEST = {"name": "t", "events": ["order.executed"]}
def on_event(f):
    return [pay_bonus(to = "u", amount = 10, key = "")]
`)
	if err := e.Compile("t", "t.star", src); err != nil {
		t.Fatalf("compile: %v", err)
	}
	_, err := e.OnEvent("t", Facts{Event: "order.executed"})
	if err == nil {
		t.Fatal("a payment without an idempotency key must be refused")
	}
}

// A runaway script fails its own call and nothing else.
func TestRunawayScriptIsStopped(t *testing.T) {
	e := New(Limits{MaxSteps: 10_000, Timeout: 50 * time.Millisecond})
	src := []byte(`
MANIFEST = {"name": "t"}
def visible(f):
    total = 0
    for i in range(1000000):
        total += i
    return True
`)
	if err := e.Compile("t", "t.star", src); err != nil {
		t.Fatalf("compile: %v", err)
	}
	visible, err := e.Visible("t", Facts{})
	if err == nil {
		t.Fatal("an endless script must fail its hook")
	}
	// Fail closed for the caller: an error means the node is not shown.
	if !visible {
		t.Log("hook failed as expected:", err)
	}
}

func TestUnknownBehaviorIsReported(t *testing.T) {
	e := New(DefaultLimits)
	if err := e.CanOrder("nope", Facts{}); err == nil {
		t.Fatal("a node naming a missing behaviour must not be silently allowed")
	}
}

func asDenied(err error, target **DeniedError) bool {
	d, ok := err.(*DeniedError)
	if ok {
		*target = d
	}
	return ok
}
