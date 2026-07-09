// Package httpclient provides a small, reusable JSON HTTP client that the
// other internal packages (trmnl, homeassistant, ...) build their
// service-specific clients on top of, instead of each hand-rolling their own
// request/response plumbing.
package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client sends JSON requests to a single base URL with a shared set of
// default headers (auth, content-type, ...) and timeout.
type Client struct {
	baseURL    string
	httpClient *http.Client
	headers    http.Header
}

type Option func(*Client)

// WithHTTPClient overrides the default *http.Client (e.g. for custom
// transports in tests).
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) { c.httpClient = hc }
}

// WithTimeout sets the request timeout. Default is 10s.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) { c.httpClient.Timeout = d }
}

// WithHeader sets a default header sent with every request.
func WithHeader(key, value string) Option {
	return func(c *Client) { c.headers.Set(key, value) }
}

// WithBearerToken sets the Authorization header to "Bearer <token>".
func WithBearerToken(token string) Option {
	return WithHeader("Authorization", "Bearer "+token)
}

func New(baseURL string, opts ...Option) *Client {
	c := &Client{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		headers:    make(http.Header),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// StatusError is returned by Do when the server responds with a status code
// >= 300. It carries the body so callers can surface the upstream error.
type StatusError struct {
	StatusCode int
	Body       string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("unexpected status %d: %s", e.StatusCode, e.Body)
}

// Do sends a request to path (joined to the client's base URL; pass "" to
// hit the base URL directly, e.g. for a webhook URL that has no sub-paths).
// If body is non-nil it's marshaled as the JSON request body. If out is
// non-nil, the JSON response body is decoded into it.
func (c *Client) Do(ctx context.Context, method, path string, body, out any) error {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.url(path), reqBody)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	for key, values := range c.headers {
		for _, v := range values {
			req.Header.Add(key, v)
		}
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode >= 300 {
		return &StatusError{StatusCode: resp.StatusCode, Body: string(respBody)}
	}

	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("decode response body: %w", err)
		}
	}

	return nil
}

func (c *Client) url(path string) string {
	if path == "" {
		return c.baseURL
	}
	return strings.TrimRight(c.baseURL, "/") + "/" + strings.TrimLeft(path, "/")
}
