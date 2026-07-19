// SPDX-License-Identifier: MPL-2.0

package acctest

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccEntityAndFieldDataSources exercises the entity/field data sources
// against an entity with one KEY field ("title") and one FRAGMENT field
// ("f"). The FRAGMENT field references the KEY field via key_field_ids,
// which both satisfies the field's create-time validation (Tasks 1-3) and
// creates the Terraform graph edge that orders KEY-before-FRAGMENT on create
// and FRAGMENT-before-KEY on destroy (both backend-enforced, LW-70; see
// fieldConfig's comment in field_acc_test.go for the same pattern).
//
// pattern = "body" on the "leifwind_fields.all" data source isolates the
// listing from the "title" KEY field so fields.# == 1 / fields.0.data_type
// == "TEXT" hold deterministically.
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
  key_field_ids   = [leifwind_field.title.id]
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
  pattern    = "body" # isolates from the title KEY field, see doc comment above
  depends_on = [leifwind_field.f]
}

data "leifwind_entity" "by_id" {
  project_id = leifwind_project.p.id
  id         = leifwind_entity.e.id
}

data "leifwind_entities" "filtered" {
  project_id = leifwind_project.p.id
  pattern    = "boo"
  depends_on = [leifwind_entity.e]
}

data "leifwind_field" "by_id" {
  project_id = leifwind_project.p.id
  entity_id  = leifwind_entity.e.id
  id         = leifwind_field.f.id
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
				// by-id lookups agree with the by-name blocks (covers the by-id
				// read paths incl. keep-config-id logic; LW-109)
				resource.TestCheckResourceAttrPair("data.leifwind_entity.by_id", "id", "data.leifwind_entity.by_name", "id"),
				resource.TestCheckResourceAttrPair("data.leifwind_entity.by_id", "name", "data.leifwind_entity.by_name", "name"),
				resource.TestCheckResourceAttrPair("data.leifwind_entity.by_id", "unique_key", "data.leifwind_entity.by_name", "unique_key"),
				resource.TestCheckResourceAttrPair("data.leifwind_field.by_id", "id", "data.leifwind_field.by_name", "id"),
				resource.TestCheckResourceAttrPair("data.leifwind_field.by_id", "name", "data.leifwind_field.by_name", "name"),
				resource.TestCheckResourceAttrPair("data.leifwind_field.by_id", "data_type", "data.leifwind_field.by_name", "data_type"),
				resource.TestCheckResourceAttrPair("data.leifwind_field.by_id", "connection_type", "data.leifwind_field.by_name", "connection_type"),
				resource.TestCheckResourceAttrPair("data.leifwind_field.by_id", "fragment_name", "data.leifwind_field.by_name", "fragment_name"),
				resource.TestCheckResourceAttrPair("data.leifwind_field.by_id", "unique_key", "data.leifwind_field.by_name", "unique_key"),
				// one unique_key assertion per singular data source (LW-109)
				resource.TestCheckResourceAttrPair("data.leifwind_entity.by_name", "unique_key", "leifwind_entity.e", "unique_key"),
				resource.TestCheckResourceAttrPair("data.leifwind_field.by_name", "unique_key", "leifwind_field.f", "unique_key"),
				// pattern-filtered entities listing (LW-109)
				resource.TestCheckResourceAttr("data.leifwind_entities.filtered", "entities.#", "1"),
				resource.TestCheckResourceAttr("data.leifwind_entities.filtered", "entities.0.name", "book"),
				// FRAGMENT projection in the fields listing (fields.go:116-120; LW-109)
				resource.TestCheckResourceAttr("data.leifwind_fields.all", "fields.0.connection_type", "FRAGMENT"),
				resource.TestCheckResourceAttr("data.leifwind_fields.all", "fields.0.fragment_name", "content"),
			),
		}},
	})
}
