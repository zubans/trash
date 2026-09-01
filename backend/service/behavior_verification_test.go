package service

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/behavior"
	"healthlogin/backend/money"
	"healthlogin/backend/repository"
)

// The verification service end to end, through the same code the application
// runs: the script decides, the core applies. What is pinned here is the part a
// script cannot be trusted with — that the money moves once, in one direction,
// and that the books still close afterwards.

var verificationVariantID = uuid.MustParse("dddddddd-dddd-dddd-dddd-dddddddddddd")

// --- fakes ------------------------------------------------------------------

// verificationUsers is a small user store. It embeds the interface so the
// methods this flow never calls do not have to be written out; calling one
// panics, which is the right outcome for a test that grew a dependency nobody
// declared.
type verificationUsers struct {
	repository.UserRepository
	users map[uuid.UUID]*repository.User
}

func (u *verificationUsers) add(role string, roles []string, verified bool) *repository.User {
	birth := time.Now().AddDate(-30, 0, 0)
	user := &repository.User{
		ID: uuid.New(), Role: role, Roles: roles, Status: "ACTIVE",
		Verified: verified, BirthDate: &birth,
	}
	u.users[user.ID] = user
	return user
}

func (u *verificationUsers) FindByID(ctx context.Context, id uuid.UUID) (*repository.User, error) {
	user, ok := u.users[id]
	if !ok {
		return nil, sql.ErrNoRows
	}
	return user, nil
}

func (u *verificationUsers) FindByIDs(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]*repository.User, error) {
	found := map[uuid.UUID]*repository.User{}
	for _, id := range ids {
		if user, ok := u.users[id]; ok {
			found[id] = user
		}
	}
	return found, nil
}

func (u *verificationUsers) UpdateVerified(ctx context.Context, id uuid.UUID, verified bool) error {
	return u.UpdateVerifiedTx(ctx, nil, id, verified)
}

func (u *verificationUsers) UpdateVerifiedTx(ctx context.Context, q repository.Querier, id uuid.UUID, verified bool) error {
	user, ok := u.users[id]
	if !ok {
		return sql.ErrNoRows
	}
	user.Verified = verified
	return nil
}

// verificationCatalog serves the seeded verification variant.
type verificationCatalog struct {
	repository.ServiceCatalogRepository
	node *repository.ServiceNode
}

func (c *verificationCatalog) GetNodeByID(ctx context.Context, id uuid.UUID) (*repository.ServiceNode, error) {
	if id != c.node.ID {
		return nil, sql.ErrNoRows
	}
	return c.node, nil
}

func (c *verificationCatalog) GetNodesByIDs(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]*repository.ServiceNode, error) {
	found := map[uuid.UUID]*repository.ServiceNode{}
	for _, id := range ids {
		if id == c.node.ID {
			found[id] = c.node
		}
	}
	return found, nil
}

type claimKey struct {
	user    uuid.UUID
	variant uuid.UUID
}

type verificationClaims struct {
	claims map[claimKey]uuid.UUID
}

func newVerificationClaims() *verificationClaims {
	return &verificationClaims{claims: map[claimKey]uuid.UUID{}}
}

func (c *verificationClaims) Claim(ctx context.Context, q repository.Querier, userID, variantID, orderID uuid.UUID) error {
	key := claimKey{userID, variantID}
	if _, taken := c.claims[key]; taken {
		return repository.ErrServiceAlreadyClaimed
	}
	c.claims[key] = orderID
	return nil
}

func (c *verificationClaims) ReleaseByOrder(ctx context.Context, q repository.Querier, orderID uuid.UUID) error {
	for key, id := range c.claims {
		if id == orderID {
			delete(c.claims, key)
		}
	}
	return nil
}

func (c *verificationClaims) CountForVariant(ctx context.Context, userID, variantID uuid.UUID) (int, error) {
	if _, ok := c.claims[claimKey{userID, variantID}]; ok {
		return 1, nil
	}
	return 0, nil
}

func (c *verificationClaims) CountsForUser(ctx context.Context, userID uuid.UUID) (map[uuid.UUID]int, error) {
	counts := map[uuid.UUID]int{}
	for key := range c.claims {
		if key.user == userID {
			counts[key.variant]++
		}
	}
	return counts, nil
}

