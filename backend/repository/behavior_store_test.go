package repository_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/google/uuid"

	"healthlogin/backend/repository"
)

// The stores behind scripted services, against real SQL. What is checked here is
// what the Go-level tests cannot check: that the database itself enforces the
// two rules the design leans on — one claim per user and service, one effect per
// idempotency key.

// seedBehaviorOrder creates the rows a claim and an event need to point at.
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
	// The second attempt is what two simultaneous order requests look like once
	// they have both passed the "has he ordered it?" check.
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
	// A cancelled order gives the attempt back.
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

	pending, err := events.ClaimPending(ctx, 50, 10)
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
	// The same key again is a redelivered event asking for a payment that has
	// already been made. It must be refused, not repeated.
	err = events.RecordEffect(ctx, nil, key, event.ID, "verification", "pay_bonus", nil)
	if !errors.Is(err, repository.ErrEffectAlreadyApplied) {
		t.Fatalf("second effect returned %v, want ErrEffectAlreadyApplied", err)
	}

	if err := events.MarkProcessed(ctx, event.ID); err != nil {
		t.Fatalf("mark processed: %v", err)
	}
	pending, err = events.ClaimPending(ctx, 50, 10)
	if err != nil {
		t.Fatalf("claim pending after processing: %v", err)
	}
	if containsEvent(pending, event.ID) {
		t.Error("a processed event came back as pending")
	}
}

// An event that keeps failing must eventually stop consuming the batch, or it
// blocks every event behind it forever.
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
		pending, err := events.ClaimPending(ctx, 50, maxAttempts)
		if err != nil {
			t.Fatalf("claim pending: %v", err)
		}
		if !containsEvent(pending, event.ID) {
			t.Fatalf("event stopped being retried after %d attempts, want %d", i, maxAttempts)
		}
		if err := events.MarkFailed(ctx, event.ID, "boom"); err != nil {
			t.Fatalf("mark failed: %v", err)
		}
	}

	pending, err := events.ClaimPending(ctx, 50, maxAttempts)
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
