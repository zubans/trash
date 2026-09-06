package service

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/achievement"
	"healthlogin/backend/money"
	"healthlogin/backend/repository"
)

// Диспетчер ачивок — то место, где решение скрипта становится баллами, а баллы
// становятся деньгами. Тесты ниже проверяют не «сработала ли ачивка», а
// границы, которых нет в скриптах и которые поэтому обязаны быть здесь:
// повторная доставка не начисляет дважды, сговор не засчитывается, суточный
// потолок держит, отменённый заказ отбирает выданное.

// --- Хранилища-заглушки -------------------------------------------------------

// dispatchEvents — outbox, которому важен только курсор и ключи эффектов.
type dispatchEvents struct {
	published []*repository.DomainEvent
	processed map[uuid.UUID]bool
	effects   map[string]bool
}

func newDispatchEvents() *dispatchEvents {
	return &dispatchEvents{processed: map[uuid.UUID]bool{}, effects: map[string]bool{}}
}

func (e *dispatchEvents) RunInTx(ctx context.Context, fn func(*sql.Tx) error) error { return fn(nil) }

func (e *dispatchEvents) Publish(ctx context.Context, q repository.Querier, event *repository.DomainEvent) error {
	if event.ID == uuid.Nil {
		event.ID = uuid.New()
	}
	e.published = append(e.published, event)
	return nil
}

func (e *dispatchEvents) ClaimPending(ctx context.Context, consumer string, limit, maxAttempts int) ([]*repository.DomainEvent, error) {
	var pending []*repository.DomainEvent
	for _, event := range e.published {
		if !e.processed[event.ID] {
			pending = append(pending, event)
		}
	}
	return pending, nil
}

func (e *dispatchEvents) MarkProcessed(ctx context.Context, consumer string, id uuid.UUID) error {
	e.processed[id] = true
	return nil
}

func (e *dispatchEvents) MarkFailed(ctx context.Context, consumer string, id uuid.UUID, reason string) error {
	return nil
}

func (e *dispatchEvents) RecordEffect(ctx context.Context, q repository.Querier, key string, eventID uuid.UUID, code, kind string, payload map[string]interface{}) error {
	if e.effects[key] {
		return repository.ErrEffectAlreadyApplied
	}
	e.effects[key] = true
	return nil
}

func (e *dispatchEvents) PurgeProcessed(ctx context.Context, olderThan time.Duration) (int64, error) {
	return 0, nil
}

func (e *dispatchEvents) CountPending(ctx context.Context, consumer string) (int, error) {
	return len(e.published) - len(e.processed), nil
}

// dispatchAchievements — каталог, выдачи и баллы в памяти. Уникальность
// (пользователь, код, ключ) воспроизведена намеренно: в бою её обеспечивает
// индекс, и без неё тест на повторную доставку проверял бы не то.
type dispatchAchievements struct {
	rows    []*repository.Achievement
	granted []*repository.UserAchievement
	points  []int
	daily   int
	revoked int
}

func (a *dispatchAchievements) List(ctx context.Context) ([]*repository.Achievement, error) {
	return a.rows, nil
}
func (a *dispatchAchievements) ListActive(ctx context.Context) ([]*repository.Achievement, error) {
	return a.rows, nil
}
func (a *dispatchAchievements) Get(ctx context.Context, code string) (*repository.Achievement, error) {
	for _, row := range a.rows {
		if row.Code == code {
			return row, nil
		}
	}
	return nil, sql.ErrNoRows
}
func (a *dispatchAchievements) Upsert(ctx context.Context, row *repository.Achievement) error {
	return nil
}
func (a *dispatchAchievements) Create(ctx context.Context, row *repository.Achievement) error {
	a.rows = append(a.rows, row)
	return nil
}
func (a *dispatchAchievements) Delete(ctx context.Context, code string) error  { return nil }
func (a *dispatchAchievements) Restore(ctx context.Context, code string) error { return nil }
func (a *dispatchAchievements) ListDeleted(ctx context.Context) ([]*repository.Achievement, error) {
	return nil, nil
}
func (a *dispatchAchievements) ListWithScript(ctx context.Context) ([]*repository.Achievement, error) {
	return nil, nil
}

