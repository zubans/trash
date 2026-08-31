package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	"healthlogin/backend/repository"
)

type ExecutorGeoService struct {
	geoRepo   repository.ExecutorGeoRepository
	orderRepo repository.OrderRepository
	// Optional. When wired, the map applies the same visibility predicate as the
	// executor order list (roles, customer verification, moderator-only), so the
	// map and the list never disagree about which orders are shown.
	userRepo     repository.UserRepository
	settingsRepo repository.SettingsRepository
	catalogRepo  repository.ServiceCatalogRepository
	// In-memory cache & mutex lock for fast cooldown checks
	cooldownMap sync.Map
}

func NewExecutorGeoService(geoRepo repository.ExecutorGeoRepository, orderRepo repository.OrderRepository) *ExecutorGeoService {
	return &ExecutorGeoService{
		geoRepo:   geoRepo,
		orderRepo: orderRepo,
	}
}

// WithEligibility wires the dependencies the map needs to apply the same
// visibility predicate as the executor order list.
func (s *ExecutorGeoService) WithEligibility(userRepo repository.UserRepository, settingsRepo repository.SettingsRepository, catalogRepo repository.ServiceCatalogRepository) *ExecutorGeoService {
	s.userRepo = userRepo
	s.settingsRepo = settingsRepo
	s.catalogRepo = catalogRepo
	return s
}

type SetLocationRequest struct {
	Lat      float64 `json:"lat"`
	Lon      float64 `json:"lon"`
	IsManual bool    `json:"is_manual"`
}

type SetLocationResponse struct {
	Success                  bool    `json:"success"`
	Message                  string  `json:"message,omitempty"`
	CooldownRemainingSeconds int     `json:"cooldown_remaining_seconds,omitempty"`
	Lat                      float64 `json:"lat"`
	Lon                      float64 `json:"lon"`
}

func getAcceptRadiusKM() float64 {
	valStr := os.Getenv("ACCEPT_RADIUS_KM")
	if valStr == "" {
		return 0.5
	}
	var val float64
	if _, err := fmt.Sscanf(valStr, "%f", &val); err != nil || val <= 0 {
		return 0.5
	}
	return val
}

func (s *ExecutorGeoService) SetLocation(ctx context.Context, executorID uuid.UUID, req SetLocationRequest) (*SetLocationResponse, error) {
	if req.Lat < -90 || req.Lat > 90 || req.Lon < -180 || req.Lon > 180 {
		return nil, fmt.Errorf("invalid coordinates")
	}

	oldLat, oldLon, lastManual, err := s.geoRepo.GetExecutorLocation(ctx, executorID)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	acceptRadiusKM := getAcceptRadiusKM()

	// Check manual shift distance
	var shiftDist float64
	if oldLat != nil && oldLon != nil {
		shiftDist = HaversineDistanceKM(*oldLat, *oldLon, req.Lat, req.Lon)
	}

	// Whether a move counts as "manual" is decided here, from the distance
	// travelled, and not by a flag the client sends: an executor could
	// otherwise bypass the cooldown by flipping is_manual to false.
	isManual := req.IsManual || shiftDist > acceptRadiusKM

	if isManual && oldLat != nil && oldLon != nil {
		// Reject manual moves within inner circle
		if shiftDist <= acceptRadiusKM {
			return &SetLocationResponse{
				Success: false,
				Message: fmt.Sprintf("Ручное перемещение разрешено только за пределы разрешенного круга (более %.1f км)", acceptRadiusKM),
				Lat:     *oldLat,
				Lon:     *oldLon,
			}, nil
		}

		// District change requires 10 min cooldown
		var lastManualTime time.Time
		if val, ok := s.cooldownMap.Load(executorID); ok {
			lastManualTime = val.(time.Time)
		} else if lastManual != nil {
			lastManualTime = *lastManual
		}

		if !lastManualTime.IsZero() {
			elapsed := now.Sub(lastManualTime)
			if elapsed < 10*time.Minute {
				remaining := int((10*time.Minute - elapsed).Seconds())
				return &SetLocationResponse{
					Success:                  false,
					Message:                  fmt.Sprintf("Смена района возможна не чаще 1 раза в 10 минут. Осталось: %d сек", remaining),
					CooldownRemainingSeconds: remaining,
					Lat:                      *oldLat,
					Lon:                      *oldLon,
				}, nil
			}
		}
	}

	// Async Goroutine: Deep Geo-Validation for Speed Spoofing Check
	if oldLat != nil && oldLon != nil && shiftDist > 2.0 {
		go func(exID uuid.UUID, oLat, oLon, nLat, nLon float64, tNow time.Time) {
			var lastTime time.Time
			if lastManual != nil {
				lastTime = *lastManual
			} else {
				lastTime = tNow.Add(-1 * time.Minute)
			}
			hours := tNow.Sub(lastTime).Hours()
			if hours > 0 {
				speed := shiftDist / hours
				if speed > 150.0 {
					// GPS Spoofing detected! Log GeoAlert for Admin
					_ = s.geoRepo.CreateGeoAlert(ctx, &repository.GeoAlert{
						ExecutorID:         exID,
						OldLat:             &oLat,
						OldLon:             &oLon,
						NewLat:             nLat,
						NewLon:             nLon,
						CalculatedSpeedKMH: speed,
						Status:             "PENDING",
					})
				}
			}
		}(executorID, *oldLat, *oldLon, req.Lat, req.Lon, now)
	}

	if err := s.geoRepo.UpdateExecutorLocation(ctx, executorID, req.Lat, req.Lon, isManual); err != nil {
		return nil, err
	}

	if isManual && shiftDist > acceptRadiusKM {
		s.cooldownMap.Store(executorID, now)
	}

	return &SetLocationResponse{
		Success: true,
		Message: "Координаты успешно обновлены",
		Lat:     req.Lat,
		Lon:     req.Lon,
	}, nil
}

