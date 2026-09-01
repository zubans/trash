package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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

// add creates a user with a name of their own: a test where everybody is called
// the same thing cannot tell "the executor sees the customer's name" from "the
// executor sees their own".
func (u *verificationUsers) add(role string, roles []string, verified bool) *repository.User {
	birth := time.Date(1990, time.March, 14, 0, 0, 0, 0, time.UTC)
	nth := len(u.users) + 1
	user := &repository.User{
		ID: uuid.New(), Role: role, Roles: roles, Status: "ACTIVE",
		Verified: verified, BirthDate: &birth,
		LastName:   fmt.Sprintf("Фамилия%d", nth),
		FirstName:  fmt.Sprintf("Имя%d", nth),
		Patronymic: fmt.Sprintf("Отчество%d", nth),
		Phone:      fmt.Sprintf("+7900000000%d", nth),
	}
	u.users[user.ID] = user
	return user
}

// passportOf is what a moderator would type off the customer's document when it
// is genuinely their document.
func passportOf(user *repository.User) map[string]string {
	return map[string]string{
		"last_name":  user.LastName,
		"first_name": user.FirstName,
		"patronymic": user.Patronymic,
		"birth_date": user.BirthDate.Format("2006-01-02"),
	}
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

// verificationSubmissions is the store behind data checks and escalations.
type verificationSubmissions struct {
	submissions []*repository.OrderSubmission
	escalations []*repository.BehaviorEscalation
}

func (s *verificationSubmissions) Record(ctx context.Context, q repository.Querier, submission *repository.OrderSubmission) error {
	submission.ID = uuid.New()
	attempt := 0
	for _, existing := range s.submissions {
		if existing.OrderID == submission.OrderID {
			attempt++
		}
	}
	submission.Attempt = attempt + 1
	submission.CreatedAt = time.Now()
	s.submissions = append(s.submissions, submission)
	return nil
}

func (s *verificationSubmissions) CountForOrder(ctx context.Context, orderID uuid.UUID) (int, error) {
	count := 0
	for _, submission := range s.submissions {
		if submission.OrderID == orderID {
			count++
		}
	}
	return count, nil
}

func (s *verificationSubmissions) ListForOrder(ctx context.Context, orderID uuid.UUID) ([]*repository.OrderSubmission, error) {
	var out []*repository.OrderSubmission
	for _, submission := range s.submissions {
		if submission.OrderID == orderID {
			out = append(out, submission)
		}
	}
	return out, nil
}

func (s *verificationSubmissions) Escalate(ctx context.Context, q repository.Querier, escalation *repository.BehaviorEscalation) error {
	open, _ := s.HasOpenEscalation(ctx, escalation.OrderID)
	if open {
		return nil
	}
	escalation.ID = uuid.New()
	escalation.Status = repository.EscalationOpen
	escalation.CreatedAt = time.Now()
	s.escalations = append(s.escalations, escalation)
	return nil
}

func (s *verificationSubmissions) HasOpenEscalation(ctx context.Context, orderID uuid.UUID) (bool, error) {
	for _, escalation := range s.escalations {
		if escalation.OrderID == orderID && escalation.Status == repository.EscalationOpen {
			return true, nil
		}
	}
	return false, nil
}

func (s *verificationSubmissions) ListEscalations(ctx context.Context, status string, limit int) ([]*repository.BehaviorEscalation, error) {
	return s.escalations, nil
}

func (s *verificationSubmissions) ResolveEscalation(ctx context.Context, id, adminID uuid.UUID) error {
	for _, escalation := range s.escalations {
		if escalation.ID == id {
			escalation.Status = repository.EscalationResolved
			return nil
		}
	}
	return repository.ErrEscalationNotFound
}

func (s *verificationSubmissions) ResolveByOrder(ctx context.Context, q repository.Querier, orderID uuid.UUID, adminID *uuid.UUID) error {
	for _, escalation := range s.escalations {
		if escalation.OrderID == orderID {
			escalation.Status = repository.EscalationResolved
		}
	}
	return nil
}

// --- harness ----------------------------------------------------------------

type verificationWorld struct {
	orders      *mockOrderRepo
	users       *verificationUsers
	catalog     *verificationCatalog
	claims      *verificationClaims
	events      *verificationEvents
	tx          *mockTransactionRepo
	accounts    *mockAccounts
	submissions *verificationSubmissions
	orderSvc    *OrderService
	dispatcher  *BehaviorDispatcher
	behaviors   *Behaviors
	settings    *orderMockSettingsRepo
	customer    *repository.User
	moderator   *repository.User
	executor    *repository.User
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
		orders:      &mockOrderRepo{},
		users:       &verificationUsers{users: map[uuid.UUID]*repository.User{}},
		catalog:     &verificationCatalog{node: node},
		claims:      newVerificationClaims(),
		events:      newVerificationEvents(),
		tx:          &mockTransactionRepo{},
		accounts:    newMockAccounts(),
		submissions: &verificationSubmissions{},
	}
	w.behaviors = NewBehaviors(engine, w.claims)
	ledger := NewLedger(w.tx, w.accounts)
	settings := &orderMockSettingsRepo{settings: map[string]string{}}
	w.settings = settings

	w.orderSvc = NewOrderService(w.orders, ledger, settings, w.users, &orderMockShiftRepo{}, nil, w.catalog, nil).
		WithBehaviors(w.behaviors, w.claims, w.events)
	w.dispatcher = NewBehaviorDispatcher(w.events, w.orders, w.users, w.catalog, w.claims, nil,
		settings, ledger, w.behaviors, w.orderSvc).
		WithSubmissions(w.submissions)

	w.customer = w.users.add(repository.RoleCustomer, nil, false)
	w.moderator = w.users.add(repository.RoleExecutor, []string{repository.RoleExecutor, repository.RoleModerator}, true)
	w.executor = w.users.add(repository.RoleExecutor, []string{repository.RoleExecutor}, true)
	return w
}

