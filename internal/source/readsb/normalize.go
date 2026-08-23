package readsb

import (
	"errors"
	"math"
	"strings"
	"time"

	"github.com/j4v3l/SkyFeed/internal/domain"
)

const metresPerNauticalMile = 1852.0

func validateAircraftResponse(response aircraftResponse) error {
	if !validUnixSeconds(response.Now) {
		return errors.New("aircraft payload has no valid generation time")
	}
	if response.Aircraft == nil {
		return errors.New("aircraft payload is missing the aircraft array")
	}
	return nil
}

func normalizeAircraft(response aircraftResponse) domain.AircraftBatch {
	batch := domain.AircraftBatch{
		Provider:            domain.ProviderReadsb,
		GeneratedAt:         unixFloat(response.Now),
		Messages:            response.Messages,
		MessageCounterValid: true,
		Aircraft:            make([]domain.Aircraft, 0, len(response.Aircraft)),
	}
	for _, raw := range response.Aircraft {
		icao := strings.ToUpper(strings.TrimSpace(raw.Hex))
		if icao == "" {
			continue
		}
		aircraft := domain.Aircraft{
			ICAO:         icao,
			Provider:     domain.ProviderReadsb,
			SourceType:   strings.TrimSpace(raw.Type),
			Callsign:     strings.ToUpper(strings.TrimSpace(raw.Flight)),
			Registration: strings.ToUpper(strings.TrimSpace(raw.Registration)),
			AircraftType: strings.ToUpper(strings.TrimSpace(raw.AircraftType)),
			Category:     strings.TrimSpace(raw.Category),
			Squawk:       strings.TrimSpace(raw.Squawk),
			Emergency:    strings.ToLower(strings.TrimSpace(raw.Emergency)),
			Messages:     raw.Messages,
			Seen:         seconds(raw.Seen),
			AltitudeFeet: raw.Altitude.Value,
			HasAltitude:  raw.Altitude.Valid && !raw.Altitude.Ground,
			OnGround:     raw.Altitude.Ground,
		}
		if raw.Latitude != nil && raw.Longitude != nil {
			if validPosition(*raw.Latitude, *raw.Longitude) {
				aircraft.Latitude = *raw.Latitude
				aircraft.Longitude = *raw.Longitude
				aircraft.HasPosition = true
			}
		}
		if raw.GroundSpeed != nil {
			aircraft.GroundSpeedKts = *raw.GroundSpeed
			aircraft.HasGroundSpeed = true
		}
		if raw.Track != nil {
			aircraft.TrackDegrees = *raw.Track
			aircraft.HasTrack = true
		}
		if raw.BaroRate != nil {
			aircraft.VerticalRateFPM = *raw.BaroRate
			aircraft.HasVerticalRate = true
		} else if raw.GeomRate != nil {
			aircraft.VerticalRateFPM = *raw.GeomRate
			aircraft.HasVerticalRate = true
		}
		if raw.SeenPosition != nil {
			aircraft.SeenPosition = seconds(*raw.SeenPosition)
		}
		if raw.RSSI != nil {
			aircraft.RSSI = *raw.RSSI
			aircraft.HasRSSI = true
		}
		batch.Aircraft = append(batch.Aircraft, aircraft)
	}
	return batch
}

func validateReceiverResponse(response receiverResponse) error {
	if strings.TrimSpace(response.Version) == "" {
		return errors.New("receiver payload is missing the version")
	}
	if response.Refresh <= 0 {
		return errors.New("receiver payload has no valid refresh interval")
	}
	if (response.Latitude == nil) != (response.Longitude == nil) {
		return errors.New("receiver payload has an incomplete position")
	}
	if response.Latitude != nil && !validPosition(*response.Latitude, *response.Longitude) {
		return errors.New("receiver payload has an invalid position")
	}
	return nil
}

