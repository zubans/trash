package service

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/achievement"
	"healthlogin/backend/achievements"
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
	// hideSummary заставляет свод молчать о выданном. Так проверяется вторая
	// линия защиты от повторной выдачи — ключ идемпотентности и уникальный
	// индекс: при живом своде до неё дело не доходит, потому что разовая
	// ачивка, которая у человека уже есть, скрипту больше не показывается.
	hideSummary bool
}

func (a *dispatchAchievements) List(ctx context.Context) ([]*repository.Achievement, error) {
	return a.rows, nil
}
func (a *dispatchAchievements) ListActive(ctx context.Context) ([]*repository.Achievement, error) {
	return a.rows, nil
}
func (a *dispatchAchievements) ListAll(ctx context.Context) ([]*repository.Achievement, error) {
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
	if a.hideSummary {
		return out, nil
	}
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

// Переписка диспетчеру не нужна: он пишет письма, а не читает ответы.
func (m *dispatchMail) Get(ctx context.Context, id uuid.UUID) (*repository.Mail, error) {
	return nil, sql.ErrNoRows
}
func (m *dispatchMail) Thread(ctx context.Context, threadID uuid.UUID) ([]*repository.Mail, error) {
	return nil, nil
}
func (m *dispatchMail) Reply(ctx context.Context, mail *repository.Mail) error { return nil }
func (m *dispatchMail) ListDialogs(ctx context.Context, onlyUnanswered bool, limit int) ([]*repository.MailDialog, error) {
	return nil, nil
}
func (m *dispatchMail) ListDirectForUser(ctx context.Context, userID uuid.UUID, limit int) ([]*repository.Mail, error) {
	return nil, nil
}
func (m *dispatchMail) MarkThreadReadByAdmin(ctx context.Context, threadID uuid.UUID) error {
	return nil
}
func (m *dispatchMail) AdminUnreadCount(ctx context.Context) (int, error) { return 0, nil }

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

// Разовая ачивка, которая у человека уже есть, до скрипта не доходит вовсе:
// иначе каждый следующий заказ каждого исполнителя открывал бы транзакцию ради
// выдачи, которую тут же отклонит ключ.
func TestOncePerUserIsNotOfferedTwice(t *testing.T) {
	h := newDispatchHarness(t, repository.ExecutorStats{OrdersCompleted: 1}, "500")
	h.confirm(t)

	// Второй заказ того же исполнителя — новое событие, а не переотправка.
	second := *h.order
	second.ID = uuid.New()
	h.orders.orders = append(h.orders.orders, &second)
	if err := h.events.Publish(context.Background(), nil, &repository.DomainEvent{
		Type:        repository.EventOrderConfirmed,
		SubjectType: repository.EventSubjectOrder,
		SubjectID:   second.ID,
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := h.dispatcher.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	if len(h.achievements.granted) != 1 {
		t.Errorf("granted %d times, want exactly 1", len(h.achievements.granted))
	}
	if len(h.achievements.points) != 1 {
		t.Errorf("points credited %d times, want exactly 1", len(h.achievements.points))
	}
}

// Переотправленное событие — обычное дело: воркер повторяет то, что не смог
// доотметить обработанным. Оно не должно начислять баллы второй раз даже тогда,
// когда свод выданного о первой выдаче почему-то не знает.
func TestRedeliveredEventDoesNotGrantTwice(t *testing.T) {
	h := newDispatchHarness(t, repository.ExecutorStats{OrdersCompleted: 1}, "500")
	h.confirm(t)
	h.achievements.hideSummary = true

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

// Порог суммы держит ядро: это единственное место, где он проверяется, и
// заказ ниже него не приносит баллов, что бы ни решил скрипт.
func TestOrderBelowTheFloorEarnsNothing(t *testing.T) {
	h := newDispatchHarness(t, repository.ExecutorStats{OrdersCompleted: 1}, "500")
	h.order.FinalAmount = money.FromRubles(1)
	h.confirm(t)

	if len(h.achievements.granted) != 0 {
		t.Errorf("granted %d achievements for a 1 ruble order, want none", len(h.achievements.granted))
	}
}

// --- Админские кнопки ---------------------------------------------------------

// Пересчёт существует ради одного случая, и он же самый частый: ачивку включили
// после того, как человек выполнил заказы. События по ним давно обработаны и
// второй раз не придут, поэтому единственный способ выдать заслуженное —
// повторить заказы.
func TestRecheckGrantsWhatTheEventNeverSaw(t *testing.T) {
	h := newDispatchHarness(t, repository.ExecutorStats{OrdersCompleted: 1}, "500")

	result, err := h.dispatcher.RecheckUser(context.Background(), h.executorID)
	if err != nil {
		t.Fatalf("recheck: %v", err)
	}
	if result.OrdersReplayed != 1 {
		t.Errorf("replayed %d orders, want 1", result.OrdersReplayed)
	}
	if len(result.Granted) != 1 || result.Granted[0] != "test_award" {
		t.Errorf("granted = %v, want [test_award]", result.Granted)
	}
	if len(h.achievements.granted) != 1 || h.achievements.granted[0].UserID != h.executorID {
		t.Errorf("grants = %+v, want one for the executor", h.achievements.granted)
	}
	if len(h.mail.sent) != 1 {
		t.Errorf("mail = %+v, want one letter", h.mail.sent)
	}
}

// Пересчёт нажимают дважды — потому что не помнят, нажимали ли. Второй раз он
// не должен ничего начислять: ключа идемпотентности события у него нет, и от
// повтора защищает та же уникальность выдачи, что и всегда.
func TestRecheckDoesNotGrantTwice(t *testing.T) {
	h := newDispatchHarness(t, repository.ExecutorStats{OrdersCompleted: 1}, "500")

	for i := 0; i < 2; i++ {
		if _, err := h.dispatcher.RecheckUser(context.Background(), h.executorID); err != nil {
			t.Fatalf("recheck %d: %v", i, err)
		}
	}
	if len(h.achievements.granted) != 1 {
		t.Errorf("granted %d times, want exactly 1", len(h.achievements.granted))
	}
	if len(h.achievements.points) != 1 {
		t.Errorf("points credited %d times, want exactly 1", len(h.achievements.points))
	}
}

// Пересчёт не выдаёт того, что не заслужено: он повторяет правило, а не обходит
// его. Заказ ниже порога не станет ачивкой оттого, что админ нажал кнопку.
func TestRecheckRespectsTheRule(t *testing.T) {
	h := newDispatchHarness(t, repository.ExecutorStats{OrdersCompleted: 1}, "500")
	h.order.FinalAmount = money.FromRubles(1)

	result, err := h.dispatcher.RecheckUser(context.Background(), h.executorID)
	if err != nil {
		t.Fatalf("recheck: %v", err)
	}
	if result.OrdersReplayed != 0 || len(result.Granted) != 0 {
		t.Errorf("result = %+v, want nothing replayed and nothing granted", result)
	}
}

// Выдача вручную правило обходит — в этом её смысл: причина выдать значок
// руками лежит вне того, что скрипт видит в фактах.
func TestManualGrantBypassesTheRule(t *testing.T) {
	h := newDispatchHarness(t, repository.ExecutorStats{}, "500")
	h.dispatcher.engine = engineRefusingEverything(t)

	row, err := h.dispatcher.GrantManually(context.Background(), h.executorID, "test_award", "разобранная жалоба")
	if err != nil {
		t.Fatalf("manual grant: %v", err)
	}
	if row == nil || row.Code != "test_award" {
		t.Fatalf("row = %+v, want the granted achievement", row)
	}
	if len(h.achievements.granted) != 1 || h.achievements.granted[0].Points != 25 {
		t.Errorf("grants = %+v, want one worth the achievement's weight", h.achievements.granted)
	}
	// Письмо приходит и при ручной выдаче: человек узнаёт о значке одинаково,
	// кем бы тот ни был выдан.
	if len(h.mail.sent) != 1 || h.mail.sent[0].Kind != repository.MailKindAchievement {
		t.Errorf("mail = %+v, want one achievement letter", h.mail.sent)
	}

	// Разовая ачивка вторым нажатием не удваивается.
	if _, err := h.dispatcher.GrantManually(context.Background(), h.executorID, "test_award", ""); !errors.Is(err, repository.ErrAchievementAlreadyGranted) {
		t.Errorf("second manual grant returned %v, want ErrAchievementAlreadyGranted", err)
	}
}

// Выключенную ачивку вручную не выдать: выключенная — это правило, которое
// администратор счёл неготовым, а её баллы всё так же снижают комиссию.
func TestManualGrantRefusesDisabledAchievement(t *testing.T) {
	h := newDispatchHarness(t, repository.ExecutorStats{}, "500")
	h.achievements.rows[0].IsActive = false

	if _, err := h.dispatcher.GrantManually(context.Background(), h.executorID, "test_award", ""); !errors.Is(err, ErrAchievementNotGrantable) {
		t.Errorf("granted a disabled achievement: err = %v", err)
	}
	if len(h.achievements.granted) != 0 {
		t.Errorf("grants = %+v, want none", h.achievements.granted)
	}
}

// engineRefusingEverything — та же ачивка, чьё правило не срабатывает никогда.
func engineRefusingEverything(t *testing.T) *achievement.Engine {
	t.Helper()
	engine := achievement.New(achievement.DefaultLimits)
	if err := engine.Compile("test_award", "achievement.star", []byte(`
MANIFEST = {
    "title": "Тестовая ачивка",
    "description": "Условие, которое не выполняется никогда.",
    "audience": "EXECUTOR",
    "events": ["order.confirmed"],
    "once_per_user": True,
    "weight": 25,
}

def check(f):
    return None
`)); err != nil {
		t.Fatalf("compile: %v", err)
	}
	return engine
}

// «Первое покаяние» выдаётся за признание в споре: заказ отменён и не оплачен,
// но ядро пропускает такое событие мимо порогов оплаты. Выдача не
// привязывается к заказу, поэтому отмена заказа её не отзывает.
func TestFirstRepentanceGrantedOnConcession(t *testing.T) {
	h := newDispatchHarness(t, repository.ExecutorStats{}, "500")

	engine := achievement.New(achievement.DefaultLimits)
	if err := engine.Load(achievements.FS, "embedded"); err != nil {
		t.Fatalf("load embedded achievements: %v", err)
	}
	h.dispatcher.engine = engine
	h.achievements.rows = []*repository.Achievement{{Code: "first_repentance", IsActive: true}}

	h.order.Status = repository.OrderStatusCanceled
	h.order.FinalAmount = money.Zero

	for _, eventType := range []string{repository.EventOrderCanceled, repository.EventDisputeConceded} {
		if err := h.events.Publish(context.Background(), nil, &repository.DomainEvent{
			Type: eventType, SubjectType: repository.EventSubjectOrder, SubjectID: h.order.ID, ActorID: &h.executorID,
		}); err != nil {
			t.Fatalf("publish: %v", err)
		}
	}
	if err := h.dispatcher.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	if len(h.achievements.granted) != 1 || h.achievements.granted[0].UserID != h.executorID ||
		h.achievements.granted[0].Code != "first_repentance" {
		t.Fatalf("granted = %+v, want first_repentance to the executor", h.achievements.granted)
	}
	if h.achievements.granted[0].OrderID != nil {
		t.Error("the grant is tied to a canceled order and would be revoked with it")
	}
	if len(h.achievements.points) != 1 || h.achievements.points[0] != 5 {
		t.Errorf("points = %v, want a single 5", h.achievements.points)
	}

	// Признание самому себе ничего не приносит.
	self := newDispatchHarness(t, repository.ExecutorStats{}, "500")
	self.dispatcher.engine = engine
	self.achievements.rows = h.achievements.rows
	self.order.Status = repository.OrderStatusCanceled
	self.order.ExecutorID = &self.order.CustomerID
	if err := self.events.Publish(context.Background(), nil, &repository.DomainEvent{
		Type: repository.EventDisputeConceded, SubjectType: repository.EventSubjectOrder, SubjectID: self.order.ID,
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := self.dispatcher.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if len(self.achievements.granted) != 0 {
		t.Fatalf("self-dealt concession granted %+v", self.achievements.granted)
	}
}

// dbGifts читает каталог из настоящей базы — именно чтение и сканирование
// суммы здесь проверяется, — а выдачу купона подменяет: остальная обвязка
// диспетчера живёт без базы и транзакции у неё нет.
type dbGifts struct {
	repository.GiftRepository
	issued []*repository.UserGift
}

func (g *dbGifts) Issue(ctx context.Context, q repository.Querier, gift *repository.Gift, userID uuid.UUID, achievementID *uuid.UUID) (*repository.UserGift, error) {
	issued := &repository.UserGift{
		ID: uuid.New(), UserID: userID, GiftCode: gift.Code, AchievementID: achievementID,
		CouponCode: "TEST-TEST-TEST", Status: repository.GiftStatusIssued, Gift: gift,
	}
	g.issued = append(g.issued, issued)
	return issued, nil
}

// Денежный подарок на 500 ₽ платит ровно 500 ₽: сумма из каталога доходит до
// проводки без масштаба, не упирается в потолок и не порождает инцидента.
func TestBonusGiftPaysCatalogAmount(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	code := "test_bonus_" + uuid.New().String()[:8]
	defer db.Exec(`DELETE FROM gifts WHERE code = $1`, code)
	want := money.FromRubles(500)
	gifts := &dbGifts{GiftRepository: repository.NewGiftRepository(db)}
	if err := gifts.Upsert(ctx, &repository.Gift{
		Code: code, Kind: repository.GiftKindBonus, Amount: want, IsActive: true,
		Title: map[string]interface{}{"ru": "Бонус"},
	}); err != nil {
		t.Fatalf("upsert gift: %v", err)
	}

	h := newDispatchHarness(t, repository.ExecutorStats{OrdersCompleted: 1}, "500")
	engine := achievement.New(achievement.DefaultLimits)
	if err := engine.Compile("test_award", "achievement.star", []byte(`
MANIFEST = {
    "title": "С бонусом",
    "audience": "EXECUTOR",
    "events": ["order.confirmed"],
    "once_per_user": True,
    "weight": 25,
}

def check(f):
    if f.order == None or f.order.executor_id != f.user.id:
        return None
    return grant(points = 25, order_id = f.order.id, effects = [gift(code = "`+code+`")])
`)); err != nil {
		t.Fatalf("compile: %v", err)
	}
	h.dispatcher.engine = engine
	h.dispatcher.gifts = gifts
	txs := &mockTransactionRepo{}
	h.dispatcher.ledger = NewLedger(txs, newMockAccounts())

	h.confirm(t)

	var paid money.Amount
	for _, tx := range txs.txs {
		if tx.Type == string(repository.TransactionTypeBonus) && tx.UserID == h.executorID {
			paid = paid.Add(tx.Amount)
		}
	}
	if paid != want {
		t.Fatalf("bonus paid = %s, want %s", paid, want)
	}
	if len(h.incidents.recorded) != 0 {
		t.Errorf("incidents = %+v, want none for a gift under the cap", h.incidents.recorded)
	}
	if len(gifts.issued) != 1 {
		t.Fatalf("issued %d gifts, want 1", len(gifts.issued))
	}
	var body string
	for _, m := range h.mail.sent {
		if m.Kind == repository.MailKindGift {
			body = m.Body
		}
	}
	if !strings.Contains(body, want.String()) {
		t.Errorf("gift mail body %q, want it to name %s", body, want)
	}
}
