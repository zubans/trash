package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"healthlogin/backend/repository"
)

// Вернуть в работу можно только заказ на проверке: исполнитель отметил
// «Исполнил», заказчик ещё не подтвердил. После возврата заказ снова у того же
// исполнителя, отметка снята, и её можно поставить заново.
func TestReturnToWorkOnlyFromReview(t *testing.T) {
	ctx := context.Background()
	orderRepo := &mockOrderRepo{}
	srv := NewOrderService(orderRepo, NewLedger(&mockTransactionRepo{}, newMockAccounts()), nil, newMockUserRepo(), &orderMockShiftRepo{}, nil, newMockCatalogRepo(), nil)
	custID, execID, adminID := uuid.New(), uuid.New(), uuid.New()

	order, err := srv.CreateOrder(ctx, custID, standardVariantID, false, false, "", nil, nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := srv.ReturnToWork(ctx, order.ID, adminID); !errors.Is(err, ErrOrderNotOnReview) {
		t.Errorf("searching order: err = %v, want ErrOrderNotOnReview", err)
	}

	if err := srv.Accept(ctx, order.ID, execID); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if err := srv.ReturnToWork(ctx, order.ID, adminID); !errors.Is(err, ErrOrderNotOnReview) {
		t.Errorf("assigned order: err = %v, want ErrOrderNotOnReview", err)
	}

	if err := srv.ExecuteOrder(ctx, order.ID, execID); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if err := srv.ReturnToWork(ctx, order.ID, adminID); err != nil {
		t.Fatalf("return to work: %v", err)
	}
	if order.Status != repository.OrderStatusAssigned || order.ExecutedAt != nil {
		t.Errorf("after return: status = %s, executed_at = %v; want ASSIGNED and no mark", order.Status, order.ExecutedAt)
	}
	if order.ExecutorID == nil || *order.ExecutorID != execID {
		t.Error("the order must stay with the same executor")
	}

	if err := srv.ExecuteOrder(ctx, order.ID, execID); err != nil {
		t.Errorf("the executor must be able to mark it executed again: %v", err)
	}

	if err := srv.ConfirmOrder(ctx, order.ID); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if err := srv.ReturnToWork(ctx, order.ID, adminID); !errors.Is(err, ErrOrderNotOnReview) {
		t.Errorf("completed order: err = %v, want ErrOrderNotOnReview", err)
	}
}
