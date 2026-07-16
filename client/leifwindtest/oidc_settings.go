// SPDX-License-Identifier: MPL-2.0

package leifwindtest

import "testing"

// oidcSettings mirrors the payload of ZITADEL's admin OIDC settings API
// (GET/PUT /admin/v1/settings/oidc). Durations are protobuf-style strings
// with a trailing "s" (e.g. "43200s" for 12h); the API rejects other
// formats, so callers pass pre-formatted seconds strings (e.g. "5s").
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
// test that needs a short lifetime (e.g. via Start(t), never the
// package-shared Stack()), since every other acceptance test sharing an
// instance would otherwise start minting tokens with the same short
// lifetime.
//
// Feasibility (verified on v4.15.3): PUT accessTokenLifetime=10s against a
// fresh instance is accepted (200) and takes effect immediately — the next
// machine-user token minted afterward had exp-iat == 10s exactly.
func (s *Stack) SetAccessTokenLifetime(t testing.TB, lifetime string) {
	t.Helper()
	var current struct {
		Settings oidcSettings `json:"settings"`
	}
	if err := s.mgmtDo("GET", "/admin/v1/settings/oidc", "", nil, &current); err != nil {
		t.Fatalf("get oidc settings: %v", err)
	}
	current.Settings.AccessTokenLifetime = lifetime
	if err := s.mgmtDo("PUT", "/admin/v1/settings/oidc", "", current.Settings, nil); err != nil {
		t.Fatalf("put oidc settings: %v", err)
	}
}
