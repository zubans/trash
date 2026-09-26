package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/money"
	"healthlogin/backend/repository"
)

type mockBidRepo struct {
	bids []*repository.Bid
}

func (m *mockBidRepo) CreateBid(ctx context.Context, orderID, executorID uuid.UUID, offeredPrice money.Amount) (*repository.Bid, error) {
	b := &repository.Bid{
		ID:           uuid.New(),
		OrderID:      orderID,
		ExecutorID:   executorID,
		OfferedPrice: offeredPrice,
		Status:       "PENDING",
		CreatedAt:    time.Now(),
	}
	m.bids = append(m.bids, b)
	return b, nil
}

func (m *mockBidRepo) GetBidsForOrder(ctx context.Context, orderID uuid.UUID) ([]*repository.Bid, error) {
	var list []*repository.Bid
	for _, b := range m.bids {
		if b.OrderID == orderID {
			list = append(list, b)
		}
	}
	return list, nil
}

func (m *mockBidRepo) LockBidForUpdate(ctx context.Context, q repository.Querier, bidID uuid.UUID) (*repository.Bid, error) {
	for _, b := range m.bids {
		if b.ID == bidID {
			return b, nil
		}
	}
	return nil, errors.New("bid not found")
}

func (m *mockBidRepo) SetBidStatus(ctx context.Context, q repository.Querier, bidID uuid.UUID, status string) error {
	for _, b := range m.bids {
		if b.ID == bidID {
			// Повторяет охраняемый UPDATE: решить можно только ожидающую ставку.
			if b.Status != "PENDING" {
				return repository.ErrConflict
			}
			b.Status = status
			return nil
		}
	}
	return repository.ErrConflict
}

func (m *mockBidRepo) RejectOtherBids(ctx context.Context, q repository.Querier, orderID, exceptBidID uuid.UUID) error {
	for _, b := range m.bids {
		if b.OrderID == orderID && b.ID != exceptBidID && b.Status == "PENDING" {
			b.Status = "REJECTED"
		}
	}
	return nil
}

func TestBidService_CreateBid(t *testing.T) {
	bidRepo := &mockBidRepo{}
	shiftRepo := &mockShiftRepo{}
	orderRepo := &mockOrderRepo{}
	catalogRepo := newMockCatalogRepo()
	userRepo := newMockUserRepo()
	srv := NewBidService(bidRepo, orderRepo, shiftRepo, testLedger(), userRepo, catalogRepo, nil)

	executorID := uuid.New()

	// Заказ обязан существовать: подача ставки теперь перепроверяет заказ и исполнителя.
	order := &repository.Order{
		ID:               uuid.New(),
		CustomerID:       uuid.New(),
		ServiceVariantID: constructionVariantID,
		Status:           repository.OrderStatusSearching,
	}
	orderRepo.orders = append(orderRepo.orders, order)
	orderID := order.ID

	// Случай 1: нет активной смены (должно упасть)
	_, err := srv.CreateBid(context.Background(), orderID, executorID, money.FromRubles(350.00))
	if err == nil {
		t.Error("expected error placing bid without active shift")
	}

	// Случай 2: активная смена (должно пройти)
	_, _ = shiftRepo.StartShift(context.Background(), executorID, 1) // Стартуем активную смену в подделке
	bid, err := srv.CreateBid(context.Background(), orderID, executorID, money.FromRubles(350.00))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if bid.OfferedPrice != money.FromRubles(350) {
		t.Errorf("expected price 350.00, got %s", bid.OfferedPrice)
	}

	// Случай 3: недопустимая цена (должно упасть)
	_, err = srv.CreateBid(context.Background(), orderID, executorID, money.FromRubles(-10.0))
	if err == nil {
		t.Error("expected error placing bid with negative price")
	}

	// Случай 4: ставка по собственному заказу отклоняется.
	_, err = srv.CreateBid(context.Background(), orderID, order.CustomerID, 350.00)
	if err == nil {
		t.Error("expected error placing bid on own order")
	}
}

// Правило «ставки только по открытому аукциону» живёт в сервисе, а не в
// репозитории: закрытый заказ и неаукционная услуга отказывают своими
// сентинелами до записи.
func TestBidService_CreateBidRequiresOpenAuction(t *testing.T) {
	bidRepo := &mockBidRepo{}
	shiftRepo := &mockShiftRepo{}
	orderRepo := &mockOrderRepo{}
	srv := NewBidService(bidRepo, orderRepo, shiftRepo, testLedger(), newMockUserRepo(), newMockCatalogRepo(), nil)
	executorID := uuid.New()
	_, _ = shiftRepo.StartShift(context.Background(), executorID, 1)

	assigned := &repository.Order{ID: uuid.New(), CustomerID: uuid.New(),
		ServiceVariantID: constructionVariantID, Status: repository.OrderStatusAssigned}
	plain := &repository.Order{ID: uuid.New(), CustomerID: uuid.New(),
		ServiceVariantID: standardVariantID, Status: repository.OrderStatusSearching}
	orderRepo.orders = append(orderRepo.orders, assigned, plain)

	if _, err := srv.CreateBid(context.Background(), assigned.ID, executorID, money.FromRubles(100)); !errors.Is(err, ErrOrderNotBiddable) {
		t.Errorf("bid on an assigned order: %v, want ErrOrderNotBiddable", err)
	}
	if _, err := srv.CreateBid(context.Background(), plain.ID, executorID, money.FromRubles(100)); !errors.Is(err, ErrNotAuction) {
		t.Errorf("bid on a non-auction order: %v, want ErrNotAuction", err)
	}
	if _, err := srv.CreateBid(context.Background(), uuid.New(), executorID, money.FromRubles(100)); !errors.Is(err, ErrOrderNotFound) {
		t.Errorf("bid on a missing order: %v, want ErrOrderNotFound", err)
	}
}
