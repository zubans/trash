package service

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"

	"healthlogin/backend/money"
	"healthlogin/backend/repository"
)

func newResolveFixture(t *testing.T) (*disputeFixture, *repository.Dispute, uuid.UUID) {
	t.Helper()
	f := newDisputeFixture(t)
	f.cleanupPenalties(t)
	f.srv.WithPenalties(newIntegrationPenaltyService(f.db, f.srv))
	dispute, err := f.srv.OpenDispute(context.Background(), f.customerID, f.order.ID, "не вывезли")
	if err != nil {
		t.Fatalf("open dispute: %v", err)
	}
	return f, dispute, seedExecutor(t, f.db)
}

func (f *disputeFixture) balance(t *testing.T, userID uuid.UUID) money.Amount {
	t.Helper()
	var b money.Amount
	if err := f.db.QueryRow(`SELECT balance FROM users WHERE id = $1`, userID).Scan(&b); err != nil {
		t.Fatalf("read balance: %v", err)
	}
	return b
}

func TestResolveDisputeIntegration(t *testing.T) {
	cases := []struct {
		decision       string
		orderStatus    repository.OrderStatus
		customerRefund bool
		executorPaid   bool
		customerPoints int
		executorPoints int
	}{
		{repository.DisputeDecisionExecutor, repository.OrderStatusCompleted, false, true, 1, 0},
		{repository.DisputeDecisionCustomer, repository.OrderStatusCanceled, true, false, 0, 1},
		{repository.DisputeDecisionUnknown, repository.OrderStatusCompleted, true, true, 1, 1},
	}
	for _, c := range cases {
		t.Run(c.decision, func(t *testing.T) {
			f, dispute, arbiterID := newResolveFixture(t)
			ctx := context.Background()
			customerBefore := f.balance(t, f.customerID)

			resolved, err := f.srv.ResolveDispute(ctx, dispute.ID, arbiterID, c.decision, " фото нет ")
			if err != nil {
				t.Fatalf("resolve: %v", err)
			}
			if resolved.Status != repository.DisputeStatusClosed || resolved.Closure != repository.DisputeClosureArbitration ||
				resolved.Decision != c.decision || resolved.ResolutionNote != "фото нет" || resolved.ClosedBy == nil || *resolved.ClosedBy != arbiterID {
				t.Fatalf("resolved dispute: %+v", resolved)
			}
			if got := f.status(t); got != c.orderStatus {
				t.Fatalf("order status %s, want %s", got, c.orderStatus)
			}

			refunded := f.balance(t, f.customerID) != customerBefore
			if refunded != c.customerRefund {
				t.Errorf("customer refunded: %v, want %v", refunded, c.customerRefund)
			}
			paid := f.balance(t, f.executorID).IsPositive()
			if paid != c.executorPaid {
				t.Errorf("executor paid: %v, want %v", paid, c.executorPaid)
			}
			if n := activePoints(t, f.db, f.customerID, repository.RoleCustomer); n != c.customerPoints {
				t.Errorf("customer points %d, want %d", n, c.customerPoints)
			}
			if n := activePoints(t, f.db, f.executorID, repository.RoleExecutor); n != c.executorPoints {
				t.Errorf("executor points %d, want %d", n, c.executorPoints)
			}

			if _, err := f.srv.ResolveDispute(ctx, dispute.ID, arbiterID, c.decision, ""); !errors.Is(err, ErrDisputeClosed) {
				t.Fatalf("second resolution: %v", err)
			}
		})
	}
}

func TestResolveDisputeRejectsBadInputIntegration(t *testing.T) {
	f, dispute, arbiterID := newResolveFixture(t)
	ctx := context.Background()

	if _, err := f.srv.ResolveDispute(ctx, uuid.New(), arbiterID, repository.DisputeDecisionCustomer, ""); !errors.Is(err, ErrDisputeNotFound) {
		t.Fatalf("missing dispute: %v", err)
	}
	if _, err := f.srv.ResolveDispute(ctx, dispute.ID, arbiterID, "BOTH", ""); !errors.Is(err, ErrDisputeDecision) {
		t.Fatalf("bad decision: %v", err)
	}
	if got := f.status(t); got != repository.OrderStatusDisputed {
		t.Fatalf("order status %s after rejected input", got)
	}

	list, err := f.srv.ListDisputes(ctx, repository.DisputeStatusOpen, 200, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var row *repository.AdminDispute
	for i := range list {
		if list[i].ID == dispute.ID {
			row = &list[i]
		}
	}
	if row == nil {
		t.Fatal("open dispute missing from the arbitration queue")
	}
	if row.OrderStatus != string(repository.OrderStatusDisputed) || row.ExecutorDisputesTotal != 1 || row.Claim != "не вывезли" || row.ServiceName == "" {
		t.Fatalf("queue row: %+v", row)
	}
	if _, err := f.srv.ListDisputes(ctx, "WHATEVER", 20, 0); err == nil {
		t.Fatal("unknown status filter accepted")
	}
}

// Заказчик подтверждает заказ в тот же момент, когда арбитр решает в его
// пользу. Проходит ровно одно из двух, и деньги двигаются один раз.
func TestResolveDisputeRacesCustomerConfirmIntegration(t *testing.T) {
	f, dispute, arbiterID := newResolveFixture(t)
	ctx := context.Background()
	customerBefore := f.balance(t, f.customerID)

	var wg sync.WaitGroup
	var confirmErr, resolveErr error
	start := make(chan struct{})
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		confirmErr = f.srv.Confirm(ctx, f.customerID, f.order.ID)
	}()
	go func() {
		defer wg.Done()
		<-start
		_, resolveErr = f.srv.ResolveDispute(ctx, dispute.ID, arbiterID, repository.DisputeDecisionCustomer, "")
	}()
	close(start)
	wg.Wait()

	if (confirmErr == nil) == (resolveErr == nil) {
		t.Fatalf("exactly one must win: confirm=%v resolve=%v", confirmErr, resolveErr)
	}

	status := f.status(t)
	customerAfter := f.balance(t, f.customerID)
	executorAfter := f.balance(t, f.executorID)
	points := activePoints(t, f.db, f.executorID, repository.RoleExecutor)
	if confirmErr == nil {
		if status != repository.OrderStatusCompleted || customerAfter != customerBefore || !executorAfter.IsPositive() || points != 0 {
			t.Fatalf("confirm won, but: status %s customer %s→%s executor %s points %d", status, customerBefore, customerAfter, executorAfter, points)
		}
	} else {
		if status != repository.OrderStatusCanceled || customerAfter == customerBefore || !executorAfter.IsZero() || points != 1 {
			t.Fatalf("resolve won, but: status %s customer %s→%s executor %s points %d", status, customerBefore, customerAfter, executorAfter, points)
		}
	}
}
