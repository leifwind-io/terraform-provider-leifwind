// SPDX-License-Identifier: MPL-2.0

package acctest

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func fieldConfig(token, fragmentName string) string {
	// The FRAGMENT field references the KEY field via key_field_ids, which
	// creates the Terraform graph edge that orders KEY-before-FRAGMENT on
	// create and FRAGMENT-before-KEY on destroy (both backend-enforced, LW-70).
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
  key_field_ids   = [leifwind_field.title.id]
}
`, fragmentName)
}

func TestAccFieldLifecycleAndFragmentUpdate(t *testing.T) {
	PreCheck(t)
	t.Parallel()
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
				ResourceName: "leifwind_field.body",
				ImportState:  true,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					rs := s.RootModule().Resources["leifwind_field.body"]
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
	t.Parallel()
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

func TestAccFieldKeyFieldIDsRequired(t *testing.T) {
	PreCheck(t)
	t.Parallel()
	org := NewOrg(t)
	tok := org.Token(t, Stack())
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: ProtoV6ProviderFactories(),
		Steps: []resource.TestStep{{
			Config: ProviderConfig(tok) + `
resource "leifwind_project" "p" {
  name = "acc_fld_keyreq"
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
  fragment_name   = "content"
}
`,
			ExpectError: regexp.MustCompile(`key_field_ids is required`),
		}},
	})
}

func TestAccFieldKeyFieldIDsMembership(t *testing.T) {
	PreCheck(t)
	t.Parallel()
	org := NewOrg(t)
	tok := org.Token(t, Stack())
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: ProtoV6ProviderFactories(),
		Steps: []resource.TestStep{{
			// "extra" references a FRAGMENT field id (body) rather than a KEY
			// field id, so its Create must fail the membership check.
			Config: ProviderConfig(tok) + `
resource "leifwind_project" "p" {
  name = "acc_fld_keymember"
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
  fragment_name   = "content"
  key_field_ids   = [leifwind_field.title.id]
}

resource "leifwind_field" "extra" {
  project_id      = leifwind_project.p.id
  entity_id       = leifwind_entity.e.id
  name            = "extra"
  data_type       = "TEXT"
  connection_type = "FRAGMENT"
  fragment_name   = "content"
  key_field_ids   = [leifwind_field.body.id]
}
`,
			// tofu's diagnostic renderer word-wraps at a fixed column, and the
			// 36-char entity UUID reliably pushes "entity" onto the next line;
			// \s+ absorbs that wrap (plain literal text would never match it).
			ExpectError: regexp.MustCompile(`not KEY fields`),
		}},
	})
}
