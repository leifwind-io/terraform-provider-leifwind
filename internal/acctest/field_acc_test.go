// SPDX-License-Identifier: MPL-2.0

package acctest

import (
	"context"
	"fmt"
	"regexp"
	"testing"

	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"gitlab.com/leifwind/stream/terraform-provider-leifwind/client"
)

func fieldConfig(token, fragmentName string) string {
	// LW-70: the FRAGMENT field depends_on the KEY field so that Terraform
	// (a) creates the KEY field first — the backend 500s in
	// sync_entity_schema when the FIRST field of an entity is a FRAGMENT
	// field — and (b) destroys the FRAGMENT field first — deleting a KEY
	// field while a FRAGMENT sibling exists also 500s. Without the edge the
	// two fields are created/destroyed in parallel, racing into both bugs.
	return ProviderConfig(token) + fmt.Sprintf(`
resource "leifwind_project" "p" {
  name = "acc_fld_proj"
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

resource "leifwind_field" "body" {
  project_id      = leifwind_project.p.id
  entity_id       = leifwind_entity.e.id
  name            = "body"
  data_type       = "TEXT"
  connection_type = "FRAGMENT"
  fragment_name   = %q

  depends_on = [leifwind_field.title] # LW-70, see fieldConfig comment
}
`, fragmentName)
}

// plantKeeperField creates an out-of-band (non-Terraform-managed) KEY field
// on the entity owning the given Terraform field resource.
//
// LW-70: the backend 500s in sync_entity_schema when the LAST field of an
// entity is deleted, so `terraform destroy` of a config managing ALL of an
// entity's fields cannot succeed (verified empirically: post-test destroy
// failed with 500 on the final field DELETE). The keeper guarantees the
// Terraform-managed fields are never the last one; the entity's own delete
// then cascades the keeper server-side.
func plantKeeperField(t *testing.T, tok, resName string) resource.TestCheckFunc {
	t.Helper()
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resName]
		if !ok {
			return fmt.Errorf("resource %s not in state", resName)
		}
		pid, err := uuid.Parse(rs.Primary.Attributes["project_id"])
		if err != nil {
			return fmt.Errorf("project_id: %w", err)
		}
		eid, err := uuid.Parse(rs.Primary.Attributes["entity_id"])
		if err != nil {
			return fmt.Errorf("entity_id: %w", err)
		}
		c, err := client.New(Stack().BackendURL, client.WithTokenSource(client.StaticToken(tok)))
		if err != nil {
			return err
		}
		// UpsertField is create-or-adopt, so re-running this check is idempotent.
		_, err = c.Metadata.UpsertField(context.Background(), client.MetadataField{
			ProjectID: pid, EntityID: eid, Name: "lw70_keeper",
			Config:     client.FieldConfig{DataType: client.DataTypeText},
			Connection: client.Connection{Type: client.ConnectionKey},
		})
		return err
	}
}

func TestAccFieldLifecycleAndFragmentUpdate(t *testing.T) {
	PreCheck(t)
	org := NewOrg(t)
	tok := org.Token(t, Stack())
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: ProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: fieldConfig(tok, "content"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("leifwind_field.title", "id"),
					resource.TestCheckResourceAttr("leifwind_field.body", "fragment_name", "content"),
					// STANDING ADDITION (owner-adjudicated after Task 20 review):
					// leifwind_field also exposes a computed unique_key, mirroring
					// leifwind_project (commit 1040092) and leifwind_entity.
					resource.TestCheckResourceAttrSet("leifwind_field.title", "unique_key"),
					plantKeeperField(t, tok, "leifwind_field.title"), // LW-70
				),
			},
			{
				// fragment_name is updatable IN PLACE — assert no replacement
				Config: fieldConfig(tok, "content_v2"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("leifwind_field.body", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.TestCheckResourceAttr("leifwind_field.body", "fragment_name", "content_v2"),
			},
			{
				ResourceName: "leifwind_field.title",
				ImportState:  true,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					rs := s.RootModule().Resources["leifwind_field.title"]
					return fmt.Sprintf("%s/%s/%s",
						rs.Primary.Attributes["project_id"],
						rs.Primary.Attributes["entity_id"],
						rs.Primary.ID), nil
				},
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccFieldFragmentValidation(t *testing.T) {
	PreCheck(t)
	org := NewOrg(t)
	tok := org.Token(t, Stack())
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: ProtoV6ProviderFactories(),
		Steps: []resource.TestStep{{
			Config: ProviderConfig(tok) + `
resource "leifwind_project" "p" {
  name = "acc_fld_bad"
}

resource "leifwind_entity" "e" {
  project_id = leifwind_project.p.id
  name       = "book"
}

resource "leifwind_field" "bad" {
  project_id      = leifwind_project.p.id
  entity_id       = leifwind_entity.e.id
  name            = "bad"
  data_type       = "TEXT"
  connection_type = "FRAGMENT"
}
`,
			ExpectError: regexp.MustCompile(`fragment_name is required`),
		}},
	})
}
