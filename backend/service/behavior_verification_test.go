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

// Услуга верификации от начала до конца, тем же кодом, который выполняет
// приложение: скрипт решает, ядро применяет. Здесь фиксируется та часть,
// которую нельзя доверить скрипту: что деньги двигаются один раз, в одну
// сторону, и что книги после этого по-прежнему сходятся.

var verificationVariantID = uuid.MustParse("dddddddd-dddd-dddd-dddd-dddddddddddd")

// --- подделки ---------------------------------------------------------------

// verificationUsers — небольшое хранилище пользователей. Оно встраивает
// интерфейс, чтобы методы, которые этот поток никогда не вызывает, не пришлось
// расписывать; вызов такого паникует, и это верный исход для теста, у которого
// завелась незаявленная зависимость.
type verificationUsers struct {
	repository.UserRepository
	users map[uuid.UUID]*repository.User
}

// add создаёт пользователя с собственным именем: тест, где всех зовут
// одинаково, не отличит «исполнитель видит имя заказчика» от «исполнитель видит
// собственное».
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

// passportOf — то, что модератор набрал бы с документа заказчика, если это
// действительно его документ.
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

// verificationCatalog отдаёт засеянный вариант верификации.
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

// verificationEvents — тот же outbox в памяти, с тем же правилом
// идемпотентности, которое обеспечивает таблица.
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

// Курсор здесь один на всех потребителей: в этих тестах диспетчер поведений
// единственный, и различать их незачем.
func (e *verificationEvents) ClaimPending(ctx context.Context, consumer string, limit, maxAttempts int) ([]*repository.DomainEvent, error) {
	var pending []*repository.DomainEvent
	for _, event := range e.published {
		if !e.processed[event.ID] {
			pending = append(pending, event)
		}
	}
	return pending, nil
}

func (e *verificationEvents) MarkProcessed(ctx context.Context, consumer string, id uuid.UUID) error {
	e.processed[id] = true
	return nil
}

func (e *verificationEvents) MarkFailed(ctx context.Context, consumer string, id uuid.UUID, reason string) error {
	return nil
}

func (e *verificationEvents) RecordEffect(ctx context.Context, q repository.Querier, key string, eventID uuid.UUID, behaviorCode, kind string, payload map[string]interface{}) error {
	if e.effects[key] {
		return repository.ErrEffectAlreadyApplied
	}
	e.effects[key] = true
	return nil
}

// PurgeProcessed подрезает историю; ничто в этих тестах от неё не зависит.
func (e *verificationEvents) PurgeProcessed(ctx context.Context, olderThan time.Duration) (int64, error) {
	return 0, nil
}

func (e *verificationEvents) CountPending(ctx context.Context, consumer string) (int, error) {
	return len(e.published) - len(e.processed), nil
}

// verificationSubmissions — хранилище за проверками данных и эскалациями.
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

// --- обвязка ----------------------------------------------------------------

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
	// rewardBase — баланс проверяющего до вознаграждения, потому что подделка
	// баланса засевает новым пользователям собственные деньги.
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
		// Пусто, ровно как засевает миграция: суммы, роль и режим — это
		// константы поведения, а в этой колонке лежит только то, что узел
		// про них меняет.
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

// orderView отрисовывает заказ так же, как это делает списковый эндпоинт, — то
// есть так, как его видит приложение исполнителя.
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

// --- тесты ------------------------------------------------------------------

// Весь цикл: бесплатный заказ, взятый модератором, закрытый отчётом о визите, с
// однократно выплаченным вознаграждением и по-прежнему сошедшимися книгами.
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

	// Один раз на пользователя: вторая попытка отклоняется скриптом, до записи
	// какой-либо строки.
	if _, err := w.orderSvc.CreateOrder(ctx, w.customer.ID, verificationVariantID, false, false, "Москва, Арбат, 10", nil, nil); err == nil {
		t.Error("the verification service must be orderable only once")
	}

	// Сторона исполнителя: только модераторы.
	if err := w.orderSvc.Accept(ctx, order.ID, w.executor.ID); err == nil {
		t.Error("a plain executor must not be able to take a verification order")
	}
	if err := w.orderSvc.Accept(ctx, order.ID, w.moderator.ID); err != nil {
		t.Fatalf("a moderator must be able to take it: %v", err)
	}

	// Модератору показывают адрес и ничего больше: то, что он набирает, он берёт с
	// документа перед собой.
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

// Комиссия не берётся с вознаграждения, пока поведение об этом не попросит, а
// когда просит, брутто всё равно делится ровно.
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
		// Брутто по-прежнему уходит с BONUSES целиком: 180 пользователю, 20 в
		// COMMISSION.
		if got, want := w.accounts.balances[repository.AccountBonuses], money.FromRubles(-200); got != want {
			t.Errorf("BONUSES = %s, want %s", got, want)
		}
	})
}

// Переотправленное событие не должно выплатить второе вознаграждение. Мешает
// этому ключ идемпотентности, который скрипт прикрепляет к платежу.
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

	// То же событие снова: повтор после падения, вторая реплика, продублированная
	// строка — всё это выглядит так.
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

// Отменённый заказ возвращает заказчику его единственную попытку — иначе одна
// отмена навсегда лишила бы его возможности верифицироваться.
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

// Админский чекбокс — второй путь: ручная верификация заказчика закрывает
// открытый у него заказ и платит тому, кто его выполнял.
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

	// Ровно то, что пишет AdminService.SetUserVerified.
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

// Кому и сколько можно платить, решает ядро, а не скрипт. Это те проверки,
// которые делают неверный скрипт неверным решением, а не украденной выплатой.
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

// Собственный заказ верификации модератора не должен обслуживаться им самим.
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

// --- Проверка личности ------------------------------------------------------

// Модератор отправляет то, что написано в документе; платформа сравнивает.
// Первое несовпадение — предупреждение перепроверить паспорт: не верификация,
// не платёж и не подсказка о том, какое поле было неверным.
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

	// Несовпавшее поле сообщается вызывающему для его собственной формы, но
	// сообщение, доходящее до модератора, не должно сужать ему поиск.
	for _, message := range result.Messages {
		if strings.Contains(message, "Петров") || strings.Contains(message, w.customer.LastName) {
			t.Errorf("the warning leaks the compared values: %q", message)
		}
	}
}

// Попытки кончились, случай уходит администратору, и модератор не может
// продолжать угадывать.
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
	// Сохраняются обе попытки: именно их и разбирает администратор.
	attempts, _ := w.submissions.ListForOrder(ctx, order.ID)
	if len(attempts) != 2 {
		t.Errorf("stored %d attempts, want 2", len(attempts))
	}

	// Даже верные данные после этого не принимаются: решение больше не за
	// модератором.
	if _, err := w.dispatcher.SubmitOrderData(ctx, order.ID, w.moderator.ID, passportOf(w.customer)); !errors.Is(err, ErrSubmissionEscalated) {
		t.Errorf("submitting after an escalation returned %v, want ErrSubmissionEscalated", err)
	}
	if w.customer.Verified {
		t.Error("the customer was verified while the case was with an administrator")
	}
}

// Администратор улаживает это обычным путём — верифицируя заказчика, — и это
// закрывает и заказ, и эскалацию.
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

	// Ровно то, что пишет AdminService.SetUserVerified.
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

// То, с чем модератору дают работать: адрес и ничего, что позволило бы
// списать ответ вместо чтения документа.
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

// acceptedVerificationOrder создаёт заказ верификации и ставит на него
// модератора — состояние, с которого начинается любая проверка.
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
