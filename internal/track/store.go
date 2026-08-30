package track

import (
	"container/list"
	"errors"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/j4v3l/SkyFeed/internal/domain"
)

const (
	DefaultRetention      = 15 * time.Minute
	DefaultSampleInterval = 5 * time.Second
	DefaultMaxPoints      = 180
	DefaultMaxAircraft    = 5_000
	defaultMaxPlots       = 64
)

var ErrNotFound = errors.New("aircraft track not found")

type Point struct {
	At             time.Time
	DistanceNM     float64
	BearingDegrees float64
	Latitude       float64
	Longitude      float64
	HasPosition    bool
	AltitudeFeet   int
	HasAltitude    bool
}

type Summary struct {
	ICAO                 string
	Points               int
	From                 time.Time
	To                   time.Time
	ClosestApproachNM    float64
	HasClosestApproach   bool
	AltitudeChangeFeet   int
	HasAltitudeChange    bool
	Direction            string
	LatestDistanceNM     float64
	LatestBearingDegrees float64
}

type record struct {
	points     []Point
	lastSeen   time.Time
	generation uint64
	element    *list.Element
	plot       []byte
	plotAt     time.Time
	plotKey    time.Time
}

type Store struct {
	mu             sync.Mutex
	records        map[string]*record
	order          *list.List
	retention      time.Duration
	sampleInterval time.Duration
	maxPoints      int
	maxAircraft    int
	plotTTL        time.Duration
	clock          func() time.Time
	generation     uint64
	nextSample     time.Time
}

func NewStore() *Store {
	return &Store{
		records:        make(map[string]*record),
		order:          list.New(),
		retention:      DefaultRetention,
		sampleInterval: DefaultSampleInterval,
		maxPoints:      DefaultMaxPoints,
		maxAircraft:    DefaultMaxAircraft,
		plotTTL:        30 * time.Second,
		clock:          time.Now,
	}
}