func (a *dispatchAchievements) Grant(ctx context.Context, q repository.Querier, grant *repository.UserAchievement) error {
	for _, existing := range a.granted {
		if existing.UserID == grant.UserID && existing.Code == grant.Code && existing.GrantKey == grant.GrantKey {
			return repository.ErrAchievementAlreadyGranted
		}
	}
	if grant.ID == uuid.Nil {
		grant.ID = uuid.New()
	}
	grant.GrantedAt = time.Now()
	a.granted = append(a.granted, grant)
	return nil
}

func (a *dispatchAchievements) AddPoints(ctx context.Context, q repository.Querier, userID uuid.UUID, points int, sourceType, sourceCode string, sourceID *uuid.UUID, reason string, expiresAt *time.Time) error {
	a.points = append(a.points, points)
	return nil
}

func (a *dispatchAchievements) ActivePoints(ctx context.Context, q repository.Querier, userID uuid.UUID) (int, error) {
	total := 0
	for _, p := range a.points {
		total += p
	}
	return total, nil
}

func (a *dispatchAchievements) PointsToday(ctx context.Context, q repository.Querier, userID uuid.UUID) (int, error) {
	return a.daily, nil
}

func (a *dispatchAchievements) BumpPointsToday(ctx context.Context, q repository.Querier, userID uuid.UUID, points int) (int, error) {
	a.daily += points
	return a.daily, nil
}

func (a *dispatchAchievements) ListForUser(ctx context.Context, userID uuid.UUID) ([]*repository.UserAchievement, error) {
	return a.granted, nil
}

func (a *dispatchAchievements) SummaryForUser(ctx context.Context, userID uuid.UUID) (map[string]repository.GrantSummary, error) {
	out := map[string]repository.GrantSummary{}
	for _, g := range a.granted {
		if g.UserID != userID || g.RevokedAt != nil {
			continue
		}
		summary := out[g.Code]
		summary.Count++
		summary.Points += g.Points
		summary.GrantedAt = g.GrantedAt
		out[g.Code] = summary
	}
	return out, nil
}

func (a *dispatchAchievements) RevokeByOrder(ctx context.Context, q repository.Querier, orderID uuid.UUID, reason string) (int, error) {
	now := time.Now()
	count := 0
	for _, g := range a.granted {
		if g.OrderID != nil && *g.OrderID == orderID && g.RevokedAt == nil {
			g.RevokedAt = &now
			g.RevokeReason = reason
			count++
		}
	}
	a.revoked += count
	return count, nil
}

func (a *dispatchAchievements) Revoke(ctx context.Context, id uuid.UUID, reason string) error {
	return nil
}

// dispatchStats отдаёт заранее заданные агрегаты.
type dispatchStats struct{ stats repository.ExecutorStats }

func (s *dispatchStats) Get(ctx context.Context, q repository.Querier, userID uuid.UUID) (*repository.ExecutorStats, error) {
	copied := s.stats
	copied.UserID = userID
	return &copied, nil
}
func (s *dispatchStats) RecordCompletion(ctx context.Context, q repository.Querier, order repository.CompletedOrder) error {
	return nil
}
func (s *dispatchStats) RecordCancel(ctx context.Context, q repository.Querier, executorID uuid.UUID) error {
	return nil
}
func (s *dispatchStats) RecordRating(ctx context.Context, q repository.Querier, executorID uuid.UUID, rating int) error {
	return nil
}
func (s *dispatchStats) Recalculate(ctx context.Context, userID uuid.UUID) error { return nil }

// dispatchMail собирает письма, чтобы тест мог убедиться, что о выдаче сказали.
type dispatchMail struct{ sent []repository.Mail }

