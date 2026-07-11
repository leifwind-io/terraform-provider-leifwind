// SPDX-License-Identifier: MPL-2.0

package leifwindtest

import (
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// UserToken mints a genuine delegated user token via RFC 8693 token
// exchange (user_id subject type): sub = a human user, email claim present.
// This is the token shape LW-44's runner forwards.
//
// This diverges from the brief in three concrete, investigated ways — see
// the task-10 report for the full trace. Summary:
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
//     required (created once per Stack via s.exchangeSetup, reused across
//     orgs since it lives in the shared s.Audience project — see Stack's
//     exchangeSetup field doc for why this is per-Stack, not
//     package-level). The actor (machine user, holding
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
func (s *Stack) UserToken(t testing.TB, org *Org) string {
	t.Helper()

	s.exchangeSetup.Do(func() {
		var features struct {
			OidcTokenExchange struct {
				Enabled bool `json:"enabled"`
			} `json:"oidcTokenExchange"`
		}
		if err := s.mgmtDo("GET", "/v2/features/instance", "", nil, &features); err != nil {
			t.Fatalf("get instance features: %v", err)
		}
		if !features.OidcTokenExchange.Enabled {
			if err := s.mgmtDo("PUT", "/v2/features/instance", "",
				map[string]any{"oidcTokenExchange": true}, nil); err != nil {
				t.Fatalf("enable oidc_token_exchange: %v", err)
			}
		}
		if err := s.mgmtDo("PUT", "/admin/v1/policies/security", "",
			map[string]any{"enableImpersonation": true}, nil); err != nil {
			t.Fatalf("enable impersonation: %v", err)
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
			t.Fatalf("create token-exchange app: %v", err)
		}
		s.exchangeAppClientID = app.ClientID
		s.exchangeAppClientSecret = app.ClientSecret
	})

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

	if err := s.mgmtDo("POST", "/management/v1/orgs/me/members", org.ID,
		map[string]any{"userId": org.MachineUserID,
			"roles": []string{"ORG_END_USER_IMPERSONATOR"}}, nil); err != nil {
		t.Fatalf("grant impersonator role: %v", err)
	}

	// The actor token carries the full scope set: with a scopeless user_id
	// subject, the exchange request below omits "scope" entirely and
	// inherits the actor's scopes verbatim (Deviation 3).
	actor, status, err := fetchToken(s.Issuer, org.ClientID, org.ClientSecret,
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
	tok, status, err := fetchToken(s.Issuer, s.exchangeAppClientID, s.exchangeAppClientSecret, form)
	if err != nil || status != 200 {
		t.Fatalf("token exchange failed (status=%d): %v — see spec 'Risks': pre-GA flag on v4.15.3; investigate before falling back", status, err)
	}
	return tok
}
