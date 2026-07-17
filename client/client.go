// SPDX-License-Identifier: MPL-2.0

package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// RetryConfig bounds the retry loop. MaxAttempts 1 disables retries.
// New normalizes invalid values: MaxAttempts < 1 becomes 1, a negative
// MinBackoff becomes 0, and MaxBackoff is raised to at least MinBackoff.
type RetryConfig struct {
	MaxAttempts int
	MinBackoff  time.Duration
	MaxBackoff  time.Duration
}

type settings struct {
	ts    TokenSource
	hc    *http.Client
	ua    string
	retry RetryConfig
}

// Option configures New.
type Option func(*settings)

// WithTokenSource sets the bearer-token source (required for authenticated APIs).
func WithTokenSource(ts TokenSource) Option { return func(s *settings) { s.ts = ts } }

// WithHTTPClient replaces the underlying *http.Client.
func WithHTTPClient(hc *http.Client) Option { return func(s *settings) { s.hc = hc } }

// WithUserAgent prepends a product token to the default User-Agent.
func WithUserAgent(ua string) Option { return func(s *settings) { s.ua = ua } }

// WithRetry overrides the default retry policy ({3, 250ms, 4s}).
func WithRetry(rc RetryConfig) Option { return func(s *settings) { s.retry = rc } }

// Client is a leifwind metadata API client. Safe for concurrent use.
type Client struct {
	Metadata *MetadataService
	Generic  *GenericService

	baseURL string
	hc      *http.Client
	ts      TokenSource
	ua      string
	retry   RetryConfig
}

// New creates a Client for the backend at endpoint.
func New(endpoint string, opts ...Option) (*Client, error) {
	if endpoint == "" {
		return nil, fmt.Errorf("client: endpoint is required")
	}
	s := settings{
		hc:    &http.Client{Timeout: 60 * time.Second},
		retry: RetryConfig{MaxAttempts: 3, MinBackoff: 250 * time.Millisecond, MaxBackoff: 4 * time.Second},
	}
	for _, o := range opts {
		o(&s)
	}
	// Normalize the retry config once: sleepBackoff must never see a
	// negative bound (rand.Int64N panics on non-positive arguments) nor a
	// bound whose +1 jitter increment would overflow int64.
	const maxJitterBackoff = time.Duration(1<<63 - 2)
	if s.retry.MaxAttempts < 1 {
		s.retry.MaxAttempts = 1
	}
	if s.retry.MinBackoff < 0 {
		s.retry.MinBackoff = 0
	}
	if s.retry.MinBackoff > maxJitterBackoff {
		s.retry.MinBackoff = maxJitterBackoff
	}
	if s.retry.MaxBackoff > maxJitterBackoff {
		s.retry.MaxBackoff = maxJitterBackoff
	}
	if s.retry.MaxBackoff < s.retry.MinBackoff {
		s.retry.MaxBackoff = s.retry.MinBackoff
	}
	ua := "terraform-provider-leifwind-client/" + Version()
	if s.ua != "" {
		ua = s.ua + " " + ua
	}
	c := &Client{
		baseURL: strings.TrimRight(endpoint, "/"),
		hc:      s.hc, ts: s.ts, ua: ua, retry: s.retry,
	}
	c.Metadata = &MetadataService{c: c}
	c.Generic = &GenericService{c: c}
	return c, nil
}

// do performs one request, routed through the retry loop (see retry.go).
func (c *Client) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	return c.doRetry(ctx, method, path, query, body, out)
}

func (c *Client) doOnce(ctx context.Context, method, path string, query url.Values, body, out any) error {
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return permanent(fmt.Errorf("%s %s: encode: %w", method, path, err))
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rdr)
	if err != nil {
		return permanent(fmt.Errorf("%s %s: build request: %w", method, path, err))
	}
	req.Header.Set("User-Agent", c.ua)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.ts != nil {
		tok, err := c.ts.Token(ctx)
		if err != nil {
			return fmt.Errorf("%s %s: token: %w", method, path, err)
		}
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	rb, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("%s %s: read body: %w", method, path, err)
	}
	if resp.StatusCode >= 400 {
		return newAPIError(method, path, resp.StatusCode, rb)
	}
	if out != nil && len(rb) > 0 {
		if err := json.Unmarshal(rb, out); err != nil {
			return permanent(fmt.Errorf("%s %s: decode: %w", method, path, err))
		}
	}
	return nil
}
