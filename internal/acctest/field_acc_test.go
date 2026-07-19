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

func fieldConfig(token, projectName, fragmentName string) string {
	// The FRAGMENT field references the KEY field via key_field_ids, which
	// creates the Terraform graph edge that orders KEY-before-FRAGMENT on
	// create and FRAGMENT-before-KEY on destroy (both backend-enforced, LW-70).
	// projectName is a parameter because parallel tests share one backend and
	// project names are globally unique (LW-71) — each caller needs its own.
	return ProviderConfig(token) + fmt.Sprintf(`
resource "leifwind_project" "p" {
  name = %q
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
`, projectName, fragmentName)
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
				Config: fieldConfig(tok, "acc_fld_proj", "content"),
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
				Config: fieldConfig(tok, "acc_fld_proj", "content_v2"),
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
			{
				// KEY-field import round-trip: ImportState must leave
				// key_field_ids unset for a KEY field (the FRAGMENT-only seed
				// guard); a regression that seeded a KEY would fail this verify.
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

func TestAccFieldStrictCreate(t *testing.T) {
	PreCheck(t)
	t.Parallel()
	org := NewOrg(t)
	tok := org.Token(t, Stack())

	// pre-create project + entity + same-named KEY field out-of-band; Create
	// must fail with the wrap-safe import hint (field.go's documented contract)
	c, err := client.New(Stack().BackendURL, client.WithTokenSource(client.StaticToken(tok)))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	p, err := c.Metadata.UpsertProject(ctx, client.MetadataProject{Name: "acc_fld_conflict"})
	if err != nil {
		t.Fatal(err)
	}
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

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: ProtoV6ProviderFactories(),
		Steps: []resource.TestStep{{
			Config: ProviderConfig(tok) + fmt.Sprintf(`
resource "leifwind_field" "title" {
  project_id      = %q
  entity_id       = %q
  name            = "title"
  data_type       = "TEXT"
  connection_type = "KEY"
}
`, p.ObjectID, e.ObjectID),
			ExpectError: regexp.MustCompile(`already exists.*terraform import`),
		}},
	})
}

func TestAccFieldDriftRecreates(t *testing.T) {
	PreCheck(t)
	t.Parallel()
	org := NewOrg(t)
	tok := org.Token(t, Stack())
	// dedicated project name: acc_fld_proj is taken by the lifecycle test
	// running in parallel against the same backend (LW-71)
	cfg := fieldConfig(tok, "acc_fld_drift", "content")
	c, err := client.New(Stack().BackendURL, client.WithTokenSource(client.StaticToken(tok)))
	if err != nil {
		t.Fatal(err)
	}
	var projectID, entityID, firstFieldID string
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: ProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: cfg,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrWith("leifwind_field.body", "project_id", func(v string) error {
						projectID = v
						return nil
					}),
					resource.TestCheckResourceAttrWith("leifwind_field.body", "entity_id", func(v string) error {
						entityID = v
						return nil
					}),
					resource.TestCheckResourceAttrWith("leifwind_field.body", "id", func(v string) error {
						firstFieldID = v
						return nil
					}),
				),
			},
			{
				PreConfig: func() {
					// Drift the FRAGMENT field, not the KEY one: LW-70 ordering
					// (FRAGMENT deleted before KEY) — the backend refuses to
					// delete a KEY field while a FRAGMENT still references it.
					pid, perr := uuid.Parse(projectID)
					eid, eerr := uuid.Parse(entityID)
					fid, ferr := uuid.Parse(firstFieldID)
					if perr != nil || eerr != nil || ferr != nil {
						t.Fatalf("drift setup: %v %v %v", perr, eerr, ferr)
					}
					if err := c.Metadata.DeleteField(context.Background(), pid, eid, fid); err != nil {
						t.Fatal(err)
					}
				},
				Config: cfg,
				Check: resource.TestCheckResourceAttrWith("leifwind_field.body", "id", func(v string) error {
					if v == firstFieldID {
						return fmt.Errorf("field was not recreated: id %s unchanged after out-of-band delete", v)
					}
					return nil
				}),
			},
		},
	})
}
