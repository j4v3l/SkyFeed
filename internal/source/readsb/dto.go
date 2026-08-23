package readsb

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
)

type aircraftResponse struct {
	Now      float64       `json:"now"`
	Messages uint64        `json:"messages"`
	Aircraft []aircraftDTO `json:"aircraft"`
}

type aircraftDTO struct {
	Hex          string      `json:"hex"`
	Type         string      `json:"type"`
	Flight       string      `json:"flight"`
	Registration string      `json:"r"`
	AircraftType string      `json:"t"`
	Category     string      `json:"category"`
	Squawk       string      `json:"squawk"`
	Emergency    string      `json:"emergency"`
	Latitude     *float64    `json:"lat"`
	Longitude    *float64    `json:"lon"`
	Altitude     altitudeDTO `json:"alt_baro"`
	GroundSpeed  *float64    `json:"gs"`
	Track        *float64    `json:"track"`
	BaroRate     *int        `json:"baro_rate"`
	GeomRate     *int        `json:"geom_rate"`
	Messages     uint64      `json:"messages"`
	Seen         float64     `json:"seen"`
	SeenPosition *float64    `json:"seen_pos"`
	RSSI         *float64    `json:"rssi"`
}

type altitudeDTO struct {
	Value  int
	Valid  bool
	Ground bool
}

func (altitude *altitudeDTO) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if bytes.Equal(data, []byte("null")) {
		return nil
	}
	if len(data) > 0 && data[0] == '"' {
		var value string
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		if value != "ground" {
			return fmt.Errorf("unexpected altitude string %q", value)
		}
		altitude.Valid = true
		altitude.Ground = true
		return nil
	}
	value, err := strconv.ParseFloat(string(data), 64)
	if err != nil {
		return fmt.Errorf("parse altitude: %w", err)
	}
	altitude.Value = int(value)
	altitude.Valid = true
	return nil
}

type receiverResponse struct {
	Version   string   `json:"version"`
	Refresh   int      `json:"refresh"`
	History   int      `json:"history"`
	Latitude  *float64 `json:"lat"`
	Longitude *float64 `json:"lon"`
}

type statsResponse struct {
	Now                     float64     `json:"now"`
	AircraftWithPosition    *int        `json:"aircraft_with_pos"`
	AircraftWithoutPosition *int        `json:"aircraft_without_pos"`
	Latest                  statsPeriod `json:"latest"`
	Last1Min                statsPeriod `json:"last1min"`
}

type statsPeriod struct {
	Start               float64  `json:"start"`
	End                 float64  `json:"end"`
	Messages            uint64   `json:"messages"`
	MaxDistance         *float64 `json:"max_distance"`
	MaxDistanceInMetres *float64 `json:"max_distance_in_metres"`
	Tracks              struct {
		All           int `json:"all"`
		SingleMessage int `json:"single_message"`
	} `json:"tracks"`
}
