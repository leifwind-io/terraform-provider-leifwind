// SPDX-License-Identifier: MPL-2.0

package client_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"gitlab.com/leifwind/stream/terraform-provider-leifwind/client"
)

// countingTransport instruments OUR client's HTTP layer in-process: it counts
// round trips and, for the token endpoint, captures the server-reported
// expires_in (real ZITADEL v4.15.3 defaults to ~12h, not the RFC/fallback
// 3600s) so the test can advance the injected clock past the *actual*
// expiry instead of a hardcoded guess. This is not backend mocking — the
// response body is read and re-wrapped unchanged.
type countingTransport struct {
	calls     atomic.Int32
	next      http.RoundTripper
	expiresIn float64
}

func (c *countingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	c.calls.Add(1)
	resp, err := c.next.RoundTrip(r)
	if err == nil && resp != nil && resp.Body != nil {
		body, rerr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if rerr == nil {
			resp.Body = io.NopCloser(bytes.NewReader(body))
			var out struct {
				ExpiresIn float64 `json:"expires_in"`
			}
			if json.Unmarshal(body, &out) == nil && out.ExpiresIn > 0 {
				c.expiresIn = out.ExpiresIn
			}
		}
	}
	return resp, err
}

func TestStaticToken(t *testing.T) {
	ts := client.StaticToken("abc")
	tok, err := ts.Token(context.Background())
	if err != nil || tok != "abc" {
		t.Fatalf("got %q, %v", tok, err)
	}
}

func TestClientCredentialsFetchesCachesAndRefreshes(t *testing.T) {
	// perf: reuse the package-shared stack from TestMain (client_test.go)
	// instead of booting a dedicated one; the org still isolates this test.
	// The shared stack's toxiproxy is unused here — we hit s.Issuer directly.
	if stackErr != nil {
		t.Fatalf("stack: %v", stackErr)
	}
	s := sharedStack
	orgMu.Lock()
	org := s.NewOrg(t)
	orgMu.Unlock()

	now := time.Now()
	clock := func() time.Time { return now }
	ct := &countingTransport{next: http.DefaultTransport}
	ts := client.ClientCredentials(s.Issuer, org.ClientID, org.ClientSecret,
		client.WithAudience(s.Audience),
		client.WithCredentialClock(clock),
		client.WithCredentialHTTPClient(&http.Client{Transport: ct}))

	ctx := context.Background()
	tok1, err := ts.Token(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if tok1 == "" || ct.calls.Load() != 1 {
		t.Fatalf("first fetch: calls=%d", ct.calls.Load())
	}
	// cached: no second HTTP call
	tok2, _ := ts.Token(ctx)
	if tok2 != tok1 || ct.calls.Load() != 1 {
		t.Fatalf("expected cache hit, calls=%d", ct.calls.Load())
	}
	// advance past the server-reported expires_in ⇒ refresh (deviation from
	// the brief's hardcoded "2 * time.Hour": real ZITADEL v4.15.3 returns
	// expires_in=43199 (~12h), which a fixed 2h advance never crosses).
	now = now.Add(time.Duration(ct.expiresIn) * time.Second)
	if _, err := ts.Token(ctx); err != nil {
		t.Fatal(err)
	}
	if ct.calls.Load() != 2 {
		t.Fatalf("expected refresh, calls=%d", ct.calls.Load())
	}
}
