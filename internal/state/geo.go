package state

import "math"

const (
	earthRadiusNM = 3440.065
	degreesToRad  = math.Pi / 180
	radiansToDeg  = 180 / math.Pi
)

func DistanceBearing(fromLatitude, fromLongitude, toLatitude, toLongitude float64) (float64, float64) {
	fromLat := fromLatitude * degreesToRad
	toLat := toLatitude * degreesToRad
	deltaLat := (toLatitude - fromLatitude) * degreesToRad
	deltaLon := (toLongitude - fromLongitude) * degreesToRad

	a := math.Sin(deltaLat/2)*math.Sin(deltaLat/2) +
		math.Cos(fromLat)*math.Cos(toLat)*math.Sin(deltaLon/2)*math.Sin(deltaLon/2)
	distance := earthRadiusNM * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	y := math.Sin(deltaLon) * math.Cos(toLat)
	x := math.Cos(fromLat)*math.Sin(toLat) - math.Sin(fromLat)*math.Cos(toLat)*math.Cos(deltaLon)
	bearing := math.Mod(math.Atan2(y, x)*radiansToDeg+360, 360)
	return distance, bearing
}
