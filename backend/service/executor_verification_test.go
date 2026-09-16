package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/repository"
)

// Заявка исполнителя на верификацию — тот же заказ на услугу верификации, что
// размещает заказчик, только собранный за него: вариант находится по поведению,
// адрес берётся из профиля, недостающие данные дозаполняются.

func (u *verificationUsers) UpdateUserName(ctx context.Context, userID uuid.UUID, lastName, firstName, patronymic string) error {
	user, ok := u.users[userID]
	if !ok {
		return errors.New("not found")
	}
	user.LastName, user.FirstName, user.Patronymic = lastName, firstName, patronymic
	return nil
}

func (u *verificationUsers) UpdateUserBirthDate(ctx context.Context, userID uuid.UUID, birthDate time.Time) error {
	user, ok := u.users[userID]
	if !ok {
		return errors.New("not found")
	}
	user.BirthDate = &birthDate
	return nil
}

func (c *verificationCatalog) GetActiveVariants(ctx context.Context) ([]*repository.ServiceNode, error) {
	if !c.node.IsActive {
		return nil, nil
	}
	return []*repository.ServiceNode{c.node}, nil
}

type verificationAddresses struct {
	repository.AddressRepository
	byUser map[uuid.UUID][]repository.Address
}

func (a *verificationAddresses) List(ctx context.Context, userID uuid.UUID) ([]repository.Address, error) {
	return a.byUser[userID], nil
}

func (a *verificationAddresses) Add(ctx context.Context, userID uuid.UUID, address repository.Address) ([]repository.Address, error) {
	address.UserID = userID
	a.byUser[userID] = append(a.byUser[userID], address)
	return a.byUser[userID], nil
}

type executorVerificationWorld struct {
	*verificationWorld
	addresses *verificationAddresses
	svc       *ExecutorVerificationService
	applicant *repository.User
}

func newExecutorVerificationWorld(t *testing.T) *executorVerificationWorld {
	t.Helper()
	w := newVerificationWorld(t)
	addresses := &verificationAddresses{byUser: map[uuid.UUID][]repository.Address{}}
	return &executorVerificationWorld{
		verificationWorld: w,
		addresses:         addresses,
		svc:               NewExecutorVerificationService(w.users, addresses, w.catalog, w.orders, w.behaviors, w.orderSvc),
		applicant:         w.users.add(repository.RoleExecutor, []string{repository.RoleExecutor}, false),
	}
}

func (w *executorVerificationWorld) giveAddress(userID uuid.UUID) {
	w.addresses.byUser[userID] = []repository.Address{{UserID: userID, Address: "Москва, Арбат, 10", IsDefault: true}}
}

func TestExecutorVerificationCreatesAModeratorOrderWhenDataIsComplete(t *testing.T) {
	w := newExecutorVerificationWorld(t)
	ctx := context.Background()
	w.giveAddress(w.applicant.ID)

	order, err := w.svc.Request(ctx, w.applicant.ID, ExecutorVerificationRequest{})
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if order.ServiceVariantID != verificationVariantID || order.CustomerID != w.applicant.ID {
		t.Errorf("order is not a verification order of the applicant: %+v", order)
	}
	if order.Address == nil || *order.Address != "Москва, Арбат, 10" {
		t.Errorf("order address = %v, want the profile address", order.Address)
	}

	// Дальше это обычный заказ на верификацию: берёт модератор, совпадение данных
	// подтверждает исполнителя.
	if err := w.orderSvc.Accept(ctx, order.ID, w.executor.ID); err == nil {
		t.Error("a plain executor must not take a verification order")
	}
	if err := w.orderSvc.Accept(ctx, order.ID, w.moderator.ID); err != nil {
		t.Fatalf("moderator accept: %v", err)
	}
	if _, err := w.dispatcher.SubmitOrderData(ctx, order.ID, w.moderator.ID, passportOf(w.applicant)); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if !w.applicant.Verified {
		t.Error("the executor was not verified by the moderator's check")
	}
}

