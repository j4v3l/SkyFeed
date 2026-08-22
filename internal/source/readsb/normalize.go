package readsb

import (
	"strings"
	"time"

	"github.com/j4v3l/SkyFeed/internal/domain"
)

const metresPerNauticalMile = 1852.0

func normalizeAircraft(response aircraftResponse) domain.AircraftBatch {
	batch := domain.AircraftBatch{
		GeneratedAt: unixFloat(response.Now),
		Messages:    response.Messages,
		Aircraft:    make([]domain.Aircraft, 0, len(response.Aircraft)),
	}
	for _, raw := range response.Aircraft {
		icao := strings.ToUpper(strings.TrimSpace(raw.Hex))
		if icao == "" {
			continue
		}
		aircraft := domain.Aircraft{
			ICAO:         icao,
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

func normalizeStats(response statsResponse, fetchedAt time.Time) domain.Statistics {
	period := response.Latest
	start := unixFloat(period.Start)
	end := unixFloat(period.End)
	duration := end.Sub(start).Seconds()
	rate := 0.0
	if duration > 0 {
		rate = float64(period.Messages) / duration
	}
	return domain.Statistics{
		WindowStart:       start,
		WindowEnd:         end,
		Messages:          period.Messages,
		MessageRate:       rate,
		MaxRangeNM:        period.MaxDistanceInMetres / metresPerNauticalMile,
		TrackedAircraft:   period.Tracks.All,
		SingleMessageOnly: period.Tracks.SingleMessage,
		FetchedAt:         fetchedAt,
	}
}

func unixFloat(value float64) time.Time {
	secondsPart := int64(value)
	nanoseconds := int64((value - float64(secondsPart)) * float64(time.Second))
	return time.Unix(secondsPart, nanoseconds).UTC()
}

func seconds(value float64) time.Duration {
	return time.Duration(value * float64(time.Second))
}
