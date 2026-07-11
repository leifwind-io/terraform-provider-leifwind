// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"strings"
	"testing"
)

func ptr(s string) *string { return &s }

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestResolveConfigStaticToken(t *testing.T) {
	r, errs := resolveConfig(RawConfig{Endpoint: ptr("https://api.example"), Token: ptr("tok")}, env(nil))
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if r.UseM2M || r.Token != "tok" || r.Endpoint != "https://api.example" {
		t.Fatalf("bad resolve: %+v", r)
	}
}

func TestResolveConfigEnvFallbacks(t *testing.T) {
	r, errs := resolveConfig(RawConfig{}, env(map[string]string{
		"LEIFWIND_ENDPOINT": "https://api.example",
		"LEIFWIND_TOKEN":    "envtok",
	}))
	if len(errs) != 0 || r.Token != "envtok" {
		t.Fatalf("env fallback failed: %+v %v", r, errs)
	}
}

func TestResolveConfigM2M(t *testing.T) {
	r, errs := resolveConfig(RawConfig{
		Endpoint: ptr("https://api.example"),
		Issuer:   ptr("https://auth.example"), ClientID: ptr("id"), ClientSecret: ptr("sec"),
		Audience: ptr("326102453042806786"),
	}, env(nil))
	if len(errs) != 0 || !r.UseM2M || r.Audience != "326102453042806786" {
		t.Fatalf("m2m resolve failed: %+v %v", r, errs)
	}
}

func TestResolveConfigMutualExclusion(t *testing.T) {
	_, errs := resolveConfig(RawConfig{
		Endpoint: ptr("https://api.example"), Token: ptr("tok"), Issuer: ptr("https://auth.example"),
	}, env(nil))
	if len(errs) != 1 || !strings.Contains(errs[0], "mutually exclusive") {
		t.Fatalf("want mutual-exclusion error, got %v", errs)
	}
}

func TestResolveConfigIncompleteM2M(t *testing.T) {
	_, errs := resolveConfig(RawConfig{
		Endpoint: ptr("https://api.example"), Issuer: ptr("https://auth.example"),
	}, env(nil))
	if len(errs) != 1 || !strings.Contains(errs[0], "LEIFWIND_CLIENT_ID") || !strings.Contains(errs[0], "LEIFWIND_CLIENT_SECRET") {
		t.Fatalf("want missing-attr error naming env vars, got %v", errs)
	}
}

func TestResolveConfigNothing(t *testing.T) {
	_, errs := resolveConfig(RawConfig{Endpoint: ptr("https://api.example")}, env(nil))
	if len(errs) != 1 || !strings.Contains(errs[0], "LEIFWIND_TOKEN") {
		t.Fatalf("want no-auth error, got %v", errs)
	}
}

func TestResolveConfigMissingEndpoint(t *testing.T) {
	_, errs := resolveConfig(RawConfig{Token: ptr("tok")}, env(nil))
	if len(errs) != 1 || !strings.Contains(errs[0], "LEIFWIND_ENDPOINT") {
		t.Fatalf("want endpoint error, got %v", errs)
	}
}
