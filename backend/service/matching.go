package service

import (
	"context"
	"log"
	"strconv"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/metrics"
	"healthlogin/backend/repository"
)

// defaultAutoMatchRadiusKM bounds automatic assignment. It matches the radius
// the executor map shows orders within, so the worker can only hand out orders
// the executor would have seen and could plausibly travel to.
const defaultAutoMatchRadiusKM = 10.0

// MatchingService matches searching orders with active executors.
type MatchingService struct {
	orderRepo    repository.OrderRepository
	shiftRepo    repository.ShiftRepository
	userRepo     repository.UserRepository
	catalogRepo  repository.ServiceCatalogRepository
	geoRepo      repository.ExecutorGeoRepository
	settingsRepo repository.SettingsRepository
}

// NewMatchingService creates a new MatchingService.
func NewMatchingService(orderRepo repository.OrderRepository, shiftRepo repository.ShiftRepository, userRepo repository.UserRepository, catalogRepo repository.ServiceCatalogRepository) *MatchingService {
	return &MatchingService{
		orderRepo:   orderRepo,
		shiftRepo:   shiftRepo,
		userRepo:    userRepo,
		catalogRepo: catalogRepo,
	}
}

// WithGeo attaches the stores automatic matching needs to bound assignment by
// distance. Without them the worker cannot tell how far an executor is from an
// order, and refuses to assign rather than guessing.
func (s *MatchingService) WithGeo(geoRepo repository.ExecutorGeoRepository, settingsRepo repository.SettingsRepository) *MatchingService {
	s.geoRepo = geoRepo
	s.settingsRepo = settingsRepo
	return s
}

// autoMatchRadiusKM reads the configured bound, falling back to the default.
func (s *MatchingService) autoMatchRadiusKM(ctx context.Context) float64 {
	if s.settingsRepo == nil {
		return defaultAutoMatchRadiusKM
	}
	settings, err := s.settingsRepo.GetSettings(ctx)
	if err != nil {
		return defaultAutoMatchRadiusKM
	}
	if v, ok := settings["auto_match_radius_km"]; ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			return f
		}
	}
	return defaultAutoMatchRadiusKM
}

// withinAutoMatchRadius reports whether an order is close enough to an executor
// to be assigned automatically.
//
// An unknown position is a "no", never a free pass. Letting an unlocatable
// executor through is how an order ends up assigned to somebody on the other
// side of the country, who can only cancel it.
func (s *MatchingService) withinAutoMatchRadius(ctx context.Context, executorID uuid.UUID, order *repository.Order, radiusKM float64) bool {
	if order.PickupLat == nil || order.PickupLon == nil {
		return false
	}
	if s.geoRepo == nil {
		return false
	}
	execLat, execLon, _, err := s.geoRepo.GetExecutorLocation(ctx, executorID)
	if err != nil {
		log.Printf("[MatchingWorker] failed to read location for executor %s: %v", executorID, err)
		return false
	}
	if execLat == nil || execLon == nil {
		return false
	}
	return HaversineDistanceKM(*order.PickupLat, *order.PickupLon, *execLat, *execLon) <= radiusKM
}

// StartMatchingWorker starts a background loop that runs matching periodically.
func (s *MatchingService) StartMatchingWorker(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			if err := metrics.TrackWorker("matching", func() error { return s.MatchOrders(ctx) }); err != nil {
				log.Printf("[MatchingWorker] Error: %v", err)
			}
		}
	}()
	log.Printf("[MatchingWorker] Started background matching every %v", interval)
}

// executorEligible re-uses the shared executor/variant rules so automatic
// matching cannot hand out an order that the executor is not allowed to take.
func (s *MatchingService) executorEligible(ctx context.Context, executorID, variantID uuid.UUID) bool {
	if s.userRepo == nil || s.catalogRepo == nil {
		return true
	}
	executor, err := s.userRepo.FindByID(ctx, executorID)
	if err != nil {
		return false
	}
	variant, err := s.catalogRepo.GetNodeByID(ctx, variantID)
	if err != nil {
		return false
	}
	return canExecutorTakeOrder(executor, variant) == nil
}

// MatchOrders executes the matching cycle.
func (s *MatchingService) MatchOrders(ctx context.Context) error {
	// 1. Get all searching orders
	orders, err := s.orderRepo.GetPendingOrders(ctx)
	if err != nil {
		return err
	}
	if len(orders) == 0 {
		metrics.SetMarketplaceDepth(0, 0)
		return nil
	}

	// 2. Fetch all active shifts
	activeShifts, err := s.shiftRepo.GetActiveShifts(ctx)
	if err != nil {
		return err
	}
	metrics.SetMarketplaceDepth(len(orders), len(activeShifts))
	if len(activeShifts) == 0 {
		return nil
	}

	// Build active executors map: executorID -> shift
	activeExecutors := make(map[uuid.UUID]*repository.Shift)
	for _, shift := range activeShifts {
		activeExecutors[shift.ExecutorID] = shift
	}

	// 3. Match each order
	radiusKM := s.autoMatchRadiusKM(ctx)
	for _, order := range orders {
		// Assign orders only from verified customers if customer exists
		if s.userRepo != nil {
			if customer, err := s.userRepo.FindByID(ctx, order.CustomerID); err == nil && customer != nil && !customer.IsVerified() {
				continue
			}
		}

		var matchedExecutorID uuid.UUID
		for execID := range activeExecutors {
			if !s.executorEligible(ctx, execID, order.ServiceVariantID) {
				continue
			}
			if execID == order.CustomerID {
				continue
			}
			if !s.withinAutoMatchRadius(ctx, execID, order, radiusKM) {
				continue
			}

			// One assigned order at a time: an executor already carrying one is
			// not a candidate.
			assigned, err := s.orderRepo.CountActiveOrdersByExecutor(ctx, execID)
			if err != nil {
				log.Printf("[MatchingWorker] failed to count assigned orders for executor %s: %v", execID, err)
				continue
			}
			if assigned == 0 {
				matchedExecutorID = execID
				break
			}
		}

		if matchedExecutorID != uuid.Nil {
			err = s.orderRepo.Assign(ctx, nil, order.ID, matchedExecutorID)
			if err != nil {
				metrics.MatchingAssignment("error")
				log.Printf("[MatchingWorker] Error assigning order %s to executor %s: %v", order.ID, matchedExecutorID, err)
			} else {
				metrics.MatchingAssignment("assigned")
				metrics.OrderEvent("assigned")
				log.Printf("[MatchingWorker] Matched order %s with executor %s", order.ID, matchedExecutorID)
			}
		}
	}

	return nil
}
