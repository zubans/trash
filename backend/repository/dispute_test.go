package repository_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"healthlogin/backend/repository"
)

// Спор закрывается один раз, решение бывает только у арбитража, открытый спор
// на заказ — один.
func TestDisputeRepository_Lifecycle(t *testing.T) {
	db := testDB(t)
	defer db.Close()
	repo := repository.NewDisputeRepository(db)
	ctx := context.Background()

	customerID := createTestUser(t, db, "CUSTOMER")
	executorID := createTestUser(t, db, "EXECUTOR")
	var variantID uuid.UUID
	if err := db.QueryRow(`SELECT id FROM service_nodes WHERE node_type = 'VARIANT' LIMIT 1`).Scan(&variantID); err != nil {
		t.Fatalf("variant: %v", err)
	}
	orderID := uuid.New()
	if _, err := db.Exec(`INSERT INTO orders (id, customer_id, executor_id, service_variant_id, status)
		VALUES ($1, $2, $3, $4, 'DISPUTED')`, orderID, customerID, executorID, variantID); err != nil {
		t.Fatalf("order: %v", err)
	}

	d := &repository.Dispute{OrderID: orderID, CustomerID: customerID, ExecutorID: executorID, Claim: "не вывезли"}
	if err := repo.Open(ctx, nil, d); err != nil {
		t.Fatalf("open: %v", err)
	}
	if d.ID == uuid.Nil || d.Status != repository.DisputeStatusOpen {
		t.Fatalf("opened: %+v", d)
	}
	if err := repo.Open(ctx, nil, &repository.Dispute{OrderID: orderID, CustomerID: customerID, ExecutorID: executorID, Claim: "ещё"}); !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("second open dispute: %v", err)
	}

	open, err := repo.FindOpenByOrder(ctx, nil, orderID)
	if err != nil || open == nil || open.ID != d.ID {
		t.Fatalf("find open: %v %+v", err, open)
	}

	// Решение без арбитража база не примет.
	if err := repo.Close(ctx, nil, d.ID, repository.DisputeClosing{
		Closure: repository.DisputeClosureCustomerConfirmed, Decision: repository.DisputeDecisionCustomer,
	}); err == nil {
		t.Fatal("decision accepted without arbitration")
	}

	if err := repo.Close(ctx, nil, d.ID, repository.DisputeClosing{
		Closure: repository.DisputeClosureArbitration, Decision: repository.DisputeDecisionUnknown,
		Note: "фото нет ни у кого", ClosedBy: &customerID,
	}); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := repo.Close(ctx, nil, d.ID, repository.DisputeClosing{Closure: repository.DisputeClosureExecutorConceded}); !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("second close: %v", err)
	}
	if open, err := repo.FindOpenByOrder(ctx, nil, orderID); err != nil || open != nil {
		t.Fatalf("closed dispute still open: %v %+v", err, open)
	}

	// После закрытого спора можно открыть новый.
	if err := repo.Open(ctx, nil, &repository.Dispute{OrderID: orderID, CustomerID: customerID, ExecutorID: executorID, Claim: "снова"}); err != nil {
		t.Fatalf("reopen after close: %v", err)
	}
}
