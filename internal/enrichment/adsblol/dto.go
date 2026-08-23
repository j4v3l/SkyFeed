package adsblol

type routesRequestDTO struct {
	Planes []planeDTO `json:"planes"`
}

type planeDTO struct {
	Callsign  string  `json:"callsign"`
	Latitude  float64 `json:"lat"`
	Longitude float64 `json:"lng"`
}

type routeDTO struct {
	IATAAirportCodes string       `json:"_airport_codes_iata"`
	Airports         []airportDTO `json:"_airports"`
	AirlineCode      string       `json:"airline_code"`
	AirportCodes     string       `json:"airport_codes"`
	Callsign         string       `json:"callsign"`
	Number           string       `json:"number"`
	Plausible        *bool        `json:"plausible"`
}

type airportDTO struct {
	AltitudeFeet   *float64 `json:"alt_feet"`
	AltitudeMeters *float64 `json:"alt_meters"`
	CountryISO2    string   `json:"countryiso2"`
	IATA           string   `json:"iata"`
	ICAO           string   `json:"icao"`
	Latitude       *float64 `json:"lat"`
	Location       string   `json:"location"`
	Longitude      *float64 `json:"lon"`
	Name           string   `json:"name"`
}