// orderView renders the order the way a list endpoint does, which is how the
// executor's app sees it.
func (w *verificationWorld) orderView(t *testing.T, orderID uuid.UUID) *repository.Order {
	t.Helper()
	orders, err := w.orderSvc.ListAssigned(context.Background(), w.moderator.ID)
	if err != nil {
		t.Fatalf("assigned orders: %v", err)
	}
	for _, order := range orders {
		if order.ID == orderID {
			return order
		}
	}
	t.Fatalf("order %s is not in the executor's list", orderID)
	return nil
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

	// The moderator is shown the address and nothing else: what they type comes
	// off the document in front of them.
	if got := w.orderView(t, order.ID).SubmitFields; len(got) != 4 {
		t.Errorf("the executor is not told which fields to submit: %v", got)
	}

	beforeReward := w.tx.balances[w.moderator.ID]
	result, err := w.dispatcher.SubmitOrderData(ctx, order.ID, w.moderator.ID, passportOf(w.customer))
	if err != nil {
		t.Fatalf("submit identity data: %v", err)
	}
	if !result.Matched {
		t.Fatalf("matching data was rejected: %+v", result)
	}

	if !w.customer.Verified {
		t.Error("the customer was not verified by the successful check")
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
		if _, err := w.dispatcher.SubmitOrderData(ctx, order.ID, w.moderator.ID, passportOf(w.customer)); err != nil {
			t.Fatalf("submit: %v", err)
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
	if _, err := w.dispatcher.SubmitOrderData(ctx, order.ID, w.moderator.ID, passportOf(w.customer)); err != nil {
		t.Fatalf("submit: %v", err)
	}
	paidOnce := w.tx.balances[w.moderator.ID]

	// The same event again: a retry after a crash, a second replica, a
	// duplicated row — all look like this.
	if err := w.events.Publish(ctx, nil, &repository.DomainEvent{
		Type:        repository.EventUserVerified,
		SubjectType: repository.EventSubjectUser,
		SubjectID:   w.customer.ID,
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
			_, err := w.dispatcher.apply(ctx, event, tgt, []behavior.Effect{c.effect})
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

// --- Identity check ---------------------------------------------------------

// The moderator submits what the document says; the platform compares. A first
// mismatch is a warning to check the passport again — not a verification, not a
// payment, and not a hint about which field was wrong.
func TestIdentityMismatchWarnsFirst(t *testing.T) {
	w := newVerificationWorld(t)
	ctx := context.Background()

	order := w.acceptedVerificationOrder(t)
	wrong := passportOf(w.customer)
	wrong["last_name"] = "Петров"

	result, err := w.dispatcher.SubmitOrderData(ctx, order.ID, w.moderator.ID, wrong)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if result.Matched {
		t.Fatal("wrong data was accepted as a match")
	}
	if result.Attempt != 1 {
		t.Errorf("attempt = %d, want 1", result.Attempt)
	}
	if result.Escalated {
		t.Error("a single typo sent the case to an administrator")
	}
	if len(result.Messages) == 0 {
		t.Error("the moderator was not told to check the document again")
	}
	if w.customer.Verified {
		t.Fatal("a failed check verified the customer")
	}
	if got := w.tx.balances[w.moderator.ID]; got != w.rewardBase {
		t.Errorf("a failed check paid %s", got.Sub(w.rewardBase))
	}

	// The mismatched field is reported to the caller for its own form, but the
	// message that reaches the moderator must not narrow the search for them.
	for _, message := range result.Messages {
		if strings.Contains(message, "Петров") || strings.Contains(message, w.customer.LastName) {
			t.Errorf("the warning leaks the compared values: %q", message)
		}
	}
}

// Out of attempts, the case goes to an administrator and the moderator cannot
// keep guessing.
func TestIdentityMismatchEscalatesAndLocksTheOrder(t *testing.T) {
	w := newVerificationWorld(t)
	ctx := context.Background()

	order := w.acceptedVerificationOrder(t)
	wrong := passportOf(w.customer)
	wrong["birth_date"] = "1980-01-01"

	if _, err := w.dispatcher.SubmitOrderData(ctx, order.ID, w.moderator.ID, wrong); err != nil {
		t.Fatalf("first submit: %v", err)
	}
	result, err := w.dispatcher.SubmitOrderData(ctx, order.ID, w.moderator.ID, wrong)
	if err != nil {
		t.Fatalf("second submit: %v", err)
	}
	if !result.Escalated {
		t.Fatalf("the case was not handed to an administrator: %+v", result)
	}

	open, _ := w.submissions.HasOpenEscalation(ctx, order.ID)
	if !open {
		t.Error("no escalation was recorded")
	}
	// Both attempts are kept: that is what the administrator reviews.
	attempts, _ := w.submissions.ListForOrder(ctx, order.ID)
	if len(attempts) != 2 {
		t.Errorf("stored %d attempts, want 2", len(attempts))
	}

	// Even correct data is not accepted afterwards: the decision is no longer
	// the moderator's.
	if _, err := w.dispatcher.SubmitOrderData(ctx, order.ID, w.moderator.ID, passportOf(w.customer)); !errors.Is(err, ErrSubmissionEscalated) {
		t.Errorf("submitting after an escalation returned %v, want ErrSubmissionEscalated", err)
	}
	if w.customer.Verified {
		t.Error("the customer was verified while the case was with an administrator")
	}
}

// The administrator settles it the ordinary way — by verifying the customer —
// and that closes both the order and the escalation.
func TestAdminResolvesAnEscalatedVerification(t *testing.T) {
	w := newVerificationWorld(t)
	ctx := context.Background()

	order := w.acceptedVerificationOrder(t)
	wrong := passportOf(w.customer)
	wrong["first_name"] = "Пётр"
	for i := 0; i < 2; i++ {
		if _, err := w.dispatcher.SubmitOrderData(ctx, order.ID, w.moderator.ID, wrong); err != nil {
			t.Fatalf("submit %d: %v", i+1, err)
		}
	}

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

	closed, _ := w.orders.GetOrderByID(ctx, order.ID)
	if closed.Status != repository.OrderStatusCompleted {
		t.Errorf("order status = %s, want COMPLETED", closed.Status)
	}
	if got := w.tx.balances[w.moderator.ID].Sub(w.rewardBase); got != money.FromRubles(200) {
		t.Errorf("verifier was paid %s, want 200.00: the visit did happen", got)
	}
	if open, _ := w.submissions.HasOpenEscalation(ctx, order.ID); open {
		t.Error("the escalation is still open on a closed order")
	}
}

// What the moderator is given to work with: the address, and nothing that would
// let them copy the answer instead of reading the document.
func TestExecutorSeesTheAddressAndNoCustomerIdentity(t *testing.T) {
	w := newVerificationWorld(t)

	order := w.acceptedVerificationOrder(t)
	view := w.orderView(t, order.ID)
	if view.Address == nil || *view.Address == "" {
		t.Error("the moderator has no address to go to")
	}
	if len(view.SubmitFields) == 0 {
		t.Error("the moderator is not told what to submit")
	}

	rendered, err := json.Marshal(view)
	if err != nil {
		t.Fatalf("render order: %v", err)
	}
	body := string(rendered)
	for _, secret := range []string{
		w.customer.LastName, w.customer.FirstName, w.customer.Patronymic,
		w.customer.BirthDate.Format("2006-01-02"), w.customer.Phone,
	} {
		if secret != "" && strings.Contains(body, secret) {
			t.Errorf("the order the executor receives carries the customer's %q", secret)
		}
	}
}

// acceptedVerificationOrder creates a verification order and puts the moderator
// on it, which is the state every check starts from.
func (w *verificationWorld) acceptedVerificationOrder(t *testing.T) *repository.Order {
	t.Helper()
	ctx := context.Background()
	order, err := w.orderSvc.CreateOrder(ctx, w.customer.ID, verificationVariantID, false, false, "Москва, Арбат, 10", nil, nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := w.orderSvc.Accept(ctx, order.ID, w.moderator.ID); err != nil {
		t.Fatalf("accept: %v", err)
	}
	w.rewardBase = w.tx.balances[w.moderator.ID]
	return order
}