func TestExecutorVerificationAsksForMissingAddress(t *testing.T) {
	w := newExecutorVerificationWorld(t)
	ctx := context.Background()

	status, err := w.svc.Status(ctx, w.applicant.ID)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if len(status.Missing) != 1 || status.Missing[0] != VerificationFieldAddress {
		t.Errorf("missing = %v, want only address", status.Missing)
	}

	_, err = w.svc.Request(ctx, w.applicant.ID, ExecutorVerificationRequest{})
	var missing *VerificationDataMissingError
	if !errors.As(err, &missing) || len(missing.Fields) != 1 || missing.Fields[0] != VerificationFieldAddress {
		t.Fatalf("err = %v, want missing address", err)
	}
	if len(w.orders.orders) != 0 {
		t.Error("no order may be created without an address")
	}

	addr := Address{City: "Москва", Street: "Арбат", House: "10"}
	order, err := w.svc.Request(ctx, w.applicant.ID, ExecutorVerificationRequest{Address: &addr})
	if err != nil {
		t.Fatalf("request with address: %v", err)
	}
	if len(w.addresses.byUser[w.applicant.ID]) != 1 {
		t.Error("the filled address was not saved to the profile")
	}
	if order.Address == nil || *order.Address == "" {
		t.Error("order has no address")
	}
}

func TestExecutorVerificationFillsOnlyEmptyIdentityFields(t *testing.T) {
	w := newExecutorVerificationWorld(t)
	ctx := context.Background()
	w.giveAddress(w.applicant.ID)
	originalLast := w.applicant.LastName
	w.applicant.FirstName = ""
	w.applicant.BirthDate = nil

	_, err := w.svc.Request(ctx, w.applicant.ID, ExecutorVerificationRequest{FirstName: "Пётр"})
	var missing *VerificationDataMissingError
	if !errors.As(err, &missing) || len(missing.Fields) != 1 || missing.Fields[0] != VerificationFieldBirthDate {
		t.Fatalf("err = %v, want missing birth_date", err)
	}
	if w.applicant.FirstName != "" {
		t.Error("nothing may be written while the request is incomplete")
	}

	if _, err := w.svc.Request(ctx, w.applicant.ID, ExecutorVerificationRequest{
		LastName: "Подмена", FirstName: "Пётр", BirthDate: "1991-05-02",
	}); err != nil {
		t.Fatalf("request: %v", err)
	}
	if w.applicant.LastName != originalLast {
		t.Errorf("filled last name was overwritten: %q", w.applicant.LastName)
	}
	if w.applicant.FirstName != "Пётр" || w.applicant.BirthDate == nil {
		t.Error("missing identity fields were not saved")
	}
}

func TestExecutorVerificationOnceAndAgainAfterCancel(t *testing.T) {
	w := newExecutorVerificationWorld(t)
	ctx := context.Background()
	w.giveAddress(w.applicant.ID)

	order, err := w.svc.Request(ctx, w.applicant.ID, ExecutorVerificationRequest{})
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if _, err := w.svc.Request(ctx, w.applicant.ID, ExecutorVerificationRequest{}); err == nil {
		t.Error("a second open verification request must be refused")
	}
	status, err := w.svc.Status(ctx, w.applicant.ID)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.Order == nil || status.Order.ID != order.ID {
		t.Errorf("status does not report the open order: %+v", status.Order)
	}

	if err := w.svc.Cancel(ctx, w.applicant.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if status, _ := w.svc.Status(ctx, w.applicant.ID); status.Order != nil {
		t.Error("a cancelled order must not be reported as open")
	}
	if _, err := w.svc.Request(ctx, w.applicant.ID, ExecutorVerificationRequest{}); err != nil {
		t.Errorf("after cancelling the executor must be able to request again: %v", err)
	}
}

func TestExecutorVerificationRefusals(t *testing.T) {
	w := newExecutorVerificationWorld(t)
	ctx := context.Background()
	w.giveAddress(w.applicant.ID)
	w.giveAddress(w.executor.ID)

	if _, err := w.svc.Request(ctx, w.executor.ID, ExecutorVerificationRequest{}); !errors.Is(err, ErrAlreadyVerified) {
		t.Errorf("verified executor: err = %v, want ErrAlreadyVerified", err)
	}

	w.catalog.node.IsActive = false
	if _, err := w.svc.Request(ctx, w.applicant.ID, ExecutorVerificationRequest{}); !errors.Is(err, ErrVerificationUnavailable) {
		t.Errorf("disabled service: err = %v, want ErrVerificationUnavailable", err)
	}
}
