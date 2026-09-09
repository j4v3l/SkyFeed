package render

import (
	"net/url"
	"strings"
)

// SafeHTTPSURL returns a normalized https URL when raw is a safe external link.
func SafeHTTPSURL(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return "", false
	}
	host := strings.ToLower(parsed.Hostname())
	if !allowedPlaneAlertHost(host) {
		return "", false
	}
	return parsed.String(), true
}

func SafePlaneAlertImageURL(raw string) (string, bool) {
	value, ok := SafeHTTPSURL(raw)
	if !ok {
		return "", false
	}
	parsed, _ := url.Parse(value)
	host := strings.ToLower(parsed.Hostname())
	if host == "upload.wikimedia.org" || host == "raw.githubusercontent.com" || host == "planespotters.net" || host == "www.planespotters.net" || strings.HasSuffix(host, ".planespotters.net") {
		return value, true
	}
	return "", false
}

func allowedPlaneAlertHost(host string) bool {
	for _, allowed := range []string{"github.com", "raw.githubusercontent.com", "w.wiki", "wikipedia.org", "wikimedia.org", "planespotters.net"} {
		if host == allowed || strings.HasSuffix(host, "."+allowed) {
			return true
		}
	}
	return false
}

func referenceLinkLabel(raw string) string {
	return "Open reference"
}
