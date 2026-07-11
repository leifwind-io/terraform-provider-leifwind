// SPDX-License-Identifier: MPL-2.0

package acctest

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccM2MClientCredentials drives an apply through the provider's
// issuer/client_id/client_secret/audience block (auto-refreshing M2M).
func TestAccM2MClientCredentials(t *testing.T) {
	PreCheck(t)
	t.Parallel()
	org := NewOrg(t)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: ProtoV6ProviderFactories(),
		Steps: []resource.TestStep{{
			Config: ProviderConfigM2M(org) + `
resource "leifwind_project" "p" {
  name = "acc_m2m"
}
`,
			Check: resource.TestCheckResourceAttrSet("leifwind_project.p", "id"),
		}},
	})
}

// TestAccDelegatedUserToken drives an apply with a REAL user-scoped token
// (sub = human user, email claim) — the LW-44 runner pattern.
func TestAccDelegatedUserToken(t *testing.T) {
	PreCheck(t)
	t.Parallel()
	org := NewOrg(t)
	userTok := Stack().UserToken(t, org)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: ProtoV6ProviderFactories(),
		Steps: []resource.TestStep{{
			Config: ProviderConfig(userTok) + `
resource "leifwind_project" "p" {
  name = "acc_delegated"
}
`,
			Check: resource.TestCheckResourceAttrSet("leifwind_project.p", "id"),
		}},
	})
}
