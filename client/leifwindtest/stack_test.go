// SPDX-License-Identifier: MPL-2.0

package leifwindtest

import (
	"net/http"
	"testing"
)

func TestStackBootsZitadel(t *testing.T) {
	s := sharedStack(t)
	resp, err := http.Get(s.Issuer + "/.well-known/openid-configuration")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		t.Fatalf("discovery returned %d", resp.StatusCode)
	}
	if s.Audience == "" {
		t.Fatal("audience (API project id) not set")
	}
}

func TestBackendEnforcesAuth(t *testing.T) {
	s := sharedStack(t)
	org := s.NewOrg(t)

	resp, err := http.Get(s.BackendURL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("healthz: %d", resp.StatusCode)
	}

	resp, err = http.Get(s.BackendURL + "/metadata/projects")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("unauthenticated list: want 401, got %d", resp.StatusCode)
	}

	req, _ := http.NewRequest("GET", s.BackendURL+"/metadata/projects", nil)
	req.Header.Set("Authorization", "Bearer "+org.Token(t, s))
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("authenticated list: want 200, got %d", resp.StatusCode)
	}

	// Task 11 VERIFY (carry-forward from Task 10): a delegated user token
	// minted via (*Stack).UserToken must also be accepted by the backend —
	// this is the first task that can exercise backend acceptance of that
	// token shape end-to-end.
	req, _ = http.NewRequest("GET", s.BackendURL+"/metadata/projects", nil)
	req.Header.Set("Authorization", "Bearer "+s.UserToken(t, org))
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("user-token authenticated list: want 200, got %d", resp.StatusCode)
	}
}
