package planealert

import (
	"strings"
	"testing"
)

func TestParseCSVMapsPlaneAlertFields(t *testing.T) {
	csv := strings.Join([]string{
		"$ICAO,$Registration,$Operator,$Type,$ICAO Type,#CMPG,$Tag 1,$#Tag 2,$#Tag 3,Category,$#Link,#ImageLink,#ImageLink2,#ImageLink3,#ImageLink4",
		"ae1234,N12345,USAF,C-17,C17,Mil,Heavy,Transport,,Military,https://example.com/info,https://example.com/1.jpg,,,",
	}, "\n")
	records, err := parseCSV(strings.NewReader(csv))
	if err != nil {
		t.Fatalf("parse CSV: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("records=%d", len(records))
	}
	record := records[0]
	if record.ICAO != "AE1234" || record.Group != "Mil" || record.Operator != "USAF" {
		t.Fatalf("record=%+v", record)
	}
	if record.Tags() != "Heavy • Transport" {
		t.Fatalf("tags=%q", record.Tags())
	}
	if record.PrimaryImage() != "https://example.com/1.jpg" {
		t.Fatalf("image=%q", record.PrimaryImage())
	}
}
