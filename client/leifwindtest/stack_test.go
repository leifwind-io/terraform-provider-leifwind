// SPDX-License-Identifier: MPL-2.0

package leifwindtest

import (
	"net/http"
	"testing"
)

func TestStackBootsZitadel(t *testing.T) {
	s := Start(t)
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