func (m *dispatchMail) Send(ctx context.Context, q repository.Querier, mail *repository.Mail) error {
	m.sent = append(m.sent, *mail)
	return nil
}
func (m *dispatchMail) Broadcast(ctx context.Context, mail *repository.Mail, userIDs []uuid.UUID) (int, error) {
	return 0, nil
}
func (m *dispatchMail) RecipientsByRole(ctx context.Context, role string) ([]uuid.UUID, error) {
	return nil, nil
}
func (m *dispatchMail) ListForUser(ctx context.Context, userID uuid.UUID, limit int) ([]*repository.Mail, error) {
	return nil, nil
}
func (m *dispatchMail) UnreadCount(ctx context.Context, userID uuid.UUID) (int, error) { return 0, nil }
func (m *dispatchMail) MarkRead(ctx context.Context, id, userID uuid.UUID) error       { return nil }
func (m *dispatchMail) MarkAllRead(ctx context.Context, userID uuid.UUID) error        { return nil }
func (m *dispatchMail) Delete(ctx context.Context, id, userID uuid.UUID) error         { return nil }

// emptyGifts — каталог подарков, в котором ничего нет: так выглядит пустой
// склад, и ачивка на нём обязана выдаться всё равно.
type emptyGifts struct{ repository.GiftRepository }

func (g *emptyGifts) Get(ctx context.Context, code string) (*repository.Gift, error) {
	return nil, sql.ErrNoRows
}

// --- Обвязка -----------------------------------------------------------------

type dispatchHarness struct {
	dispatcher   *AchievementDispatcher
	events       *dispatchEvents
	achievements *dispatchAchievements
	mail         *dispatchMail
	incidents    *recordingIncidents
	orders       *mockOrderRepo
	order        *repository.Order
	executorID   uuid.UUID
	customerID   uuid.UUID
}

// newDispatchHarness поднимает диспетчер с одной ачивкой «первый заказ» и одним
// подтверждённым заказом, готовым к событию.
func newDispatchHarness(t *testing.T, stats repository.ExecutorStats, maxPointsPerDay string) *dispatchHarness {
	t.Helper()

	engine := achievement.New(achievement.DefaultLimits)
	// Скрипт нарочно простейший: тест проверяет ядро, а не логику ачивки.
	if err := engine.Compile("test_award", "achievement.star", []byte(`
MANIFEST = {
    "title": "Тестовая ачивка",
    "audience": "EXECUTOR",
    "events": ["order.confirmed"],
    "once_per_user": True,
    "weight": 25,
}

def check(f):
    if f.order == None or f.order.executor_id != f.user.id:
        return None
    return grant(points = 25, order_id = f.order.id, effects = [notify(text = "готово")])
`)); err != nil {
		t.Fatalf("compile: %v", err)
	}

	settings := &orderMockSettingsRepo{settings: map[string]string{
		SettingAchievementMaxPointsPerDay: maxPointsPerDay,
		SettingAchievementMinOrderAmount:  "300",
		SettingAchievementLevelPoints:     "500",
		SettingAchievementLevelDiscountPP: "1",
	}}

	achievements := &dispatchAchievements{
		rows: []*repository.Achievement{{Code: "test_award", IsActive: true}},
	}
	events := newDispatchEvents()
	mail := &dispatchMail{}
	incidents := &recordingIncidents{}
	orders := &mockOrderRepo{}

	customerID, executorID := uuid.New(), uuid.New()
	order := &repository.Order{
		ID: uuid.New(), CustomerID: customerID, ExecutorID: &executorID,
		Status: repository.OrderStatusCompleted, FinalAmount: money.FromRubles(1000),
		CreatedAt: time.Now().Add(-10 * time.Minute),
	}
	orders.orders = append(orders.orders, order)

	dispatcher := NewAchievementDispatcher(
		events, orders, newMockUserRepo(), achievements, &dispatchStats{stats: stats},
		&emptyGifts{}, mail, incidents,
		NewLedger(&mockTransactionRepo{}, newMockAccounts()),
		NewLevels(achievements, settings), engine,
	)

	return &dispatchHarness{
		dispatcher: dispatcher, events: events, achievements: achievements,
		mail: mail, incidents: incidents, orders: orders, order: order,
		executorID: executorID, customerID: customerID,
	}
}

