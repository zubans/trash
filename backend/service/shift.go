package service

import (
	"database/sql"
	"errors"
	"log"
	"strconv"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/repository"
)

// ShiftService manages executor shifts and geofence checks.
type ShiftService struct {
	shiftRepo       repository.ShiftRepository
	geozoneRepo     repository.GeozoneRepository
	transactionRepo repository.TransactionRepository
	settingsRepo    repository.SettingsRepository
	orderRepo       repository.OrderRepository
	db              *sql.DB
}

// NewShiftService creates a ShiftService.
func NewShiftService(shiftRepo repository.ShiftRepository, geozoneRepo repository.GeozoneRepository, transactionRepo repository.TransactionRepository, settingsRepo repository.SettingsRepository, orderRepo repository.OrderRepository, db *sql.DB) *ShiftService {
	return &ShiftService{shiftRepo: shiftRepo, geozoneRepo: geozoneRepo, transactionRepo: transactionRepo, settingsRepo: settingsRepo, orderRepo: orderRepo, db: db}
}

// StartShift begins a new shift for an executor.
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

// Start begins a new shift (alias compatible with handler).
func (s *ShiftService) Start(executorID uuid.UUID, durationHours int) (*repository.Shift, error) {
	return s.StartShift(executorID, durationHours)
}

// GetActive returns the active shift for an executor.
func (s *ShiftService) GetActive(executorID uuid.UUID) (*repository.Shift, error) {
	return s.shiftRepo.GetActiveShift(executorID)
}

// GetCurrent returns the active shift, or the most recent shift if no active
// shift exists. This is used by the executor dashboard to always display
// current/last shift status.
func (s *ShiftService) GetCurrent(executorID uuid.UUID) (*repository.Shift, error) {
	shift, err := s.shiftRepo.GetActiveShift(executorID)
	if err == nil {
		return shift, nil
	}
	return s.shiftRepo.GetLastShiftByExecutor(executorID)
}

// End terminates the active shift for an executor.
func (s *ShiftService) End(executorID uuid.UUID) error {
	shift, err := s.shiftRepo.GetActiveShift(executorID)
	if err != nil {
		return errors.New("no active shift")
	}
	return s.shiftRepo.UpdateShiftStatus(shift.ID, string(repository.ShiftStatusCompleted))
}

// EarlyEnd terminates the active shift before its planned end time and charges
// a penalty configured in system_settings (default 50). If the executor has
// assigned orders at the moment of early termination, each order is canceled,
// the customer is refunded the held amount, and the executor is charged double
// the penalty plus the total cost of those orders.
func (s *ShiftService) EarlyEnd(executorID uuid.UUID) (*repository.Shift, error) {
	shift, err := s.shiftRepo.GetActiveShift(executorID)
	if err != nil {
		return nil, errors.New("no active shift")
	}

	basePenalty := s.earlyExitPenaltyAmount()

	var assignedOrders []repository.Order
	if s.orderRepo != nil {
		assignedOrders, err = s.orderRepo.FindAssignedByExecutor(executorID)
		if err != nil {
			return nil, err
		}
	}

	orderCost := 0.0
	for _, o := range assignedOrders {
		orderCost += o.HoldAmount
	}

	// With assigned orders the fine is doubled and includes the order cost.
	var totalFine float64
	if len(assignedOrders) > 0 {
		totalFine = basePenalty*2 + orderCost
	} else {
		totalFine = basePenalty
	}

	if s.transactionRepo != nil {
		if err := s.transactionRepo.RunInTx(func(tx *sql.Tx) error {
			// Refund customers and cancel their orders first.
			for _, o := range assignedOrders {
				if err := s.transactionRepo.UpdateBalance(tx, o.CustomerID, o.HoldAmount); err != nil {
					return err
				}
				if err := s.transactionRepo.CreateTransaction(tx, &repository.Transaction{
					UserID:  o.CustomerID,
					OrderID: &o.ID,
					Type:    string(repository.TransactionTypeRefund),
					Amount:  o.HoldAmount,
				}); err != nil {
					return err
				}
				if err := s.orderRepo.Cancel(o.ID); err != nil {
					return err
				}
			}

			// Charge the executor.
			if err := s.transactionRepo.UpdateBalance(tx, executorID, -totalFine); err != nil {
				return err
			}
			return s.transactionRepo.CreateTransaction(tx, &repository.Transaction{
				UserID: executorID,
				Type:   string(repository.TransactionTypeFine),
				Amount: totalFine,
			})
		}); err != nil {
			return nil, err
		}
	}

	if err := s.shiftRepo.EarlyEnd(shift.ID, totalFine); err != nil {
		return nil, err
	}

	// Return the updated shift for the response.
	updated, err := s.shiftRepo.GetShiftByID(shift.ID)
	if err != nil {
		// Fallback to the original shift with the changes applied in memory.
		now := time.Now()
		shift.Status = repository.ShiftStatusPenalized
		shift.ActualEndAt = &now
		shift.FineAmount += totalFine
		return shift, nil
	}
	return updated, nil
}

func (s *ShiftService) geofenceFineAmount() float64 {
	return s.settingsFloat("geofence_fine_amount", 500.0)
}

// earlyExitPenaltyAmount returns the fine charged when an executor ends a
// shift before its planned end time.
func (s *ShiftService) earlyExitPenaltyAmount() float64 {
	return s.settingsFloat("shift_early_exit_penalty", 50.0)
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
	shift, err := s.shiftRepo.GetActiveShift(executorID)
	if err != nil {
		return errors.New("no active shift")
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
		return err
	}

	if inside {
		return nil
	}

	logs, err := s.shiftRepo.GetLastGPSLogs(shift.ID, 3)
	if err != nil {
		log.Printf("[ShiftService] failed to load gps logs for shift %s: %v", shift.ID, err)
		return nil
	}
	if len(logs) < 3 {
		return nil
	}
	for _, v := range logs {
		if v {
			return nil
		}
	}

	fine := s.geofenceFineAmount()
	if s.transactionRepo != nil {
		if err := s.transactionRepo.RunInTx(func(tx *sql.Tx) error {
			if err := s.transactionRepo.UpdateBalance(tx, executorID, -fine); err != nil {
				return err
			}
			return s.transactionRepo.CreateTransaction(tx, &repository.Transaction{
				UserID: executorID,
				Type:   string(repository.TransactionTypeFine),
				Amount: fine,
			})
		}); err != nil {
			log.Printf("[ShiftService] failed to charge fine for executor %s: %v", executorID, err)
		}
	}

	if err := s.shiftRepo.Penalize(shift.ID, fine); err != nil {
		log.Printf("[ShiftService] failed to penalize shift %s: %v", shift.ID, err)
	}
	return nil
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
