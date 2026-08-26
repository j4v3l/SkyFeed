package aviationweather

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	windPattern        = regexp.MustCompile(`^(VRB|[0-9]{3})([0-9]{2,3})(?:G([0-9]{2,3}))?KT$`)
	visibilityPattern  = regexp.MustCompile(`^(P)?([0-9]+(?:\.[0-9]+)?|[0-9]+/[0-9]+)SM$`)
	temperaturePattern = regexp.MustCompile(`^(M?[0-9]{2})/(M?[0-9]{2}|//)$`)
	altimeterPattern   = regexp.MustCompile(`^A([0-9]{4})$`)
	cloudPattern       = regexp.MustCompile(`^(FEW|SCT|BKN|OVC|VV)([0-9]{3}|///)(?:CB|TCU)?$`)
)

var conditionLabels = map[string]string{
	"DZ": "drizzle", "RA": "rain", "SN": "snow", "SG": "snow grains", "IC": "ice crystals",
	"PL": "ice pellets", "GR": "hail", "GS": "small hail", "UP": "unknown precipitation",
	"BR": "mist", "FG": "fog", "FU": "smoke", "VA": "volcanic ash", "DU": "dust", "SA": "sand", "HZ": "haze",
	"PO": "dust whirls", "SQ": "squalls", "FC": "funnel cloud", "SS": "sandstorm", "DS": "dust storm", "TS": "thunderstorms",
}

func populateMETAR(observation *Observation) {
	if observation == nil || strings.TrimSpace(observation.METAR) == "" {
		return
	}
	tokens := strings.Fields(strings.ToUpper(observation.METAR))
	for _, token := range tokens {
		if match := windPattern.FindStringSubmatch(token); match != nil {
			observation.HasWind = true
			observation.WindVariable = match[1] == "VRB"
			if !observation.WindVariable {
				observation.WindDirectionDegrees, _ = strconv.Atoi(match[1])
			}
			observation.WindSpeedKts, _ = strconv.Atoi(match[2])
			observation.WindGustKts, _ = strconv.Atoi(match[3])
			continue
		}
		if match := visibilityPattern.FindStringSubmatch(token); match != nil {
			if value, ok := parseVisibility(match[2]); ok {
				observation.HasVisibility = true
				observation.VisibilityAtLeast = match[1] == "P"
				observation.VisibilitySM = value
			}
			continue
		}
		if match := temperaturePattern.FindStringSubmatch(token); match != nil {
			if value, ok := signedMETARNumber(match[1]); ok {
				observation.TemperatureC = value
				observation.HasTemperature = true
			}
			if value, ok := signedMETARNumber(match[2]); ok {
				observation.DewpointC = value
				observation.HasDewpoint = true
			}
			continue
		}
		if match := altimeterPattern.FindStringSubmatch(token); match != nil {
			value, _ := strconv.Atoi(match[1])
			observation.AltimeterInHg = float64(value) / 100
			observation.HasAltimeter = true
			continue
		}
		if match := cloudPattern.FindStringSubmatch(token); match != nil {
			layer := CloudLayer{Cover: match[1]}
			if match[2] != "///" {
				base, _ := strconv.Atoi(match[2])
				layer.BaseFeet = base * 100
				layer.HasBase = true
			}
			observation.Clouds = append(observation.Clouds, layer)
			continue
		}
		if label := weatherCondition(token); label != "" {
			observation.Conditions = appendUnique(observation.Conditions, label)
		}
	}
	if observation.FlightCategory == "" {
		observation.FlightCategory = inferFlightCategory(*observation)
	}
}

func parseVisibility(value string) (float64, bool) {
	if numerator, denominator, ok := strings.Cut(value, "/"); ok {
		left, leftErr := strconv.ParseFloat(numerator, 64)
		right, rightErr := strconv.ParseFloat(denominator, 64)
		return left / right, leftErr == nil && rightErr == nil && right > 0
	}
	parsed, err := strconv.ParseFloat(value, 64)
	return parsed, err == nil
}

func signedMETARNumber(value string) (int, bool) {
	if value == "//" {
		return 0, false
	}
	negative := strings.HasPrefix(value, "M")
	value = strings.TrimPrefix(value, "M")
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, false
	}
	if negative {
		parsed = -parsed
	}
	return parsed, true
}

func weatherCondition(token string) string {
	intensity := ""
	if strings.HasPrefix(token, "-") {
		intensity = "light "
	} else if strings.HasPrefix(token, "+") {
		intensity = "heavy "
	}
	value := strings.TrimPrefix(strings.TrimPrefix(token, "-"), "+")
	for _, descriptor := range []string{"MI", "PR", "BC", "DR", "BL", "SH", "FZ"} {
		value = strings.TrimPrefix(value, descriptor)
	}
	for index := 0; index+2 <= len(value); index += 2 {
		if label := conditionLabels[value[index:index+2]]; label != "" {
			return intensity + label
		}
	}
	return ""
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func inferFlightCategory(observation Observation) string {
	ceiling := 100_000
	for _, layer := range observation.Clouds {
		if layer.HasBase && (layer.Cover == "BKN" || layer.Cover == "OVC" || layer.Cover == "VV") && layer.BaseFeet < ceiling {
			ceiling = layer.BaseFeet
		}
	}
	visibility := 100.0
	if observation.HasVisibility {
		visibility = observation.VisibilitySM
	}
	switch {
	case visibility < 1 || ceiling < 500:
		return "LIFR"
	case visibility < 3 || ceiling < 1000:
		return "IFR"
	case visibility <= 5 || ceiling <= 3000:
		return "MVFR"
	default:
		return "VFR"
	}
}
