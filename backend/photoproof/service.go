package photoproof

import (
	"context"
	"database/sql"
	"strconv"
	"time"

	"github.com/google/uuid"
)

// Настройки модуля (system_settings, миграции 053 и 055).
const (
	SettingMaxTrackGapMin = "photo_proof_max_track_gap_min"
	SettingTrackDays      = "executor_track_days"
)

const (
	defaultMaxTrackGapMin = 15
	defaultTrackDays      = 30
)

// SettingsReader отдаёт системные настройки. Ему удовлетворяет
// repository.SettingsRepository; модулю нужно ровно столько.
type SettingsReader interface {
	GetSettings(ctx context.Context) (map[string]string, error)
}

// Service — правила модуля: что можно сделать с жестами, какой жест получает
// новый заказ и что модуль знает о треке исполнителя.
type Service struct {
	symbols  SymbolRepository
	track    TrackRepository
	settings SettingsReader
	now      func() time.Time

	// Приём снимков (WithProofs).
	db      *sql.DB
	storage Storage
	checker Checker
}

// NewService создаёт Service.
func NewService(symbols SymbolRepository) *Service {
	return &Service{symbols: symbols, now: time.Now}
}

// WithTrack подключает трек исполнителя.
func (s *Service) WithTrack(track TrackRepository, settings SettingsReader) *Service {
	s.track = track
	s.settings = settings
	return s
}

// setting читает целую настройку модуля; нечитаемая или неположительная —
// умолчание, как если бы строки не было.
func (s *Service) setting(ctx context.Context, key string, fallback int) int {
	if s.settings == nil {
		return fallback
	}
	settings, err := s.settings.GetSettings(ctx)
	if err != nil {
		return fallback
	}
	if v, err := strconv.Atoi(settings[key]); err == nil && v > 0 {
		return v
	}
	return fallback
}

// RecordPositions дописывает точки трека. executorID проставляется сервером —
// чужой трек пополнить нельзя, как бы ни было заполнено тело запроса.
func (s *Service) RecordPositions(ctx context.Context, q Querier, executorID uuid.UUID, positions []Position) (int, error) {
	if s.track == nil || len(positions) == 0 {
		return 0, nil
	}
	for i := range positions {
		positions[i].ExecutorID = executorID
	}
	return s.track.Add(ctx, q, positions)
}

// RecordLive записывает обычный отчёт приложения о местоположении.
//
// Отчёты принимаются независимо от настройки geofence_tracking_enabled: она
// решает, шлёт ли их приложение по своей инициативе, а в периоде
// фото-подтверждения приложение шлёт их всегда — иначе проверять снимки было бы
// нечем именно у тех, ради кого механика заведена.
func (s *Service) RecordLive(ctx context.Context, executorID uuid.UUID, lat, lon float64, at time.Time) error {
	if s.track == nil {
		return nil
	}
	if at.IsZero() {
		at = s.now()
	}
	_, err := s.RecordPositions(ctx, nil, executorID, []Position{
		{Lat: lat, Lon: lon, Source: SourceLive, DeviceAt: at},
	})
	return err
}

// NearestPosition отдаёт точку трека, ближайшую по времени устройства к
// моменту at, в пределах окна из настроек. Нет такой точки — nil без ошибки:
// отсутствие трека само по себе не сбой.
func (s *Service) NearestPosition(ctx context.Context, q Querier, executorID uuid.UUID, at time.Time) (*Position, error) {
	if s.track == nil || at.IsZero() {
		return nil, nil
	}
	window := time.Duration(s.setting(ctx, SettingMaxTrackGapMin, defaultMaxTrackGapMin)) * time.Minute
	return s.track.Nearest(ctx, q, executorID, at, window)
}

// SweepTrack удаляет точки старше срока хранения.
func (s *Service) SweepTrack(ctx context.Context) (int, error) {
	if s.track == nil {
		return 0, nil
	}
	days := s.setting(ctx, SettingTrackDays, defaultTrackDays)
	return s.track.DeleteOlderThan(ctx, s.now().AddDate(0, 0, -days))
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