func normalizeReceiver(response receiverResponse, fetchedAt time.Time) domain.Receiver {
	receiver := domain.Receiver{
		Version:     strings.TrimSpace(response.Version),
		Refresh:     time.Duration(response.Refresh) * time.Millisecond,
		HistorySize: response.History,
		FetchedAt:   fetchedAt,
	}
	if response.Latitude != nil && response.Longitude != nil && validPosition(*response.Latitude, *response.Longitude) {
		receiver.Latitude = *response.Latitude
		receiver.Longitude = *response.Longitude
		receiver.HasPosition = true
	}
	return receiver
}

func validPosition(latitude, longitude float64) bool {
	return latitude >= -90 && latitude <= 90 && longitude >= -180 && longitude <= 180
}

func validateStatsResponse(response statsResponse) error {
	period, ok := selectStatsPeriod(response)
	if !ok {
		return errors.New("stats payload has no valid latest or last1min period")
	}
	if distance := statsDistanceMetres(period); math.IsNaN(distance) || math.IsInf(distance, 0) || distance < 0 {
		return errors.New("stats payload has an invalid maximum distance")
	}
	if period.Tracks.All < 0 || period.Tracks.SingleMessage < 0 {
		return errors.New("stats payload has a negative track count")
	}
	if response.AircraftWithPosition != nil && *response.AircraftWithPosition < 0 {
		return errors.New("stats payload has a negative positioned-aircraft count")
	}
	if response.AircraftWithoutPosition != nil && *response.AircraftWithoutPosition < 0 {
		return errors.New("stats payload has a negative unpositioned-aircraft count")
	}
	return nil
}

func normalizeStats(response statsResponse, fetchedAt time.Time) domain.Statistics {
	period, ok := selectStatsPeriod(response)
	if !ok {
		return domain.Statistics{FetchedAt: fetchedAt}
	}
	start := unixFloat(period.Start)
	end := unixFloat(period.End)
	duration := end.Sub(start).Seconds()
	rate := 0.0
	if duration > 0 {
		rate = float64(period.Messages) / duration
	}
	trackedAircraft := period.Tracks.All
	if response.AircraftWithPosition != nil || response.AircraftWithoutPosition != nil {
		trackedAircraft = 0
		if response.AircraftWithPosition != nil {
			trackedAircraft += *response.AircraftWithPosition
		}
		if response.AircraftWithoutPosition != nil {
			trackedAircraft += *response.AircraftWithoutPosition
		}
	}
	return domain.Statistics{
		WindowStart:       start,
		WindowEnd:         end,
		Messages:          period.Messages,
		MessageRate:       rate,
		MaxRangeNM:        statsDistanceMetres(period) / metresPerNauticalMile,
		TrackedAircraft:   trackedAircraft,
		SingleMessageOnly: period.Tracks.SingleMessage,
		FetchedAt:         fetchedAt,
	}
}

func selectStatsPeriod(response statsResponse) (statsPeriod, bool) {
	if validStatsPeriod(response.Latest) {
		return response.Latest, true
	}
	if validStatsPeriod(response.Last1Min) {
		return response.Last1Min, true
	}
	return statsPeriod{}, false
}

func validStatsPeriod(period statsPeriod) bool {
	return validUnixSeconds(period.Start) && validUnixSeconds(period.End) && period.End > period.Start
}

func statsDistanceMetres(period statsPeriod) float64 {
	if period.MaxDistance != nil {
		return *period.MaxDistance
	}
	if period.MaxDistanceInMetres != nil {
		return *period.MaxDistanceInMetres
	}
	return 0
}

func validUnixSeconds(value float64) bool {
	return value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func unixFloat(value float64) time.Time {
	secondsPart := int64(value)
	nanoseconds := int64((value - float64(secondsPart)) * float64(time.Second))
	return time.Unix(secondsPart, nanoseconds).UTC()
}

func seconds(value float64) time.Duration {
	return time.Duration(value * float64(time.Second))
}
