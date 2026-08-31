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
	// leaderGuard, when set, runs a cycle only on the process holding the
	// matching job's lock. See WithLeaderGuard.
	leaderGuard func(func() error) error
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

// SettingAutoMatchingEnabled is the system_settings key that turns automatic
// assignment on or off. It defaults to OFF: with it off, orders are only ever
// taken by an executor pressing "take", never assigned by the worker.
const SettingAutoMatchingEnabled = "auto_matching_enabled"

// autoMatchingEnabled reports whether the worker may assign orders. Off unless an
// admin explicitly turns it on, so the default behaviour is manual-only.
func (s *MatchingService) autoMatchingEnabled(ctx context.Context) bool {
	if s.settingsRepo == nil {
		return false
	}
	settings, err := s.settingsRepo.GetSettings(ctx)
	if err != nil {
		return false
	}
	return settings[SettingAutoMatchingEnabled] == "1"
}

// withinAutoMatchRadius reports whether an order is close enough to an executor
// to be assigned automatically.
//
// An unknown position is a "no", never a free pass. Letting an unlocatable
// executor through is how an order ends up assigned to somebody on the other
// side of the country, who can only cancel it. The positions are loaded once
// per cycle rather than per candidate, so a missing entry in the map is exactly
// that unknown position.
func withinAutoMatchRadius(position repository.ExecutorPosition, known bool, order *repository.Order, radiusKM float64) bool {
	if !known {
		return false
	}
	if order.PickupLat == nil || order.PickupLon == nil {
		return false
	}
	return HaversineDistanceKM(*order.PickupLat, *order.PickupLon, position.Lat, position.Lon) <= radiusKM
}

// WithLeaderGuard makes the matching worker run at most once across every
// process. The guard comes from the caller (worker.Leader) rather than being
// built here, because this package must not depend on the worker package.
//
// Without it, two processes would each assign the same waiting orders, and an
// order can only be assigned once — the loser logs an error every cycle.
func (s *MatchingService) WithLeaderGuard(guard func(func() error) error) *MatchingService {
	s.leaderGuard = guard
	return s
}

// runGuarded runs one matching cycle, under the guard when one is wired.
func (s *MatchingService) runGuarded(ctx context.Context) error {
	job := func() error { return s.MatchOrders(ctx) }
	if s.leaderGuard == nil {
		return job()
	}
	return s.leaderGuard(job)
}

// StartMatchingWorker starts a background loop that runs matching periodically.
func (s *MatchingService) StartMatchingWorker(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			if err := metrics.TrackWorker("matching", func() error { return s.runGuarded(ctx) }); err != nil {
				log.Printf("[MatchingWorker] Error: %v", err)
			}
		}
	}()
	log.Printf("[MatchingWorker] Started background matching every %v", interval)
}

// matchingRound holds everything one matching cycle needs, loaded up front.
//
// The worker compares every waiting order against every executor on shift. When
// each of those comparisons went to the database for the executor, the service
// variant, the position and the assigned-order count, a cycle cost roughly four
// queries per pair — which grows as the product of orders and executors, on a
// five-second timer. Loading each of those sets once turns the comparison into
// arithmetic.
type matchingRound struct {
	users    map[uuid.UUID]*repository.User
	variants map[uuid.UUID]*repository.ServiceNode
	// positions holds only executors with a stored position; absence means
	// unknown, which is never eligible.
	positions map[uuid.UUID]repository.ExecutorPosition
	// activeOrders counts assigned orders per executor. It is updated as the
	// cycle assigns, so an executor cannot be handed a second order by a later
	// iteration of the same cycle.
	activeOrders map[uuid.UUID]int
}