func (store *Store) Observe(snapshot *domain.Snapshot) {
	if snapshot == nil {
		return
	}
	now := snapshot.PublishedAt
	if now.IsZero() {
		now = store.clock().UTC()
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if !store.nextSample.IsZero() && now.Before(store.nextSample) {
		return
	}
	store.nextSample = now.Add(store.sampleInterval)
	store.pruneLocked(now)
	store.generation++
	for _, aircraft := range snapshot.Aircraft {
		icao := strings.ToUpper(strings.TrimSpace(aircraft.ICAO))
		if icao == "" || !aircraft.HasDistance {
			continue
		}
		recordValue, exists := store.records[icao]
		if !exists {
			if len(store.records) >= store.maxAircraft {
				oldest := store.order.Back()
				if oldest == nil {
					continue
				}
				oldICAO := oldest.Value.(string)
				oldRecord := store.records[oldICAO]
				if oldRecord == nil || oldRecord.generation == store.generation {
					continue
				}
				store.deleteLocked(oldICAO, oldRecord)
			}
			recordValue = &record{points: make([]Point, 0, min(store.maxPoints, 32))}
			recordValue.element = store.order.PushFront(icao)
			store.records[icao] = recordValue
		} else {
			store.order.MoveToFront(recordValue.element)
		}
		recordValue.generation = store.generation
		recordValue.lastSeen = now
		recordValue.points = append(recordValue.points, Point{
			At:             now,
			DistanceNM:     aircraft.DistanceNM,
			BearingDegrees: normalizeBearing(aircraft.BearingDegrees),
			Latitude:       aircraft.Latitude,
			Longitude:      aircraft.Longitude,
			HasPosition:    aircraft.HasPosition,
			AltitudeFeet:   aircraft.AltitudeFeet,
			HasAltitude:    aircraft.HasAltitude,
		})
		if len(recordValue.points) > store.maxPoints {
			copy(recordValue.points, recordValue.points[len(recordValue.points)-store.maxPoints:])
			recordValue.points = recordValue.points[:store.maxPoints]
		}
		recordValue.plot = nil
	}
}

func (store *Store) Summary(icao string) (Summary, error) {
	points, err := store.points(icao)
	if err != nil {
		return Summary{}, err
	}
	return summarize(strings.ToUpper(strings.TrimSpace(icao)), points), nil
}

func (store *Store) Len() int {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.pruneLocked(store.clock().UTC())
	return len(store.records)
}

func (store *Store) points(icao string) ([]Point, error) {
	icao = strings.ToUpper(strings.TrimSpace(icao))
	store.mu.Lock()
	defer store.mu.Unlock()
	store.pruneLocked(store.clock().UTC())
	recordValue := store.records[icao]
	if recordValue == nil || len(recordValue.points) == 0 {
		return nil, ErrNotFound
	}
	return append([]Point(nil), recordValue.points...), nil
}

func (store *Store) pruneLocked(now time.Time) {
	cutoff := now.Add(-store.retention)
	for icao, recordValue := range store.records {
		first := sort.Search(len(recordValue.points), func(index int) bool { return !recordValue.points[index].At.Before(cutoff) })
		if first > 0 {
			copy(recordValue.points, recordValue.points[first:])
			recordValue.points = recordValue.points[:len(recordValue.points)-first]
			recordValue.plot = nil
		}
		if recordValue.lastSeen.Before(cutoff) || len(recordValue.points) == 0 {
			store.deleteLocked(icao, recordValue)
		}
	}
}

func (store *Store) deleteLocked(icao string, recordValue *record) {
	delete(store.records, icao)
	if recordValue != nil && recordValue.element != nil {
		store.order.Remove(recordValue.element)
	}
}

func summarize(icao string, points []Point) Summary {
	result := Summary{ICAO: icao, Points: len(points), From: points[0].At, To: points[len(points)-1].At}
	result.ClosestApproachNM = points[0].DistanceNM
	result.HasClosestApproach = true
	for _, point := range points[1:] {
		if point.DistanceNM < result.ClosestApproachNM {
			result.ClosestApproachNM = point.DistanceNM
		}
	}
	latest := points[len(points)-1]
	result.LatestDistanceNM = latest.DistanceNM
	result.LatestBearingDegrees = latest.BearingDegrees
	for left := 0; left < len(points); left++ {
		if !points[left].HasAltitude {
			continue
		}
		for right := len(points) - 1; right > left; right-- {
			if points[right].HasAltitude {
				result.AltitudeChangeFeet = points[right].AltitudeFeet - points[left].AltitudeFeet
				result.HasAltitudeChange = true
				left = len(points)
				break
			}
		}
	}
	result.Direction = directionSummary(points)
	return result
}

func directionSummary(points []Point) string {
	if len(points) < 2 {
		return "insufficient samples"
	}
	first, latest := points[0], points[len(points)-1]
	if first.HasPosition && latest.HasPosition {
		movedNM := geographicDistanceNM(first.Latitude, first.Longitude, latest.Latitude, latest.Longitude)
		if movedNM >= 0.25 {
			return "tracking " + compass(geographicBearing(first.Latitude, first.Longitude, latest.Latitude, latest.Longitude))
		}
	}
	delta := points[len(points)-1].DistanceNM - points[0].DistanceNM
	switch {
	case delta < -0.5:
		return "approaching from " + compass(points[0].BearingDegrees)
	case delta > 0.5:
		return "departing toward " + compass(points[len(points)-1].BearingDegrees)
	default:
		return "crossing near " + compass(points[len(points)-1].BearingDegrees)
	}
}

func geographicDistanceNM(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadiusNM = 3440.065
	lat1Radians, lat2Radians := lat1*math.Pi/180, lat2*math.Pi/180
	deltaLat := (lat2 - lat1) * math.Pi / 180
	deltaLon := (lon2 - lon1) * math.Pi / 180
	a := math.Sin(deltaLat/2)*math.Sin(deltaLat/2) + math.Cos(lat1Radians)*math.Cos(lat2Radians)*math.Sin(deltaLon/2)*math.Sin(deltaLon/2)
	return earthRadiusNM * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

func geographicBearing(lat1, lon1, lat2, lon2 float64) float64 {
	lat1Radians, lat2Radians := lat1*math.Pi/180, lat2*math.Pi/180
	deltaLon := (lon2 - lon1) * math.Pi / 180
	y := math.Sin(deltaLon) * math.Cos(lat2Radians)
	x := math.Cos(lat1Radians)*math.Sin(lat2Radians) - math.Sin(lat1Radians)*math.Cos(lat2Radians)*math.Cos(deltaLon)
	return normalizeBearing(math.Atan2(y, x) * 180 / math.Pi)
}

func compass(degrees float64) string {
	directions := [...]string{"N", "NE", "E", "SE", "S", "SW", "W", "NW"}
	return directions[int((normalizeBearing(degrees)+22.5)/45)%len(directions)]
}

func normalizeBearing(value float64) float64 {
	for value < 0 {
		value += 360
	}
	for value >= 360 {
		value -= 360
	}
	return value
}
