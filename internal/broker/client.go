package broker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// BaseURL is the base URL used with UnixClient; the host is ignored.
const BaseURL = "http://broker"

// UnixClient returns an HTTP client that sends every request to the broker
// socket at path.
func UnixClient(path string) *http.Client {
	var d net.Dialer
	return &http.Client{
		Timeout: 3 * time.Minute, // above update.check's own timeout
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return d.DialContext(ctx, "unix", path)
			},
		},
	}
}

// Client calls the broker from the berth CLI.
type Client struct {
	HTTP *http.Client
}

// NewClient returns a client for the socket at path.
func NewClient(path string) Client { return Client{HTTP: UnixClient(path)} }

// Status fetches GET /status.
func (c Client) Status(ctx context.Context) (StatusReport, error) {
	var st StatusReport
	code, raw, err := c.do(ctx, http.MethodGet, "/status", nil)
	if err != nil {
		return st, err
	}
	if code != http.StatusOK {
		return st, fmt.Errorf("the supervisor answered %d: %s", code, bytes.TrimSpace(raw))
	}
	return st, json.Unmarshal(raw, &st)
}

// Verb posts in (nil sends {}) to a verb and returns the status code and the
// response body.
func (c Client) Verb(ctx context.Context, verb string, in any) (int, json.RawMessage, error) {
	body := []byte("{}")
	if in != nil {
		var err error
		if body, err = json.Marshal(in); err != nil {
			return 0, nil, err
		}
	}
	return c.do(ctx, http.MethodPost, "/verbs/"+verb, body)
}

func (c Client) do(ctx context.Context, method, path string, body []byte) (int, json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, method, BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("the supervisor is not reachable: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	return resp.StatusCode, raw, err
}