// loadRound fetches the round's inputs in a fixed number of queries.
func (s *MatchingService) loadRound(ctx context.Context, orders []*repository.Order, executorIDs []uuid.UUID) (*matchingRound, error) {
	round := &matchingRound{
		users:        map[uuid.UUID]*repository.User{},
		variants:     map[uuid.UUID]*repository.ServiceNode{},
		positions:    map[uuid.UUID]repository.ExecutorPosition{},
		activeOrders: map[uuid.UUID]int{},
	}

	// Customers and executors come from the same table, so they are one lookup.
	userIDs := make([]uuid.UUID, 0, len(orders)+len(executorIDs))
	variantIDs := make([]uuid.UUID, 0, len(orders))
	for _, o := range orders {
		userIDs = append(userIDs, o.CustomerID)
		variantIDs = append(variantIDs, o.ServiceVariantID)
	}
	userIDs = append(userIDs, executorIDs...)

	if s.userRepo != nil {
		users, err := s.userRepo.FindByIDs(ctx, userIDs)
		if err != nil {
			return nil, err
		}
		round.users = users
	}
	if s.catalogRepo != nil {
		variants, err := s.catalogRepo.GetNodesByIDs(ctx, variantIDs)
		if err != nil {
			return nil, err
		}
		round.variants = variants
	}
	if s.geoRepo != nil {
		positions, err := s.geoRepo.GetExecutorLocations(ctx, executorIDs)
		if err != nil {
			return nil, err
		}
		round.positions = positions
	}
	counts, err := s.orderRepo.CountActiveOrdersByExecutors(ctx, executorIDs)
	if err != nil {
		return nil, err
	}
	round.activeOrders = counts

	return round, nil
}

// executorEligible re-uses the shared visibility/accept predicate so automatic
// matching cannot hand out an order that the executor is not allowed to take —
// including moderator-only orders (moderators only) and the customer-verification
// segmentation.
//
// The inputs come from the pre-loaded round; the predicate is the same one the
// map, the order list and the accept path use.
func (s *MatchingService) executorEligible(round *matchingRound, executorID uuid.UUID, order *repository.Order) bool {
	if s.userRepo == nil || s.catalogRepo == nil {
		return true
	}
	executor, ok := round.users[executorID]
	if !ok {
		return false
	}
	variant, ok := round.variants[order.ServiceVariantID]
	if !ok {
		return false
	}
	return canViewOrTakeOrder(executor, round.users[order.CustomerID], variant) == nil
}

// MatchOrders executes the matching cycle.
func (s *MatchingService) MatchOrders(ctx context.Context) error {
	// Automatic assignment is opt-in. While it is off (the default), the worker
	// does nothing and orders are taken only by an executor pressing "take".
	if !s.autoMatchingEnabled(ctx) {
		return nil
	}

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

	// Candidate executors: one entry per active shift.
	executorIDs := make([]uuid.UUID, 0, len(activeShifts))
	for _, shift := range activeShifts {
		executorIDs = append(executorIDs, shift.ExecutorID)
	}

	// Everything the pairing below needs, loaded once for the whole cycle.
	round, err := s.loadRound(ctx, orders, executorIDs)
	if err != nil {
		return err
	}

	// 3. Match each order
	radiusKM := s.autoMatchRadiusKM(ctx)
	for _, order := range orders {
		var matchedExecutorID uuid.UUID
		for _, execID := range executorIDs {
			if execID == order.CustomerID {
				continue
			}
			// One assigned order at a time: an executor already carrying one is
			// not a candidate. Checked first because it is the cheapest test and
			// it accounts for orders assigned earlier in this same cycle.
			if round.activeOrders[execID] > 0 {
				continue
			}
			if !s.executorEligible(round, execID, order) {
				continue
			}
			position, known := round.positions[execID]
			if !withinAutoMatchRadius(position, known, order, radiusKM) {
				continue
			}

			matchedExecutorID = execID
			break
		}

		if matchedExecutorID != uuid.Nil {
			err = s.orderRepo.Assign(ctx, nil, order.ID, matchedExecutorID)
			if err != nil {
				metrics.MatchingAssignment("error")
				log.Printf("[MatchingWorker] Error assigning order %s to executor %s: %v", order.ID, matchedExecutorID, err)
			} else {
				// Only a successful assignment takes the executor out of the
				// running: a failed one leaves them free for the next order.
				round.activeOrders[matchedExecutorID]++
				metrics.MatchingAssignment("assigned")
				metrics.OrderEvent("assigned")
				log.Printf("[MatchingWorker] Matched order %s with executor %s", order.ID, matchedExecutorID)
			}
		}
	}

	return nil
}
