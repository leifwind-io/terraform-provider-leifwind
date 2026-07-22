// SPDX-License-Identifier: MPL-2.0

package leifwindtest

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// A failed exchange setup must not poison the Stack: the next call retries.
func TestExchangeSetupRetriesAfterFailure(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	env := attachEnvFixture()
	env["LW_TEST_ZITADEL_ISSUER_URL"] = srv.URL
	s, err := attachFromEnv(mapEnv(env))
	if err != nil {
		t.Fatal(err)
	}

	if err := s.ensureTokenExchange(); err == nil {
		t.Fatal("first setup against a 500-ing server must fail")
	}
	if s.exchangeReady {
		t.Fatal("exchangeReady must stay false after a failed setup")
	}
	after := calls.Load()
	if err := s.ensureTokenExchange(); err == nil {
		t.Fatal("second setup must also fail")
	}
	if calls.Load() == after {
		t.Fatal("second call made no HTTP calls — sync.Once poisoning is back")
	}
	if s.exchangeReady {
		t.Fatal("exchangeReady must stay false after failures")
	}
}
