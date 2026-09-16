package photoproof

import (
	"context"

	"github.com/google/uuid"
)

// Service — правила модуля: что можно сделать с жестами и какой жест получает
// новый заказ.
type Service struct {
	symbols SymbolRepository
}

// NewService создаёт Service.
func NewService(symbols SymbolRepository) *Service {
	return &Service{symbols: symbols}
}

// ListSymbols отдаёт справочник жестов.
func (s *Service) ListSymbols(ctx context.Context, includeDeleted bool) ([]Symbol, error) {
	return s.symbols.List(ctx, includeDeleted)
}

// GetSymbol отдаёт жест по id.
func (s *Service) GetSymbol(ctx context.Context, id uuid.UUID) (*Symbol, error) {
	return s.symbols.Get(ctx, id)
}

// CreateSymbol заводит жест.
func (s *Service) CreateSymbol(ctx context.Context, symbol *Symbol) error {
	if err := symbol.normalize(); err != nil {
		return err
	}
	return s.symbols.Create(ctx, symbol)
}

// UpdateSymbol правит жест. Номер и удалённость правкой не меняются: номер —
// ссылка старых снимков, удаление и восстановление — отдельные действия.
func (s *Service) UpdateSymbol(ctx context.Context, symbol *Symbol) error {
	if err := symbol.normalize(); err != nil {
		return err
	}
	return s.symbols.Update(ctx, symbol)
}

// DeleteSymbol снимает жест с выдачи, не трогая заказы и снимки, которые его
// уже получили.
func (s *Service) DeleteSymbol(ctx context.Context, id uuid.UUID) error {
	return s.symbols.Delete(ctx, id)
}

// RestoreSymbol возвращает жест в выдачу.
func (s *Service) RestoreSymbol(ctx context.Context, id uuid.UUID) error {
	return s.symbols.Restore(ctx, id)
}

// PickSymbol выбирает жест для заказа. Случайный из действующих: исполнитель
// не должен уметь предсказать, что от него попросят.
func (s *Service) PickSymbol(ctx context.Context, q Querier) (*Symbol, error) {
	return s.symbols.PickRandomLive(ctx, q)
}
