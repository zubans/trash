package service

import (
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
	geocoder  *Geocoder
	// In-memory cache & mutex lock for fast cooldown checks
	cooldownMap sync.Map
}

func NewExecutorGeoService(geoRepo repository.ExecutorGeoRepository, orderRepo repository.OrderRepository, geocoder *Geocoder) *ExecutorGeoService {
	return &ExecutorGeoService{
		geoRepo:   geoRepo,
		orderRepo: orderRepo,
		geocoder:  geocoder,
	}
}

type SetLocationRequest struct {
	Lat      float64 `json:"lat"`
	Lon      float64 `json:"lon"`
	IsManual bool    `json:"is_manual"`
}

type SetLocationResponse struct {
	Success                 bool     `json:"success"`
	Message                 string   `json:"message,omitempty"`
	CooldownRemainingSeconds int      `json:"cooldown_remaining_seconds,omitempty"`
	Lat                     float64  `json:"lat"`
	Lon                     float64  `json:"lon"`
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

func (s *ExecutorGeoService) SetLocation(executorID uuid.UUID, req SetLocationRequest) (*SetLocationResponse, error) {
	if req.Lat < -90 || req.Lat > 90 || req.Lon < -180 || req.Lon > 180 {
		return nil, fmt.Errorf("invalid coordinates")
	}

	oldLat, oldLon, lastManual, err := s.geoRepo.GetExecutorLocation(executorID)
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

	if req.IsManual && oldLat != nil && oldLon != nil {
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
					_ = s.geoRepo.CreateGeoAlert(&repository.GeoAlert{
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

	if err := s.geoRepo.UpdateExecutorLocation(executorID, req.Lat, req.Lon, req.IsManual); err != nil {
		return nil, err
	}

	if req.IsManual && shiftDist > acceptRadiusKM {
		s.cooldownMap.Store(executorID, now)
	}

	return &SetLocationResponse{
		Success: true,
		Message: "Координаты успешно обновлены",
		Lat:     req.Lat,
		Lon:     req.Lon,
	}, nil
}

func (s *ExecutorGeoService) GetMapOrders(executorID uuid.UUID, lat, lon float64) ([]repository.MapOrder, error) {
	// Find pending orders within 10km
	const overviewRadiusKM = 10.0
	acceptRadiusKM := getAcceptRadiusKM()

	// Use existing orderRepo to get searching orders
	pendingOrders, err := s.orderRepo.GetPendingOrders()
	if err != nil {
		return nil, err
	}

	var mapOrders []repository.MapOrder

	for _, o := range pendingOrders {
		var oLat, oLon float64
		if o.PickupLat != nil && o.PickupLon != nil {
			oLat = *o.PickupLat
			oLon = *o.PickupLon
		} else if s.geocoder != nil && o.Address != nil && *o.Address != "" {
			geo, err := s.geocoder.Geocode(*o.Address)
			if err == nil {
				oLat = geo.Lat
				oLon = geo.Lon
			} else {
				continue
			}
		} else {
			continue
		}

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

func (s *ExecutorGeoService) GetGeoAlerts(status string, limit, offset int) ([]repository.GeoAlert, error) {
	return s.geoRepo.GetGeoAlerts(status, limit, offset)
}
