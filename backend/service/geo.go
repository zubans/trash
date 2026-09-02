package service

import (
	"math"
)

// IsWithinRadius проверяет, лежит ли точка внутри круга, в метрах.
func IsWithinRadius(lat1, lon1, lat2, lon2 float64, radius int) bool {
	return haversineDistance(lat1, lon1, lat2, lon2) <= float64(radius)
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