// verificationEvents is the outbox, in memory, with the same idempotency rule
// the table enforces.
type verificationEvents struct {
	published []*repository.DomainEvent
	processed map[uuid.UUID]bool
	effects   map[string]bool
}

func newVerificationEvents() *verificationEvents {
	return &verificationEvents{processed: map[uuid.UUID]bool{}, effects: map[string]bool{}}
}

func (e *verificationEvents) RunInTx(ctx context.Context, fn func(*sql.Tx) error) error {
	return fn(nil)
}

func (e *verificationEvents) Publish(ctx context.Context, q repository.Querier, event *repository.DomainEvent) error {
	if event.ID == uuid.Nil {
		event.ID = uuid.New()
	}
	e.published = append(e.published, event)
	return nil
}

func (e *verificationEvents) ClaimPending(ctx context.Context, limit, maxAttempts int) ([]*repository.DomainEvent, error) {
	var pending []*repository.DomainEvent
	for _, event := range e.published {
		if !e.processed[event.ID] {
			pending = append(pending, event)
		}
	}
	return pending, nil
}

func (e *verificationEvents) MarkProcessed(ctx context.Context, id uuid.UUID) error {
	e.processed[id] = true
	return nil
}

func (e *verificationEvents) MarkFailed(ctx context.Context, id uuid.UUID, reason string) error {
	return nil
}

func (e *verificationEvents) RecordEffect(ctx context.Context, q repository.Querier, key string, eventID uuid.UUID, behaviorCode, kind string, payload map[string]interface{}) error {
	if e.effects[key] {
		return repository.ErrEffectAlreadyApplied
	}
	e.effects[key] = true
	return nil
}

// PurgeProcessed trims history; nothing in these tests depends on it.
func (e *verificationEvents) PurgeProcessed(ctx context.Context, olderThan time.Duration) (int64, error) {
	return 0, nil
}

func (e *verificationEvents) CountPending(ctx context.Context) (int, error) {
	return len(e.published) - len(e.processed), nil
}

// --- harness ----------------------------------------------------------------

type verificationWorld struct {
	orders     *mockOrderRepo
	users      *verificationUsers
	catalog    *verificationCatalog
	claims     *verificationClaims
	events     *verificationEvents
	tx         *mockTransactionRepo
	accounts   *mockAccounts
	orderSvc   *OrderService
	dispatcher *BehaviorDispatcher
	behaviors  *Behaviors
	settings   *orderMockSettingsRepo
	customer   *repository.User
	moderator  *repository.User
	executor   *repository.User
	// rewardBase is the verifier's balance before the reward, because the
	// balance fake seeds new users with money of their own.
	rewardBase money.Amount
}

func newVerificationWorld(t *testing.T) *verificationWorld {
	t.Helper()

	engine := behavior.New(behavior.DefaultLimits)
	if err := engine.Load(os.DirFS("../behaviors"), "behaviors"); err != nil {
		t.Fatalf("load behaviors: %v", err)
	}

	free := money.Zero
	node := &repository.ServiceNode{
		ID:           verificationVariantID,
		Code:         "account_verification_visit",
		NodeType:     repository.ServiceNodeTypeVariant,
		BasePrice:    &free,
		IsActive:     true,
		BehaviorCode: "verification",
		// Empty, exactly as the migration seeds it: the amounts, the role and
		// the mode are constants of the behaviour, and this column holds only
		// what a node changes about them.
		BehaviorConfig: repository.BehaviorConfig{},
	}

	w := &verificationWorld{
		orders:   &mockOrderRepo{},
		users:    &verificationUsers{users: map[uuid.UUID]*repository.User{}},
		catalog:  &verificationCatalog{node: node},
		claims:   newVerificationClaims(),
		events:   newVerificationEvents(),
		tx:       &mockTransactionRepo{},
		accounts: newMockAccounts(),
	}
	w.behaviors = NewBehaviors(engine, w.claims)
	ledger := NewLedger(w.tx, w.accounts)
	settings := &orderMockSettingsRepo{settings: map[string]string{}}
	w.settings = settings

	w.orderSvc = NewOrderService(w.orders, ledger, settings, w.users, &orderMockShiftRepo{}, nil, w.catalog, nil).
		WithBehaviors(w.behaviors, w.claims, w.events)
	w.dispatcher = NewBehaviorDispatcher(w.events, w.orders, w.users, w.catalog, w.claims, nil,
		settings, ledger, w.behaviors, w.orderSvc)

	w.customer = w.users.add(repository.RoleCustomer, nil, false)
	w.moderator = w.users.add(repository.RoleExecutor, []string{repository.RoleExecutor, repository.RoleModerator}, true)
	w.executor = w.users.add(repository.RoleExecutor, []string{repository.RoleExecutor}, true)
	return w
}

