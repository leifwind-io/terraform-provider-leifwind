// SPDX-License-Identifier: MPL-2.0

package acctest

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccProjectDataSourceByIDAndName(t *testing.T) {
	PreCheck(t)
	t.Parallel()
	org := NewOrg(t)
	cfg := ProviderConfig(org.Token(t, Stack())) + `
resource "leifwind_project" "p" {
  name = "ds_project"
}

data "leifwind_project" "by_id" {
  id = leifwind_project.p.id
}

data "leifwind_project" "by_name" {
  name = leifwind_project.p.name
}

data "leifwind_projects" "all" {
  depends_on = [leifwind_project.p]
}

data "leifwind_projects" "filtered" {
  pattern    = "ds_pro"
  depends_on = [leifwind_project.p]
}
`
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: ProtoV6ProviderFactories(),
		Steps: []resource.TestStep{{
			Config: cfg,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.leifwind_project.by_id", "name", "ds_project"),
				resource.TestCheckResourceAttrPair("data.leifwind_project.by_name", "id", "leifwind_project.p", "id"),
				resource.TestCheckResourceAttr("data.leifwind_projects.all", "projects.#", "1"),
				resource.TestCheckResourceAttr("data.leifwind_projects.filtered", "projects.#", "1"),
				resource.TestCheckResourceAttr("data.leifwind_projects.filtered", "projects.0.name", "ds_project"),
			),
		}},
	})
}

func TestAccProjectDataSourceValidation(t *testing.T) {
	PreCheck(t)
	t.Parallel()
	org := NewOrg(t)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: ProtoV6ProviderFactories(),
		Steps: []resource.TestStep{{
			Config: ProviderConfig(org.Token(t, Stack())) + `
data "leifwind_project" "bad" {}
`,
			ExpectError: regexp.MustCompile(`Exactly one of these attributes must be configured`),
		}},
	})
}
