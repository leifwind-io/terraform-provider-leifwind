// SPDX-License-Identifier: MPL-2.0

package leifwindtest

import (
	"context"
	"os"
	"strings"
	"testing"
)

// Attach builds a Stack from the LW_TEST_* environment contract (stack.env,
// written by the backend's `leifwind-stack seed`) instead of booting
// containers. The attached stack is owned by whoever started it — `make -C
// ../backend stack-up stack-seed` locally, or a CI `services:` block — so
// teardown is a no-op and nothing is terminated on cleanup.
//
// The contract's major version (LW_STACK_CONTRACT_VERSION) is checked
// against this package and Attach fails fast on drift or on any missing
// variable. WithToxiproxy is unavailable in attach mode: fault injection
// needs container control, keep Start for that.
func Attach(t testing.TB) *Stack {
	t.Helper()
	s, _, err := AttachMain()
	if err != nil {
		t.Fatalf("leifwindtest.Attach: %v", err)
	}
	return s
}

// AttachMain is the TestMain-friendly variant of Attach, symmetric with
// StartMain. The returned cleanup is safe to call and does nothing.
func AttachMain() (*Stack, func(), error) {
	s, err := attachFromEnv(os.Getenv)
	if err != nil {
		return nil, func() {}, err
	}
	return s, s.cleanup, nil
}

func attachFromEnv(getenv func(string) string) (*Stack, error) {
	if err := checkContractVersion(getenv); err != nil {
		return nil, err
	}
	s := &Stack{ctx: context.Background()}
	for _, f := range []struct {
		key string
		dst *string
	}{
		{"LW_TEST_ZITADEL_ISSUER_URL", &s.Issuer},
		{"LW_TEST_ZITADEL_PROJECT_ID", &s.Audience},
		{"LW_TEST_BACKEND_URL", &s.BackendURL},
		{"LW_TEST_ZITADEL_MGMT_PAT", &s.mgmtPAT},
		{"LW_TEST_ZITADEL_DEFAULT_ORG_ID", &s.defaultOrgID},
	} {
		v, err := requireEnv(getenv, f.key)
		if err != nil {
			return nil, err
		}
		*f.dst = v
	}
	s.Issuer = strings.TrimRight(s.Issuer, "/")
	return s, nil
}
