package service

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"strconv"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/metrics"
	"healthlogin/backend/money"
	"healthlogin/backend/repository"
)

// ExecutorLocationRecorder сохраняет позицию, о которой исполнитель сообщает во
// время работы. ShiftService владеет сменами, а не местонахождением, поэтому он
// делегирует: у сохранённой позиции один писатель и один набор правил, в ExecutorGeoService.
type ExecutorLocationRecorder interface {
	RecordLiveLocation(ctx context.Context, executorID uuid.UUID, lat, lon float64) (bool, error)
}

// ShiftService управляет сменами исполнителей.
type ShiftService struct {
	shiftRepo    repository.ShiftRepository
	ledger       *Ledger
	settingsRepo repository.SettingsRepository
	orderRepo    repository.OrderRepository
	catalogRepo  repository.ServiceCatalogRepository
	locations    ExecutorLocationRecorder
	db           *sql.DB
}

// NewShiftService создаёт ShiftService.
func NewShiftService(shiftRepo repository.ShiftRepository, ledger *Ledger, settingsRepo repository.SettingsRepository, orderRepo repository.OrderRepository, catalogRepo repository.ServiceCatalogRepository, db *sql.DB) *ShiftService {
	return &ShiftService{shiftRepo: shiftRepo, ledger: ledger, settingsRepo: settingsRepo, orderRepo: orderRepo, catalogRepo: catalogRepo, db: db}
}

// WithExecutorLocation присоединяет хранилище, через которое пишутся отчёты о
// местоположении в смене. Без него RecordLocation сообщает, что сохранить
// ничего не может, вместо того чтобы принимать позиции и выбрасывать их.
func (s *ShiftService) WithExecutorLocation(recorder ExecutorLocationRecorder) *ShiftService {
	s.locations = recorder
	return s
}

