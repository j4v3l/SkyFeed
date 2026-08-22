package domain

import "time"

type AircraftMetadata struct {
	Registration string
	AircraftType string
	ICAOType     string
	Manufacturer string
	Owner        string
	OwnerCountry string
	OperatorFlag string
	PhotoURL     string
	ThumbnailURL string
}

type Airport struct {
	CountryCode  string
	Municipality string
	Name         string
	IATA         string
	ICAO         string
}

type Route struct {
	Callsign    string
	AirlineName string
	AirlineICAO string
	AirlineIATA string
	Origin      Airport
	Midpoint    *Airport
	Destination Airport
}

type Enrichment struct {
	ICAO        string
	Callsign    string
	Aircraft    *AircraftMetadata
	Route       *Route
	Found       bool
	FetchedAt   time.Time
	ExpiresAt   time.Time
	Stale       bool
	Attribution string
}