// RecordLiveLocation stores a position the executor's app reports on its own
// during a shift.
//
// It deliberately goes through the same rules as a move made on the map: the
// passive channel would otherwise be a way around the district-change cooldown,
// since an executor could post any coordinates they liked. A move that those
// rules reject is not an error — the ping is telemetry, not a command — so the
// outcome is reported as a boolean and the caller decides what to say about it.
func (s *ExecutorGeoService) RecordLiveLocation(ctx context.Context, executorID uuid.UUID, lat, lon float64) (bool, error) {
	resp, err := s.SetLocation(ctx, executorID, SetLocationRequest{Lat: lat, Lon: lon})
	if err != nil {
		return false, err
	}
	return resp.Success, nil
}

// LocationResponse reports the executor's authoritative stored position. It is
// the single source of truth the map UI centers on, so the client never has to
// guess from a possibly stale device fix.
type LocationResponse struct {
	HasLocation bool     `json:"has_location"`
	Lat         *float64 `json:"lat,omitempty"`
	Lon         *float64 `json:"lon,omitempty"`
}

// GetLocation returns the executor's own stored coordinates. Like GetMapOrders,
// the position comes from the database and is scoped to the caller, so it can
// never be used to read another executor's whereabouts.
func (s *ExecutorGeoService) GetLocation(ctx context.Context, executorID uuid.UUID) (*LocationResponse, error) {
	lat, lon, _, err := s.geoRepo.GetExecutorLocation(ctx, executorID)
	if err != nil {
		return nil, err
	}
	if lat == nil || lon == nil {
		return &LocationResponse{HasLocation: false}, nil
	}
	return &LocationResponse{HasLocation: true, Lat: lat, Lon: lon}, nil
}

// GetMapOrders returns searching orders around the executor's own stored
// position. The position deliberately comes from the database rather than from
// request parameters: with client supplied coordinates any account could sweep
// the map and harvest customer addresses country-wide.
func (s *ExecutorGeoService) GetMapOrders(ctx context.Context, executorID uuid.UUID) ([]repository.MapOrder, error) {
	lat, lon, _, err := s.geoRepo.GetExecutorLocation(ctx, executorID)
	if err != nil {
		return nil, err
	}
	if lat == nil || lon == nil {
		return nil, errors.New("местоположение исполнителя не задано")
	}
	return s.mapOrdersAround(ctx, executorID, *lat, *lon)
}

