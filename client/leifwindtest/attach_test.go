// SPDX-License-Identifier: MPL-2.0

package leifwindtest

import (
	"errors"
	"strings"
	"testing"
)

func attachEnvFixture() map[string]string {
	return map[string]string{
		"LW_STACK_CONTRACT_VERSION":      "1",
		"LW_TEST_ZITADEL_ISSUER_URL":     "http://localhost:8081/",
		"LW_TEST_ZITADEL_PROJECT_ID":     "3141592653589793",
		"LW_TEST_BACKEND_URL":            "http://localhost:8080",
		"LW_TEST_ZITADEL_MGMT_PAT":       "pat-secret",
		"LW_TEST_ZITADEL_DEFAULT_ORG_ID": "2718281828459045",
	}
}

func TestAttachFromEnvFillsStack(t *testing.T) {
	s, err := attachFromEnv(mapEnv(attachEnvFixture()))
	if err != nil {
		t.Fatal(err)
	}
	if s.Issuer != "http://localhost:8081" { // trailing slash trimmed
		t.Errorf("Issuer = %q", s.Issuer)
	}
	if s.Audience != "3141592653589793" || s.BackendURL != "http://localhost:8080" {
		t.Errorf("Audience/BackendURL = %q/%q", s.Audience, s.BackendURL)
	}
	if s.mgmtPAT != "pat-secret" || s.defaultOrgID != "2718281828459045" {
		t.Error("unexported PAT / default org not filled")
	}
	if s.ctx == nil {
		t.Error("ctx must be non-nil")
	}
	s.cleanup() // no-op teardown must not panic
}

func TestAttachFromEnvMissingKey(t *testing.T) {
	for key := range attachEnvFixture() {
		if key == "LW_STACK_CONTRACT_VERSION" {
			continue // covered by version tests
		}
		t.Run(key, func(t *testing.T) {
			env := attachEnvFixture()
			delete(env, key)
			_, err := attachFromEnv(mapEnv(env))
			var ce *ContractError
			if !errors.As(err, &ce) || !strings.Contains(err.Error(), key) {
				t.Fatalf("want ContractError naming %s, got %v", key, err)
			}
		})
	}
}

func TestAttachFromEnvVersionMismatch(t *testing.T) {
	env := attachEnvFixture()
	env["LW_STACK_CONTRACT_VERSION"] = "2.0"
	if _, err := attachFromEnv(mapEnv(env)); err == nil ||
		!strings.Contains(err.Error(), "incompatible") {
		t.Fatalf("want incompatible-version error, got %v", err)
	}
}

func TestAttachReadsProcessEnv(t *testing.T) {
	for k, v := range attachEnvFixture() {
		t.Setenv(k, v)
	}
	s := Attach(t)
	if s.Issuer != "http://localhost:8081" {
		t.Errorf("Issuer = %q", s.Issuer)
	}
}
