// SPDX-License-Identifier: MPL-2.0

package leifwindtest

import (
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// UserToken mints a genuine delegated user token via RFC 8693 token
// exchange (user_id subject type): sub = a human user, email claim present.
// This is the token shape the plan/apply runner forwards on behalf of a
// human user.
//
// The exchange flow deviates from ZITADEL's documented behavior in three
// concrete, investigated ways on v4.15.3:
//
//  1. The v4.15.3 image ships oidcTokenExchange already enabled
//     instance-wide (source: SOURCE_SYSTEM). PUTting the same value ZITADEL
//     already holds 400s with COMMAND-Vigh1 "No changes", so the setup PUT
//     is guarded by a GET.
//  2. ZITADEL's VerifyClient (internal/api/oidc/client.go) special-cases
//     grant_type=client_credentials for machine-user auth; every other
//     grant type — including token-exchange — is authenticated as a real
//     OIDC "Application" via ActiveOIDCClientByID. The org's machine-user
//     client_id/secret can never authenticate the exchange call itself
//     ("invalid_client: no active client not found"); a confidential OIDC
//     Web app with grantTypes including OIDC_GRANT_TYPE_TOKEN_EXCHANGE is
//     required (created once per Stack via s.setupTokenExchange, reused
//     across orgs since it lives in the shared s.Audience project — see
//     Stack's exchangeMu/exchangeReady field doc for why this is per-Stack,
//     not package-level). The actor (machine user, holding
//     ORG_END_USER_IMPERSONATOR) is unaffected — it still supplies the
//     actor_token.
//  3. In v4.15.3, validateTokenExchangeScopes (internal/api/oidc/
//     token_exchange.go) requires every non-empty requested scope to be
//     present on BOTH the subject and actor token. A user_id subject token
//     carries no scopes at all, so any non-empty "scope" parameter on the
//     exchange request always fails. Omitting "scope" falls through to
//     "use the actor token's scopes", so the full scope set (openid, email,
//     the resourceowner and project-audience scopes) is requested on the
//     actor token instead. Separately, requested_token_type=jwt yields an
//     access-token-shaped JWT that never carries email (only .Claims, an
//     ad-hoc map, is copied onto it — UserInfo's structured Email field is
//     not); requested_token_type=id_token returns the full user-info-bearing
//     token (email included, given the app's idTokenUserinfoAssertion) as
//     the response's access_token field, which is what DecodeClaims reads.
//
// UserToken is safe to call any number of times for the same Org: each call
// mints a fresh human user, and ZITADEL's 409 AlreadyExists on the
// impersonator re-grant is tolerated (LW-110).
func (s *Stack) UserToken(t testing.TB, org *Org) string {
	t.Helper()

	if err := s.ensureTokenExchange(); err != nil {
		t.Fatalf("token-exchange setup: %v", err)
	}

	// Reads of exchangeAppClientID/Secret below are safe: they are written
	// only under exchangeMu, before exchangeReady flips to true.

	suffix := uuid.NewString()[:8]
	var human struct {
		UserID string `json:"userId"`
	}
	if err := s.mgmtDo("POST", "/v2/users/human", org.ID, map[string]any{
		"username": "alice-" + suffix,
		"profile":  map[string]string{"givenName": "Alice", "familyName": "Test"},
		"email":    map[string]any{"email": "alice-" + suffix + "@example.com", "isVerified": true},
		"password": map[string]any{"password": "Password1!", "changeRequired": false},
	}, &human); err != nil {
		t.Fatalf("create human user: %v", err)
	}

	// Idempotency (LW-110): a second UserToken on the same Org re-grants
	// ORG_END_USER_IMPERSONATOR to the same machine user and ZITADEL answers
	// 409 AlreadyExists — tolerate exactly that; anything else still fails.
	// Handled here at the grant site: mgmtDo's strict ≥400 semantics are
	// relied on by every other caller and stay untouched.
	if err := s.mgmtDo("POST", "/management/v1/orgs/me/members", org.ID,
		map[string]any{"userId": org.MachineUserID,
			"roles": []string{"ORG_END_USER_IMPERSONATOR"}}, nil); err != nil && !isAlreadyExists(err) {
		t.Fatalf("grant impersonator role: %v", err)
	}

	// The actor token carries the full scope set: with a scopeless user_id
	// subject, the exchange request below omits "scope" entirely and
	// inherits the actor's scopes verbatim (Deviation 3).
	actor, status, err := fetchToken(s.ctx, s.Issuer, org.ClientID, org.ClientSecret,
		url.Values{"grant_type": {"client_credentials"}, "scope": {strings.Join([]string{
			"openid", "email",
			"urn:zitadel:iam:user:resourceowner",
			"urn:zitadel:iam:org:project:id:" + s.Audience + ":aud",
		}, " ")}})
	if err != nil || status != 200 {
		t.Fatalf("actor token: status=%d err=%v", status, err)
	}

	form := url.Values{
		"grant_type":         {"urn:ietf:params:oauth:grant-type:token-exchange"},
		"subject_token":      {human.UserID},
		"subject_token_type": {"urn:zitadel:params:oauth:token-type:user_id"},
		"actor_token":        {actor},
		"actor_token_type":   {"urn:ietf:params:oauth:token-type:access_token"},
		// id_token, not jwt: see Deviation 3 — this is the requested_token_type
		// that actually carries the email claim in v4.15.3.
		"requested_token_type": {"urn:ietf:params:oauth:token-type:id_token"},
	}
	tok, status, err := fetchToken(s.ctx, s.Issuer, s.exchangeAppClientID, s.exchangeAppClientSecret, form)
	if err != nil || status != 200 {
		t.Fatalf("token exchange failed (status=%d): %v (oidcTokenExchange is pre-GA in ZITADEL v4.15.3 — investigate before changing the flow)", status, err)
	}
	return tok
}

// ensureTokenExchange runs setupTokenExchange once per Stack under
// exchangeMu, leaving exchangeReady false on failure so the next call
// retries (LW-85). The deferred unlock keeps a panicking setup from
// deadlocking every later UserToken call.
func (s *Stack) ensureTokenExchange() error {
	s.exchangeMu.Lock()
	defer s.exchangeMu.Unlock()
	if s.exchangeReady {
		return nil
	}
	if err := s.setupTokenExchange(); err != nil {
		return err
	}
	s.exchangeReady = true
	return nil
}

// setupTokenExchange performs the one-time-per-Stack RFC 8693 prerequisites
// (feature flag + impersonation policy + token-exchange OIDC app) and
// stores the exchange app credentials. Caller holds exchangeMu.
func (s *Stack) setupTokenExchange() error {
	var features struct {
		OidcTokenExchange struct {
			Enabled bool `json:"enabled"`
		} `json:"oidcTokenExchange"`
	}
	if err := s.mgmtDo("GET", "/v2/features/instance", "", nil, &features); err != nil {
		return fmt.Errorf("get instance features: %w", err)
	}
	if !features.OidcTokenExchange.Enabled {
		if err := s.mgmtDo("PUT", "/v2/features/instance", "",
			map[string]any{"oidcTokenExchange": true}, nil); err != nil {
			return fmt.Errorf("enable oidc_token_exchange: %w", err)
		}
	}
	if err := s.mgmtDo("PUT", "/admin/v1/policies/security", "",
		map[string]any{"enableImpersonation": true}, nil); err != nil {
		return fmt.Errorf("enable impersonation: %w", err)
	}

	// Confidential OIDC app to authenticate the exchange call (see
	// Deviation 2 above). responseTypes/grantTypes must include the
	// authorization_code pair: ZITADEL's OIDCApp.IsValid derives
	// "required" grant types from responseTypes and rejects the app
	// otherwise, even though we never use that flow.
	var app struct {
		ClientID     string `json:"clientId"`
		ClientSecret string `json:"clientSecret"`
	}
	if err := s.mgmtDo("POST", "/management/v1/projects/"+s.Audience+"/apps/oidc", "",
		map[string]any{
			"name":                     "leifwindtest-token-exchange",
			"appType":                  "OIDC_APP_TYPE_WEB",
			"authMethodType":           "OIDC_AUTH_METHOD_TYPE_BASIC",
			"responseTypes":            []string{"OIDC_RESPONSE_TYPE_CODE"},
			"grantTypes":               []string{"OIDC_GRANT_TYPE_AUTHORIZATION_CODE", "OIDC_GRANT_TYPE_TOKEN_EXCHANGE"},
			"redirectUris":             []string{"http://localhost/callback"},
			"idTokenUserinfoAssertion": true,
		}, &app); err != nil {
		return fmt.Errorf("create token-exchange app: %w", err)
	}
	s.exchangeAppClientID = app.ClientID
	s.exchangeAppClientSecret = app.ClientSecret
	return nil
}

// isAlreadyExists reports whether err is mgmtDo's formatted error for a
// ZITADEL AlreadyExists response. mgmtDo returns "<METHOD> <path>: <status>
// <body>", so the HTTP status is matched as text; 409 is ZITADEL's HTTP
// mapping of gRPC AlreadyExists. Matched on status, not the body's error
// text, which is i18n-translated.
func isAlreadyExists(err error) bool {
	return err != nil && strings.Contains(err.Error(), ": 409 ")
}
