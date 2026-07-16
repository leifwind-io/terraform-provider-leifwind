// SPDX-License-Identifier: MPL-2.0

package leifwindtest

import (
	"fmt"
	"testing"
	"time"
)

// oidcSettings mirrors the payload of ZITADEL's admin OIDC settings API
// (GET/PUT /admin/v1/settings/oidc). Durations are protobuf-style strings
// with a trailing "s" (e.g. "43200s" for 12h); SetAccessTokenLifetime
// formats these strings internally before sending to the wire.
type oidcSettings struct {
	AccessTokenLifetime        string `json:"accessTokenLifetime"`
	IDTokenLifetime            string `json:"idTokenLifetime"`
	RefreshTokenIdleExpiration string `json:"refreshTokenIdleExpiration"`
	RefreshTokenExpiration     string `json:"refreshTokenExpiration"`
}

// SetAccessTokenLifetime overrides this Stack's INSTANCE-WIDE OIDC
// access-token lifetime via ZITADEL's admin API. There is no org- or
// app-level granularity in v4.15.3 (zitadel/zitadel#5219 is still open as
// of writing): every org and every app on the instance is affected for the
// rest of the Stack's lifetime. Callers MUST use a Stack dedicated to the
// test that needs a short lifetime (e.g. via Start(t), never a
// package-shared stack), since every other acceptance test sharing an
// instance would otherwise start minting tokens with the same short
// lifetime.
//
// lifetime must be a positive whole number of seconds — seconds are
// ZITADEL's evidenced granularity, and silently rounding would change test
// semantics, so anything finer fails the test.
//
// Feasibility (verified on v4.15.3): PUT accessTokenLifetime=10s against a
// fresh instance is accepted (200) and takes effect immediately — the next
// machine-user token minted afterward had exp-iat == 10s exactly.
func (s *Stack) SetAccessTokenLifetime(t testing.TB, lifetime time.Duration) {
	t.Helper()
	if lifetime <= 0 || lifetime%time.Second != 0 {
		t.Fatalf("SetAccessTokenLifetime: lifetime must be a positive whole number of seconds, got %v", lifetime)
	}
	var current struct {
		Settings oidcSettings `json:"settings"`
	}
	if err := s.mgmtDo("GET", "/admin/v1/settings/oidc", "", nil, &current); err != nil {
		t.Fatalf("get oidc settings: %v", err)
	}
	current.Settings.AccessTokenLifetime = fmt.Sprintf("%ds", int64(lifetime/time.Second))
	if err := s.mgmtDo("PUT", "/admin/v1/settings/oidc", "", current.Settings, nil); err != nil {
		t.Fatalf("put oidc settings: %v", err)
	}
}
