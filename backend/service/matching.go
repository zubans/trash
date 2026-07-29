package service

import (
	"database/sql"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/repository"
)

// MatchingService matches searching orders with active executors.
type MatchingService struct {
	orderRepo repository.OrderRepository
	shiftRepo repository.ShiftRepository
	db        *sql.DB
}

// NewMatchingService creates a new MatchingService.
func NewMatchingService(orderRepo repository.OrderRepository, shiftRepo repository.ShiftRepository, db *sql.DB) *MatchingService {
	return &MatchingService{
		orderRepo: orderRepo,
		shiftRepo: shiftRepo,
		db:        db,
	}
}

// StartMatchingWorker starts a background loop that runs matching periodically.
func (s *MatchingService) StartMatchingWorker(interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			if err := s.MatchOrders(); err != nil {
				log.Printf("[MatchingWorker] Error: %v", err)
			}
		}
	}()
	log.Printf("[MatchingWorker] Started background matching every %v", interval)
}

// MatchOrders executes the matching cycle.
func (s *MatchingService) MatchOrders() error {
	// 1. Get all searching orders
	orders, err := s.orderRepo.GetPendingOrders()
	if err != nil {
		return err
	}
	if len(orders) == 0 {
		return nil
	}

	// 2. Fetch all geozones
	rows, err := s.db.Query(`SELECT id, name, type, center_latitude, center_longitude, radius_meters, coordinates FROM geozones`)
	if err != nil {
		return err
	}
	defer rows.Close()

	var zones []*repository.Geozone
	for rows.Next() {
		var g repository.Geozone
		var coordinatesRaw []byte
		err := rows.Scan(&g.ID, &g.Name, &g.Type, &g.CenterLatitude, &g.CenterLongitude, &g.RadiusMeters, &coordinatesRaw)
		if err != nil {
			return err
		}
		if coordinatesRaw != nil {
			cStr := string(coordinatesRaw)
			g.Coordinates = &cStr
		}
		zones = append(zones, &g)
	}

	// 3. Fetch all active shifts
	activeShifts, err := s.shiftRepo.GetActiveShifts()
	if err != nil {
		return err
	}
	if len(activeShifts) == 0 {
		return nil
	}

	// Build active executors map: executorID -> shift
	activeExecutors := make(map[uuid.UUID]*repository.Shift)
	for _, shift := range activeShifts {
		activeExecutors[shift.ExecutorID] = shift
	}

	// 4. Match each order
	for _, order := range orders {
		// Prefer order pickup coordinates; fall back to customer profile last_geo.
		var lat, lon float64
		hasCoords := false
		if order.PickupLat != nil && order.PickupLon != nil {
			lat = *order.PickupLat
			lon = *order.PickupLon
			hasCoords = true
		} else {
			var lastGeo string
			err = s.db.QueryRow(`SELECT last_geo FROM customer_profiles WHERE user_id = $1`, order.CustomerID).Scan(&lastGeo)
			if err != nil && err != sql.ErrNoRows {
				log.Printf("[MatchingWorker] Failed to query last_geo for customer %s: %v", order.CustomerID, err)
				continue
			}
			if lastGeo != "" {
				parts := strings.Split(lastGeo, ",")
				if len(parts) == 2 {
					var err1, err2 error
					lat, err1 = strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
					lon, err2 = strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
					hasCoords = err1 == nil && err2 == nil
				}
			}
		}

		// Resolve Customer Geozone ID (default to Geozone 1 if empty or unparseable)
		geozoneID := 1
		if hasCoords {
			for _, zone := range zones {
				isInside := false
				if zone.Type == "CIRCLE" {
					if zone.CenterLatitude != nil && zone.CenterLongitude != nil && zone.RadiusMeters != nil {
						isInside = IsWithinRadius(lat, lon, *zone.CenterLatitude, *zone.CenterLongitude, int(*zone.RadiusMeters))
					}
				} else if zone.Type == "POLYGON" {
					if zone.Coordinates != nil {
						poly, err := parsePolygon(*zone.Coordinates)
						if err == nil {
							isInside = IsPointInPolygon(Point{Lat: lat, Lon: lon}, poly)
						}
					}
				}
				if isInside {
					geozoneID = zone.ID
					break
				}
			}
		}

		// Find executor in the same geozone who is active and does not have an assigned order
		var matchedExecutorID uuid.UUID
		for execID := range activeExecutors {
			// Check executor work area geozone
			var execWorkAreaID int
			err = s.db.QueryRow(`SELECT work_area_id FROM executor_profiles WHERE user_id = $1`, execID).Scan(&execWorkAreaID)
			if err != nil {
				continue
			}

			if execWorkAreaID == geozoneID {
				// Check if this executor already has an assigned order
				var hasAssigned bool
				err = s.db.QueryRow(`
					SELECT EXISTS(
						SELECT 1 FROM orders
						WHERE executor_id = $1 AND status = 'ASSIGNED'
					)`, execID).Scan(&hasAssigned)

				if err == nil && !hasAssigned {
					matchedExecutorID = execID
					break
				}
			}
		}

		if matchedExecutorID != uuid.Nil {
			err = s.orderRepo.AssignOrder(order.ID, matchedExecutorID)
			if err != nil {
				log.Printf("[MatchingWorker] Error assigning order %s to executor %s: %v", order.ID, matchedExecutorID, err)
			} else {
				log.Printf("[MatchingWorker] Matched order %s with executor %s in Zone %d", order.ID, matchedExecutorID, geozoneID)
			}
		}
	}

	return nil
}