func (w *verificationWorld) books() money.Amount {
	return booksTotal(w.tx, w.accounts)
}

// --- tests ------------------------------------------------------------------

// The whole loop: a free order, taken by a moderator, closed by the visit
// report, with the reward paid once and the books still closed.
func TestVerificationServiceFullFlow(t *testing.T) {
	w := newVerificationWorld(t)
	ctx := context.Background()

	if _, err := w.tx.GetBalance(ctx, w.customer.ID); err != nil {
		t.Fatalf("balance: %v", err)
	}
	if _, err := w.tx.GetBalance(ctx, w.moderator.ID); err != nil {
		t.Fatalf("balance: %v", err)
	}
	opening := w.books()

	order, err := w.orderSvc.CreateOrder(ctx, w.customer.ID, verificationVariantID, false, false, "Москва, Арбат, 10", nil, nil)
	if err != nil {
		t.Fatalf("create verification order: %v", err)
	}
	if !order.HoldAmount.IsZero() {
		t.Errorf("the service is free, but %s was held", order.HoldAmount)
	}

	// Once per user: the second attempt is refused by the script, before any
	// row is written.
	if _, err := w.orderSvc.CreateOrder(ctx, w.customer.ID, verificationVariantID, false, false, "Москва, Арбат, 10", nil, nil); err == nil {
		t.Error("the verification service must be orderable only once")
	}

	// Executor side: moderators only.
	if err := w.orderSvc.Accept(ctx, order.ID, w.executor.ID); err == nil {
		t.Error("a plain executor must not be able to take a verification order")
	}
	if err := w.orderSvc.Accept(ctx, order.ID, w.moderator.ID); err != nil {
		t.Fatalf("a moderator must be able to take it: %v", err)
	}

	if err := w.orderSvc.ExecuteOrder(ctx, order.ID, w.moderator.ID); err != nil {
		t.Fatalf("mark executed: %v", err)
	}

	if w.customer.Verified {
		t.Error("the customer must not be verified before the event is dispatched")
	}

	beforeReward := w.tx.balances[w.moderator.ID]
	if err := w.dispatcher.Tick(ctx); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	if !w.customer.Verified {
		t.Error("the customer was not verified by the completed visit")
	}
	closed, err := w.orders.GetOrderByID(ctx, order.ID)
	if err != nil {
		t.Fatalf("reload order: %v", err)
	}
	if closed.Status != repository.OrderStatusCompleted {
		t.Errorf("order status = %s, want COMPLETED", closed.Status)
	}
	if got, want := w.tx.balances[w.moderator.ID].Sub(beforeReward), money.FromRubles(200); got != want {
		t.Errorf("verifier was paid %s, want %s", got, want)
	}
	if got, want := w.accounts.balances[repository.AccountBonuses], money.FromRubles(-200); got != want {
		t.Errorf("BONUSES = %s, want %s: the reward must come from an account, not from nowhere", got, want)
	}
	if got := w.books(); got != opening {
		t.Errorf("the books moved by %s: a scripted payout must keep them closed", got.Sub(opening))
	}
}

