package repository

import (
	"context"

	"github.com/google/uuid"
)

// ListSubmissionsForOrder открывает тестам отправки по одному заказу: в
// интерфейсе этого метода больше нет, а проверить, что записанное читается
// обратно, всё ещё нужно.
func ListSubmissionsForOrder(ctx context.Context, repo SubmissionRepository, orderID uuid.UUID) ([]*OrderSubmission, error) {
	byOrder, err := repo.(*submissionRepo).listForOrders(ctx, []uuid.UUID{orderID})
	if err != nil {
		return nil, err
	}
	return byOrder[orderID], nil
}

// Тексты горячих запросов — для проверки планов через EXPLAIN.
const (
	FindByPhoneSQL    = findByPhoneSQL
	FindByEmailSQL    = findByEmailSQL
	UnreadOrderIDsSQL = unreadOrderIDsSQL
)
