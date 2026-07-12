// SPDX-License-Identifier: MPL-2.0

package acctest

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccEntityFragmentsDataSource exercises the leifwind_entity_fragments
// data source against an entity with one KEY field ("title") and one
// FRAGMENT field ("a"). The FRAGMENT field references the KEY field via
// key_field_ids, which both satisfies the field's create-time validation
// (Tasks 1-3) and creates the Terraform graph edge that orders
// KEY-before-FRAGMENT on create and FRAGMENT-before-KEY on destroy
// (both backend-enforced, LW-70; see fieldConfig's comment in
// field_acc_test.go for the same pattern).
func TestAccEntityFragmentsDataSource(t *testing.T) {
	PreCheck(t)
	t.Parallel()
	org := NewOrg(t)
	tok := org.Token(t, Stack())
	cfg := ProviderConfig(tok) + `
resource "leifwind_project" "p" {
  name = "ds_frag_proj"
}

resource "leifwind_entity" "e" {
  project_id = leifwind_project.p.id
  name       = "doc"
}

resource "leifwind_field" "title" {
  project_id      = leifwind_project.p.id
  entity_id       = leifwind_entity.e.id
  name            = "title"
  data_type       = "TEXT"
  connection_type = "KEY"
}

resource "leifwind_field" "a" {
  project_id      = leifwind_project.p.id
  entity_id       = leifwind_entity.e.id
  name            = "body"
  data_type       = "TEXT"
  connection_type = "FRAGMENT"
  fragment_name   = "content"
  key_field_ids   = [leifwind_field.title.id]
}

data "leifwind_entity_fragments" "f" {
  project_id  = leifwind_project.p.id
  entity_name = leifwind_entity.e.name
  depends_on  = [leifwind_field.a]
}
`
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: ProtoV6ProviderFactories(),
		Steps: []resource.TestStep{{
			Config: cfg,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.leifwind_entity_fragments.f", "fragments.#", "1"),
				resource.TestCheckResourceAttr("data.leifwind_entity_fragments.f", "fragments.0", "content"),
			),
		}},
	})
}
