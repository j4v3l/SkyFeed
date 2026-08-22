package health

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
)

const maxHealthBodyBytes = 16 << 10

func Check(ctx context.Context, address string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("parse health address: %w", err)
	}
	switch host {
	case "", "0.0.0.0", "::":
		host = "127.0.0.1"
	}

	endpoint := url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort(host, port),
		Path:   "/healthz",
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return fmt.Errorf("create health request: %w", err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return fmt.Errorf("health request: %w", err)
	}
	defer response.Body.Close()
	if _, err := io.Copy(io.Discard, io.LimitReader(response.Body, maxHealthBodyBytes)); err != nil {
		return fmt.Errorf("read health response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("health endpoint returned %s", response.Status)
	}
	return nil
}
