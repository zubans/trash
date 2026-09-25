package service

import (
	"context"
	"database/sql"
	"errors"
	"log"
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

// ShiftDurationsHours — длительности смены, которые принимает платформа.
// Список один на всех: его проверяет ручной старт смены, по нему же
// валидируется настройка автооткрытия, поэтому смена, открытая автоматически,
// не может получить длительность, которую исполнителю выбрать не дали бы.
var ShiftDurationsHours = []int{1, 3, 5}

// IsValidShiftDuration сообщает, входит ли длительность в разрешённые.
func IsValidShiftDuration(hours int) bool {
	for _, h := range ShiftDurationsHours {
		if h == hours {
			return true
		}
	}
	return false
}

// ErrInvalidShiftDuration — длительность не из ShiftDurationsHours.
var ErrInvalidShiftDuration = validationError("invalid shift duration")

// ShiftService управляет сменами исполнителей.
type ShiftService struct {
	shiftRepo    repository.ShiftRepository
	ledger       *Ledger
	settingsRepo repository.SettingsRepository
	orderRepo    repository.OrderRepository
	locations    ExecutorLocationRecorder
	history      ExecutorOrderHistory
}

// ExecutorOrderHistory отдаёт заказы для экрана истории исполнителя. Как
// выглядит заказ, решает OrderService; у смен своего представления заказа нет.
type ExecutorOrderHistory interface {
	ExecutorHistory(ctx context.Context, executorID uuid.UUID) ([]*OrderView, error)
}

// NewShiftService создаёт ShiftService.
func NewShiftService(shiftRepo repository.ShiftRepository, ledger *Ledger, settingsRepo repository.SettingsRepository, orderRepo repository.OrderRepository) *ShiftService {
	return &ShiftService{shiftRepo: shiftRepo, ledger: ledger, settingsRepo: settingsRepo, orderRepo: orderRepo}
}

// WithExecutorLocation присоединяет хранилище, через которое пишутся отчёты о
// местоположении в смене. Без него RecordLocation сообщает, что сохранить
// ничего не может, вместо того чтобы принимать позиции и выбрасывать их.
func (s *ShiftService) WithExecutorLocation(recorder ExecutorLocationRecorder) *ShiftService {
	s.locations = recorder
	return s
}

// WithOrderHistory присоединяет источник заказов для экрана истории. Без него
// история отдаёт только проводки.
func (s *ShiftService) WithOrderHistory(history ExecutorOrderHistory) *ShiftService {
	s.history = history
	return s
}

// StartShift начинает новую смену исполнителя.
func (s *ShiftService) StartShift(ctx context.Context, executorID uuid.UUID, durationHours int) (*repository.Shift, error) {
	if !IsValidShiftDuration(durationHours) {
		return nil, ErrInvalidShiftDuration
	}

	existing, err := s.shiftRepo.FindActiveByExecutor(ctx, executorID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, ErrShiftAlreadyActive
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
// смену всё равно терялись при каждом перезапуске. Чтение (GetCurrent) смену
// не закрывает: истёкшая, но ещё активная смена — дело воркера, а не того, кто
// первым открыл экран.

// AutoEndExpiredShifts одним оператором закрывает все активные смены, прошедшие
// planned_end_at.
func (s *ShiftService) AutoEndExpiredShifts(ctx context.Context) error {
	ids, err := s.shiftRepo.EndExpired(ctx, time.Now())
	if err != nil {
		return err
	}
	for _, id := range ids {
		log.Printf("[ShiftService] auto-closed expired shift %s", id)
		metrics.ShiftEvent("auto_closed")
	}
	return nil
}

// GetCurrent возвращает активную смену или самую свежую, если активной нет.
func (s *ShiftService) GetCurrent(ctx context.Context, executorID uuid.UUID) (*repository.Shift, error) {
	shift, err := s.shiftRepo.FindActiveByExecutor(ctx, executorID)
	if err != nil {
		return nil, err
	}
	if shift != nil {
		return shift, nil
	}
	last, err := s.shiftRepo.GetLastShiftByExecutor(ctx, executorID)
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, repository.ErrNotFound) {
		return nil, ErrShiftNotFound
	}
	return last, err
}

// End завершает активную смену исполнителя. Завершение смены раньше
// запланированного конца — штрафуемое событие независимо от того, какой
// эндпоинт вызвал клиент: раньше штраф можно было пропустить, просто вызвав
// /shifts/end. Штраф, снятие назначения с открытых заказов и смена статуса
// смены применяются в одной транзакции, поэтому исполнителя никогда не штрафуют
// за заказы, оставшиеся за ним, и не закрывают смену, не списав штраф.
//
// Штраф — настраиваемая shift_early_exit_penalty (по умолчанию 50). Если на
// момент завершения у исполнителя есть назначенные заказы, эти заказы
// возвращаются в пул поиска, а с исполнителя берут двойной штраф плюс общую
// стоимость этих заказов.
func (s *ShiftService) End(ctx context.Context, executorID uuid.UUID) (*repository.Shift, error) {
	if s.ledger == nil {
		return nil, ErrNotConfigured
	}
	shift, err := s.shiftRepo.FindActiveByExecutor(ctx, executorID)
	if err != nil {
		return nil, err
	}
	if shift == nil {
		return nil, ErrNoActiveShift
	}

	// Смена, уже дошедшая до запланированного конца, штрафа не несёт.
	if !time.Now().Before(shift.PlannedEndAt) {
		if err := s.shiftRepo.End(ctx, nil, shift.ID); err != nil {
			return nil, err
		}
		metrics.ShiftEvent("ended")
		return s.shiftRepo.GetShiftByID(ctx, shift.ID)
	}

	basePenalty := money.FromRubles(settingFloat(ctx, s.settingsRepo, "shift_early_exit_penalty", 50.0))

	var assignedOrders []*repository.Order
	if s.orderRepo != nil {
		assignedOrders, err = s.orderRepo.FindAssignedByExecutor(ctx, executorID)
		if err != nil {
			return nil, err
		}
	}

	orderCost := money.Zero
	openOrders := make([]*repository.Order, 0, len(assignedOrders))
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

	if err := s.ledger.RunInTx(ctx, func(tx *sql.Tx) error {
		for _, o := range openOrders {
			if err := s.orderRepo.Unassign(ctx, tx, o.ID); err != nil {
				return err
			}
		}
		// Штраф собирается на счёт штрафов, а не просто исчезает с баланса
		// исполнителя.
		if err := s.ledger.Charge(ctx, tx, executorID, repository.AccountFines, totalFine, repository.TransactionTypeFine, nil); err != nil {
			return err
		}
		// Закрытие охраняется статусом: смена, которую тем временем закрыл
		// воркер, откатывает штраф вместе с собой.
		return s.shiftRepo.EarlyEnd(ctx, tx, shift.ID, totalFine)
	}); err != nil {
		if errors.Is(err, repository.ErrConflict) {
			return nil, ErrNoActiveShift
		}
		return nil, err
	}
	metrics.ShiftEvent("ended_early")

	// Возвращается то, что записано, а не то, что мы думаем о записанном:
	// сфабрикованное состояние скрыло бы расхождение, которое стоит увидеть.
	return s.shiftRepo.GetShiftByID(ctx, shift.ID)
}

// RecordLocation сохраняет позицию, о которой приложение исполнителя сообщает во время смены.
//
// Именно по этой позиции автоподбор меряет расстояние, поэтому отчёт, который
// приняли, но не сохранили, оставил бы подбор работать по устаревшей координате.
// Булево значение говорит, была ли позиция действительно принята: правила
// местоположения могут отклонить перемещение (смену района, ещё не вышедшую из
// паузы), и это законный исход, а не сбой.
func (s *ShiftService) RecordLocation(ctx context.Context, executorID uuid.UUID, lat, lon float64) (bool, error) {
	shift, err := s.shiftRepo.FindActiveByExecutor(ctx, executorID)
	if err != nil {
		return false, err
	}
	if shift == nil {
		return false, ErrNoActiveShift
	}
	if s.locations == nil {
		return false, ErrNotConfigured
	}
	return s.locations.RecordLiveLocation(ctx, executorID, lat, lon)
}

// ExecutorHistoryResult содержит заказы и историю транзакций исполнителя.
type ExecutorHistoryResult struct {
	Orders       []*OrderView              `json:"orders"`
	Transactions []*repository.Transaction `json:"transactions"`
}

// GetExecutorFinancialHistory отдаёт журналы заказов и транзакций исполнителя.
func (s *ShiftService) GetExecutorFinancialHistory(ctx context.Context, executorID uuid.UUID) (*ExecutorHistoryResult, error) {
	res := &ExecutorHistoryResult{
		Orders:       []*OrderView{},
		Transactions: []*repository.Transaction{},
	}

	// Оба списка ограничены размером страницы по умолчанию из репозитория. Этот
	// экран показывает недавнюю историю; исполнитель с годами заказов за спиной
	// раньше вытягивал их все, и каждую проводку, при каждом открытии.
	if s.history != nil {
		orders, err := s.history.ExecutorHistory(ctx, executorID)
		if err == nil && orders != nil {
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
