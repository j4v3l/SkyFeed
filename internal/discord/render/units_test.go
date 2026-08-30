package render

import (
	"strings"
	"testing"
	"time"

	"github.com/j4v3l/SkyFeed/internal/domain"
	"github.com/j4v3l/SkyFeed/internal/privacy"
	"github.com/j4v3l/SkyFeed/internal/storage"
)

func TestUnitFormatterMatrix(t *testing.T) {
	tests := []struct {
		name                                                string
		units                                               domain.UnitSystem
		distance, altitude, speed, vertical, temp, pressure string
	}{
		{"imperial", domain.UnitsImperial, "11.5 mi", "10000 ft", "115 mph", "-500 ft/min", "68°F", "29.92 inHg"},
		{"aviation", domain.UnitsAviation, "10.0 NM", "10000 ft", "100 kt", "-500 ft/min", "20°C", "29.92 inHg"},
		{"metric", domain.UnitsMetric, "18.5 km", "3048 m", "185 km/h", "-2.5 m/s", "20°C", "1013 hPa"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			formatter := unitsFor(test.units)
			got := []string{
				formatter.distanceNM(10),
				formatter.altitudeFeet(10_000),
				formatter.speedKts(100),
				formatter.verticalRateFPM(-500),
				formatter.temperatureC(20),
				formatter.pressureInHg(29.92),
			}
			want := []string{test.distance, test.altitude, test.speed, test.vertical, test.temp, test.pressure}
			for index := range want {
				if got[index] != want[index] {
					t.Fatalf("measurement %d=%q want=%q", index, got[index], want[index])
				}
			}
		})
	}
}

func TestImperialWeatherAndRawReports(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	embed := AirportDashboard(domain.Airport{ICAO: "KXYZ", HasElevation: true, ElevationFeet: 1000}, WeatherView{
		METAR: "KXYZ 231453Z 18012KT P6SM SCT040 20/10 A2992", TAF: "TAF KXYZ 231100Z", METARStatus: "available", TAFStatus: "available",
		HasWind: true, WindSpeedKts: 12, HasVisibility: true, VisibilitySM: 6,
		HasTemperature: true, TemperatureC: 20, HasDewpoint: true, DewpointC: 10,
		HasAltimeter: true, AltimeterInHg: 29.92, Clouds: []WeatherCloudView{{Cover: "SCT", BaseFeet: 4000, HasBase: true}},
	}, domain.AirportActivity{}, "weather-details", now, domain.UnitsImperial)
	values := embedFieldValues(embed)
	for _, want := range []string{"elevation 1000 ft", "14 mph", "6.0 statute miles", "temperature 68°F", "dew point 50°F", "29.92 inHg", "4,000 ft", "231453Z", "231100Z"} {
		if !strings.Contains(values, want) {
			t.Fatalf("imperial weather missing %q: %q", want, values)
		}
	}
}

func TestAlertsAuditsAndPrivacyRespectUnits(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	alert := domain.Alert{
		AircraftICAO: "ABC123", Callsign: "SKY123", Description: "Matched safely.", ObservedAt: now,
		Observation: domain.AlertObservation{DistanceNM: 10, HasDistance: true, AltitudeFeet: 10_000, HasAltitude: true, GroundSpeedKts: 100, HasGroundSpeed: true},
	}
	if values := embedFieldValues(AlertWithUnits(alert, domain.UnitsImperial)); !strings.Contains(values, "11.5 mi") || !strings.Contains(values, "115 mph") {
		t.Fatalf("imperial alert=%q", values)
	}
	data := SystemAuditData{GeneratedAt: now, Units: domain.UnitsMetric, MaxRangeNM: 10, Report24h: storage.ReportSummary{MaximumRangeNM: 20}}
	values := embedFieldValues(SystemAudit(data))
	if !strings.Contains(values, "18.5 km") || !strings.Contains(values, "37.0 km") {
		t.Fatalf("metric audit=%q", values)
	}
	disclosure := privacy.NewDisclosure([]string{"readsb", "airplanes.live"}, "KXYZ", 50, nil, nil)
	values = embedFieldValues(PrivacyWithUnits(disclosure, domain.UnitsImperial))
	if !strings.Contains(values, "57.5 mi") || !strings.Contains(values, "287.7 mi") || strings.Contains(values, " NM") {
		t.Fatalf("imperial privacy=%q", values)
	}
}
