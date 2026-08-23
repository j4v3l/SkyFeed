package privacy

// Disclosure is the shared privacy-safe view for diagnostics, documentation,
// and Discord presentation. It intentionally has no coordinate, URL, host, or
// identifier fields.
type Disclosure struct {
	Providers         []string      `json:"providers"`
	PublicAirportCode string        `json:"public_airport_code"`
	RadiusNM          int           `json:"radius_nm"`
	Retention         []Retention   `json:"retention"`
	Attribution       []Attribution `json:"attribution"`
}

type Retention struct {
	Category string `json:"category"`
	Period   string `json:"period"`
}

type Attribution struct {
	Provider string `json:"provider"`
	Notice   string `json:"notice"`
}

func NewDisclosure(providers []string, publicAirportCode string, radiusNM int, retention []Retention, attribution []Attribution) Disclosure {
	return Disclosure{
		Providers:         append([]string(nil), providers...),
		PublicAirportCode: publicAirportCode,
		RadiusNM:          radiusNM,
		Retention:         append([]Retention(nil), retention...),
		Attribution:       append([]Attribution(nil), attribution...),
	}
}

func (disclosure Disclosure) Clone() Disclosure {
	return NewDisclosure(disclosure.Providers, disclosure.PublicAirportCode, disclosure.RadiusNM, disclosure.Retention, disclosure.Attribution)
}
