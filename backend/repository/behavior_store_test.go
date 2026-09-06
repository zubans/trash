package repository_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/google/uuid"

	"healthlogin/backend/repository"
)

// Хранилища за скриптовыми услугами, на настоящем SQL. Здесь проверяется то,
// что не могут проверить тесты уровня Go: что сама база обеспечивает два
// правила, на которые опирается замысел, — один claim на пользователя и услугу
// и один эффект на ключ идемпотентности.

// seedBehaviorOrder создаёт строки, на которые должны ссылаться claim и событие.
func seedBehaviorOrder(t *testing.T, db *sql.DB) (customerID, variantID, orderID uuid.UUID) {
	t.Helper()
	customerID = createTestUser(t, db, "CUSTOMER")

	variantID = uuid.New()
	_, err := db.Exec(
		`INSERT INTO service_nodes (id, code, name, node_type, base_price, is_active, behavior_code)
		 VALUES ($1, $2, $3::jsonb, 'VARIANT', 0, true, 'verification')`,
		variantID, "behavior-test-"+uuid.New().String()[:8], `{"ru": "Тест"}`,
	)
	if err != nil {
		t.Fatalf("insert variant: %v", err)
	}

	orderID = uuid.New()
	_, err = db.Exec(
		`INSERT INTO orders (id, customer_id, service_variant_id, status, hold_amount, final_amount)
		 VALUES ($1, $2, $3, 'SEARCHING', 0, 0)`,
		orderID, customerID, variantID,
	)
	if err != nil {
		t.Fatalf("insert order: %v", err)
	}
	return customerID, variantID, orderID
}

func TestServiceClaimIsOncePerUserAndVariant(t *testing.T) {
	db := testDB(t)
	defer db.Close()
	ctx := context.Background()

	customerID, variantID, orderID := seedBehaviorOrder(t, db)
	claims := repository.NewServiceClaimRepository(db)

	if err := claims.Claim(ctx, nil, customerID, variantID, orderID); err != nil {
		t.Fatalf("first claim: %v", err)
	}
	// Вторая попытка — это то, как выглядят два одновременных запроса заказа,
	// когда оба уже прошли проверку «а заказывал ли он это?».
	err := claims.Claim(ctx, nil, customerID, variantID, orderID)
	if !errors.Is(err, repository.ErrServiceAlreadyClaimed) {
		t.Fatalf("second claim returned %v, want ErrServiceAlreadyClaimed", err)
	}

	count, err := claims.CountForVariant(ctx, customerID, variantID)
	if err != nil || count != 1 {
		t.Fatalf("claims for variant = %d (%v), want 1", count, err)
	}

	if err := claims.ReleaseByOrder(ctx, nil, orderID); err != nil {
		t.Fatalf("release: %v", err)
	}
	// Отменённый заказ возвращает попытку обратно.
	if err := claims.Claim(ctx, nil, customerID, variantID, orderID); err != nil {
		t.Fatalf("claim after release: %v", err)
	}
}

func TestDomainEventsAreClaimedOnceAndEffectsAreIdempotent(t *testing.T) {
	db := testDB(t)
	defer db.Close()
	ctx := context.Background()

	_, _, orderID := seedBehaviorOrder(t, db)
	events := repository.NewEventRepository(db)

	event := &repository.DomainEvent{
		Type:        repository.EventOrderExecuted,
		SubjectType: repository.EventSubjectOrder,
		SubjectID:   orderID,
	}
	if err := events.Publish(ctx, nil, event); err != nil {
		t.Fatalf("publish: %v", err)
	}

	pending, err := events.ClaimPending(ctx, repository.ConsumerBehaviors, 50, 10)
	if err != nil {
		t.Fatalf("claim pending: %v", err)
	}
	if !containsEvent(pending, event.ID) {
		t.Fatalf("the published event was not returned as pending")
	}

	key := "verification:" + orderID.String()
	if err := events.RecordEffect(ctx, nil, key, event.ID, "verification", "pay_bonus", map[string]interface{}{"amount": 200}); err != nil {
		t.Fatalf("record effect: %v", err)
	}
	// Тот же ключ снова — это переотправленное событие, просящее выплату, которая
	// уже сделана. Он должен быть отвергнут, а не повторён.
	err = events.RecordEffect(ctx, nil, key, event.ID, "verification", "pay_bonus", nil)
	if !errors.Is(err, repository.ErrEffectAlreadyApplied) {
		t.Fatalf("second effect returned %v, want ErrEffectAlreadyApplied", err)
	}

	if err := events.MarkProcessed(ctx, repository.ConsumerBehaviors, event.ID); err != nil {
		t.Fatalf("mark processed: %v", err)
	}
	pending, err = events.ClaimPending(ctx, repository.ConsumerBehaviors, 50, 10)
	if err != nil {
		t.Fatalf("claim pending after processing: %v", err)
	}
	if containsEvent(pending, event.ID) {
		t.Error("a processed event came back as pending")
	}
}

