package adsbdb

type responseDTO struct {
	Response payloadDTO `json:"response"`
}

type payloadDTO struct {
	Aircraft    *aircraftDTO `json:"aircraft"`
	FlightRoute *routeDTO    `json:"flightroute"`
}

type aircraftDTO struct {
	Type                            string `json:"type"`
	ICAOType                        string `json:"icao_type"`
	Manufacturer                    string `json:"manufacturer"`
	ModeS                           string `json:"mode_s"`
	Registration                    string `json:"registration"`
	RegisteredOwnerCountryISOName   string `json:"registered_owner_country_iso_name"`
	RegisteredOwnerCountryName      string `json:"registered_owner_country_name"`
	RegisteredOwnerOperatorFlagCode string `json:"registered_owner_operator_flag_code"`
	RegisteredOwner                 string `json:"registered_owner"`
	PhotoURL                        string `json:"url_photo"`
	ThumbnailURL                    string `json:"url_photo_thumbnail"`
}

type routeDTO struct {
	Callsign     string      `json:"callsign"`
	CallsignICAO string      `json:"callsign_icao"`
	CallsignIATA string      `json:"callsign_iata"`
	Airline      *airlineDTO `json:"airline"`
	Origin       *airportDTO `json:"origin"`
	Midpoint     *airportDTO `json:"midpoint"`
	Destination  *airportDTO `json:"destination"`
}

type airlineDTO struct {
	Name string `json:"name"`
	ICAO string `json:"icao"`
	IATA string `json:"iata"`
}

type airportDTO struct {
	CountryISOName string `json:"country_iso_name"`
	Municipality   string `json:"municipality"`
	Name           string `json:"name"`
	IATACode       string `json:"iata_code"`
	ICAOCode       string `json:"icao_code"`
}
