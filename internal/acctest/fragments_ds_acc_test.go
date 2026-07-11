// SPDX-License-Identifier: MPL-2.0

package acctest

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccEntityFragmentsDataSource deviates from the task-25 brief's literal
// test config (owner-adjudicated, concrete named failure LW-70, same
// deviation already applied in entity_field_ds_acc_test.go / documented in
// field_acc_test.go's fieldConfig/plantKeeperField): the brief declares a
// single FRAGMENT field ("body") as the entity's only field. Empirically (in
// task 24) the backend 500s in sync_entity_schema when the FIRST field ever
// created on an entity is a FRAGMENT field, and again when the LAST field of
// an entity is deleted. This test therefore:
//  1. adds a KEY field "title", with "body" depends_on it (creation order);
//  2. plants an out-of-band KEY "keeper" field after apply, so `terraform
//     destroy` (which every resource.Test performs) doesn't hit the "last
//     field of an entity" 500 either.
//
// All of the brief's original assertions (fragments.# == 1, fragments.0 ==
// "content") are preserved byte-for-byte; only the Terraform config gained
// the KEY field and its depends_on edge.
func TestAccEntityFragmentsDataSource(t *testing.T) {
	PreCheck(t)
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

  depends_on = [leifwind_field.title] # LW-70, see fieldConfig comment in field_acc_test.go
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
				plantKeeperField(t, tok, "leifwind_field.title"), // LW-70 destroy-safety, see field_acc_test.go
			),
		}},
	})
}
