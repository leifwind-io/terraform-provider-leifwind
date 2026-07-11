// SPDX-License-Identifier: MPL-2.0

package leifwindtest

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gitlab.com/leifwind/stream/terraform-provider-leifwind/client"
)

// Org is a fresh tenant with one machine user (JWT access tokens).
type Org struct {
	ID            string
	MachineUserID string
	ClientID      string
	ClientSecret  string
}

// NewOrg creates an isolated org: org + machine user + client secret +
// API-project grant (required for the project-audience scope).
func (s *Stack) NewOrg(t testing.TB) *Org {
	t.Helper()
	name := "org-" + uuid.NewString()[:12]

	var created struct {
		OrganizationID string `json:"organizationId"`
	}
	if err := s.mgmtDo("POST", "/v2/organizations", "",
		map[string]string{"name": name}, &created); err != nil {
		t.Fatalf("create org: %v", err)
	}

	var user struct {
		UserID string `json:"userId"`
	}
	if err := s.mgmtDo("POST", "/management/v1/users/machine", created.OrganizationID,
		map[string]string{
			"userName":        "m2m-" + name,
			"name":            "m2m-" + name,
			"accessTokenType": "ACCESS_TOKEN_TYPE_JWT",
		}, &user); err != nil {
		t.Fatalf("create machine user: %v", err)
	}

	var secret struct {
		ClientID     string `json:"clientId"`
		ClientSecret string `json:"clientSecret"`
	}
	if err := s.mgmtDo("PUT", "/management/v1/users/"+user.UserID+"/secret",
		created.OrganizationID, map[string]string{}, &secret); err != nil {
		t.Fatalf("create secret: %v", err)
	}

	if err := s.mgmtDo("POST", "/management/v1/projects/"+s.Audience+"/grants",
		s.defaultOrgID, map[string]string{"grantedOrgId": created.OrganizationID}, nil); err != nil {
		t.Fatalf("grant project: %v", err)
	}

	return &Org{
		ID:            created.OrganizationID,
		MachineUserID: user.UserID,
		ClientID:      secret.ClientID,
		ClientSecret:  secret.ClientSecret,
	}
}

// TokenSource returns an auto-refreshing client_credentials TokenSource
// for this org against the stack's ZITADEL.
func (o *Org) TokenSource(s *Stack) client.TokenSource {
	return client.ClientCredentials(s.Issuer, o.ClientID, o.ClientSecret,
		client.WithAudience(s.Audience))
}

// Token fetches one raw machine access token (client_credentials).
//
// ZITADEL's token endpoint reads the machine secret through an eventually
// consistent projection: immediately after NewOrg it can briefly answer
// 400 "Errors.User.Machine.Secret.NotExisting" (seen under CPU-starved CI
// runners), so poll that specific error until the secret lands.
func (o *Org) Token(t testing.TB, s *Stack) string {
	t.Helper()
	form := url.Values{
		"grant_type": {"client_credentials"},
		"scope": {strings.Join([]string{
			"openid",
			"urn:zitadel:iam:user:resourceowner",
			"urn:zitadel:iam:org:project:id:" + s.Audience + ":aud",
		}, " ")},
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		tok, status, err := fetchToken(s.Issuer, o.ClientID, o.ClientSecret, form)
		if err == nil && status == http.StatusOK {
			return tok
		}
		if status == http.StatusBadRequest && err != nil &&
			strings.Contains(err.Error(), "Errors.User.Machine.Secret.NotExisting") &&
			time.Now().Before(deadline) {
			time.Sleep(250 * time.Millisecond)
			continue
		}
		t.Fatalf("token fetch: status=%d err=%v", status, err)
		return "" // unreachable (t.Fatalf on testing.T panics/exits); keeps vet happy for testing.TB
	}
}

func fetchToken(issuer, clientID, clientSecret string, form url.Values) (string, int, error) {
	req, err := http.NewRequest("POST", issuer+"/oauth/v2/token",
		strings.NewReader(form.Encode()))
	if err != nil {
		return "", 0, err
	}
	req.SetBasicAuth(clientID, clientSecret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", resp.StatusCode, fmt.Errorf("token endpoint: %s", body)
	}
	var out struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", resp.StatusCode, err
	}
	return out.AccessToken, resp.StatusCode, nil
}

// DecodeClaims decodes a JWT payload WITHOUT verification (test helper).
func DecodeClaims(t testing.TB, token string) map[string]any {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("not a JWT: %d segments", len(parts))
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatalf("unmarshal claims: %v", err)
	}
	return claims
}
