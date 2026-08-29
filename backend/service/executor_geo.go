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
	// Optional. When wired, the map applies the same customer-verification gate
	// as the executor order list, honouring the show_unverified_customer_orders
	// flag, so the map and the list never disagree about which orders are shown.
	userRepo     repository.UserRepository
	settingsRepo repository.SettingsRepository
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
// customer-verification visibility rule as the executor order list.
func (s *ExecutorGeoService) WithEligibility(userRepo repository.UserRepository, settingsRepo repository.SettingsRepository) *ExecutorGeoService {
	s.userRepo = userRepo
	s.settingsRepo = settingsRepo
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

	// Use existing orderRepo to get searching orders
	pendingOrders, err := s.orderRepo.GetPendingOrders(ctx)
	if err != nil {
		return nil, err
	}

	// Admin toggle: same rule the executor order list uses. Off by default, so
	// unverified customers' orders are hidden on the map too and the two surfaces
	// agree. On -> show every customer's orders.
	showUnverified := false
	if s.settingsRepo != nil {
		if all, err := s.settingsRepo.GetSettings(ctx); err == nil {
			showUnverified = showUnverifiedCustomerOrders(all)
		}
	}

	var mapOrders []repository.MapOrder

	for _, o := range pendingOrders {
		// Only orders that already carry coordinates are considered. Resolving
		// them here would put a network call inside the loop; coordinate capture
		// at order creation and the backfill worker fill them instead.
		if o.PickupLat == nil || o.PickupLon == nil {
			continue
		}

		// Hide orders from customers who are not manually verified, unless the
		// admin flag is on — mirroring the executor order list.
		if !showUnverified && s.userRepo != nil {
			customer, err := s.userRepo.FindByID(ctx, o.CustomerID)
			if err == nil && customer != nil && !customer.IsVerified() {
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

func (s *ExecutorGeoService) GetGeoAlerts(ctx context.Context, status string, limit, offset int) ([]repository.GeoAlert, error) {
	return s.geoRepo.GetGeoAlerts(ctx, status, limit, offset)
}
