package planealert

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Loader struct {
	url        string
	httpClient *http.Client
	now        func() time.Time
}

func NewLoader(url string, timeout time.Duration) *Loader {
	if strings.TrimSpace(url) == "" {
		url = DefaultCSVURL
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Loader{
		url:        url,
		httpClient: &http.Client{Timeout: timeout},
		now:        time.Now,
	}
}

func (loader *Loader) LatestCommitHash(ctx context.Context) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/sdr-enthusiasts/plane-alert-db/contents/", nil)
	if err != nil {
		return "", err
	}
	response, err := loader.httpClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("plane-alert-db commit lookup: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("plane-alert-db commit lookup: HTTP %s", response.Status)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return "", err
	}
	var files []struct {
		SHA      string `json:"sha"`
		Filename string `json:"name"`
	}
	if err := json.Unmarshal(body, &files); err != nil {
		return "", fmt.Errorf("plane-alert-db commit lookup: %w", err)
	}
	for _, file := range files {
		if file.Filename == "plane-alert-db-images.csv" {
			return file.SHA, nil
		}
	}
	return "", fmt.Errorf("plane-alert-db commit lookup: plane-alert-db-images.csv not found")
}

func (loader *Loader) FetchRecords(ctx context.Context) ([]Record, string, error) {
	commitHash := ""
	if !isCustomURL(loader.url) {
		hash, err := loader.LatestCommitHash(ctx)
		if err != nil {
			commitHash = "unknown"
		} else {
			commitHash = hash
		}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, loader.url, nil)
	if err != nil {
		return nil, "", err
	}
	response, err := loader.httpClient.Do(request)
	if err != nil {
		return nil, "", fmt.Errorf("fetch plane-alert-db CSV: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("fetch plane-alert-db CSV: HTTP %s", response.Status)
	}
	records, err := parseCSV(response.Body)
	if err != nil {
		return nil, "", err
	}
	if commitHash == "" {
		commitHash = loader.now().UTC().Format(time.RFC3339)
	}
	return records, commitHash, nil
}

func isCustomURL(url string) bool {
	return url != DefaultCSVURL && !strings.Contains(url, "sdr-enthusiasts/plane-alert-db")
}

func parseCSV(reader io.Reader) ([]Record, error) {
	csvReader := csv.NewReader(reader)
	rows, err := csvReader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("read plane-alert-db CSV: %w", err)
	}
	if len(rows) < 2 {
		return nil, fmt.Errorf("read plane-alert-db CSV: no data rows")
	}
	headers := headerMap(rows[0])
	records := make([]Record, 0, len(rows)-1)
	for _, row := range rows[1:] {
		icao := field(row, headers, "$ICAO")
		if icao == "" {
			continue
		}
		records = append(records, Record{
			ICAO:         strings.ToUpper(icao),
			Registration: field(row, headers, "$Registration"),
			Operator:     field(row, headers, "$Operator"),
			Type:         field(row, headers, "$Type"),
			ICAOType:     field(row, headers, "$ICAO Type"),
			Group:        field(row, headers, "#CMPG"),
			Tag1:         field(row, headers, "$Tag 1"),
			Tag2:         field(row, headers, "$#Tag 2"),
			Tag3:         field(row, headers, "$#Tag 3"),
			Category:     field(row, headers, "Category"),
			Link:         field(row, headers, "$#Link"),
			Image1:       field(row, headers, "#ImageLink"),
			Image2:       field(row, headers, "#ImageLink2"),
			Image3:       field(row, headers, "#ImageLink3"),
			Image4:       field(row, headers, "#ImageLink4"),
		})
	}
	return records, nil
}

func headerMap(headers []string) map[string]int {
	result := make(map[string]int, len(headers))
	for index, header := range headers {
		result[header] = index
	}
	return result
}

func field(row []string, headers map[string]int, name string) string {
	index, ok := headers[name]
	if !ok || index >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[index])
}
