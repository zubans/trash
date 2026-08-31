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

// ExecutorLocationRecorder stores a position an executor reports while working.
// ShiftService owns shifts, not whereabouts, so it delegates: the stored
// position has exactly one writer and one set of rules, in ExecutorGeoService.
type ExecutorLocationRecorder interface {
	RecordLiveLocation(ctx context.Context, executorID uuid.UUID, lat, lon float64) (bool, error)
}

// ShiftService manages executor shifts.
type ShiftService struct {
	shiftRepo    repository.ShiftRepository
	ledger       *Ledger
	settingsRepo repository.SettingsRepository
	orderRepo    repository.OrderRepository
	catalogRepo  repository.ServiceCatalogRepository
	locations    ExecutorLocationRecorder
	db           *sql.DB
}

// NewShiftService creates a ShiftService.
func NewShiftService(shiftRepo repository.ShiftRepository, ledger *Ledger, settingsRepo repository.SettingsRepository, orderRepo repository.OrderRepository, catalogRepo repository.ServiceCatalogRepository, db *sql.DB) *ShiftService {
	return &ShiftService{shiftRepo: shiftRepo, ledger: ledger, settingsRepo: settingsRepo, orderRepo: orderRepo, catalogRepo: catalogRepo, db: db}
}

// WithExecutorLocation attaches the store that shift location reports are
// written through. Without it RecordLocation reports that it cannot store
// anything, rather than accepting positions and dropping them.
func (s *ShiftService) WithExecutorLocation(recorder ExecutorLocationRecorder) *ShiftService {
	s.locations = recorder
	return s
}

// StartShift begins a new shift for an executor and schedules auto-end timer.
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

// Shifts are closed by a single mechanism: ShiftWorker scans for expired ones
// on a timer (see AutoEndExpiredShifts). There used to be three — a goroutine
// with a timer per shift, this scan, and a restore pass on boot that recreated
// the timers — which meant a shift could be closed by whichever raced first, and
// the per-shift goroutines were lost on every restart anyway.

// EndShiftByID completes an active shift if it hasn't already been finished.
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

// AutoEndExpiredShifts scans all active shifts and completes any that have passed their planned_end_at.
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

// Start begins a new shift (alias compatible with handler).
func (s *ShiftService) Start(ctx context.Context, executorID uuid.UUID, durationHours int) (*repository.Shift, error) {
	return s.StartShift(ctx, executorID, durationHours)
}

// GetActive returns the active shift for an executor, auto-ending it if expired.
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

// GetCurrent returns the active shift, or the most recent shift if no active
// shift exists. Checks for expiration on active shifts.
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

// End terminates the active shift for an executor. Ending a shift before its
// planned end is a penalised event regardless of which endpoint the client
// calls, so this delegates to the same routine as EarlyEnd — previously the
// fine could be skipped simply by calling /shifts/end instead of /early-end.
func (s *ShiftService) End(ctx context.Context, executorID uuid.UUID) error {
	_, err := s.finishShift(ctx, executorID)
	return err
}

// EarlyEnd terminates the active shift and charges the penalty configured in
// system_settings (default 50). If the executor has assigned orders at the
// moment of termination, those orders are returned to the search pool and the
// executor is charged double the penalty plus the total value of those orders.
func (s *ShiftService) EarlyEnd(ctx context.Context, executorID uuid.UUID) (*repository.Shift, error) {
	return s.finishShift(ctx, executorID)
}

// finishShift is the single exit path for an active shift. The fine, the
// unassignment of open orders and the shift status change are applied together,
// so an executor is never charged for orders that stayed assigned to them.
func (s *ShiftService) finishShift(ctx context.Context, executorID uuid.UUID) (*repository.Shift, error) {
	shift, err := s.shiftRepo.GetActiveShift(ctx, executorID)
	if err != nil {
		return nil, errors.New("no active shift")
	}

	// A shift that already reached its planned end carries no penalty.
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
		// Orders already marked EXECUTED are awaiting customer confirmation and
		// must not be pulled back from the executor.
		if o.Status != repository.OrderStatusAssigned {
			continue
		}
		openOrders = append(openOrders, o)
		orderCost = orderCost.Add(o.HoldAmount)
	}

	// With open orders the fine is doubled and includes the order cost.
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
			// The penalty is collected onto the fines account rather than simply
			// disappearing from the executor's balance.
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
		// Fallback to the original shift with the changes applied in memory.
		now := time.Now()
		shift.Status = repository.ShiftStatusPenalized
		shift.ActualEndAt = &now
		shift.FineAmount = shift.FineAmount.Add(totalFine)
		return shift, nil
	}
	return updated, nil
}

// earlyExitPenaltyAmount returns the fine charged when an executor ends a
// shift before its planned end time.
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

// RecordLocation stores the position an executor's app reports during a shift.
//
// The position is what automatic matching measures distance against, so a
// report that is accepted but not stored would leave matching working from a
// stale fix. The boolean says whether the position was actually taken: the
// location rules can decline a move (a district change still inside its
// cooldown), which is a legitimate outcome rather than a failure.
func (s *ShiftService) RecordLocation(ctx context.Context, executorID uuid.UUID, lat, lon float64) (bool, error) {
	// A nil shift with no error also means "not on shift": the repository
	// reports an absent row that way, so checking only the error would accept
	// positions from an executor who is not working.
	shift, err := s.shiftRepo.GetActiveShift(ctx, executorID)
	if err != nil || shift == nil {
		return false, errors.New("no active shift")
	}
	if s.locations == nil {
		return false, errors.New("executor location storage is not configured")
	}
	return s.locations.RecordLiveLocation(ctx, executorID, lat, lon)
}

// ExecutorHistoryResult contains orders and transaction history for an executor.
type ExecutorHistoryResult struct {
	Orders       []repository.Order        `json:"orders"`
	Transactions []*repository.Transaction `json:"transactions"`
}

// GetExecutorFinancialHistory retrieves order and transaction logs for an executor.
// hydrateHistoryVariants attaches the service variant to each order in a
// history page, resolving the whole page in one query instead of one per order.
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

	// Both lists are bounded by the repository's default page size. This screen
	// shows a recent history; an executor with years of orders behind them used
	// to pull every one of them, and every ledger entry, on each open.
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
