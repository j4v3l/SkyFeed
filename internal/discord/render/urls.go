package render

import (
	"net/url"
	"strings"
	"unicode/utf8"
)

// SafeHTTPSURL returns a normalized https URL when raw is a safe external link.
func SafeHTTPSURL(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return "", false
	}
	return parsed.String(), true
}

func referenceLinkLabel(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return "Reference"
	}
	host := parsed.Host
	if strings.HasPrefix(strings.ToLower(host), "www.") {
		host = host[4:]
	}
	if utf8.RuneCountInString(host) > 32 {
		return "Reference"
	}
	return host
}
