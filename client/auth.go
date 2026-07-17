// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// TokenSource supplies the bearer token for every request. Implementations
// must be safe for concurrent use.
type TokenSource interface {
	Token(ctx context.Context) (string, error)
}

type staticToken string

func (s staticToken) Token(context.Context) (string, error) { return string(s), nil }

// StaticToken returns a TokenSource that always yields token (delegated /
// runner path: the caller owns acquisition and refresh).
func StaticToken(token string) TokenSource { return staticToken(token) }

const refreshMargin = 60 * time.Second

type ccSettings struct {
	audience string
	now      func() time.Time
	hc       *http.Client
}

// CredentialOption configures ClientCredentials.
type CredentialOption func(*ccSettings)

// WithAudience requests the ZITADEL project-audience scope.
func WithAudience(audience string) CredentialOption {
	return func(s *ccSettings) { s.audience = audience }
}

// WithCredentialClock injects the clock (tests).
func WithCredentialClock(now func() time.Time) CredentialOption {
	return func(s *ccSettings) { s.now = now }
}

// WithCredentialHTTPClient injects the HTTP client used for token fetches.
func WithCredentialHTTPClient(hc *http.Client) CredentialOption {
	return func(s *ccSettings) { s.hc = hc }
}

type ccTokenSource struct {
	issuer, clientID, clientSecret string
	scopes                         []string
	now                            func() time.Time
	hc                             *http.Client

	mu        sync.Mutex
	token     string
	expiresAt time.Time
}

// ClientCredentials returns an auto-refreshing M2M TokenSource against
// ZITADEL's token endpoint (mirrors the Python ClientCredentialsTokenProvider:
// basic auth, resourceowner + project-audience scopes, refresh 60s early).
//
//nolint:revive // name is the plan-wide public API contract (task brief); renaming would break it
func ClientCredentials(issuer, clientID, clientSecret string, opts ...CredentialOption) TokenSource {
	s := ccSettings{now: time.Now, hc: http.DefaultClient}
	for _, o := range opts {
		o(&s)
	}
	scopes := []string{"openid", "urn:zitadel:iam:user:resourceowner"}
	if s.audience != "" {
		scopes = append(scopes, "urn:zitadel:iam:org:project:id:"+s.audience+":aud")
	}
	return &ccTokenSource{
		issuer: strings.TrimRight(issuer, "/"), clientID: clientID, clientSecret: clientSecret,
		scopes: scopes, now: s.now, hc: s.hc,
	}
}

func (c *ccTokenSource) Token(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && c.now().Before(c.expiresAt.Add(-refreshMargin)) {
		return c.token, nil
	}
	form := url.Values{
		"grant_type": {"client_credentials"},
		"scope":      {strings.Join(c.scopes, " ")},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.issuer+"/oauth/v2/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(c.clientID, c.clientSecret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.hc.Do(req)
	if err != nil {
		return "", fmt.Errorf("token endpoint: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("token endpoint: read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", &APIError{StatusCode: resp.StatusCode, Detail: string(body),
			Method: http.MethodPost, Path: "/oauth/v2/token"}
	}
	var out struct {
		AccessToken string  `json:"access_token"`
		ExpiresIn   float64 `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", err
	}
	if out.ExpiresIn == 0 {
		out.ExpiresIn = 3600
	}
	if out.AccessToken == "" {
		return "", fmt.Errorf("token endpoint: 200 response without access_token")
	}
	c.token = out.AccessToken
	c.expiresAt = c.now().Add(time.Duration(out.ExpiresIn) * time.Second)
	return c.token, nil
}
