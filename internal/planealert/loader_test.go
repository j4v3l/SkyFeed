package planealert

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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

func TestParseCSVRejectsOversizedFieldAndMalformedInput(t *testing.T) {
	header := "$ICAO,$Registration\n"
	if _, err := parseCSV(strings.NewReader(header + "ABC123," + strings.Repeat("x", maxCSVFieldBytes+1))); err == nil || !strings.Contains(err.Error(), "oversized field") {
		t.Fatalf("oversized field error = %v", err)
	}
	if _, err := parseCSV(strings.NewReader(header + `"unterminated`)); err == nil {
		t.Fatal("malformed CSV accepted")
	}
}

func TestLoaderRejectsRedirectAndOversizedContentLength(t *testing.T) {
	t.Run("redirect", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			http.Redirect(writer, request, "https://example.com/untrusted.csv", http.StatusFound)
		}))
		defer server.Close()
		loader := NewLoader(server.URL, time.Second)
		if _, _, err := loader.FetchRecords(context.Background()); err == nil || !strings.Contains(err.Error(), "302") {
			t.Fatalf("redirect error = %v", err)
		}
	})
	t.Run("content-length", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.Header().Set("Content-Length", "40000000")
			writer.WriteHeader(http.StatusOK)
		}))
		defer server.Close()
		loader := NewLoader(server.URL, time.Second)
		if _, _, err := loader.FetchRecords(context.Background()); err == nil || !strings.Contains(err.Error(), "too large") {
			t.Fatalf("oversized response error = %v", err)
		}
	})
}

func TestSafeSourceURLRejectsCredentialsQueryAndNonLoopbackHTTP(t *testing.T) {
	for _, raw := range []string{"http://example.com/data.csv", "https://user@example.com/data.csv", "https://example.com/data.csv?q=1"} {
		request, err := http.NewRequest(http.MethodGet, raw, nil)
		if err != nil {
			t.Fatal(err)
		}
		if safeSourceURL(request.URL) {
			t.Fatalf("unsafe URL accepted: %s", raw)
		}
	}
}
