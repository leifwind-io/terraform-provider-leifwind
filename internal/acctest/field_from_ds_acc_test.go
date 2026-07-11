// SPDX-License-Identifier: MPL-2.0

package acctest

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"gitlab.com/leifwind/stream/terraform-provider-leifwind/client"
)

// provisionEntityWithKey creates a project + "book" entity + "title" KEY field
// OUT OF BAND (not Terraform-managed), returning the unique project name. A
// t.Cleanup deletes the project (cascading the entity and its fields). This
// models a pre-existing entity that a consumer adopts via data sources — no
// terraform import of the entity or its fields.
func provisionEntityWithKey(t *testing.T, tok string) string {
	t.Helper()
	ctx := context.Background()
	c, err := client.New(Stack().BackendURL, client.WithTokenSource(client.StaticToken(tok)))
	if err != nil {
		t.Fatal(err)
	}
	// Project names are globally unique across tenants (LW-71) — use a unique one.
	name := "acc_fld_ds_" + uuid.NewString()[:8]
	p, err := c.Metadata.UpsertProject(ctx, client.MetadataProject{Name: name})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = c.Metadata.DeleteProject(context.Background(), *p.ObjectID)
	})
	e, err := c.Metadata.UpsertEntity(ctx, client.MetadataEntity{ProjectID: *p.ObjectID, Name: "book"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Metadata.UpsertField(ctx, client.MetadataField{
		ProjectID: *p.ObjectID, EntityID: *e.ObjectID, Name: "title",
		Config:     client.FieldConfig{DataType: client.DataTypeText},
		Connection: client.Connection{Type: client.ConnectionKey},
	}); err != nil {
		t.Fatal(err)
	}
	return name
}

// TestAccFieldFromDataSources proves the adoption path (LW-44 style): read a
// pre-existing entity and its KEY field entirely via data sources, then create
// a NEW FRAGMENT field via the resource with key_field_ids sourced from the
// data source. Nothing is imported into Terraform state — the data sources
// supply everything (project_id, entity_id, and the KEY field id) needed to
// create the fragment, and the data-source-sourced id passes apply-time
// membership validation.
func TestAccFieldFromDataSources(t *testing.T) {
	PreCheck(t)
	t.Parallel()
	org := NewOrg(t)
	tok := org.Token(t, Stack())
	projName := provisionEntityWithKey(t, tok)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: ProtoV6ProviderFactories(),
		Steps: []resource.TestStep{{
			Config: ProviderConfig(tok) + fmt.Sprintf(`
data "leifwind_project" "p" {
  name = %q
}

data "leifwind_entity" "e" {
  project_id = data.leifwind_project.p.id
  name       = "book"
}

data "leifwind_field" "key" {
  project_id = data.leifwind_project.p.id
  entity_id  = data.leifwind_entity.e.id
  name       = "title"
}

resource "leifwind_field" "body" {
  project_id      = data.leifwind_project.p.id
  entity_id       = data.leifwind_entity.e.id
  name            = "body"
  data_type       = "TEXT"
  connection_type = "FRAGMENT"
  fragment_name   = "content"
  key_field_ids   = [data.leifwind_field.key.id]
}
`, projName),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("leifwind_field.body", "fragment_name", "content"),
				resource.TestCheckResourceAttr("leifwind_field.body", "connection_type", "FRAGMENT"),
				resource.TestCheckResourceAttr("leifwind_field.body", "key_field_ids.#", "1"),
				resource.TestCheckResourceAttrPair("leifwind_field.body", "key_field_ids.0", "data.leifwind_field.key", "id"),
			),
		}},
	})
}
