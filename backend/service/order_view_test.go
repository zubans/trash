package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/repository"
)

func TestActionsFor(t *testing.T) {
	customerID, executorID, otherID := uuid.New(), uuid.New(), uuid.New()
	now := time.Now()
	recent := now.Add(-time.Hour)
	stale := now.Add(-ReviewWindow - time.Hour)
	order := func(status repository.OrderStatus, completedAt *time.Time) *OrderView {
		return &OrderView{Order: repository.Order{CustomerID: customerID, ExecutorID: &executorID, Status: status, CompletedAt: completedAt}}
	}
	searching := &OrderView{Order: repository.Order{CustomerID: customerID, Status: repository.OrderStatusSearching}}
	scripted := order(repository.OrderStatusExecuted, nil)
	scripted.ServiceVariant = &repository.ServiceNode{BehaviorCode: "verification"}
	scriptExecuted := order(repository.OrderStatusAssigned, nil)
	scriptExecuted.ScriptExecuted = true

	cases := []struct {
		name   string
		viewer orderViewer
		order  *OrderView
		want   OrderActions
	}{
		{"customer cancels a searching order", customerViewer(customerID), searching, OrderActions{Cancel: true}},
		{"customer cancels an assigned order", customerViewer(customerID), order(repository.OrderStatusAssigned, nil), OrderActions{Cancel: true}},
		{"customer disputes, but cannot cancel, an executed order", customerViewer(customerID), order(repository.OrderStatusExecuted, nil), OrderActions{Dispute: true}},
		{"customer cannot dispute an assigned order", customerViewer(customerID), order(repository.OrderStatusAssigned, nil), OrderActions{Cancel: true}},
		{"customer cannot dispute twice", customerViewer(customerID), order(repository.OrderStatusDisputed, nil), OrderActions{}},
		{"another customer cannot dispute", customerViewer(otherID), order(repository.OrderStatusExecuted, nil), OrderActions{}},
		{"scripted service is not disputed", customerViewer(customerID), scripted, OrderActions{}},
		{"executor concedes a disputed order", executorViewer(executorID), order(repository.OrderStatusDisputed, nil), OrderActions{Concede: true}},
		{"another executor cannot concede", executorViewer(otherID), order(repository.OrderStatusDisputed, nil), OrderActions{}},
		{"executor cannot concede an undisputed order", executorViewer(executorID), order(repository.OrderStatusExecuted, nil), OrderActions{}},
		{"another customer gets nothing", customerViewer(otherID), order(repository.OrderStatusAssigned, nil), OrderActions{}},
		{"executor rejects or executes their assigned order", executorViewer(executorID), order(repository.OrderStatusAssigned, nil), OrderActions{Reject: true, Execute: true}},
		{"executor cannot execute an order its script closes", executorViewer(executorID), scriptExecuted, OrderActions{Reject: true}},
		{"executor cannot reject someone else's order", executorViewer(otherID), order(repository.OrderStatusAssigned, nil), OrderActions{}},
		{"executor cannot reject an executed order", executorViewer(executorID), order(repository.OrderStatusExecuted, nil), OrderActions{}},
		{"executor cannot act on a searching order", executorViewer(executorID), searching, OrderActions{}},
		{"customer reviews a recent completed order", customerViewer(customerID), order(repository.OrderStatusCompleted, &recent), OrderActions{Review: true}},
		{"executor reviews a recent completed order", executorViewer(executorID), order(repository.OrderStatusCompleted, &recent), OrderActions{Review: true}},
		{"review window closes", customerViewer(customerID), order(repository.OrderStatusCompleted, &stale), OrderActions{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := actionsFor(c.viewer, c.order, now); *got != c.want {
				t.Errorf("actions = %+v, want %+v", *got, c.want)
			}
		})
	}
}

func TestCounterpartyFor(t *testing.T) {
	users := &verificationUsers{users: map[uuid.UUID]*repository.User{}}
	customer := users.add(repository.RoleCustomer, nil, false)
	executor := users.add(repository.RoleExecutor, []string{repository.RoleExecutor}, true)
	assigned := &OrderView{Order: repository.Order{CustomerID: customer.ID, ExecutorID: &executor.ID}}

	t.Run("customer sees the executor", func(t *testing.T) {
		got := counterpartyFor(customerViewer(customer.ID), assigned, users.users)
		want := OrderParty{Role: repository.RoleExecutor, Name: shortDisplayName(executor), Phone: executor.Phone}
		if *got != want {
			t.Errorf("counterparty = %+v, want %+v", *got, want)
		}
	})
	t.Run("customer of an unassigned order sees nobody yet", func(t *testing.T) {
		got := counterpartyFor(customerViewer(customer.ID), &OrderView{Order: repository.Order{CustomerID: customer.ID}}, users.users)
		if want := (OrderParty{Role: repository.RoleExecutor}); *got != want {
			t.Errorf("counterparty = %+v, want %+v", *got, want)
		}
	})
	t.Run("executor sees the customer's name but not the phone", func(t *testing.T) {
		got := counterpartyFor(executorViewer(executor.ID), assigned, users.users)
		if want := (OrderParty{Role: repository.RoleCustomer, Name: shortDisplayName(customer)}); *got != want {
			t.Errorf("counterparty = %+v, want %+v", *got, want)
		}
	})
	t.Run("identity check hides the customer", func(t *testing.T) {
		check := &OrderView{Order: repository.Order{CustomerID: customer.ID, ExecutorID: &executor.ID}, SubmitFields: []string{"last_name"}}
		got := counterpartyFor(executorViewer(executor.ID), check, users.users)
		if want := (OrderParty{Role: repository.RoleCustomer, Hidden: true}); *got != want {
			t.Errorf("counterparty = %+v, want %+v", *got, want)
		}
	})
}

// Лента исполнителя приходит собранной под него: заказчик во второй стороне и
// отказ среди действий.
func TestExecutorListPresentsCustomer(t *testing.T) {
	users := &verificationUsers{users: map[uuid.UUID]*repository.User{}}
	customer := users.add(repository.RoleCustomer, nil, false)
	executor := users.add(repository.RoleExecutor, []string{repository.RoleExecutor}, true)
	svc := &OrderService{userRepo: users}

	order := &repository.Order{ID: uuid.New(), CustomerID: customer.ID, ExecutorID: &executor.ID, Status: repository.OrderStatusAssigned}
	views := svc.presentOrders(context.Background(), executorViewer(executor.ID), []*repository.Order{order})
	if len(views) != 1 {
		t.Fatalf("views = %d, want 1", len(views))
	}
	if p := views[0].Counterparty; p == nil || p.Role != repository.RoleCustomer || p.Name != shortDisplayName(customer) {
		t.Errorf("counterparty = %+v, want the customer", p)
	}
	if a := views[0].Actions; a == nil || !a.Reject || a.Cancel {
		t.Errorf("actions = %+v, want reject only", a)
	}
}
