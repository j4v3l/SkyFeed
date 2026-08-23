package airplaneslive

import (
	"errors"
	"math"
	"strings"
	"time"
	"unicode"

	"github.com/j4v3l/SkyFeed/internal/domain"
)

const (
	minimumUnixMilliseconds = int64(1_000_000_000_000)
	maximumUnixMilliseconds = int64(4_102_444_800_000)
	maximumAircraft         = 10_000
)

func validatePointResponse(response pointResponse) error {
	if response.Aircraft == nil {
		return errors.New("point payload is missing the aircraft array")
	}
	if response.Now == nil || !validUnixMilliseconds(*response.Now) {
		return errors.New("point payload has no valid millisecond generation time")
	}
	if response.Total == nil || *response.Total < 0 || *response.Total != len(response.Aircraft) {
		return errors.New("point payload aircraft total does not match the array")
	}
	if len(response.Aircraft) > maximumAircraft {
		return errors.New("point payload contains too many aircraft")
	}
	if strings.TrimSpace(response.Message) != "No error" {
		return errors.New("point payload reports a provider error")
	}
	if response.CacheAt == nil || !validUnixMilliseconds(*response.CacheAt) {
		return errors.New("point payload has an invalid millisecond cache time")
	}
	if response.Process == nil || !finite(*response.Process) || *response.Process < 0 {
		return errors.New("point payload has an invalid processing time")
	}

	seen := make(map[string]struct{}, len(response.Aircraft))
	for _, aircraft := range response.Aircraft {
		icao := strings.ToUpper(strings.TrimSpace(aircraft.Hex))
		if !validAircraftIdentifier(icao) {
			return errors.New("point payload contains an invalid aircraft identifier")
		}
		if _, duplicate := seen[icao]; duplicate {
			return errors.New("point payload contains a duplicate aircraft identifier")
		}
		seen[icao] = struct{}{}
		if strings.TrimSpace(aircraft.Type) == "" {
			return errors.New("point payload contains an aircraft without a source type")
		}
		if aircraft.Messages == nil {
			return errors.New("point payload contains an aircraft without a message count")
		}
		if aircraft.Seen == nil || !finite(*aircraft.Seen) || *aircraft.Seen < 0 {
			return errors.New("point payload contains an invalid seen age")
		}
		if (aircraft.Latitude == nil) != (aircraft.Longitude == nil) {
			return errors.New("point payload contains an incomplete aircraft position")
		}
		if aircraft.Latitude != nil && !validPosition(*aircraft.Latitude, *aircraft.Longitude) {
			return errors.New("point payload contains an invalid aircraft position")
		}
		if aircraft.GroundSpeed != nil && (!finite(*aircraft.GroundSpeed) || *aircraft.GroundSpeed < 0) {
			return errors.New("point payload contains an invalid ground speed")
		}
		if aircraft.Track != nil && (!finite(*aircraft.Track) || *aircraft.Track < 0 || *aircraft.Track > 360) {
			return errors.New("point payload contains an invalid track")
		}
		if aircraft.SeenPosition != nil && (!finite(*aircraft.SeenPosition) || *aircraft.SeenPosition < 0) {
			return errors.New("point payload contains an invalid position age")
		}
		if aircraft.RSSI != nil && !finite(*aircraft.RSSI) {
			return errors.New("point payload contains an invalid signal value")
		}
	}
	return nil
}

func normalizePoint(response pointResponse) domain.AircraftBatch {
	batch := domain.AircraftBatch{
		Provider:            domain.ProviderAirplanesLive,
		GeneratedAt:         time.UnixMilli(*response.Now).UTC(),
		MessageCounterValid: false,
		Aircraft:            make([]domain.Aircraft, 0, len(response.Aircraft)),
	}
	for _, raw := range response.Aircraft {
		aircraft := domain.Aircraft{
			ICAO:         normalizedText(raw.Hex, 7, true),
			Provider:     domain.ProviderAirplanesLive,
			SourceType:   normalizedText(raw.Type, 32, false),
			Callsign:     normalizedText(raw.Flight, 16, true),
			Registration: normalizedText(raw.Registration, 16, true),
			AircraftType: normalizedText(raw.AircraftType, 16, true),
			Category:     normalizedText(raw.Category, 8, false),
			Squawk:       normalizedText(raw.Squawk, 8, false),
			Emergency:    strings.ToLower(normalizedText(raw.Emergency, 32, false)),
			Messages:     *raw.Messages,
			Seen:         durationSeconds(*raw.Seen),
			AltitudeFeet: raw.Altitude.Value,
			HasAltitude:  raw.Altitude.Valid && !raw.Altitude.Ground,
			OnGround:     raw.Altitude.Ground,
		}
		if raw.Latitude != nil {
			aircraft.Latitude = *raw.Latitude
			aircraft.Longitude = *raw.Longitude
			aircraft.HasPosition = true
		}
		if raw.GroundSpeed != nil {
			aircraft.GroundSpeedKts = *raw.GroundSpeed
			aircraft.HasGroundSpeed = true
		}
		if raw.Track != nil {
			aircraft.TrackDegrees = *raw.Track
			if aircraft.TrackDegrees == 360 {
				aircraft.TrackDegrees = 0
			}
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
			aircraft.SeenPosition = durationSeconds(*raw.SeenPosition)
		}
		if raw.RSSI != nil {
			aircraft.RSSI = *raw.RSSI
			aircraft.HasRSSI = true
		}
		batch.Aircraft = append(batch.Aircraft, aircraft)
	}
	return batch
}

func validUnixMilliseconds(value int64) bool {
	return value >= minimumUnixMilliseconds && value <= maximumUnixMilliseconds
}

func validAircraftIdentifier(value string) bool {
	if len(value) == 7 && value[0] == '~' {
		value = value[1:]
	}
	if len(value) != 6 {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'A' || character > 'F') {
			return false
		}
	}
	return true
}

func validPosition(latitude, longitude float64) bool {
	return finite(latitude) && finite(longitude) &&
		latitude >= -90 && latitude <= 90 &&
		longitude >= -180 && longitude <= 180
}

func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func durationSeconds(value float64) time.Duration {
	return time.Duration(value * float64(time.Second))
}

func normalizedText(value string, maximum int, upper bool) string {
	value = strings.TrimSpace(value)
	var builder strings.Builder
	builder.Grow(min(len(value), maximum))
	count := 0
	for _, character := range value {
		if count == maximum {
			break
		}
		if unicode.IsControl(character) {
			continue
		}
		builder.WriteRune(character)
		count++
	}
	value = strings.TrimSpace(builder.String())
	if upper {
		return strings.ToUpper(value)
	}
	return value
}
