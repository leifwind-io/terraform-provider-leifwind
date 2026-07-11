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

// Version is stamped into the default User-Agent.
const Version = "0.1.0-dev"

// RetryConfig bounds the retry loop (Task: retries). MaxAttempts 1 disables.
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
	ua := "terraform-provider-leifwind-client/" + Version
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

// do performs one request (retry wrapping added in the retry task).
func (c *Client) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	return c.doOnce(ctx, method, path, query, body, out)
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
			return fmt.Errorf("%s %s: encode: %w", method, path, err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rdr)
	if err != nil {
		return err
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
	if out != nil {
		if err := json.Unmarshal(rb, out); err != nil {
			return fmt.Errorf("%s %s: decode: %w", method, path, err)
		}
	}
	return nil
}
