// SPDX-License-Identifier: MPL-2.0

package client_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"gitlab.com/leifwind/stream/terraform-provider-leifwind/client"
)

// proxiedClient returns a client whose traffic crosses toxiproxy, plus the
// counting transport for attempt assertions.
func proxiedClient(t *testing.T, rc client.RetryConfig) (*client.Client, *countingTransport) {
	t.Helper()
	orgMu.Lock()
	org := sharedStack.NewOrg(t)
	orgMu.Unlock()
	ct := &countingTransport{next: http.DefaultTransport}
	c, err := client.New(sharedStack.ProxiedBackendURL,
		client.WithTokenSource(org.TokenSource(sharedStack)),
		client.WithHTTPClient(&http.Client{Transport: ct, Timeout: 30 * time.Second}),
		client.WithRetry(rc))
	if err != nil {
		t.Fatal(err)
	}
	return c, ct
}

func TestRetriesTransportErrorThenSucceeds(t *testing.T) {
	proxy := sharedStack.Toxiproxy() // shared proxy: no t.Parallel in this file
	c, ct := proxiedClient(t, client.RetryConfig{MaxAttempts: 5, MinBackoff: 300 * time.Millisecond, MaxBackoff: time.Second})

	if err := proxy.Disable(); err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(700 * time.Millisecond)
		_ = proxy.Enable()
	}()
	_, err := c.Metadata.ListProjects(context.Background(), client.ListOpts{})
	if err != nil {
		t.Fatalf("expected recovery via retry, got %v", err)
	}
	if ct.calls.Load() < 2 {
		t.Fatalf("expected ≥2 attempts, got %d", ct.calls.Load())
	}
}

func TestNoRetryOn4xx(t *testing.T) {
	c, ct := proxiedClient(t, client.RetryConfig{MaxAttempts: 5, MinBackoff: 100 * time.Millisecond, MaxBackoff: time.Second})
	ctx := context.Background()
	p, err := c.Metadata.UpsertProject(ctx, client.MetadataProject{Name: "retry_proj"})
	if err != nil {
		t.Fatal(err)
	}
	before := ct.calls.Load()
	_, err = c.Metadata.UpsertProject(ctx, client.MetadataProject{ObjectID: p.ObjectID, Name: "renamed"})
	if !errors.Is(err, client.ErrValidation) {
		t.Fatalf("want ErrValidation, got %v", err)
	}
	if ct.calls.Load()-before != 1 {
		t.Fatalf("4xx must not retry: %d attempts", ct.calls.Load()-before)
	}
}

func TestContextCancelAbortsBackoff(t *testing.T) {
	proxy := sharedStack.Toxiproxy()
	c, _ := proxiedClient(t, client.RetryConfig{MaxAttempts: 10, MinBackoff: 2 * time.Second, MaxBackoff: 8 * time.Second})
	if err := proxy.Disable(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = proxy.Enable() }()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := c.Metadata.ListProjects(ctx, client.ListOpts{})
	if err == nil {
		t.Fatal("expected error")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("backoff ignored context: took %v", elapsed)
	}
}

func TestDeleteRetryTolerates404(t *testing.T) {
	proxy := sharedStack.Toxiproxy()
	// MaxAttempts 10: the toxic below is lifted on a wall-clock timer while
	// full-jitter backoff has no lower bound, so with few attempts the whole
	// retry budget can fit inside the toxic window and every attempt fails
	// (observed on CI). Ten attempts make that practically impossible while
	// the happy path still finishes after the first post-window attempt.
	c, ct := proxiedClient(t, client.RetryConfig{MaxAttempts: 10, MinBackoff: 400 * time.Millisecond, MaxBackoff: 2 * time.Second})
	ctx := context.Background()
	p, err := c.Metadata.UpsertProject(ctx, client.MetadataProject{Name: "del_retry"})
	if err != nil {
		t.Fatal(err)
	}

	// Truncate the RESPONSE of the next call: the backend processes the
	// DELETE, the client sees a transport error, the retry sees 404 —
	// which must be treated as success.
	toxic, err := proxy.AddToxic("truncate-down", "limit_data", "downstream", 1.0,
		map[string]any{"bytes": 1})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(600 * time.Millisecond)
		_ = proxy.RemoveToxic(toxic.Name)
	}()

	before := ct.calls.Load()
	if err := c.Metadata.DeleteProject(ctx, *p.ObjectID); err != nil {
		t.Fatalf("DELETE retry must tolerate 404 after failed attempt (%d wire attempts), got %v",
			ct.calls.Load()-before, err)
	}
	if attempts := ct.calls.Load() - before; attempts < 2 {
		t.Fatalf("DELETE retry test made only %d wire attempt(s); expected the toxic first attempt plus a retry", attempts)
	}
	if _, err := c.Metadata.GetProject(ctx, *p.ObjectID); !errors.Is(err, client.ErrNotFound) {
		t.Fatalf("project should be gone, got %v", err)
	}
}
