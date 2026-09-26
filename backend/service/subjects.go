package service

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"

	"healthlogin/backend/repository"
)

// subjectLoader читает субъектов доменного события — заказ и людей — для
// диспетчеров outbox. Раньше каждый диспетчер держал свою копию «найти, а
// если строки нет — это не ошибка, событие просто не о ком».
type subjectLoader struct {
	orders repository.OrderRepository
	users  repository.UserRepository
}

// order отдаёт заказ или nil, когда его нет: событие о заказе, который успели
// удалить, не о ком, и это не сбой.
func (l subjectLoader) order(ctx context.Context, id uuid.UUID) (*repository.Order, error) {
	order, err := l.orders.FindByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, repository.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return order, nil
}

// user — то же для пользователя.
func (l subjectLoader) user(ctx context.Context, id uuid.UUID) (*repository.User, error) {
	if l.users == nil {
		return nil, nil
	}
	user, err := l.users.FindByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, repository.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return user, nil
}

// requireUser — пользователь, без которого событие обработать нельзя:
// сторона заказа, о котором оно.
func (l subjectLoader) requireUser(ctx context.Context, id uuid.UUID) (*repository.User, error) {
	user, err := l.user(ctx, id)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, ErrUserNotFound
	}
	return user, nil
}

// usersByID грузит набор пользователей разом — для пересчёта по истории, где
// одного и того же заказчика иначе читали бы на каждый его заказ.
func (l subjectLoader) usersByID(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]*repository.User, error) {
	if l.users == nil || len(ids) == 0 {
		return map[uuid.UUID]*repository.User{}, nil
	}
	return l.users.FindByIDs(ctx, ids)
}
