// SPDX-License-Identifier: MPL-2.0

package acctest

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"gitlab.com/leifwind/stream/terraform-provider-leifwind/client"
	"gitlab.com/leifwind/stream/terraform-provider-leifwind/client/leifwindtest"
)

func TestAccMissingCredentials(t *testing.T) {
	PreCheck(t)
	// NOT t.Parallel(): t.Setenv below is incompatible with parallel tests
	// (Go panics on the combination). This test is seconds-fast anyway.
	// no token, no M2M block, and empty env (TF_ACC runner must not leak LEIFWIND_*)
	t.Setenv("LEIFWIND_TOKEN", "")
	cfg := fmt.Sprintf(`
provider "leifwind" {
  endpoint = %q
}

resource "leifwind_project" "p" {
  name = "never_created"
}
`, Stack().BackendURL)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: ProtoV6ProviderFactories(),
		Steps: []resource.TestStep{{
			Config:      cfg,
			ExpectError: regexp.MustCompile(`no credentials`),
		}},
	})
}

func TestAccGarbageToken(t *testing.T) {
	PreCheck(t)
	t.Parallel()
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: ProtoV6ProviderFactories(),
		Steps: []resource.TestStep{{
			Config: ProviderConfig("not-a-jwt") + `
resource "leifwind_project" "p" {
  name = "never_created"
}
`,
			ExpectError: regexp.MustCompile(`(?i)401|unauthenticated`),
		}},
	})
}

func TestAccForgedTokenRejected(t *testing.T) {
	PreCheck(t)
	t.Parallel()
	org := NewOrg(t)
	forged := Stack().ForgedToken(t, org)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: ProtoV6ProviderFactories(),
		Steps: []resource.TestStep{{
			Config: ProviderConfig(forged) + `
resource "leifwind_project" "p" {
  name = "never_created"
}
`,
			ExpectError: regexp.MustCompile(`(?i)401|unauthenticated`),
		}},
	})
}

// TestAccCrossOrgIsolation: a project of org A is a 404 for org B.
// The org-A project is created via the raw client (NOT a resource.Test,
// whose cleanup would destroy it before org B's step runs).
func TestAccCrossOrgIsolation(t *testing.T) {
	PreCheck(t)
	t.Parallel()
	orgA := NewOrg(t)
	orgB := NewOrg(t)

	ca, err := client.New(Stack().BackendURL,
		client.WithTokenSource(client.StaticToken(orgA.Token(t, Stack()))))
	if err != nil {
		t.Fatal(err)
	}
	p, err := ca.Metadata.UpsertProject(context.Background(),
		client.MetadataProject{Name: "org_a_project"})
	if err != nil {
		t.Fatal(err)
	}

	// org B cannot see it — cross-tenant reads are 404, no existence oracle
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: ProtoV6ProviderFactories(),
		Steps: []resource.TestStep{{
			Config: ProviderConfig(orgB.Token(t, Stack())) + fmt.Sprintf(`
data "leifwind_project" "peek" {
  id = %q
}
`, p.ObjectID),
			ExpectError: regexp.MustCompile(`(?i)not found`),
		}},
	})
}

func TestAccInvalidImportID(t *testing.T) {
	PreCheck(t)
	t.Parallel()
	org := NewOrg(t)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: ProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: ProviderConfig(org.Token(t, Stack())) + `
resource "leifwind_entity" "e" {
  project_id = "00000000-0000-0000-0000-000000000000"
  name       = "never"
}
`,
				ResourceName:  "leifwind_entity.e",
				ImportState:   true,
				ImportStateId: "not-two-uuids",
				ExpectError:   regexp.MustCompile(`import ID must have 2`),
			},
		},
	})
}

// TestAccExpiredToken: real expired-token acceptance test (owner-adjudicated
// deviation from the brief's t.Skip — see task-27 report for the full
// investigation). PLAN A from the spec's Risks turned out feasible: a
// throwaway spike proved ZITADEL v4.15.3's admin API (PUT
// /admin/v1/settings/oidc) accepts an accessTokenLifetime as low as "5s"
// and applies it immediately to newly minted tokens (verified exp-iat == 5s
// on the next machine-user token).
//
// That setting is INSTANCE-WIDE, not per-org/per-app (zitadel/zitadel#5219
// is still open) — mutating it on the package-shared Stack() would give
// every other sequentially-run acceptance test in this package a 5s token
// lifetime too. So this test boots its OWN dedicated Stack
// (leifwindtest.Start) instead and pays the extra container-boot cost.
//
// The wait is NOT just "past the token's exp" (LW-76): a second spike (raw
// HTTP, bypassing Terraform, single fresh token, no reuse) showed the
// backend still accepts this exact JWT up to ~55s past its own exp claim
// (200 at +10s and +25s past exp; 401 first observed at +55s past exp).
// Since this held on the token's very first backend contact, it cannot be
// a positive validation cache (nothing to have cached yet) — the evidence
// is consistent with a configured exp leeway/clock-skew allowance of
// roughly 30-50s in the backend's JWT validation, though the exact
// mechanism is unconfirmed (backend source is out of scope here; reported
// to the owner as a finding — see task-27 report). Because the exact
// leeway boundary is the backend's business (LW-76), we don't hardcode a
// sleep past it: we poll an authenticated backend route with the expired-
// lifetime token every 3s until the FIRST 401 (cap 150s, comfortably past
// the observed ~55s boundary), then run the Terraform step against the
// now-provably-rejected token.
func TestAccExpiredToken(t *testing.T) {
	PreCheck(t)
	t.Parallel() // dedicated-stack boot + expiry poll overlap the shared-stack tests
	s := leifwindtest.Start(t)
	s.SetAccessTokenLifetime(t, "5s")
	org := s.NewOrg(t)
	tok := org.Token(t, s)

	// LW-76 poll (see doc comment): wait for the backend itself to start
	// rejecting the token instead of sleeping past a guessed leeway. The
	// probe goes through the client module (depguard dogfooding rule bans
	// net/http under internal/**), retries disabled so each probe is
	// exactly one HTTP round trip.
	probe, err := client.New(s.BackendURL,
		client.WithTokenSource(client.StaticToken(tok)),
		client.WithRetry(client.RetryConfig{MaxAttempts: 1}))
	if err != nil {
		t.Fatal(err)
	}
	pollDeadline := time.Now().Add(150 * time.Second)
	for {
		_, err := probe.Metadata.ListProjects(context.Background(), client.ListOpts{})
		if errors.Is(err, client.ErrUnauthenticated) {
			break // first 401: the backend now rejects the expired token
		}
		if time.Now().After(pollDeadline) {
			t.Fatalf("backend never rejected the expired token within 150s (LW-76 leeway grew?); last err: %v", err)
		}
		time.Sleep(3 * time.Second)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: ProtoV6ProviderFactories(),
		Steps: []resource.TestStep{{
			Config: fmt.Sprintf(`
provider "leifwind" {
  endpoint = %q
  token    = %q
}

resource "leifwind_project" "p" {
  name = "never_created"
}
`, s.BackendURL, tok),
			ExpectError: regexp.MustCompile(`(?i)401|unauthenticated`),
		}},
	})
}
