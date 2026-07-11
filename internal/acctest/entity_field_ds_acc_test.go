// SPDX-License-Identifier: MPL-2.0

package acctest

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccEntityAndFieldDataSources deviates from the task-24 brief's literal
// test config (owner-adjudicated, concrete named failure LW-70): the brief's
// config declares a single FRAGMENT field ("body") as the only field of the
// entity. Empirically (RED run) the backend 500s in sync_entity_schema when
// the FIRST field ever created on an entity is a FRAGMENT field — the same
// bug documented in field_acc_test.go's fieldConfig/plantKeeperField. This
// test therefore:
//  1. adds a KEY field "title", with "body" depends_on it (creation order);
//  2. plants an out-of-band KEY "keeper" field after apply, so `terraform
//     destroy` (which every resource.Test performs) doesn't hit the "last
//     field of an entity" 500 either;
//  3. adds pattern = "body" to the "leifwind_fields.all" data source so the
//     brief's literal fields.# == 1 / fields.0.data_type == "TEXT" checks
//     still hold deterministically even though the entity now has 2 (later
//     3, once the keeper lands) fields — an unfiltered listing would
//     otherwise return title too.
//
// All of the brief's original assertions are preserved byte-for-byte; only
// the Terraform config and the "all" data source's pattern were adjusted.
func TestAccEntityAndFieldDataSources(t *testing.T) {
	PreCheck(t)
	t.Parallel()
	org := NewOrg(t)
	tok := org.Token(t, Stack())
	cfg := ProviderConfig(tok) + `
resource "leifwind_project" "p" {
  name = "ds_ef_proj"
}

resource "leifwind_entity" "e" {
  project_id = leifwind_project.p.id
  name       = "book"
}

resource "leifwind_field" "title" {
  project_id      = leifwind_project.p.id
  entity_id       = leifwind_entity.e.id
  name            = "title"
  data_type       = "TEXT"
  connection_type = "KEY"
}

resource "leifwind_field" "f" {
  project_id      = leifwind_project.p.id
  entity_id       = leifwind_entity.e.id
  name            = "body"
  data_type       = "TEXT"
  connection_type = "FRAGMENT"
  fragment_name   = "content"

  depends_on = [leifwind_field.title] # LW-70, see fieldConfig comment in field_acc_test.go
}

data "leifwind_entity" "by_name" {
  project_id = leifwind_project.p.id
  name       = leifwind_entity.e.name
}

data "leifwind_entities" "all" {
  project_id = leifwind_project.p.id
  depends_on = [leifwind_entity.e]
}

data "leifwind_field" "by_name" {
  project_id = leifwind_project.p.id
  entity_id  = leifwind_entity.e.id
  name       = leifwind_field.f.name
}

data "leifwind_fields" "all" {
  project_id = leifwind_project.p.id
  entity_id  = leifwind_entity.e.id
  pattern    = "body" # LW-70 deviation: isolates from the title KEY field / keeper, see doc comment above
  depends_on = [leifwind_field.f]
}
`
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: ProtoV6ProviderFactories(),
		Steps: []resource.TestStep{{
			Config: cfg,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrPair("data.leifwind_entity.by_name", "id", "leifwind_entity.e", "id"),
				resource.TestCheckResourceAttr("data.leifwind_entities.all", "entities.#", "1"),
				resource.TestCheckResourceAttr("data.leifwind_field.by_name", "fragment_name", "content"),
				resource.TestCheckResourceAttr("data.leifwind_field.by_name", "connection_type", "FRAGMENT"),
				resource.TestCheckResourceAttr("data.leifwind_fields.all", "fields.#", "1"),
				resource.TestCheckResourceAttr("data.leifwind_fields.all", "fields.0.data_type", "TEXT"),
				plantKeeperField(t, tok, "leifwind_field.title"), // LW-70 destroy-safety, see field_acc_test.go
			),
		}},
	})
}
