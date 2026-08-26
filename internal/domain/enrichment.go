package domain

import "time"

type AircraftMetadata struct {
	Source       DataSource
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

type Airline struct {
	Name          string
	ICAO          string
	IATA          string
	Country       string
	CountryISO    string
	RadioCallsign string
	Attribution   string
}

type Airport struct {
	CountryCode   string
	Municipality  string
	Name          string
	IATA          string
	ICAO          string
	Latitude      float64
	Longitude     float64
	HasPosition   bool
	ElevationFeet float64
	HasElevation  bool
	FetchedAt     time.Time
	ExpiresAt     time.Time
	Stale         bool
	Attribution   string
}

type Route struct {
	Source            DataSource
	Callsign          string
	AirlineName       string
	AirlineICAO       string
	AirlineIATA       string
	Origin            Airport
	Midpoint          *Airport
	Destination       Airport
	Airports          []Airport
	Plausible         bool
	PlausibilityKnown bool
	FetchedAt         time.Time
	ExpiresAt         time.Time
	Stale             bool
	Attribution       string
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
