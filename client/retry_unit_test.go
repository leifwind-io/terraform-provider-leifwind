// SPDX-License-Identifier: MPL-2.0

package client_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"gitlab.com/leifwind/stream/terraform-provider-leifwind/client"
)

// countingServer returns a server running handler and a counter of requests
// that actually reached the wire.
func countingServer(t *testing.T, handler func(w http.ResponseWriter, _ *http.Request)) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func TestNegativeMaxBackoffDoesNotPanic(t *testing.T) {
	t.Parallel()
	srv, calls := countingServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	c, err := client.New(srv.URL, client.WithRetry(client.RetryConfig{MaxAttempts: 3, MaxBackoff: -1}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Metadata.ListProjects(context.Background(), client.ListOpts{}); err == nil {
		t.Fatal("expected 500 error")
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("want 3 attempts against a 5xx server, got %d", got)
	}
}

func TestMalformedResponseBodyIsNotRetried(t *testing.T) {
	t.Parallel()
	srv, calls := countingServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{not json`))
	})
	c, err := client.New(srv.URL, client.WithRetry(client.RetryConfig{
		MaxAttempts: 5, MinBackoff: time.Millisecond, MaxBackoff: time.Millisecond}))
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Metadata.ListProjects(context.Background(), client.ListOpts{})
	if err == nil || !strings.Contains(err.Error(), "decode") {
		t.Fatalf("want decode error, got %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("a 2xx decode failure must not re-execute the request: %d attempts", got)
	}
}

func TestUnmarshalableRequestBodyIsNotRetried(t *testing.T) {
	// Regression guard for spec test 3: MetadataField.MarshalJSON fails for
	// FRAGMENT without FragmentName — a deterministic encode error that must
	// never reach the wire. (The retry classification's red/green cycle is
	// driven by TestMalformedResponseBodyIsNotRetried; this test also passes
	// pre-change because the encode error fires before any request.)
	t.Parallel()
	srv, calls := countingServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	})
	c, err := client.New(srv.URL, client.WithRetry(client.RetryConfig{
		MaxAttempts: 5, MinBackoff: time.Millisecond, MaxBackoff: time.Millisecond}))
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	_, err = c.Metadata.UpsertField(context.Background(), client.MetadataField{
		ProjectID: id, EntityID: id, Name: "f",
		Config:     client.FieldConfig{DataType: client.DataTypeText},
		Connection: client.Connection{Type: client.ConnectionFragment}, // no FragmentName → marshal error
	})
	if err == nil || !strings.Contains(err.Error(), "encode") {
		t.Fatalf("want encode error, got %v", err)
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("an encode failure must issue zero requests, got %d", got)
	}
}
