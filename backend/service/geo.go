package service

import (
	"encoding/json"
	"math"
)

// Point represents a geographic coordinate.
type Point struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

// IsWithinRadius checks whether a point is within a circle.
func IsWithinRadius(lat1, lon1, lat2, lon2 float64, radius int) bool {
	return haversineDistance(lat1, lon1, lat2, lon2) <= float64(radius)
}

// IsPointInPolygon checks whether a point is inside a polygon using ray-casting.
func IsPointInPolygon(p Point, polygon []Point) bool {
	inside := false
	j := len(polygon) - 1
	for i := 0; i < len(polygon); i++ {
		if (polygon[i].Lon > p.Lon) != (polygon[j].Lon > p.Lon) &&
			p.Lat < (polygon[j].Lat-polygon[i].Lat)*(p.Lon-polygon[i].Lon)/(polygon[j].Lon-polygon[i].Lon)+polygon[i].Lat {
			inside = !inside
		}
		j = i
	}
	return inside
}

func parsePolygon(raw string) ([]Point, error) {
	var points []Point
	if err := json.Unmarshal([]byte(raw), &points); err != nil {
		return nil, err
	}
	return points, nil
}

func haversineDistance(lat1, lon1, lat2, lon2 float64) float64 {
	const EarthRadius = 6371000.0
	dLat := (lat2 - lat1) * math.Pi / 180.0
	dLon := (lon2 - lon1) * math.Pi / 180.0
	lat1Rad := lat1 * math.Pi / 180.0
	lat2Rad := lat2 * math.Pi / 180.0
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1Rad)*math.Cos(lat2Rad)*math.Sin(dLon/2)*math.Sin(dLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return EarthRadius * c
}

func HaversineDistanceKM(lat1, lon1, lat2, lon2 float64) float64 {
	return haversineDistance(lat1, lon1, lat2, lon2) / 1000.0
}