// Событие, которое продолжает падать, должно в итоге перестать занимать пачку,
// иначе оно навсегда блокирует все события за собой.
func TestFailingEventStopsAfterMaxAttempts(t *testing.T) {
	db := testDB(t)
	defer db.Close()
	ctx := context.Background()

	_, _, orderID := seedBehaviorOrder(t, db)
	events := repository.NewEventRepository(db)

	event := &repository.DomainEvent{
		Type:        repository.EventOrderExecuted,
		SubjectType: repository.EventSubjectOrder,
		SubjectID:   orderID,
	}
	if err := events.Publish(ctx, nil, event); err != nil {
		t.Fatalf("publish: %v", err)
	}

	const maxAttempts = 3
	for i := 0; i < maxAttempts; i++ {
		pending, err := events.ClaimPending(ctx, repository.ConsumerBehaviors, 50, maxAttempts)
		if err != nil {
			t.Fatalf("claim pending: %v", err)
		}
		if !containsEvent(pending, event.ID) {
			t.Fatalf("event stopped being retried after %d attempts, want %d", i, maxAttempts)
		}
		if err := events.MarkFailed(ctx, repository.ConsumerBehaviors, event.ID, "boom"); err != nil {
			t.Fatalf("mark failed: %v", err)
		}
	}

	pending, err := events.ClaimPending(ctx, repository.ConsumerBehaviors, 50, maxAttempts)
	if err != nil {
		t.Fatalf("claim pending: %v", err)
	}
	if containsEvent(pending, event.ID) {
		t.Error("an event past its attempt limit is still being retried")
	}
}

func containsEvent(events []*repository.DomainEvent, id uuid.UUID) bool {
	for _, e := range events {
		if e.ID == id {
			return true
		}
	}
	return false
}

func TestSubmissionsNumberTheirAttempts(t *testing.T) {
	db := testDB(t)
	defer db.Close()
	ctx := context.Background()

	customerID, _, orderID := seedBehaviorOrder(t, db)
	executorID := createTestUser(t, db, "EXECUTOR")
	submissions := repository.NewSubmissionRepository(db)

	for want := 1; want <= 3; want++ {
		submission := &repository.OrderSubmission{
			OrderID:    orderID,
			ExecutorID: executorID,
			Matched:    false,
			Fields:     map[string]string{"last_name": "Петров"},
			Mismatches: []string{"last_name"},
		}
		if err := submissions.Record(ctx, nil, submission); err != nil {
			t.Fatalf("record: %v", err)
		}
		// Номер приходит из того же оператора, что пишет строку, поэтому две
		// гоняющиеся отправки не могут обе назваться одной и той же попыткой.
		if submission.Attempt != want {
			t.Errorf("attempt = %d, want %d", submission.Attempt, want)
		}
	}

	stored, err := submissions.ListForOrder(ctx, orderID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(stored) != 3 {
		t.Fatalf("stored %d submissions, want 3", len(stored))
	}
	if stored[0].Fields["last_name"] != "Петров" || len(stored[0].Mismatches) != 1 {
		t.Errorf("submission did not round-trip: %+v", stored[0])
	}
	_ = customerID
}

func TestEscalationIsOpenedOncePerOrder(t *testing.T) {
	db := testDB(t)
	defer db.Close()
	ctx := context.Background()

	_, _, orderID := seedBehaviorOrder(t, db)
	adminID := createTestUser(t, db, "ADMIN")
	submissions := repository.NewSubmissionRepository(db)

	escalation := &repository.BehaviorEscalation{
		OrderID:      orderID,
		BehaviorCode: "verification",
		Reason:       "данные не совпали",
	}
	if err := submissions.Escalate(ctx, nil, escalation); err != nil {
		t.Fatalf("escalate: %v", err)
	}
	// Поведение, спрашивающее дважды, описывает тот же случай, а не второй.
	if err := submissions.Escalate(ctx, nil, &repository.BehaviorEscalation{
		OrderID: orderID, BehaviorCode: "verification", Reason: "снова",
	}); err != nil {
		t.Fatalf("second escalate: %v", err)
	}

	open, err := submissions.ListEscalations(ctx, repository.EscalationOpen, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	forOrder := 0
	for _, e := range open {
		if e.OrderID == orderID {
			forOrder++
		}
	}
	if forOrder != 1 {
		t.Fatalf("%d open escalations for one order, want 1", forOrder)
	}

	if err := submissions.ResolveEscalation(ctx, escalation.ID, adminID); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if has, _ := submissions.HasOpenEscalation(ctx, orderID); has {
		t.Error("the escalation is still open after being resolved")
	}
	// Разрешить дважды — это не второе разрешение.
	if err := submissions.ResolveEscalation(ctx, escalation.ID, adminID); !errors.Is(err, repository.ErrEscalationNotFound) {
		t.Errorf("second resolve returned %v, want ErrEscalationNotFound", err)
	}

	// Заказ можно эскалировать снова позднее: индекс запрещает лишь две открытые
	// эскалации одновременно.
	if err := submissions.Escalate(ctx, nil, &repository.BehaviorEscalation{
		OrderID: orderID, BehaviorCode: "verification", Reason: "новый случай",
	}); err != nil {
		t.Fatalf("escalate after resolving: %v", err)
	}
	if has, _ := submissions.HasOpenEscalation(ctx, orderID); !has {
		t.Error("a new escalation was not opened")
	}
}
