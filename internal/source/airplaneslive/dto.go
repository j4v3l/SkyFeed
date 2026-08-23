package airplaneslive

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
)

type pointResponse struct {
	Aircraft []aircraftDTO `json:"ac"`
	Message  string        `json:"msg"`
	Now      *int64        `json:"now"`
	Total    *int          `json:"total"`
	CacheAt  *int64        `json:"ctime"`
	Process  *float64      `json:"ptime"`
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
	Messages     *uint64     `json:"messages"`
	Seen         *float64    `json:"seen"`
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
			return fmt.Errorf("decode altitude string: %w", err)
		}
		if value != "ground" {
			return fmt.Errorf("altitude string is not supported")
		}
		altitude.Valid = true
		altitude.Ground = true
		return nil
	}

	number := json.Number(string(data))
	value, err := strconv.ParseInt(number.String(), 10, 32)
	if err != nil {
		return fmt.Errorf("altitude must be an integer")
	}
	altitude.Value = int(value)
	altitude.Valid = true
	return nil
}
