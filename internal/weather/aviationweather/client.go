package aviationweather

import (
	"container/list"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

const (
	DefaultBaseURL           = "https://aviationweather.gov/api/data"
	maxResponseBytes         = 1 << 20
	defaultCacheTTL          = 10 * time.Minute
	defaultNegativeTTL       = 5 * time.Minute
	defaultErrorTTL          = time.Minute
	defaultStaleTTL          = 30 * time.Minute
	defaultStationTTL        = 24 * time.Hour
	defaultCacheMax          = 500
	defaultNearbyRadiusNM    = 25.0
	defaultObservationMaxAge = 2 * time.Hour
	defaultLookupTimeout     = 8 * time.Second
	Attribution              = "Weather by aviationweather.gov"
	defaultUserAgent         = "SkyFeed/dev (github.com/j4v3l/SkyFeed)"
	stationStatusExact       = "exact"
	stationStatusAlias       = "alias"
	stationStatusNearby      = "nearby"
	stationStatusUnavailable = "unavailable"
)

type Observation struct {
	RequestedICAO        string
	ReportingICAO        string
	StationStatus        string
	StationDistanceNM    float64
	HasStationDistance   bool
	METAR                string
	TAF                  string
	FlightCategory       string
	METARStatus          string
	TAFStatus            string
	ObservedAt           time.Time
	FetchedAt            time.Time
	Attribution          string
	Stale                bool
	WindDirectionDegrees int
	WindVariable         bool
	WindSpeedKts         int
	WindGustKts          int
	HasWind              bool
	VisibilitySM         float64
	VisibilityAtLeast    bool
	VisibilityLessThan   bool
	HasVisibility        bool
	TemperatureC         int
	DewpointC            int
	HasTemperature       bool
	HasDewpoint          bool
	AltimeterInHg        float64
	HasAltimeter         bool
	Clouds               []CloudLayer
	Conditions           []string
}

type CloudLayer struct {
	Cover    string
	BaseFeet int
	HasBase  bool
}

type Client struct {
	base          *url.URL
	httpClient    *http.Client
	ttl           time.Duration
	now           func() time.Time
	lookupTimeout time.Duration
	userAgent     string

	mu       sync.Mutex
	cache    map[string]*list.Element
	order    *list.List
	max      int
	stations map[string]stationCache
	group    singleflight.Group
	limiter  *tokenBucket
}

type cachedObservation struct {
	code         string
	value        Observation
	err          error
	expires      time.Time
	staleExpires time.Time
}

type stationCache struct {
	station stationResolution
	expires time.Time
}

type stationResolution struct {
	Requested string
	Reporting string
	Status    string
	Distance  float64
	HasData   bool
	METAR     responseDTO
}

type tokenBucket struct {
	mu       sync.Mutex
	tokens   float64
	last     time.Time
	rate     float64
	capacity float64
}

func newTokenBucket(rate float64, burst int) *tokenBucket {
	return &tokenBucket{tokens: float64(burst), last: time.Now(), rate: rate, capacity: float64(burst)}
}

func (bucket *tokenBucket) wait(ctx context.Context) error {
	for {
		bucket.mu.Lock()
		now := time.Now()
		bucket.tokens = math.Min(bucket.capacity, bucket.tokens+now.Sub(bucket.last).Seconds()*bucket.rate)
		bucket.last = now
		if bucket.tokens >= 1 {
			bucket.tokens--
			bucket.mu.Unlock()
			return nil
		}
		wait := time.Duration((1 - bucket.tokens) / bucket.rate * float64(time.Second))
		bucket.mu.Unlock()
		timer := time.NewTimer(max(wait, time.Millisecond))
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func NewClient(timeout time.Duration) (*Client, error) {
	base, err := url.Parse(DefaultBaseURL)
	if err != nil {
		return nil, err
	}
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 750 * time.Millisecond, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          8,
		MaxIdleConnsPerHost:   4,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   time.Second,
		ResponseHeaderTimeout: 1500 * time.Millisecond,
	}
	return &Client{
		base:          base,
		httpClient:    &http.Client{Transport: transport, Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		ttl:           defaultCacheTTL,
		now:           time.Now,
		lookupTimeout: defaultLookupTimeout,
		userAgent:     defaultUserAgent,
		cache:         make(map[string]*list.Element),
		order:         list.New(),
		max:           defaultCacheMax,
		stations:      make(map[string]stationCache),
		limiter:       newTokenBucket(1, 2),
	}, nil
}

func (client *Client) Lookup(ctx context.Context, icao string) (Observation, error) {
	return client.LookupAt(ctx, icao, "")
}

// LookupAt keeps the requested airport distinct from an administrator-approved
// reporting-station override. The override changes only the weather source; it
// never changes the public airport identity shown to users.
func (client *Client) LookupAt(ctx context.Context, icao, reportingStation string) (Observation, error) {
	code := strings.ToUpper(strings.TrimSpace(icao))
	station := strings.ToUpper(strings.TrimSpace(reportingStation))
	if len(code) != 4 || !asciiLetters(code) {
		return Observation{}, errors.New("airport ICAO must be four ASCII letters")
	}
	if station != "" && (len(station) != 4 || !asciiLetters(station)) {
		return Observation{}, errors.New("weather station ICAO must be four ASCII letters")
	}
	cacheKey := code
	if station != "" && station != code {
		cacheKey += "@" + station
	}
	if value, err, ok := client.cached(cacheKey); ok {
		return value, err
	}
	resultChannel := client.group.DoChan(cacheKey, func() (any, error) {
		if value, cachedErr, ok := client.cached(cacheKey); ok {
			return value, cachedErr
		}
		lookupContext, cancel := context.WithTimeout(context.Background(), client.lookupTimeout)
		defer cancel()
		return client.lookup(lookupContext, cacheKey, code, station)
	})
	select {
	case <-ctx.Done():
		return Observation{}, ctx.Err()
	case result := <-resultChannel:
		if result.Val == nil {
			return Observation{}, result.Err
		}
		return result.Val.(Observation), result.Err
	}
}

func (client *Client) lookup(ctx context.Context, cacheKey, code, reportingStation string) (Observation, error) {
	var resolution stationResolution
	var err error
	if reportingStation == "" || reportingStation == code {
		resolution, err = client.resolveStation(ctx, code)
	} else {
		resolution, err = client.resolveOverride(ctx, cacheKey, code, reportingStation)
	}
	if err != nil {
		if stale, ok := client.stale(cacheKey); ok {
			return stale, nil
		}
		client.store(cacheKey, Observation{}, err, defaultErrorTTL)
		return Observation{}, err
	}
	now := client.now().UTC()
	if !resolution.HasData {
		observation := Observation{
			RequestedICAO: code, ReportingICAO: firstNonEmpty(reportingStation, code), StationStatus: stationStatusUnavailable,
			METARStatus: "not-found", TAFStatus: "not-found", FetchedAt: now, Attribution: Attribution,
		}
		client.store(cacheKey, observation, nil, defaultNegativeTTL)
		return observation, nil
	}

	tafRows, tafErr := client.fetch(ctx, "taf", url.Values{"ids": {resolution.Reporting}})
	observation := observationFromDTO(code, resolution, now)
	if len(tafRows) > 0 {
		observation.TAF = strings.TrimSpace(tafRows[0].RawTAF)
	}
	switch {
	case tafErr != nil:
		observation.TAFStatus = "unavailable"
	case observation.TAF == "":
		observation.TAFStatus = "not-found"
	default:
		observation.TAFStatus = "available"
	}
	client.store(cacheKey, observation, nil, client.ttl)
	return observation, nil
}

type cloudDTO struct {
	Cover string   `json:"cover"`
	Base  *float64 `json:"base"`
}

type responseDTO struct {
	ICAOID         string          `json:"icaoId"`
	RawObservation string          `json:"rawOb"`
	RawTAF         string          `json:"rawTAF"`
	ReportTime     string          `json:"reportTime"`
	FlightCategory string          `json:"fltCat"`
	Temperature    *float64        `json:"temp"`
	Dewpoint       *float64        `json:"dewp"`
	WindDirection  json.RawMessage `json:"wdir"`
	WindSpeed      *float64        `json:"wspd"`
	WindGust       *float64        `json:"wgst"`
	Visibility     json.RawMessage `json:"visib"`
	Altimeter      *float64        `json:"altim"`
	Weather        string          `json:"wxString"`
	Clouds         []cloudDTO      `json:"clouds"`
	Latitude       *float64        `json:"lat"`
	Longitude      *float64        `json:"lon"`
}

type airportDTO struct {
	ICAOID    string   `json:"icaoId"`
	Latitude  *float64 `json:"lat"`
	Longitude *float64 `json:"lon"`
}

func observationFromDTO(requested string, resolution stationResolution, fetchedAt time.Time) Observation {
	dto := resolution.METAR
	observation := Observation{
		RequestedICAO: requested, ReportingICAO: resolution.Reporting, StationStatus: resolution.Status,
		StationDistanceNM: resolution.Distance, HasStationDistance: true,
		METAR: strings.TrimSpace(dto.RawObservation), FlightCategory: strings.ToUpper(strings.TrimSpace(dto.FlightCategory)),
		METARStatus: "available", FetchedAt: fetchedAt, Attribution: Attribution,
	}
	populateMETAR(&observation)
	applyTypedMETAR(&observation, dto)
	if observedAt, err := time.Parse(time.RFC3339, dto.ReportTime); err == nil {
		observation.ObservedAt = observedAt.UTC()
	}
	return observation
}

func applyTypedMETAR(observation *Observation, dto responseDTO) {
	if dto.Temperature != nil {
		observation.TemperatureC = int(math.Round(*dto.Temperature))
		observation.HasTemperature = true
	}
	if dto.Dewpoint != nil {
		observation.DewpointC = int(math.Round(*dto.Dewpoint))
		observation.HasDewpoint = true
	}
	if direction, _, ok := flexibleNumber(dto.WindDirection); ok {
		observation.WindDirectionDegrees = int(math.Round(direction))
		observation.WindVariable = false
		observation.HasWind = true
	} else if strings.EqualFold(strings.Trim(string(dto.WindDirection), `"`), "VRB") {
		observation.WindVariable = true
		observation.HasWind = true
	}
	if dto.WindSpeed != nil {
		observation.WindSpeedKts = int(math.Round(*dto.WindSpeed))
		observation.HasWind = true
	}
	if dto.WindGust != nil {
		observation.WindGustKts = int(math.Round(*dto.WindGust))
		observation.HasWind = true
	}
	if visibility, text, ok := flexibleNumber(dto.Visibility); ok {
		observation.VisibilitySM = visibility
		observation.VisibilityAtLeast = strings.HasSuffix(text, "+") || strings.HasPrefix(strings.ToUpper(text), "P")
		observation.VisibilityLessThan = strings.HasPrefix(strings.ToUpper(text), "M")
		observation.HasVisibility = true
	}
	if dto.Altimeter != nil {
		switch {
		case *dto.Altimeter >= 800 && *dto.Altimeter <= 1100:
			observation.AltimeterInHg = *dto.Altimeter / 33.8638866667
			observation.HasAltimeter = true
		case *dto.Altimeter >= 20 && *dto.Altimeter <= 40:
			observation.AltimeterInHg = *dto.Altimeter
			observation.HasAltimeter = true
		}
	}
	if len(dto.Clouds) > 0 {
		observation.Clouds = observation.Clouds[:0]
		for _, cloud := range dto.Clouds {
			layer := CloudLayer{Cover: strings.ToUpper(strings.TrimSpace(cloud.Cover))}
			if cloud.Base != nil {
				layer.BaseFeet = int(math.Round(*cloud.Base))
				layer.HasBase = true
			}
			observation.Clouds = append(observation.Clouds, layer)
		}
	}
	if strings.TrimSpace(dto.Weather) != "" {
		observation.Conditions = observation.Conditions[:0]
		for _, token := range strings.Fields(strings.ToUpper(dto.Weather)) {
			if label := weatherCondition(token); label != "" {
				observation.Conditions = appendUnique(observation.Conditions, label)
			}
		}
	}
	if category := strings.ToUpper(strings.TrimSpace(dto.FlightCategory)); category != "" {
		observation.FlightCategory = category
	} else {
		observation.FlightCategory = inferFlightCategory(*observation)
	}
}

func flexibleNumber(raw json.RawMessage) (float64, string, bool) {
	text := strings.TrimSpace(strings.Trim(string(raw), `"`))
	if text == "" || text == "null" {
		return 0, text, false
	}
	numeric := strings.TrimPrefix(strings.TrimSuffix(text, "+"), "P")
	numeric = strings.TrimPrefix(numeric, "M")
	value, err := strconv.ParseFloat(numeric, 64)
	return value, text, err == nil && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func (client *Client) resolveStation(ctx context.Context, requested string) (stationResolution, error) {
	if cached, ok := client.cachedStation(requested); ok {
		if !cached.HasData {
			return cached, nil
		}
		rows, err := client.fetch(ctx, "metar", url.Values{"ids": {cached.Reporting}})
		if err != nil {
			return stationResolution{}, err
		}
		if len(rows) > 0 {
			cached.METAR = rows[0]
			return cached, nil
		}
		client.deleteStation(requested)
	}

	rows, err := client.fetch(ctx, "metar", url.Values{"ids": {requested}})
	if err != nil {
		return stationResolution{}, err
	}
	if len(rows) > 0 && strings.TrimSpace(rows[0].RawObservation) != "" {
		resolution := stationResolution{Requested: requested, Reporting: firstNonEmpty(strings.ToUpper(rows[0].ICAOID), requested), Status: stationStatusExact, HasData: true, METAR: rows[0]}
		client.storeStation(requested, resolution, defaultStationTTL)
		return resolution, nil
	}

	latitude, longitude, found, err := client.airportPosition(ctx, requested)
	if err != nil {
		return stationResolution{}, err
	}
	if !found {
		resolution := stationResolution{Requested: requested, Reporting: requested, Status: stationStatusUnavailable}
		client.storeStation(requested, resolution, defaultNegativeTTL)
		return resolution, nil
	}
	latDelta := defaultNearbyRadiusNM / 60
	lonScale := math.Max(math.Cos(latitude*math.Pi/180), 0.1)
	lonDelta := defaultNearbyRadiusNM / (60 * lonScale)
	bbox := fmt.Sprintf("%.5f,%.5f,%.5f,%.5f", latitude-latDelta, longitude-lonDelta, latitude+latDelta, longitude+lonDelta)
	nearby, err := client.fetch(ctx, "metar", url.Values{"bbox": {bbox}})
	if err != nil {
		return stationResolution{}, err
	}
	resolution := nearestStation(requested, latitude, longitude, nearby, client.now().UTC())
	if resolution.HasData {
		client.storeStation(requested, resolution, defaultStationTTL)
	} else {
		client.storeStation(requested, resolution, defaultNegativeTTL)
	}
	return resolution, nil
}

func (client *Client) resolveOverride(ctx context.Context, cacheKey, requested, reporting string) (stationResolution, error) {
	if cached, ok := client.cachedStation(cacheKey); ok {
		rows, err := client.fetch(ctx, "metar", url.Values{"ids": {cached.Reporting}})
		if err != nil {
			return stationResolution{}, err
		}
		if len(rows) > 0 && strings.TrimSpace(rows[0].RawObservation) != "" {
			cached.METAR = rows[0]
			return cached, nil
		}
		client.deleteStation(cacheKey)
	}
	rows, err := client.fetch(ctx, "metar", url.Values{"ids": {reporting}})
	if err != nil {
		return stationResolution{}, err
	}
	if len(rows) == 0 || strings.TrimSpace(rows[0].RawObservation) == "" {
		resolution := stationResolution{Requested: requested, Reporting: reporting, Status: stationStatusUnavailable}
		client.storeStation(cacheKey, resolution, defaultNegativeTTL)
		return resolution, nil
	}
	row := rows[0]
	resolution := stationResolution{Requested: requested, Reporting: firstNonEmpty(strings.ToUpper(row.ICAOID), reporting), Status: stationStatusNearby, HasData: true, METAR: row}
	if requested == resolution.Reporting {
		resolution.Status = stationStatusExact
	} else if row.Latitude != nil && row.Longitude != nil {
		latitude, longitude, found, positionErr := client.airportPosition(ctx, requested)
		if positionErr != nil {
			return stationResolution{}, positionErr
		}
		if found {
			resolution.Distance = distanceNM(latitude, longitude, *row.Latitude, *row.Longitude)
			if resolution.Distance <= 0.25 {
				resolution.Status = stationStatusAlias
			}
		}
	}
	client.storeStation(cacheKey, resolution, defaultStationTTL)
	return resolution, nil
}

func (client *Client) airportPosition(ctx context.Context, requested string) (float64, float64, bool, error) {
	for _, kind := range []string{"airport", "stationinfo"} {
		rows, err := client.fetchAirports(ctx, kind, url.Values{"ids": {requested}})
		if err != nil {
			return 0, 0, false, err
		}
		if len(rows) > 0 && rows[0].Latitude != nil && rows[0].Longitude != nil {
			return *rows[0].Latitude, *rows[0].Longitude, true, nil
		}
	}
	return 0, 0, false, nil
}

func nearestStation(requested string, latitude, longitude float64, rows []responseDTO, now time.Time) stationResolution {
	result := stationResolution{Requested: requested, Reporting: requested, Status: stationStatusUnavailable}
	best := math.MaxFloat64
	for _, row := range rows {
		if row.Latitude == nil || row.Longitude == nil || strings.TrimSpace(row.RawObservation) == "" {
			continue
		}
		if observedAt, err := time.Parse(time.RFC3339, row.ReportTime); err == nil && now.Sub(observedAt) > defaultObservationMaxAge {
			continue
		}
		distance := distanceNM(latitude, longitude, *row.Latitude, *row.Longitude)
		if distance > defaultNearbyRadiusNM || distance >= best {
			continue
		}
		best = distance
		status := stationStatusNearby
		if distance <= 0.25 {
			status = stationStatusAlias
		}
		result = stationResolution{Requested: requested, Reporting: strings.ToUpper(strings.TrimSpace(row.ICAOID)), Status: status, Distance: distance, HasData: true, METAR: row}
	}
	return result
}

func distanceNM(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadiusNM = 3440.065
	toRadians := math.Pi / 180
	lat1 *= toRadians
	lat2 *= toRadians
	deltaLat := lat2 - lat1
	deltaLon := (lon2 - lon1) * toRadians
	a := math.Sin(deltaLat/2)*math.Sin(deltaLat/2) + math.Cos(lat1)*math.Cos(lat2)*math.Sin(deltaLon/2)*math.Sin(deltaLon/2)
	return 2 * earthRadiusNM * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

func (client *Client) fetch(ctx context.Context, kind string, query url.Values) ([]responseDTO, error) {
	var payload []responseDTO
	noContent, err := client.fetchJSON(ctx, kind, query, &payload)
	if noContent {
		return nil, nil
	}
	return payload, err
}

func (client *Client) fetchAirports(ctx context.Context, kind string, query url.Values) ([]airportDTO, error) {
	var payload []airportDTO
	noContent, err := client.fetchJSON(ctx, kind, query, &payload)
	if noContent {
		return nil, nil
	}
	return payload, err
}

func (client *Client) fetchJSON(ctx context.Context, kind string, query url.Values, destination any) (bool, error) {
	endpoint := *client.base
	endpoint.Path = strings.TrimSuffix(endpoint.Path, "/") + "/" + kind
	query.Set("format", "json")
	endpoint.RawQuery = query.Encode()
	if strings.EqualFold(endpoint.Hostname(), "aviationweather.gov") && client.limiter != nil {
		if err := client.limiter.wait(ctx); err != nil {
			return false, err
		}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return false, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", client.userAgent)
	response, err := client.httpClient.Do(request)
	if err != nil {
		return false, sanitizeError(err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNoContent {
		return true, nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxResponseBytes))
		return false, fmt.Errorf("aviation weather returned HTTP %d", response.StatusCode)
	}
	limited := &io.LimitedReader{R: response.Body, N: maxResponseBytes + 1}
	decoder := json.NewDecoder(limited)
	if err := decoder.Decode(destination); err != nil {
		return false, fmt.Errorf("decode aviation weather response: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return false, errors.New("decode aviation weather response: multiple JSON values")
		}
		return false, fmt.Errorf("decode aviation weather response: %w", err)
	}
	if limited.N <= 0 {
		return false, fmt.Errorf("aviation weather response exceeds %d bytes", maxResponseBytes)
	}
	return false, nil
}

func (client *Client) cachedStation(code string) (stationResolution, bool) {
	client.mu.Lock()
	defer client.mu.Unlock()
	entry, ok := client.stations[code]
	if !ok || !client.now().Before(entry.expires) {
		delete(client.stations, code)
		return stationResolution{}, false
	}
	return entry.station, true
}

func (client *Client) storeStation(code string, resolution stationResolution, ttl time.Duration) {
	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.stations) >= client.max {
		for existing := range client.stations {
			delete(client.stations, existing)
			break
		}
	}
	client.stations[code] = stationCache{station: resolution, expires: client.now().Add(ttl)}
}

func (client *Client) deleteStation(code string) {
	client.mu.Lock()
	delete(client.stations, code)
	client.mu.Unlock()
}

func (client *Client) cached(code string) (Observation, error, bool) {
	client.mu.Lock()
	defer client.mu.Unlock()
	element, ok := client.cache[code]
	if !ok {
		return Observation{}, nil, false
	}
	entry := element.Value.(cachedObservation)
	if !client.now().Before(entry.expires) {
		if client.now().Before(entry.staleExpires) {
			return Observation{}, nil, false
		}
		client.order.Remove(element)
		delete(client.cache, code)
		return Observation{}, nil, false
	}
	client.order.MoveToFront(element)
	return entry.value, entry.err, true
}

func (client *Client) stale(code string) (Observation, bool) {
	client.mu.Lock()
	defer client.mu.Unlock()
	element, ok := client.cache[code]
	if !ok {
		return Observation{}, false
	}
	entry := element.Value.(cachedObservation)
	if entry.err != nil || entry.value.METAR == "" || !client.now().Before(entry.staleExpires) {
		return Observation{}, false
	}
	value := entry.value
	value.Stale = true
	client.order.MoveToFront(element)
	return value, true
}

func (client *Client) store(code string, value Observation, err error, ttl time.Duration) {
	client.mu.Lock()
	defer client.mu.Unlock()
	entry := cachedObservation{code: code, value: value, err: err, expires: client.now().Add(ttl), staleExpires: client.staleExpiry(err, ttl)}
	if element, ok := client.cache[code]; ok {
		element.Value = entry
		client.order.MoveToFront(element)
		return
	}
	element := client.order.PushFront(entry)
	client.cache[code] = element
	for client.order.Len() > client.max {
		oldest := client.order.Back()
		oldestEntry := oldest.Value.(cachedObservation)
		delete(client.cache, oldestEntry.code)
		client.order.Remove(oldest)
	}
}

func (client *Client) staleExpiry(err error, ttl time.Duration) time.Time {
	if err != nil {
		return client.now().Add(ttl)
	}
	return client.now().Add(ttl + defaultStaleTTL)
}

func (client *Client) CacheLen() int {
	client.mu.Lock()
	defer client.mu.Unlock()
	return len(client.cache)
}

func sanitizeError(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return fmt.Errorf("aviation weather request failed: %v", urlErr.Err)
	}
	return err
}

func asciiLetters(value string) bool {
	for _, character := range value {
		if character < 'A' || character > 'Z' {
			return false
		}
	}
	return true
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
