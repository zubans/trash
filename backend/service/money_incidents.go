package service

import (
	"context"
	"log"
	"strings"

	"github.com/google/uuid"

	"healthlogin/backend/repository"
)

// MoneyIncidents — разбор денежных инцидентов администратором. Инциденты
// живут рядом с ачивками, но существуют сами по себе: они охраняют
// распределение заказа и нужны, даже когда ни одна ачивка не включена.
type MoneyIncidents struct {
	repo repository.MoneyIncidentRepository
}

// NewMoneyIncidents создаёт MoneyIncidents.
func NewMoneyIncidents(repo repository.MoneyIncidentRepository) *MoneyIncidents {
	return &MoneyIncidents{repo: repo}
}

// defaultIncidentPage — прежний потолок списка; теперь размер страницы по умолчанию.
const defaultIncidentPage = 200

// ErrResolutionRequired — инцидент закрывается разбором, а не кнопкой:
// пустое объяснение превращает журнал в список того, что кто-то когда-то смахнул.
var ErrResolutionRequired = validationError("resolution is required")

// List — страница инцидентов: открытые или все.
func (m *MoneyIncidents) List(ctx context.Context, openOnly bool, limit, offset int) ([]*repository.MoneyIncident, error) {
	limit, offset = pageLimit(limit, defaultIncidentPage, 500), pageOffset(offset)
	if openOnly {
		return m.repo.ListOpen(ctx, limit, offset)
	}
	return m.repo.List(ctx, limit, offset)
}

// Resolve закрывает инцидент разбором администратора.
func (m *MoneyIncidents) Resolve(ctx context.Context, adminID, id uuid.UUID, resolution string) error {
	if strings.TrimSpace(resolution) == "" {
		return ErrResolutionRequired
	}
	if err := m.repo.Resolve(ctx, id, adminID, resolution); err != nil {
		return conflictError("cannot resolve")
	}
	log.Printf("[AUDIT] admin %s resolved money incident %s: %s", adminID, id, resolution)
	return nil
}
