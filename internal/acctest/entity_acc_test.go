// SPDX-License-Identifier: MPL-2.0

package acctest

import (
	"context"
	"fmt"
	"regexp"
	"testing"

	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"gitlab.com/leifwind/stream/terraform-provider-leifwind/client"
)

func entityConfig(token string) string {
	return ProviderConfig(token) + `
resource "leifwind_project" "p" {
  name = "acc_ent_proj"
}

resource "leifwind_entity" "e" {
  project_id = leifwind_project.p.id
  name       = "book"
}
`
}

func TestAccEntityLifecycle(t *testing.T) {
	PreCheck(t)
	t.Parallel()
	org := NewOrg(t)
	tok := org.Token(t, Stack())
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: ProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: entityConfig(tok),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("leifwind_entity.e", "id"),
					resource.TestCheckResourceAttr("leifwind_entity.e", "name", "book"),
					// STANDING ADDITION (owner-adjudicated after Task 20 review):
					// leifwind_entity also exposes a computed unique_key, mirroring
					// leifwind_project (commit 1040092).
					resource.TestCheckResourceAttrSet("leifwind_entity.e", "unique_key"),
				),
			},
			{
				ResourceName: "leifwind_entity.e",
				ImportState:  true,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					rs := s.RootModule().Resources["leifwind_entity.e"]
					return fmt.Sprintf("%s/%s", rs.Primary.Attributes["project_id"], rs.Primary.ID), nil
				},
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccEntityStrictCreate(t *testing.T) {
	PreCheck(t)
	t.Parallel()
	org := NewOrg(t)
	tok := org.Token(t, Stack())

	// pre-create project + same-named entity out-of-band; Create must fail
	// with the wrap-safe import hint (entity.go's documented contract)
	c, err := client.New(Stack().BackendURL, client.WithTokenSource(client.StaticToken(tok)))
	if err != nil {
		t.Fatal(err)
	}
	p, err := c.Metadata.UpsertProject(context.Background(),
		client.MetadataProject{Name: "acc_ent_conflict"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Metadata.UpsertEntity(context.Background(),
		client.MetadataEntity{ProjectID: *p.ObjectID, Name: "book"}); err != nil {
		t.Fatal(err)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: ProtoV6ProviderFactories(),
		Steps: []resource.TestStep{{
			Config: ProviderConfig(tok) + fmt.Sprintf(`
resource "leifwind_entity" "e" {
  project_id = %q
  name       = "book"
}
`, p.ObjectID),
			ExpectError: regexp.MustCompile(`already exists.*terraform import`),
		}},
	})
}

func TestAccEntityDriftRecreates(t *testing.T) {
	PreCheck(t)
	t.Parallel()
	org := NewOrg(t)
	tok := org.Token(t, Stack())
	cfg := ProviderConfig(tok) + `
resource "leifwind_project" "p" {
  name = "acc_ent_drift"
}

resource "leifwind_entity" "e" {
  project_id = leifwind_project.p.id
  name       = "book"
}
`
	c, err := client.New(Stack().BackendURL, client.WithTokenSource(client.StaticToken(tok)))
	if err != nil {
		t.Fatal(err)
	}
	var projectID, firstEntityID string
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: ProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: cfg,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrWith("leifwind_entity.e", "project_id", func(v string) error {
						projectID = v
						return nil
					}),
					resource.TestCheckResourceAttrWith("leifwind_entity.e", "id", func(v string) error {
						firstEntityID = v
						return nil
					}),
				),
			},
			{
				PreConfig: func() {
					// delete out-of-band: Read must RemoveResource, apply recreates
					pid, perr := uuid.Parse(projectID)
					eid, eerr := uuid.Parse(firstEntityID)
					if perr != nil || eerr != nil {
						t.Fatalf("drift setup: %v %v", perr, eerr)
					}
					if err := c.Metadata.DeleteEntity(context.Background(), pid, eid); err != nil {
						t.Fatal(err)
					}
				},
				Config: cfg,
				Check: resource.TestCheckResourceAttrWith("leifwind_entity.e", "id", func(v string) error {
					if v == firstEntityID {
						return fmt.Errorf("entity was not recreated: id %s unchanged after out-of-band delete", v)
					}
					return nil
				}),
			},
		},
	})
}