// Commission is not taken out of a reward unless the behaviour asks for it, and
// when it does, the gross still splits exactly.
func TestRewardCommissionIsOptIn(t *testing.T) {
	ctx := context.Background()

	run := func(t *testing.T, applyCommission bool) *verificationWorld {
		t.Helper()
		w := newVerificationWorld(t)
		w.settings.settings[SettingOrderCommissionPercent] = "10"
		if applyCommission {
			w.catalog.node.BehaviorConfig["apply_commission"] = true
		}

		order, err := w.orderSvc.CreateOrder(ctx, w.customer.ID, verificationVariantID, false, false, "Москва", nil, nil)
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if err := w.orderSvc.Accept(ctx, order.ID, w.moderator.ID); err != nil {
			t.Fatalf("accept: %v", err)
		}
		w.rewardBase = w.tx.balances[w.moderator.ID]
		if err := w.orderSvc.ExecuteOrder(ctx, order.ID, w.moderator.ID); err != nil {
			t.Fatalf("execute: %v", err)
		}
		if err := w.dispatcher.Tick(ctx); err != nil {
			t.Fatalf("dispatch: %v", err)
		}
		return w
	}

	t.Run("off by default", func(t *testing.T) {
		w := run(t, false)
		if got, want := w.tx.balances[w.moderator.ID].Sub(w.rewardBase), money.FromRubles(200); got != want {
			t.Errorf("verifier got %s, want the whole %s: a platform reward is not commissioned", got, want)
		}
		if got := w.accounts.balances[repository.AccountCommission]; !got.IsZero() {
			t.Errorf("COMMISSION collected %s from a reward nobody paid for", got)
		}
	})

	t.Run("on when the behaviour asks", func(t *testing.T) {
		w := run(t, true)
		if got, want := w.tx.balances[w.moderator.ID].Sub(w.rewardBase), money.FromRubles(180); got != want {
			t.Errorf("verifier got %s, want %s (200 less 10%%)", got, want)
		}
		if got, want := w.accounts.balances[repository.AccountCommission], money.FromRubles(20); got != want {
			t.Errorf("COMMISSION = %s, want %s", got, want)
		}
		// The gross still leaves BONUSES in one piece: 180 to the user, 20 to
		// COMMISSION.
		if got, want := w.accounts.balances[repository.AccountBonuses], money.FromRubles(-200); got != want {
			t.Errorf("BONUSES = %s, want %s", got, want)
		}
	})
}

