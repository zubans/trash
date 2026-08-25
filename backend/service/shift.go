package service

import (
	"database/sql"
	"errors"
	"log"
	"strconv"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/money"
	"healthlogin/backend/repository"
)

// ShiftService manages executor shifts and geofence checks.
type ShiftService struct {
	shiftRepo    repository.ShiftRepository
	geozoneRepo  repository.GeozoneRepository
	ledger       *Ledger
	settingsRepo repository.SettingsRepository
	orderRepo    repository.OrderRepository
	catalogRepo  repository.ServiceCatalogRepository
	db           *sql.DB
}

// NewShiftService creates a ShiftService.
func NewShiftService(shiftRepo repository.ShiftRepository, geozoneRepo repository.GeozoneRepository, ledger *Ledger, settingsRepo repository.SettingsRepository, orderRepo repository.OrderRepository, catalogRepo repository.ServiceCatalogRepository, db *sql.DB) *ShiftService {
	return &ShiftService{shiftRepo: shiftRepo, geozoneRepo: geozoneRepo, ledger: ledger, settingsRepo: settingsRepo, orderRepo: orderRepo, catalogRepo: catalogRepo, db: db}
}

// StartShift begins a new shift for an executor and schedules auto-end timer.
func (s *ShiftService) StartShift(executorID uuid.UUID, durationHours int) (*repository.Shift, error) {
	if durationHours != 1 && durationHours != 3 && durationHours != 5 {
		return nil, errors.New("invalid shift duration")
	}

	existing, err := s.shiftRepo.GetActiveShift(executorID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if existing != nil {
		return nil, errors.New("active shift already exists")
	}

	return s.shiftRepo.StartShift(executorID, durationHours)
}

// Shifts are closed by a single mechanism: ShiftWorker scans for expired ones
// on a timer (see AutoEndExpiredShifts). There used to be three — a goroutine
// with a timer per shift, this scan, and a restore pass on boot that recreated
// the timers — which meant a shift could be closed by whichever raced first, and
// the per-shift goroutines were lost on every restart anyway.

// EndShiftByID completes an active shift if it hasn't already been finished.
func (s *ShiftService) EndShiftByID(shiftID uuid.UUID) error {
	shift, err := s.shiftRepo.GetShiftByID(shiftID)
	if err != nil || shift.Status != repository.ShiftStatusActive {
		return nil
	}
	log.Printf("[ShiftService] Auto-closing expired shift %s for executor %s (planned_end_at: %v)", shift.ID, shift.ExecutorID, shift.PlannedEndAt)
	return s.shiftRepo.End(shift.ID)
}

// AutoEndExpiredShifts scans all active shifts and completes any that have passed their planned_end_at.
func (s *ShiftService) AutoEndExpiredShifts() error {
	shifts, err := s.shiftRepo.GetActiveShifts()
	if err != nil {
		return err
	}
	now := time.Now()
	for _, shift := range shifts {
		if now.After(shift.PlannedEndAt) {
			_ = s.EndShiftByID(shift.ID)
		}
	}
	return nil
}

// Start begins a new shift (alias compatible with handler).
func (s *ShiftService) Start(executorID uuid.UUID, durationHours int) (*repository.Shift, error) {
	return s.StartShift(executorID, durationHours)
}

// GetActive returns the active shift for an executor, auto-ending it if expired.
func (s *ShiftService) GetActive(executorID uuid.UUID) (*repository.Shift, error) {
	shift, err := s.shiftRepo.GetActiveShift(executorID)
	if err == nil && shift != nil {
		if time.Now().After(shift.PlannedEndAt) {
			_ = s.EndShiftByID(shift.ID)
			return nil, errors.New("no active shift")
		}
		return shift, nil
	}
	return nil, err
}

// GetCurrent returns the active shift, or the most recent shift if no active
// shift exists. Checks for expiration on active shifts.
func (s *ShiftService) GetCurrent(executorID uuid.UUID) (*repository.Shift, error) {
	shift, err := s.shiftRepo.GetActiveShift(executorID)
	if err == nil && shift != nil {
		if time.Now().After(shift.PlannedEndAt) {
			_ = s.EndShiftByID(shift.ID)
			return s.shiftRepo.GetLastShiftByExecutor(executorID)
		}
		return shift, nil
	}
	return s.shiftRepo.GetLastShiftByExecutor(executorID)
}

// End terminates the active shift for an executor. Ending a shift before its
// planned end is a penalised event regardless of which endpoint the client
// calls, so this delegates to the same routine as EarlyEnd — previously the
// fine could be skipped simply by calling /shifts/end instead of /early-end.
func (s *ShiftService) End(executorID uuid.UUID) error {
	_, err := s.finishShift(executorID)
	return err
}

// EarlyEnd terminates the active shift and charges the penalty configured in
// system_settings (default 50). If the executor has assigned orders at the
// moment of termination, those orders are returned to the search pool and the
// executor is charged double the penalty plus the total value of those orders.
func (s *ShiftService) EarlyEnd(executorID uuid.UUID) (*repository.Shift, error) {
	return s.finishShift(executorID)
}

// finishShift is the single exit path for an active shift. The fine, the
// unassignment of open orders and the shift status change are applied together,
// so an executor is never charged for orders that stayed assigned to them.
func (s *ShiftService) finishShift(executorID uuid.UUID) (*repository.Shift, error) {
	shift, err := s.shiftRepo.GetActiveShift(executorID)
	if err != nil {
		return nil, errors.New("no active shift")
	}

	// A shift that already reached its planned end carries no penalty.
	if !time.Now().Before(shift.PlannedEndAt) {
		if err := s.shiftRepo.End(shift.ID); err != nil {
			return nil, err
		}
		return s.shiftRepo.GetShiftByID(shift.ID)
	}

	basePenalty := s.earlyExitPenaltyAmount()

	var assignedOrders []repository.Order
	if s.orderRepo != nil {
		assignedOrders, err = s.orderRepo.FindAssignedByExecutor(executorID)
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
		if err := s.ledger.RunInTx(func(tx *sql.Tx) error {
			for _, o := range openOrders {
				if err := s.orderRepo.Unassign(tx, o.ID); err != nil {
					return err
				}
			}
			// The penalty is collected onto the fines account rather than simply
			// disappearing from the executor's balance.
			return s.ledger.Charge(tx, executorID, repository.AccountFines, totalFine, repository.TransactionTypeFine, nil)
		}); err != nil {
			return nil, err
		}
	}

	if err := s.shiftRepo.EarlyEnd(shift.ID, totalFine); err != nil {
		return nil, err
	}

	updated, err := s.shiftRepo.GetShiftByID(shift.ID)
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

func (s *ShiftService) geofenceFineAmount() money.Amount {
	return money.FromRubles(s.settingsFloat("geofence_fine_amount", 500.0))
}

// earlyExitPenaltyAmount returns the fine charged when an executor ends a
// shift before its planned end time.
func (s *ShiftService) earlyExitPenaltyAmount() money.Amount {
	return money.FromRubles(s.settingsFloat("shift_early_exit_penalty", 50.0))
}

func (s *ShiftService) settingsFloat(key string, defaultValue float64) float64 {
	if s.settingsRepo == nil {
		return defaultValue
	}
	settings, err := s.settingsRepo.GetSettings()
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

// RecordLocation stores a GPS point, checks geofence compliance and penalizes
// the executor after three consecutive violations.
func (s *ShiftService) RecordLocation(executorID uuid.UUID, lat, lon float64) error {
	_, err := s.RecordLocationWithResult(executorID, lat, lon)
	return err
}

// RecordLocationWithResult stores a GPS point and returns whether the point is
// inside the executor geofence.
func (s *ShiftService) RecordLocationWithResult(executorID uuid.UUID, lat, lon float64) (bool, error) {
	shift, err := s.shiftRepo.GetActiveShift(executorID)
	if err != nil {
		return false, errors.New("no active shift")
	}

	inside := true
	if s.geozoneRepo != nil {
		geozone, err := s.geozoneRepo.FindByExecutor(executorID)
		if err == nil && geozone != nil {
			inside, err = s.IsWithinGeozone(geozone, lat, lon)
			if err != nil {
				log.Printf("[ShiftService] geozone check failed for executor %s: %v", executorID, err)
				inside = true
			}
		}
	}

	if err := s.shiftRepo.AddGPSLog(shift.ID, lat, lon, inside); err != nil {
		return inside, err
	}

	if inside {
		return inside, nil
	}

	logs, err := s.shiftRepo.GetLastGPSLogs(shift.ID, 3)
	if err != nil {
		log.Printf("[ShiftService] failed to load gps logs for shift %s: %v", shift.ID, err)
		return inside, nil
	}
	if len(logs) < 3 {
		return inside, nil
	}
	for _, v := range logs {
		if v {
			return inside, nil
		}
	}

	fine := s.geofenceFineAmount()
	if s.ledger != nil {
		if err := s.ledger.RunInTx(func(tx *sql.Tx) error {
			return s.ledger.Charge(tx, executorID, repository.AccountFines, fine, repository.TransactionTypeFine, nil)
		}); err != nil {
			log.Printf("[ShiftService] failed to charge fine for executor %s: %v", executorID, err)
		}
	}

	if err := s.shiftRepo.Penalize(shift.ID, fine); err != nil {
		log.Printf("[ShiftService] failed to penalize shift %s: %v", shift.ID, err)
	}
	return inside, nil
}

// IsWithinGeozone checks whether a point is inside the executor working area.
func (s *ShiftService) IsWithinGeozone(geozone *repository.Geozone, lat, lon float64) (bool, error) {
	switch geozone.Type {
	case string(repository.GeozoneTypeCircle):
		if geozone.CenterLatitude == nil || geozone.CenterLongitude == nil || geozone.RadiusMeters == nil {
			return false, errors.New("invalid circle geozone")
		}
		return IsWithinRadius(lat, lon, *geozone.CenterLatitude, *geozone.CenterLongitude, int(*geozone.RadiusMeters)), nil
	case string(repository.GeozoneTypePolygon):
		polygon := make([]Point, len(geozone.Polygon))
		for i, p := range geozone.Polygon {
			polygon[i] = Point{Lat: p.Lat, Lon: p.Lon}
		}
		return IsPointInPolygon(Point{Lat: lat, Lon: lon}, polygon), nil
	default:
		return false, errors.New("unknown geozone type")
	}
}

// ExecutorHistoryResult contains orders and transaction history for an executor.
type ExecutorHistoryResult struct {
	Orders       []repository.Order        `json:"orders"`
	Transactions []*repository.Transaction `json:"transactions"`
}

// GetExecutorFinancialHistory retrieves order and transaction logs for an executor.
func (s *ShiftService) GetExecutorFinancialHistory(executorID uuid.UUID) (*ExecutorHistoryResult, error) {
	res := &ExecutorHistoryResult{
		Orders:       []repository.Order{},
		Transactions: []*repository.Transaction{},
	}

	if s.orderRepo != nil {
		orders, err := s.orderRepo.FindAllByExecutor(executorID)
		if err == nil && orders != nil {
			for i := range orders {
				if s.catalogRepo != nil {
					if variant, err := s.catalogRepo.GetNodeByID(orders[i].ServiceVariantID); err == nil {
						orders[i].ServiceVariant = variant
					}
				}
			}
			res.Orders = orders
		}
	}

	if s.ledger != nil {
		txs, err := s.ledger.History(executorID)
		if err == nil && txs != nil {
			res.Transactions = txs
		}
	}

	return res, nil
}