func (s *ExecutorGeoService) mapOrdersAround(ctx context.Context, executorID uuid.UUID, lat, lon float64) ([]repository.MapOrder, error) {
	// Find pending orders within 10km
	const overviewRadiusKM = 10.0
	acceptRadiusKM := getAcceptRadiusKM()

	// Bound the search at the database rather than in this loop. Reading every
	// searching order in the country and discarding all but the nearby ones made
	// the cost of this endpoint grow with the whole marketplace — on a screen
	// every executor keeps open and polls.
	pendingOrders, err := s.orderRepo.FindNearbyOrders(ctx, lat, lon, int(overviewRadiusKM*1000))
	if err != nil {
		return nil, err
	}
	if len(pendingOrders) == 0 {
		return nil, nil
	}

	// The viewer's roles and verification drive visibility, exactly like the
	// executor order list, so the map and the list never disagree.
	var viewer *repository.User
	if s.userRepo != nil {
		viewer, _ = s.userRepo.FindByID(ctx, executorID)
	}

	// Everything the predicate needs, in two queries instead of two per order.
	// The predicate itself is unchanged and still the only thing deciding
	// visibility: only how its inputs are loaded is different.
	customers, variants := s.eligibilityInputs(ctx, pendingOrders)

	var mapOrders []repository.MapOrder

	for _, o := range pendingOrders {
		// FindNearbyOrders only returns orders that carry coordinates, so the
		// dereference below is safe; the guard stays as a belt-and-braces check
		// in case the query's contract ever changes.
		if o.PickupLat == nil || o.PickupLon == nil {
			continue
		}

		// Same predicate as FindNearbyOrdersForExecutor and the accept path:
		// moderator-only orders → moderators; normal orders → customer-verification
		// segmentation plus the standard executor gates.
		if s.userRepo != nil {
			if canViewOrTakeOrder(viewer, customers[o.CustomerID], variants[o.ServiceVariantID]) != nil {
				continue
			}
		}

		oLat, oLon := *o.PickupLat, *o.PickupLon

		dist := HaversineDistanceKM(lat, lon, oLat, oLon)
		if dist <= overviewRadiusKM {
			mapOrders = append(mapOrders, repository.MapOrder{
				Order:      *o,
				CanAccept:  dist <= acceptRadiusKM,
				DistanceKM: dist,
			})
		}
	}

	return mapOrders, nil
}

// eligibilityInputs batch-loads the customers and service variants that
// canViewOrTakeOrder needs for a page of orders.
//
// A lookup failure yields a missing map entry rather than an error, matching
// what the per-order calls did before: the predicate already treats a nil
// customer or variant as "no extra restriction", and one unreadable row must
// not blank out the whole map.
func (s *ExecutorGeoService) eligibilityInputs(ctx context.Context, orders []*repository.Order) (map[uuid.UUID]*repository.User, map[uuid.UUID]*repository.ServiceNode) {
	customerIDs := make([]uuid.UUID, 0, len(orders))
	variantIDs := make([]uuid.UUID, 0, len(orders))
	for _, o := range orders {
		customerIDs = append(customerIDs, o.CustomerID)
		variantIDs = append(variantIDs, o.ServiceVariantID)
	}

	customers := map[uuid.UUID]*repository.User{}
	if s.userRepo != nil {
		if loaded, err := s.userRepo.FindByIDs(ctx, customerIDs); err == nil {
			customers = loaded
		}
	}
	variants := map[uuid.UUID]*repository.ServiceNode{}
	if s.catalogRepo != nil {
		if loaded, err := s.catalogRepo.GetNodesByIDs(ctx, variantIDs); err == nil {
			variants = loaded
		}
	}
	return customers, variants
}

func (s *ExecutorGeoService) GetGeoAlerts(ctx context.Context, status string, limit, offset int) ([]repository.GeoAlert, error) {
	return s.geoRepo.GetGeoAlerts(ctx, status, limit, offset)
}
