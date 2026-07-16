// SPDX-License-Identifier: MPL-2.0

package client_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"gitlab.com/leifwind/stream/terraform-provider-leifwind/client"
)

func TestTokenEndpointEmptyAccessTokenIsErrorAndNotCached(t *testing.T) {
	t.Parallel()
	for _, body := range []string{`{}`, `{"access_token": ""}`} {
		var calls atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			calls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		}))
		ts := client.ClientCredentials(srv.URL, "id", "secret")
		if _, err := ts.Token(context.Background()); err == nil || !strings.Contains(err.Error(), "access_token") {
			t.Errorf("body %s: want access_token error, got %v", body, err)
		}
		// nothing cached: the next call must hit the endpoint again
		_, _ = ts.Token(context.Background())
		if got := calls.Load(); got != 2 {
			t.Errorf("body %s: want a re-fetch after the failure, got %d calls", body, got)
		}
		srv.Close()
	}
}

func TestTokenEndpointBodyReadErrorIsWrapped(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "1000") // promise more than we send
		_, _ = w.Write([]byte(`{"access_`))
	}))
	t.Cleanup(srv.Close)
	ts := client.ClientCredentials(srv.URL, "id", "secret")
	if _, err := ts.Token(context.Background()); err == nil || !strings.Contains(err.Error(), "read body") {
		t.Fatalf("want wrapped read error, got %v", err)
	}
}
