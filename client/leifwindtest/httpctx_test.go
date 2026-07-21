// SPDX-License-Identifier: MPL-2.0

package leifwindtest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

// blockingServer returns a server whose handler blocks until the test ends,
// plus an attach-mode Stack pointed at it.
func blockingServer(t *testing.T) (*httptest.Server, *Stack) {
	t.Helper()
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-done
	}))
	t.Cleanup(func() { close(done); srv.Close() })
	env := attachEnvFixture()
	env["LW_TEST_ZITADEL_ISSUER_URL"] = srv.URL
	s, err := attachFromEnv(mapEnv(env))
	if err != nil {
		t.Fatal(err)
	}
	return srv, s
}

func TestMgmtDoHonorsStackContext(t *testing.T) {
	_, s := blockingServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	s.ctx = ctx
	start := time.Now()
	err := s.mgmtDo("GET", "/management/v1/ping", "", nil, nil)
	if err == nil {
		t.Fatal("want error from cancelled context")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("mgmtDo ignored ctx cancellation, took %v", elapsed)
	}
}

func TestFetchTokenHonorsContext(t *testing.T) {
	srv, _ := blockingServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, _, err := fetchToken(ctx, srv.URL, "id", "secret", url.Values{})
	if err == nil {
		t.Fatal("want error from cancelled context")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("fetchToken ignored ctx cancellation, took %v", elapsed)
	}
}