func (h *dispatchHarness) confirm(t *testing.T) {
	t.Helper()
	if err := h.events.Publish(context.Background(), nil, &repository.DomainEvent{
		Type:        repository.EventOrderConfirmed,
		SubjectType: repository.EventSubjectOrder,
		SubjectID:   h.order.ID,
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := h.dispatcher.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
}

// --- Тесты -------------------------------------------------------------------

func TestDispatcherGrantsOnceAndWritesMail(t *testing.T) {
	h := newDispatchHarness(t, repository.ExecutorStats{OrdersCompleted: 1}, "500")
	h.confirm(t)

	if len(h.achievements.granted) != 1 {
		t.Fatalf("granted %d achievements, want 1", len(h.achievements.granted))
	}
	if h.achievements.granted[0].UserID != h.executorID {
		t.Error("the achievement went to somebody other than the executor")
	}
	if len(h.achievements.points) != 1 || h.achievements.points[0] != 25 {
		t.Errorf("points = %v, want a single 25", h.achievements.points)
	}
	if len(h.mail.sent) != 1 || h.mail.sent[0].Kind != repository.MailKindAchievement {
		t.Errorf("mail = %+v, want one achievement letter", h.mail.sent)
	}
}

// Переотправленное событие — обычное дело: воркер повторяет то, что не смог
// доотметить обработанным. Оно не должно начислять баллы второй раз.
func TestRedeliveredEventDoesNotGrantTwice(t *testing.T) {
	h := newDispatchHarness(t, repository.ExecutorStats{OrdersCompleted: 1}, "500")
	h.confirm(t)

	// Тот же заказ, новое событие — так выглядит и переотправка, и второе
	// событие, описывающее тот же исход.
	h.events.processed = map[uuid.UUID]bool{}
	if err := h.dispatcher.Tick(context.Background()); err != nil {
		t.Fatalf("second tick: %v", err)
	}

	if len(h.achievements.granted) != 1 {
		t.Errorf("granted %d times, want exactly 1", len(h.achievements.granted))
	}
	if len(h.achievements.points) != 1 {
		t.Errorf("points credited %d times, want exactly 1", len(h.achievements.points))
	}
}

// Заказ самому себе — простейший способ нарисовать ачивку, и проверка живёт в
// ядре, потому что скрипт может её забыть.
func TestOrderToOneselfEarnsNothing(t *testing.T) {
	h := newDispatchHarness(t, repository.ExecutorStats{OrdersCompleted: 1}, "500")
	h.order.ExecutorID = &h.order.CustomerID
	h.confirm(t)

	if len(h.achievements.granted) != 0 {
		t.Errorf("granted %d achievements for a self-dealt order, want none", len(h.achievements.granted))
	}
}

// Неоплаченный заказ ачивок не приносит, как бы он ни закрылся.
func TestUnpaidOrderEarnsNothing(t *testing.T) {
	h := newDispatchHarness(t, repository.ExecutorStats{OrdersCompleted: 1}, "500")
	h.order.FinalAmount = money.Zero
	h.confirm(t)

	if len(h.achievements.granted) != 0 {
		t.Errorf("granted %d achievements for an unpaid order, want none", len(h.achievements.granted))
	}
}

// Суточный потолок — цена накрутки: сколько её ни устраивай, за сутки больше
// потолка не заработать. Ачивка при этом выдаётся: значок человек заслужил,
// ограничены баллы.
func TestDailyCapClampsPointsAndRecordsAnIncident(t *testing.T) {
	h := newDispatchHarness(t, repository.ExecutorStats{OrdersCompleted: 1}, "10")
	h.confirm(t)

	if len(h.achievements.granted) != 1 {
		t.Fatalf("granted %d achievements, want 1", len(h.achievements.granted))
	}
	if len(h.achievements.points) != 1 || h.achievements.points[0] != 10 {
		t.Errorf("points = %v, want the 25 clamped to the daily 10", h.achievements.points)
	}
	if len(h.incidents.recorded) != 1 || h.incidents.recorded[0].Kind != repository.IncidentPointsCapHit {
		t.Errorf("incidents = %+v, want a single points_cap_hit", h.incidents.recorded)
	}
}

// Отменённый заказ отбирает выданное. Без этого накрутка сводится к «создать,
// закрыть, отменить»: деньги возвращаются заказчику, а баллы остаются.
func TestCancelingAnOrderRevokesWhatItEarned(t *testing.T) {
	h := newDispatchHarness(t, repository.ExecutorStats{OrdersCompleted: 1}, "500")
	h.confirm(t)
	if len(h.achievements.granted) != 1 {
		t.Fatalf("granted %d achievements, want 1", len(h.achievements.granted))
	}

	h.events.processed = map[uuid.UUID]bool{}
	h.events.published = nil
	if err := h.events.Publish(context.Background(), nil, &repository.DomainEvent{
		Type:        repository.EventOrderCanceled,
		SubjectType: repository.EventSubjectOrder,
		SubjectID:   h.order.ID,
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := h.dispatcher.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	if h.achievements.revoked != 1 {
		t.Errorf("revoked %d grants, want 1", h.achievements.revoked)
	}
	if h.achievements.granted[0].RevokedAt == nil {
		t.Error("the grant is still active after its order was cancelled")
	}
}

// Пустой склад подарков не должен останавливать очередь: ачивка выдаётся,
// баллы начисляются, подарок — нет, а расхождение записывается инцидентом.
func TestMissingGiftDoesNotBlockTheGrant(t *testing.T) {
	h := newDispatchHarness(t, repository.ExecutorStats{OrdersCompleted: 1}, "500")
	// Ачивка, просящая подарок, которого нет в каталоге.
	engine := achievement.New(achievement.DefaultLimits)
	if err := engine.Compile("test_award", "achievement.star", []byte(`
MANIFEST = {
    "title": "С подарком",
    "audience": "EXECUTOR",
    "events": ["order.confirmed"],
    "once_per_user": True,
    "weight": 25,
}

def check(f):
    if f.order == None or f.order.executor_id != f.user.id:
        return None
    return grant(points = 25, order_id = f.order.id, effects = [gift(code = "missing_gift")])
`)); err != nil {
		t.Fatalf("compile: %v", err)
	}
	h.dispatcher.engine = engine

	h.confirm(t)

	if len(h.achievements.granted) != 1 {
		t.Fatalf("granted %d achievements, want 1 even without the gift", len(h.achievements.granted))
	}
	if len(h.incidents.recorded) != 1 || h.incidents.recorded[0].Kind != repository.IncidentGiftOutOfStock {
		t.Errorf("incidents = %+v, want a single gift_out_of_stock", h.incidents.recorded)
	}
	// Событие обработано, а не оставлено на повтор: пустой склад — операционная
	// проблема, а не повод задерживать ачивки всех остальных.
	if pending, _ := h.events.CountPending(context.Background(), repository.ConsumerAchievements); pending != 0 {
		t.Errorf("%d events left pending, want the queue to move on", pending)
	}
}

// Порог суммы держит ядро, а не только скрипты: заказ в рубль не должен
// приносить баллы из-за того, что в одном из скриптов о пороге забыли.
func TestOrderBelowTheFloorEarnsNothing(t *testing.T) {
	h := newDispatchHarness(t, repository.ExecutorStats{OrdersCompleted: 1}, "500")
	h.order.FinalAmount = money.FromRubles(1)
	h.confirm(t)

	if len(h.achievements.granted) != 0 {
		t.Errorf("granted %d achievements for a 1 ruble order, want none", len(h.achievements.granted))
	}
}
