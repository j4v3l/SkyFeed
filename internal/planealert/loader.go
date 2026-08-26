package planealert

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	maxMetadataBytes = 256 << 10
	maxCSVBytes      = 32 << 20
	maxCSVRows       = 100_000
	maxCSVFieldBytes = 4 << 10
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
	transport := &http.Transport{
		Proxy:             http.ProxyFromEnvironment,
		DialContext:       (&net.Dialer{Timeout: 2 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2: true, MaxIdleConns: 4, MaxIdleConnsPerHost: 2,
		IdleConnTimeout: 60 * time.Second, TLSHandshakeTimeout: 2 * time.Second,
		ResponseHeaderTimeout: min(timeout, 10*time.Second),
	}
	return &Loader{
		url:        url,
		httpClient: &http.Client{Transport: transport, Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
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
		discardBounded(response.Body, maxMetadataBytes)
		return "", fmt.Errorf("plane-alert-db commit lookup: HTTP %s", response.Status)
	}
	body, err := readBounded(response.Body, maxMetadataBytes)
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
	if !safeSourceURL(request.URL) {
		return nil, "", fmt.Errorf("fetch plane-alert-db CSV: source URL must use HTTPS or loopback HTTP")
	}
	request.Header.Set("Accept", "text/csv, application/csv;q=0.9")
	response, err := loader.httpClient.Do(request)
	if err != nil {
		return nil, "", fmt.Errorf("fetch plane-alert-db CSV: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		discardBounded(response.Body, maxMetadataBytes)
		return nil, "", fmt.Errorf("fetch plane-alert-db CSV: HTTP %s", response.Status)
	}
	if response.ContentLength > maxCSVBytes {
		return nil, "", fmt.Errorf("fetch plane-alert-db CSV: response is too large")
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
	limited := &io.LimitedReader{R: reader, N: maxCSVBytes + 1}
	csvReader := csv.NewReader(limited)
	csvReader.FieldsPerRecord = -1
	csvReader.ReuseRecord = true
	headings, err := csvReader.Read()
	if err != nil {
		return nil, fmt.Errorf("read plane-alert-db CSV header: %w", err)
	}
	headers := headerMap(append([]string(nil), headings...))
	records := make([]Record, 0, 4096)
	for rowNumber := 1; ; rowNumber++ {
		row, readErr := csvReader.Read()
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			if limited.N <= 0 {
				return nil, fmt.Errorf("read plane-alert-db CSV: response exceeds %d bytes", maxCSVBytes)
			}
			return nil, fmt.Errorf("read plane-alert-db CSV: %w", readErr)
		}
		if rowNumber > maxCSVRows {
			return nil, fmt.Errorf("read plane-alert-db CSV: exceeds %d rows", maxCSVRows)
		}
		for _, value := range row {
			if len(value) > maxCSVFieldBytes {
				return nil, fmt.Errorf("read plane-alert-db CSV: row %d contains an oversized field", rowNumber)
			}
		}
		icao := field(row, headers, "$ICAO")
		if icao == "" {
			continue
		}
		records = append(records, Record{
			ICAO: strings.ToUpper(icao), Registration: field(row, headers, "$Registration"), Operator: field(row, headers, "$Operator"),
			Type: field(row, headers, "$Type"), ICAOType: field(row, headers, "$ICAO Type"), Group: field(row, headers, "#CMPG"),
			Tag1: field(row, headers, "$Tag 1"), Tag2: field(row, headers, "$#Tag 2"), Tag3: field(row, headers, "$#Tag 3"),
			Category: field(row, headers, "Category"), Link: field(row, headers, "$#Link"), Image1: field(row, headers, "#ImageLink"),
			Image2: field(row, headers, "#ImageLink2"), Image3: field(row, headers, "#ImageLink3"), Image4: field(row, headers, "#ImageLink4"),
		})
	}
	if limited.N <= 0 {
		return nil, fmt.Errorf("read plane-alert-db CSV: response exceeds %d bytes", maxCSVBytes)
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("read plane-alert-db CSV: no data rows")
	}
	return records, nil
}

func readBounded(reader io.Reader, limit int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("response exceeds %d bytes", limit)
	}
	return body, nil
}

func discardBounded(reader io.Reader, limit int64) {
	_, _ = io.Copy(io.Discard, io.LimitReader(reader, limit))
}

func safeSourceURL(value *url.URL) bool {
	if value == nil || value.User != nil || value.Hostname() == "" || value.RawQuery != "" || value.Fragment != "" {
		return false
	}
	if value.Scheme == "https" {
		return true
	}
	if value.Scheme != "http" {
		return false
	}
	host := value.Hostname()
	return strings.EqualFold(host, "localhost") || (net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback())
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
