package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/repository"
)

// Список открытых аукционов фильтруется тем же предикатом, что и ставка:
// исполнитель, чью ставку отклонят, заказа не видит. Раньше список проверял
// только верификацию и возраст, и забаненный исполнитель или сам заказчик
// видели заказ, по которому ставку затем отклоняли.
func TestConstructionListHidesOrdersTheExecutorCannotBidOn(t *testing.T) {
	ctx := context.Background()
	birth := time.Now().AddDate(-30, 0, 0)
	customer := &repository.User{ID: uuid.New(), Role: repository.RoleCustomer, Status: repository.UserStatusActive, Verified: true, BirthDate: &birth}
	free := &repository.User{ID: uuid.New(), Role: repository.RoleExecutor, Status: repository.UserStatusActive, Verified: true, BirthDate: &birth}
	banned := &repository.User{ID: uuid.New(), Role: repository.RoleExecutor, Status: repository.UserStatusBanned, Verified: true, BirthDate: &birth}
	users := newMockUserRepo()
	users.users = map[uuid.UUID]*repository.User{customer.ID: customer, free.ID: free, banned.ID: banned}

	orderRepo := &mockOrderRepo{orders: []*repository.Order{{
		ID: uuid.New(), CustomerID: customer.ID, ServiceVariantID: constructionVariantID, Status: repository.OrderStatusSearching,
	}}}
	orders := NewOrderService(orderRepo, testLedger(), nil, users, &orderMockShiftRepo{}, nil, newMockCatalogRepo(), nil)
	bids := NewBidService(&mockBidRepo{}, orderRepo, &mockShiftRepo{}, testLedger(), users, newMockCatalogRepo(), nil)
	orderID := orderRepo.orders[0].ID

	sees := func(executorID uuid.UUID) bool {
		t.Helper()
		list, err := orders.GetAvailableConstructionOrdersForExecutor(ctx, executorID)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		for _, o := range list {
			if o.ID == orderID {
				return true
			}
		}
		return false
	}
	canBid := func(executorID uuid.UUID) bool {
		t.Helper()
		shiftRepo := &mockShiftRepo{}
		_, _ = shiftRepo.StartShift(ctx, executorID, 1)
		bids.shiftRepo = shiftRepo
		_, err := bids.CreateBid(ctx, orderID, executorID, 1000)
		return err == nil
	}

	if !sees(free.ID) || !canBid(free.ID) {
		t.Fatal("an eligible executor must both see the auction and be able to bid")
	}
	if sees(banned.ID) {
		t.Error("a banned executor sees an auction he cannot bid on")
	}
	if canBid(banned.ID) {
		t.Error("a banned executor placed a bid")
	}
	// Собственный заказ: ставку отклоняют, значит, и в списке его нет.
	if sees(customer.ID) {
		t.Error("the customer sees his own auction in the executor list")
	}
}
