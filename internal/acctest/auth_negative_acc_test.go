// SPDX-License-Identifier: MPL-2.0

package acctest

import (
	"context"
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
// The wait is NOT just "past the token's exp": a second spike (raw HTTP,
// bypassing Terraform, single fresh token, no reuse) showed the backend
// still accepts this exact JWT up to ~55s past its own exp claim (200 at
// +10s and +25s past exp; 401 first observed at +55s past exp; a fixed
// 65s-since-mint wait then reproduced 401 reliably across repeated runs).
// Since this held on the token's very first backend contact, it cannot be
// a positive validation cache (nothing to have cached yet) — the evidence
// is consistent with a configured exp leeway/clock-skew allowance of
// roughly 30-50s in the backend's JWT validation, though the exact
// mechanism is unconfirmed (backend source is out of scope here; reported
// to the owner as a finding — see task-27 report). 75s gives comfortable
// margin over the observed ~55s boundary.
func TestAccExpiredToken(t *testing.T) {
	PreCheck(t)
	s := leifwindtest.Start(t)
	s.SetAccessTokenLifetime(t, "5s")
	org := s.NewOrg(t)
	tok := org.Token(t, s)

	time.Sleep(75 * time.Second) // clears both the 5s token lifetime and the backend's apparent exp leeway (see doc comment)

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
