package cmd

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

// parseHeaderFlags parses repeated -H 'Key: Value' flags into the given map,
// overriding entries that are already present.
func parseHeaderFlags(headerFlags []string, into map[string]string) error {
	for _, h := range headerFlags {
		key, value, found := strings.Cut(h, ":")
		if !found {
			return fmt.Errorf("invalid header format: %s (expected 'Key: Value')", h)
		}

		into[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}

	return nil
}

// newHTTPClient creates an HTTP client with a timeout that sets the given
// headers on every request.
func newHTTPClient(headers map[string]string) *http.Client {
	client := &http.Client{
		Timeout: 60 * time.Second,
	}

	if len(headers) > 0 {
		client.Transport = &headerTransport{
			base:    http.DefaultTransport,
			headers: headers,
		}
	}

	return client
}

// headerTransport is a RoundTripper that sets headers on every request.
type headerTransport struct {
	base    http.RoundTripper
	headers map[string]string
}

func (t *headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// clone the request to avoid modifying the original
	req = req.Clone(req.Context())

	// Set, not Add: a user-supplied Content-Type must replace the default one
	for key, value := range t.headers {
		req.Header.Set(key, value)
	}

	return t.base.RoundTrip(req)
}
