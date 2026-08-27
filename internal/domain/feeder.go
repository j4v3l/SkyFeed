package domain

import (
	"errors"
	"strings"
	"time"
)

type FeederID string

const (
	FeederLocal FeederID = "local"
	FeederAll   FeederID = "all"
)

type FeederSourceKind string

const (
	FeederSourceLocal FeederSourceKind = "local"
	FeederSourceAgent FeederSourceKind = "agent"
)

type FeederDescriptor struct {
	ID                 FeederID
	DisplayName        string
	PublicArea         string
	AirportICAO        string
	WeatherStationICAO string
	Latitude           float64
	Longitude          float64
	HasCenter          bool
	SourceKind         FeederSourceKind
	Enabled            bool
}

type FeederSummary struct {
	FeederDescriptor
	Health        HealthStatus
	LastPublished time.Time
	Aircraft      int
}

func NormalizeFeederID(value string) (FeederID, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" || len(value) > 48 {
		return "", errors.New("feeder ID must contain 1 to 48 characters")
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' {
			return "", errors.New("feeder ID may contain only lowercase letters, numbers, and hyphens")
		}
	}
	return FeederID(value), nil
}

func (id FeederID) Valid() bool {
	_, err := NormalizeFeederID(string(id))
	return err == nil
}
