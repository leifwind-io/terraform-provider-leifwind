// SPDX-License-Identifier: MPL-2.0

// Package acctest hosts ALL acceptance tests and their shared harness.
package acctest

import (
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"

	"gitlab.com/leifwind/stream/terraform-provider-leifwind/client/leifwindtest"
	"gitlab.com/leifwind/stream/terraform-provider-leifwind/internal/provider"
)

var (
	shared        *leifwindtest.Stack
	sharedCleanup func()
	orgMu         sync.Mutex
)

// attachMode reports whether the LW_TEST_* attach contract is in the
// environment (a sourced stack.env) — the same dispatch key the backend's
// Python fixtures use. Attach: shared long-lived stack, seconds per run.
// Boot: testcontainers, the local default.
func attachMode() bool {
	return os.Getenv("LW_TEST_ZITADEL_ISSUER_URL") != ""
}

func startShared() error {
	if attachMode() {
		s, cleanup, err := leifwindtest.AttachMain()
		if err != nil {
			return err
		}
		shared, sharedCleanup = s, cleanup
		return nil
	}
	var err error
	shared, sharedCleanup, err = leifwindtest.StartMain()
	return err
}

func stopShared() {
	if sharedCleanup != nil {
		sharedCleanup()
	}
}

// Stack returns the shared containerized stack (TF_ACC runs only).
func Stack() *leifwindtest.Stack { return shared }

// NewOrg mints a fresh isolated tenant.
func NewOrg(t *testing.T) *leifwindtest.Org {
	t.Helper()
	orgMu.Lock()
	defer orgMu.Unlock()
	return shared.NewOrg(t)
}

// PreCheck gates a test on TF_ACC.
func PreCheck(t *testing.T) {
	t.Helper()
	if os.Getenv("TF_ACC") == "" {
		t.Skip("set TF_ACC=1 to run acceptance tests")
	}
}

// ProtoV6ProviderFactories serves the provider in-process (protocol 6).
func ProtoV6ProviderFactories() map[string]func() (tfprotov6.ProviderServer, error) {
	return map[string]func() (tfprotov6.ProviderServer, error){
		"leifwind": providerserver.NewProtocol6WithError(provider.New("acctest")()),
	}
}

// ProviderConfig renders a provider block with a static token.
func ProviderConfig(token string) string {
	return fmt.Sprintf(`
provider "leifwind" {
  endpoint = %q
  token    = %q
}
`, shared.BackendURL, token)
}

// ProviderConfigM2M renders a provider block using client_credentials.
func ProviderConfigM2M(org *leifwindtest.Org) string {
	return fmt.Sprintf(`
provider "leifwind" {
  endpoint      = %q
  issuer        = %q
  client_id     = %q
  client_secret = %q
  audience      = %q
}
`, shared.BackendURL, shared.Issuer, org.ClientID, org.ClientSecret, shared.Audience)
}
