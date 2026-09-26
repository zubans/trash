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

// Аукцион открыт только по заказу заказчика с подтверждённой личностью. Одно
// правило действует во всех точках: заказ неподтверждённого заказчика не виден
// в списке аукционов, ставку по нему нельзя подать, принять уже поданную тоже
// нельзя, и удержание с заказчика при этом не берётся. Как только заказчик
// подтверждён, заказ появляется в списке, ставка подаётся и принимается.
func TestAuctionRequiresVerifiedCustomer(t *testing.T) {
	ctx := context.Background()
	birth := time.Now().AddDate(-30, 0, 0)
	customer := &repository.User{ID: uuid.New(), Role: repository.RoleCustomer, Status: repository.UserStatusActive, Verified: false, BirthDate: &birth}
	executor := &repository.User{ID: uuid.New(), Role: repository.RoleExecutor, Status: repository.UserStatusActive, Verified: true, BirthDate: &birth}
	users := newMockUserRepo()
	users.users = map[uuid.UUID]*repository.User{customer.ID: customer, executor.ID: executor}

	order := &repository.Order{
		ID: uuid.New(), CustomerID: customer.ID, ServiceVariantID: constructionVariantID, Status: repository.OrderStatusSearching,
	}
	orderRepo := &mockOrderRepo{orders: []*repository.Order{order}}
	shiftRepo := &mockShiftRepo{shifts: []*repository.Shift{{
		ID: uuid.New(), ExecutorID: executor.ID, Status: repository.ShiftStatusActive, PlannedEndAt: time.Now().Add(time.Hour),
	}}}
	txRepo := &mockTransactionRepo{}
	ledger := NewLedger(txRepo, newMockAccounts())
	bidRepo := &mockBidRepo{}
	orders := NewOrderService(orderRepo, ledger, nil, users, &orderMockShiftRepo{}, nil, newMockCatalogRepo(), nil)
	bids := NewBidService(bidRepo, orderRepo, shiftRepo, ledger, users, newMockCatalogRepo(), nil)

	listed := func() bool {
		t.Helper()
		list, err := orders.GetAvailableConstructionOrdersForExecutor(ctx, executor.ID)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		for _, o := range list {
			if o.ID == order.ID {
				return true
			}
		}
		return false
	}

	// Заказчик не подтверждён.
	if listed() {
		t.Error("an unverified customer's auction is listed")
	}
	if _, err := bids.CreateBid(ctx, order.ID, executor.ID, money.FromRubles(500)); !errors.Is(err, ErrAuctionCustomerNotVerified) {
		t.Errorf("bid on an unverified customer's auction: got %v, want ErrAuctionCustomerNotVerified", err)
	}
	// Ставку, поданную до того, как правило вступило в силу, принять тоже нельзя.
	stale, err := bidRepo.CreateBid(ctx, order.ID, executor.ID, money.FromRubles(500))
	if err != nil {
		t.Fatalf("seed bid: %v", err)
	}
	before, _ := txRepo.GetBalance(ctx, customer.ID)
	if err := bids.AcceptBid(ctx, stale.ID, customer.ID); !errors.Is(err, ErrAuctionCustomerNotVerified) {
		t.Errorf("accept on an unverified customer's auction: got %v, want ErrAuctionCustomerNotVerified", err)
	}
	if stale.Status != "PENDING" {
		t.Errorf("refused accept changed the bid status to %s", stale.Status)
	}
	if after, _ := txRepo.GetBalance(ctx, customer.ID); after != before {
		t.Errorf("refused accept moved money: balance %s → %s", before, after)
	}

	// Заказчик подтвердил личность: заказ виден, ставка подаётся и принимается.
	customer.Verified = true
	if !listed() {
		t.Error("a verified customer's auction is not listed")
	}
	bid, err := bids.CreateBid(ctx, order.ID, executor.ID, money.FromRubles(400))
	if err != nil {
		t.Fatalf("bid on a verified customer's auction: %v", err)
	}
	if err := bids.AcceptBid(ctx, bid.ID, customer.ID); err != nil {
		t.Fatalf("accept on a verified customer's auction: %v", err)
	}
	if after, _ := txRepo.GetBalance(ctx, customer.ID); after != before.Sub(money.FromRubles(400)) {
		t.Errorf("accepted bid must hold the price: balance %s → %s", before, after)
	}
}

// Создавать аукцион неподтверждённому заказчику незачем: его заказ никто не
// увидит. Отказ приходит при создании, заказ не записывается.
func TestConstructionOrderRequiresVerifiedCustomer(t *testing.T) {
	ctx := context.Background()
	customer := &repository.User{ID: uuid.New(), Role: repository.RoleCustomer, Status: repository.UserStatusActive, Verified: false}
	users := newMockUserRepo()
	users.users = map[uuid.UUID]*repository.User{customer.ID: customer}
	orderRepo := &mockOrderRepo{}
	srv := NewOrderService(orderRepo, testLedger(), nil, users, &orderMockShiftRepo{}, nil, newMockCatalogRepo(), nil)

	_, err := srv.CreateConstructionOrder(ctx, customer.ID, "/uploads/chat/photo.jpg", "55.75,37.61", "", nil, nil)
	if !errors.Is(err, ErrCustomerNotEligible) {
		t.Fatalf("unverified customer created an auction: got %v, want ErrCustomerNotEligible", err)
	}
	if len(orderRepo.orders) != 0 {
		t.Fatalf("refused auction left %d order(s) behind", len(orderRepo.orders))
	}

	customer.Verified = true
	if _, err := srv.CreateConstructionOrder(ctx, customer.ID, "/uploads/chat/photo.jpg", "55.75,37.61", "", nil, nil); err != nil {
		t.Fatalf("verified customer could not create an auction: %v", err)
	}
}

// Правило касается только аукционов: заказ неподтверждённого заказчика по
// обычной услуге по-прежнему виден и доступен исполнителю. Аукцион без строки
// заказчика — отказ: подтверждённость без неё не установить.
func TestAuctionVerificationRuleScope(t *testing.T) {
	ctx := context.Background()
	birth := time.Now().AddDate(-30, 0, 0)
	executor := &repository.User{ID: uuid.New(), Role: repository.RoleExecutor, Status: repository.UserStatusActive, Verified: true, BirthDate: &birth}
	unverified := &repository.User{ID: uuid.New(), Role: repository.RoleCustomer, Status: repository.UserStatusActive, Verified: false}
	regular := &repository.ServiceNode{ID: uuid.New(), NodeType: repository.ServiceNodeTypeVariant, IsActive: true}
	auction := &repository.ServiceNode{ID: uuid.New(), NodeType: repository.ServiceNodeTypeVariant, IsActive: true, IsAuction: true}

	if err := canViewOrTakeOrder(ctx, nil, false, executor, unverified, regular); err != nil {
		t.Errorf("regular order of an unverified customer must stay available, got %v", err)
	}
	if err := canViewOrTakeOrder(ctx, nil, false, executor, unverified, auction); !errors.Is(err, ErrAuctionCustomerNotVerified) {
		t.Errorf("auction of an unverified customer: got %v, want ErrAuctionCustomerNotVerified", err)
	}
	if err := canViewOrTakeOrder(ctx, nil, false, executor, nil, auction); !errors.Is(err, ErrAuctionCustomerNotVerified) {
		t.Errorf("auction without a customer row: got %v, want ErrAuctionCustomerNotVerified", err)
	}
}