// A redelivered event must not pay a second reward. The idempotency key the
// script attaches to the payment is what stops it.
func TestVerificationRewardIsPaidOnce(t *testing.T) {
	w := newVerificationWorld(t)
	ctx := context.Background()

	order, err := w.orderSvc.CreateOrder(ctx, w.customer.ID, verificationVariantID, false, false, "Москва", nil, nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := w.orderSvc.Accept(ctx, order.ID, w.moderator.ID); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if err := w.orderSvc.ExecuteOrder(ctx, order.ID, w.moderator.ID); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if err := w.dispatcher.Tick(ctx); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	paidOnce := w.tx.balances[w.moderator.ID]

	// The same event again: a retry after a crash, a second replica, a
	// duplicated row — all look like this.
	if err := w.events.Publish(ctx, nil, &repository.DomainEvent{
		Type:        repository.EventOrderExecuted,
		SubjectType: repository.EventSubjectOrder,
		SubjectID:   order.ID,
	}); err != nil {
		t.Fatalf("republish: %v", err)
	}
	if err := w.dispatcher.Tick(ctx); err != nil {
		t.Fatalf("second dispatch: %v", err)
	}
	if got := w.tx.balances[w.moderator.ID]; got != paidOnce {
		t.Errorf("the reward was paid again: %s, want %s", got, paidOnce)
	}
}

// A cancelled order gives the customer their one attempt back — otherwise
// cancelling once would leave them unable to ever get verified.
func TestCancelledVerificationOrderReleasesTheClaim(t *testing.T) {
	w := newVerificationWorld(t)
	ctx := context.Background()

	order, err := w.orderSvc.CreateOrder(ctx, w.customer.ID, verificationVariantID, false, false, "Москва", nil, nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := w.orderSvc.CancelOrder(ctx, order.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if _, err := w.orderSvc.CreateOrder(ctx, w.customer.ID, verificationVariantID, false, false, "Москва", nil, nil); err != nil {
		t.Errorf("a customer who cancelled must be able to order verification again: %v", err)
	}
}

// The admin checkbox is the other way in: verifying a customer by hand closes
// the order they had open and pays whoever was performing it.
func TestAdminVerificationClosesTheOpenOrder(t *testing.T) {
	w := newVerificationWorld(t)
	ctx := context.Background()

	order, err := w.orderSvc.CreateOrder(ctx, w.customer.ID, verificationVariantID, false, false, "Москва", nil, nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := w.orderSvc.Accept(ctx, order.ID, w.moderator.ID); err != nil {
		t.Fatalf("accept: %v", err)
	}
	beforeReward := w.tx.balances[w.moderator.ID]

	// Exactly what AdminService.SetUserVerified writes.
	if err := w.users.UpdateVerified(ctx, w.customer.ID, true); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if err := w.events.Publish(ctx, nil, &repository.DomainEvent{
		Type:        repository.EventUserVerified,
		SubjectType: repository.EventSubjectUser,
		SubjectID:   w.customer.ID,
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := w.dispatcher.Tick(ctx); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	closed, err := w.orders.GetOrderByID(ctx, order.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if closed.Status != repository.OrderStatusCompleted {
		t.Errorf("order status = %s, want COMPLETED", closed.Status)
	}
	if got, want := w.tx.balances[w.moderator.ID].Sub(beforeReward), money.FromRubles(200); got != want {
		t.Errorf("verifier was paid %s, want %s", got, want)
	}
}

// The core, not the script, decides who may be paid and how much. These are the
// guards that make a wrong script a wrong decision rather than a stolen payout.
func TestEffectGuardsRefuseWhatAScriptMayNotDo(t *testing.T) {
	w := newVerificationWorld(t)
	ctx := context.Background()

	order, err := w.orderSvc.CreateOrder(ctx, w.customer.ID, verificationVariantID, false, false, "Москва", nil, nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := w.orderSvc.Accept(ctx, order.ID, w.moderator.ID); err != nil {
		t.Fatalf("accept: %v", err)
	}
	stored, _ := w.orders.GetOrderByID(ctx, order.ID)
	variant, _ := w.catalog.GetNodeByID(ctx, verificationVariantID)

	event := &repository.DomainEvent{ID: uuid.New(), Type: repository.EventOrderExecuted}
	tgt := target{order: stored, variant: variant}
	outsider := w.users.add(repository.RoleExecutor, nil, true)

	cases := []struct {
		name   string
		effect behavior.Effect
		want   string
	}{
		{
			name:   "paying somebody who is not on the order",
			effect: behavior.Effect{Kind: behavior.EffectPayBonus, UserID: outsider.ID.String(), Amount: 100, Key: "a"},
			want:   "not a party",
		},
		{
			name:   "paying more than the ceiling allows",
			effect: behavior.Effect{Kind: behavior.EffectPayBonus, UserID: w.moderator.ID.String(), Amount: 1_000_000, Key: "b"},
			want:   "ceiling",
		},
		{
			name:   "verifying somebody other than the customer",
			effect: behavior.Effect{Kind: behavior.EffectVerifyUser, UserID: outsider.ID.String()},
			want:   "not the customer",
		},
		{
			name:   "acting on a different order",
			effect: behavior.Effect{Kind: behavior.EffectCompleteOrder, OrderID: uuid.New().String()},
			want:   "was about",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := w.dispatcher.apply(ctx, event, tgt, []behavior.Effect{c.effect})
			if err == nil {
				t.Fatalf("the core allowed %s", c.name)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error %q does not explain the refusal (%q)", err, c.want)
			}
		})
	}

	if _, paid := w.tx.balances[outsider.ID]; paid {
		t.Errorf("a refused payout still touched the balance of %s", outsider.ID)
	}
}

// A moderator's own verification order must not be self-served.
func TestVerifierCannotBeTheCustomer(t *testing.T) {
	w := newVerificationWorld(t)
	ctx := context.Background()

	selfServing := w.users.add(repository.RoleExecutor, []string{repository.RoleExecutor, repository.RoleModerator}, false)
	order, err := w.orderSvc.CreateOrder(ctx, selfServing.ID, verificationVariantID, false, false, "Москва", nil, nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	err = w.orderSvc.Accept(ctx, order.ID, selfServing.ID)
	if err == nil {
		t.Fatal("a moderator took their own verification order")
	}
	if errors.Is(err, ErrBehaviorUnavailable) {
		t.Errorf("the refusal should come from the rule, not from a broken script: %v", err)
	}
}
