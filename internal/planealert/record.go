package planealert

import "strings"

const (
	DefaultCSVURL = "https://raw.githubusercontent.com/sdr-enthusiasts/plane-alert-db/refs/heads/main/plane-alert-db-images.csv"
	Attribution   = "Interesting aircraft classifications from plane-alert-db (community curated ICAO list)"
)

type Record struct {
	ICAO         string
	Registration string
	Operator     string
	Type         string
	ICAOType     string
	Group        string
	Tag1         string
	Tag2         string
	Tag3         string
	Category     string
	Link         string
	Image1       string
	Image2       string
	Image3       string
	Image4       string
}

func (record Record) Tags() string {
	parts := make([]string, 0, 3)
	for _, tag := range []string{record.Tag1, record.Tag2, record.Tag3} {
		tag = strings.TrimSpace(tag)
		if tag != "" {
			parts = append(parts, tag)
		}
	}
	return strings.Join(parts, " • ")
}

func (record Record) PrimaryImage() string {
	for _, image := range []string{record.Image1, record.Image2, record.Image3, record.Image4} {
		image = strings.TrimSpace(image)
		if image != "" {
			return image
		}
	}
	return ""
}
