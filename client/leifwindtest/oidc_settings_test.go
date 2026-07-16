// SPDX-License-Identifier: MPL-2.0

package leifwindtest

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fatalRecorder captures Fatalf and panics to stop execution the way a real
// t.Fatalf would (the guard under test must not fall through to HTTP).
type fatalRecorder struct {
	testing.TB
	msg string
}

func (f *fatalRecorder) Fatalf(format string, args ...any) {
	f.msg = fmt.Sprintf(format, args...)
	panic("fatalRecorder")
}
func (f *fatalRecorder) Helper() {}

func TestSetAccessTokenLifetimeRejectsNonWholeSeconds(t *testing.T) {
	t.Parallel()
	for _, d := range []time.Duration{0, -time.Second, 1500 * time.Millisecond} {
		rec := &fatalRecorder{TB: t}
		func() {
			defer func() { _ = recover() }()
			(&Stack{}).SetAccessTokenLifetime(rec, d)
		}()
		if !strings.Contains(rec.msg, "whole number of seconds") {
			t.Errorf("lifetime %v: guard did not fire (msg=%q)", d, rec.msg)
		}
	}
}

func TestSetAccessTokenLifetimeSerializesWholeSeconds(t *testing.T) {
	t.Parallel()
	var putBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/admin/v1/settings/oidc" {
			http.NotFound(w, r)
			return
		}
		switch r.Method {
		case http.MethodGet:
			_, _ = w.Write([]byte(`{"settings":{"accessTokenLifetime":"43200s","idTokenLifetime":"43200s","refreshTokenIdleExpiration":"1296000s","refreshTokenExpiration":"7776000s"}}`))
		case http.MethodPut:
			b, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			putBody = b
			_, _ = w.Write([]byte(`{}`))
		default:
			http.Error(w, "method", http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(srv.Close)

	(&Stack{Issuer: srv.URL}).SetAccessTokenLifetime(t, 5*time.Second)

	var sent oidcSettings
	if err := json.Unmarshal(putBody, &sent); err != nil {
		t.Fatalf("unmarshal PUT body: %v", err)
	}
	if sent.AccessTokenLifetime != "5s" {
		t.Fatalf("accessTokenLifetime = %q, want \"5s\"", sent.AccessTokenLifetime)
	}
}
