package service

import (
	"context"
	"slices"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/repository"
)

// Карточка заказа в приложении одна на обе роли. Что в ней показать и какие
// кнопки дать, решается здесь, под того, кто смотрит, — приложение только
// рисует пришедшие counterparty и actions.

// ReviewWindow — сколько после завершения заказа можно оставить отзыв.
const ReviewWindow = 7 * 24 * time.Hour

// customerCancelStatuses — статусы, в которых заказчик может отменить заказ.
var customerCancelStatuses = []repository.OrderStatus{
	repository.OrderStatusSearching,
	repository.OrderStatusAssigned,
}

// orderViewer — тот, для кого собирается ответ.
type orderViewer struct {
	role   string
	userID uuid.UUID
}

func customerViewer(id uuid.UUID) orderViewer {
	return orderViewer{role: repository.RoleCustomer, userID: id}
}

func executorViewer(id uuid.UUID) orderViewer {
	return orderViewer{role: repository.RoleExecutor, userID: id}
}

// presentOrders заполняет заказы и собирает их под смотрящего.
func (s *OrderService) presentOrders(ctx context.Context, viewer orderViewer, orders []*repository.Order) {
	users := s.hydrateServiceVariants(ctx, orders)
	presentFor(viewer, orders, users, time.Now())
}

// presentFor собирает уже заполненные заказы под смотрящего. users — участники
// заказов, загруженные hydrateServiceVariants.
func presentFor(viewer orderViewer, orders []*repository.Order, users map[uuid.UUID]*repository.User, now time.Time) {
	for _, o := range orders {
		if o == nil {
			continue
		}
		o.Counterparty = counterpartyFor(viewer, o, users)
		o.Actions = actionsFor(viewer, o, now)
	}
}

func counterpartyFor(viewer orderViewer, o *repository.Order, users map[uuid.UUID]*repository.User) *repository.OrderParty {
	switch viewer.role {
	case repository.RoleCustomer:
		party := &repository.OrderParty{Role: repository.RoleExecutor}
		if o.ExecutorID != nil {
			if u := users[*o.ExecutorID]; u != nil {
				party.Name = shortDisplayName(u)
				party.Phone = u.Phone
			}
		}
		return party
	case repository.RoleExecutor:
		party := &repository.OrderParty{Role: repository.RoleCustomer}
		// В заказе со сверкой личности имя заказчика — то, что исполнитель должен
		// прочитать с документа, а не получить готовым.
		if len(o.SubmitFields) > 0 {
			party.Hidden = true
			return party
		}
		if u := users[o.CustomerID]; u != nil {
			party.Name = shortDisplayName(u)
		}
		return party
	}
	return nil
}

func actionsFor(viewer orderViewer, o *repository.Order, now time.Time) *repository.OrderActions {
	isCustomer := viewer.role == repository.RoleCustomer && o.CustomerID == viewer.userID
	isExecutor := viewer.role == repository.RoleExecutor && executorOf(o, viewer.userID)
	return &repository.OrderActions{
		Cancel: isCustomer && slices.Contains(customerCancelStatuses, o.Status),
		Reject: isExecutor && executorCanReject(o, viewer.userID),
		Review: (isCustomer || isExecutor) && o.ExecutorID != nil && reviewOpen(o, now),
	}
}

func executorOf(o *repository.Order, executorID uuid.UUID) bool {
	return o.ExecutorID != nil && *o.ExecutorID == executorID
}

// executorCanReject — исполнитель может отказаться только от своего
// назначенного, ещё не исполненного заказа.
func executorCanReject(o *repository.Order, executorID uuid.UUID) bool {
	return o.Status == repository.OrderStatusAssigned && executorOf(o, executorID)
}

func reviewOpen(o *repository.Order, now time.Time) bool {
	if o.Status != repository.OrderStatusCompleted {
		return false
	}
	return o.CompletedAt == nil || now.Sub(*o.CompletedAt) <= ReviewWindow
}
