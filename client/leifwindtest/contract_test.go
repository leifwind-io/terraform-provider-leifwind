// SPDX-License-Identifier: MPL-2.0

package leifwindtest

import (
	"errors"
	"strings"
	"testing"
)

func mapEnv(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestRequireEnvMissingKeyIsContractError(t *testing.T) {
	_, err := requireEnv(mapEnv(nil), "LW_TEST_ZITADEL_MGMT_PAT")
	var ce *ContractError
	if !errors.As(err, &ce) {
		t.Fatalf("want ContractError, got %T: %v", err, err)
	}
	for _, want := range []string{"LW_TEST_ZITADEL_MGMT_PAT", "make stack-seed"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should contain %q", err, want)
		}
	}
}

func TestRequireEnvPresent(t *testing.T) {
	v, err := requireEnv(mapEnv(map[string]string{"K": "v"}), "K")
	if err != nil || v != "v" {
		t.Fatalf("got (%q, %v)", v, err)
	}
}

func TestCheckContractVersion(t *testing.T) {
	cases := []struct {
		name, version string
		wantErr       string // "" = ok
	}{
		{"exact", "1", ""},
		{"minor", "1.2", ""},
		{"missing", "", "LW_STACK_CONTRACT_VERSION is missing"},
		{"major2", "2.0", "incompatible"},
		{"majorText", "x.1", "incompatible"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := map[string]string{}
			if tc.version != "" {
				env["LW_STACK_CONTRACT_VERSION"] = tc.version
			}
			err := checkContractVersion(mapEnv(env))
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("want ok, got %v", err)
				}
				return
			}
			var ce *ContractError
			if !errors.As(err, &ce) || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("want ContractError containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}