// StartShift начинает новую смену исполнителя и планирует таймер автозавершения.
func (s *ShiftService) StartShift(ctx context.Context, executorID uuid.UUID, durationHours int) (*repository.Shift, error) {
	if durationHours != 1 && durationHours != 3 && durationHours != 5 {
		return nil, errors.New("invalid shift duration")
	}

	existing, err := s.shiftRepo.GetActiveShift(ctx, executorID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if existing != nil {
		return nil, errors.New("active shift already exists")
	}

	shift, err := s.shiftRepo.StartShift(ctx, executorID, durationHours)
	if err != nil {
		return nil, err
	}
	metrics.ShiftEvent("started")
	return shift, nil
}

// Смены закрываются одним механизмом: ShiftWorker по таймеру ищет истёкшие
// (см. AutoEndExpiredShifts). Раньше их было три — горутина с таймером на
// каждую смену, этот скан и восстановительный проход при старте, пересоздававший
// таймеры, — из-за чего смену закрывал тот, кто выиграл гонку, а горутины на
// смену всё равно терялись при каждом перезапуске.

// EndShiftByID завершает активную смену, если она ещё не была закрыта.
func (s *ShiftService) EndShiftByID(ctx context.Context, shiftID uuid.UUID) error {
	shift, err := s.shiftRepo.GetShiftByID(ctx, shiftID)
	if err != nil || shift.Status != repository.ShiftStatusActive {
		return nil
	}
	log.Printf("[ShiftService] Auto-closing expired shift %s for executor %s (planned_end_at: %v)", shift.ID, shift.ExecutorID, shift.PlannedEndAt)
	if err := s.shiftRepo.End(ctx, shift.ID); err != nil {
		return err
	}
	metrics.ShiftEvent("auto_closed")
	return nil
}

// AutoEndExpiredShifts просматривает все активные смены и завершает те, что прошли planned_end_at.
func (s *ShiftService) AutoEndExpiredShifts(ctx context.Context) error {
	shifts, err := s.shiftRepo.GetActiveShifts(ctx)
	if err != nil {
		return err
	}
	now := time.Now()
	for _, shift := range shifts {
		if now.After(shift.PlannedEndAt) {
			_ = s.EndShiftByID(ctx, shift.ID)
		}
	}
	return nil
}

// Start начинает новую смену (псевдоним, совместимый с обработчиком).
func (s *ShiftService) Start(ctx context.Context, executorID uuid.UUID, durationHours int) (*repository.Shift, error) {
	return s.StartShift(ctx, executorID, durationHours)
}

// GetActive возвращает активную смену исполнителя, автоматически завершая её при истечении.
func (s *ShiftService) GetActive(ctx context.Context, executorID uuid.UUID) (*repository.Shift, error) {
	shift, err := s.shiftRepo.GetActiveShift(ctx, executorID)
	if err == nil && shift != nil {
		if time.Now().After(shift.PlannedEndAt) {
			_ = s.EndShiftByID(ctx, shift.ID)
			return nil, errors.New("no active shift")
		}
		return shift, nil
	}
	return nil, err
}

// GetCurrent возвращает активную смену или самую свежую, если активной нет.
// Активные смены проверяются на истечение.
func (s *ShiftService) GetCurrent(ctx context.Context, executorID uuid.UUID) (*repository.Shift, error) {
	shift, err := s.shiftRepo.GetActiveShift(ctx, executorID)
	if err == nil && shift != nil {
		if time.Now().After(shift.PlannedEndAt) {
			_ = s.EndShiftByID(ctx, shift.ID)
			return s.shiftRepo.GetLastShiftByExecutor(ctx, executorID)
		}
		return shift, nil
	}
	return s.shiftRepo.GetLastShiftByExecutor(ctx, executorID)
}

// End завершает активную смену исполнителя. Завершение смены раньше
// запланированного конца — штрафуемое событие независимо от того, какой
// эндпоинт вызвал клиент, поэтому здесь делегируется той же процедуре, что и
// EarlyEnd: раньше штраф можно было пропустить, просто вызвав /shifts/end.
func (s *ShiftService) End(ctx context.Context, executorID uuid.UUID) error {
	_, err := s.finishShift(ctx, executorID)
	return err
}

// EarlyEnd завершает активную смену и списывает штраф, настроенный в
// system_settings (по умолчанию 50). Если на момент завершения у исполнителя
// есть назначенные заказы, эти заказы возвращаются в пул поиска, а с
// исполнителя берут двойной штраф плюс общую стоимость этих заказов.
func (s *ShiftService) EarlyEnd(ctx context.Context, executorID uuid.UUID) (*repository.Shift, error) {
	return s.finishShift(ctx, executorID)
}

// finishShift — единственный путь выхода из активной смены. Штраф, снятие
// назначения с открытых заказов и смена статуса смены применяются вместе,
// поэтому исполнителя никогда не штрафуют за заказы, оставшиеся за ним.
func (s *ShiftService) finishShift(ctx context.Context, executorID uuid.UUID) (*repository.Shift, error) {
	shift, err := s.shiftRepo.GetActiveShift(ctx, executorID)
	if err != nil {
		return nil, errors.New("no active shift")
	}

	// Смена, уже дошедшая до запланированного конца, штрафа не несёт.
	if !time.Now().Before(shift.PlannedEndAt) {
		if err := s.shiftRepo.End(ctx, shift.ID); err != nil {
			return nil, err
		}
		metrics.ShiftEvent("ended")
		return s.shiftRepo.GetShiftByID(ctx, shift.ID)
	}

	basePenalty := s.earlyExitPenaltyAmount(ctx)

	var assignedOrders []repository.Order
	if s.orderRepo != nil {
		assignedOrders, err = s.orderRepo.FindAssignedByExecutor(ctx, executorID)
		if err != nil {
			return nil, err
		}
	}

	orderCost := money.Zero
	openOrders := make([]repository.Order, 0, len(assignedOrders))
	for _, o := range assignedOrders {
		// Заказы, уже помеченные EXECUTED, ждут подтверждения заказчика, и
		// отбирать их у исполнителя нельзя.
		if o.Status != repository.OrderStatusAssigned {
			continue
		}
		openOrders = append(openOrders, o)
		orderCost = orderCost.Add(o.HoldAmount)
	}

	// При открытых заказах штраф удваивается и включает стоимость заказов.
	totalFine := basePenalty
	if len(openOrders) > 0 {
		totalFine = basePenalty.Scale(2).Add(orderCost)
	}

	if s.ledger != nil {
		if err := s.ledger.RunInTx(ctx, func(tx *sql.Tx) error {
			for _, o := range openOrders {
				if err := s.orderRepo.Unassign(ctx, tx, o.ID); err != nil {
					return err
				}
			}
			// Штраф собирается на счёт штрафов, а не просто исчезает с баланса
			// исполнителя.
			return s.ledger.Charge(ctx, tx, executorID, repository.AccountFines, totalFine, repository.TransactionTypeFine, nil)
		}); err != nil {
			return nil, err
		}
	}

	if err := s.shiftRepo.EarlyEnd(ctx, shift.ID, totalFine); err != nil {
		return nil, err
	}
	metrics.ShiftEvent("ended_early")

	updated, err := s.shiftRepo.GetShiftByID(ctx, shift.ID)
	if err != nil {
		// Откатываемся к исходной смене с изменениями, применёнными в памяти.
		now := time.Now()
		shift.Status = repository.ShiftStatusPenalized
		shift.ActualEndAt = &now
		shift.FineAmount = shift.FineAmount.Add(totalFine)
		return shift, nil
	}
	return updated, nil
}

// earlyExitPenaltyAmount возвращает штраф, который берут, когда исполнитель
// завершает смену раньше запланированного времени.
func (s *ShiftService) earlyExitPenaltyAmount(ctx context.Context) money.Amount {
	return money.FromRubles(s.settingsFloat(ctx, "shift_early_exit_penalty", 50.0))
}

func (s *ShiftService) settingsFloat(ctx context.Context, key string, defaultValue float64) float64 {
	if s.settingsRepo == nil {
		return defaultValue
	}
	settings, err := s.settingsRepo.GetSettings(ctx)
	if err != nil {
		return defaultValue
	}
	if v, ok := settings[key]; ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return defaultValue
}

// RecordLocation сохраняет позицию, о которой приложение исполнителя сообщает во время смены.
//
// Именно по этой позиции автоподбор меряет расстояние, поэтому отчёт, который
// приняли, но не сохранили, оставил бы подбор работать по устаревшей координате.
// Булево значение говорит, была ли позиция действительно принята: правила
// местоположения могут отклонить перемещение (смену района, ещё не вышедшую из
// паузы), и это законный исход, а не сбой.
func (s *ShiftService) RecordLocation(ctx context.Context, executorID uuid.UUID, lat, lon float64) (bool, error) {
	// Nil-смена без ошибки тоже означает «не на смене»: репозиторий так сообщает
	// об отсутствующей строке, поэтому проверка только ошибки принимала бы
	// позиции от исполнителя, который не работает.
	shift, err := s.shiftRepo.GetActiveShift(ctx, executorID)
	if err != nil || shift == nil {
		return false, errors.New("no active shift")
	}
	if s.locations == nil {
		return false, errors.New("executor location storage is not configured")
	}
	return s.locations.RecordLiveLocation(ctx, executorID, lat, lon)
}

// ExecutorHistoryResult содержит заказы и историю транзакций исполнителя.
type ExecutorHistoryResult struct {
	Orders       []repository.Order        `json:"orders"`
	Transactions []*repository.Transaction `json:"transactions"`
}

// GetExecutorFinancialHistory отдаёт журналы заказов и транзакций исполнителя.
// hydrateHistoryVariants прикрепляет вариант услуги к каждому заказу страницы
// истории, разрешая всю страницу одним запросом вместо одного на заказ.
func (s *ShiftService) hydrateHistoryVariants(ctx context.Context, orders []repository.Order) {
	if s.catalogRepo == nil || len(orders) == 0 {
		return
	}
	ids := make([]uuid.UUID, 0, len(orders))
	for i := range orders {
		ids = append(ids, orders[i].ServiceVariantID)
	}
	variants, err := s.catalogRepo.GetNodesByIDs(ctx, ids)
	if err != nil {
		return
	}
	for i := range orders {
		if variant := variants[orders[i].ServiceVariantID]; variant != nil {
			orders[i].ServiceVariant = variant
		}
	}
}

func (s *ShiftService) GetExecutorFinancialHistory(ctx context.Context, executorID uuid.UUID) (*ExecutorHistoryResult, error) {
	res := &ExecutorHistoryResult{
		Orders:       []repository.Order{},
		Transactions: []*repository.Transaction{},
	}

	// Оба списка ограничены размером страницы по умолчанию из репозитория. Этот
	// экран показывает недавнюю историю; исполнитель с годами заказов за спиной
	// раньше вытягивал их все, и каждую проводку, при каждом открытии.
	if s.orderRepo != nil {
		orders, err := s.orderRepo.FindAllByExecutor(ctx, executorID, 0)
		if err == nil && orders != nil {
			s.hydrateHistoryVariants(ctx, orders)
			res.Orders = orders
		}
	}

	if s.ledger != nil {
		txs, err := s.ledger.History(ctx, executorID, 0)
		if err == nil && txs != nil {
			res.Transactions = txs
		}
	}

	return res, nil
}
