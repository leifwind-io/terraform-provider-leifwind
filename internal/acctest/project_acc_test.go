// SPDX-License-Identifier: MPL-2.0

package acctest

import (
	"context"
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"gitlab.com/leifwind/stream/terraform-provider-leifwind/client"
)

func TestAccProjectLifecycle(t *testing.T) {
	PreCheck(t)
	t.Parallel()
	org := NewOrg(t)
	cfg := ProviderConfig(org.Token(t, Stack())) + `
resource "leifwind_project" "p" {
  name = "acc_project"
}
`
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: ProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: cfg,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("leifwind_project.p", "id"),
					resource.TestCheckResourceAttr("leifwind_project.p", "name", "acc_project"),
					resource.TestCheckResourceAttrPair("leifwind_project.p", "unique_key", "leifwind_project.p", "name"),
				),
			},
			{
				ResourceName:      "leifwind_project.p",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccProjectStrictCreate(t *testing.T) {
	PreCheck(t)
	t.Parallel()
	org := NewOrg(t)
	tok := org.Token(t, Stack())

	// pre-create the same name out-of-band
	c, err := client.New(Stack().BackendURL, client.WithTokenSource(client.StaticToken(tok)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Metadata.UpsertProject(context.Background(),
		client.MetadataProject{Name: "acc_conflict"}); err != nil {
		t.Fatal(err)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: ProtoV6ProviderFactories(),
		Steps: []resource.TestStep{{
			Config: ProviderConfig(tok) + `
resource "leifwind_project" "p" {
  name = "acc_conflict"
}
`,
			ExpectError: regexp.MustCompile(`already exists.*terraform import`),
		}},
	})
}

func TestAccProjectDriftRecreates(t *testing.T) {
	PreCheck(t)
	t.Parallel()
	org := NewOrg(t)
	tok := org.Token(t, Stack())
	cfg := ProviderConfig(tok) + `
resource "leifwind_project" "p" {
  name = "acc_drift"
}
`
	c, err := client.New(Stack().BackendURL, client.WithTokenSource(client.StaticToken(tok)))
	if err != nil {
		t.Fatal(err)
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: ProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{Config: cfg},
			{
				PreConfig: func() {
					// delete out-of-band: Read must RemoveResource, apply recreates
					page, err := c.Metadata.ListProjects(context.Background(), client.ListOpts{Pattern: "acc_drift"})
					if err != nil || len(page.Objects) != 1 {
						t.Fatalf("drift setup: %v %d", err, len(page.Objects))
					}
					if err := c.Metadata.DeleteProject(context.Background(), *page.Objects[0].ObjectID); err != nil {
						t.Fatal(err)
					}
				},
				Config: cfg,
				Check:  resource.TestCheckResourceAttrSet("leifwind_project.p", "id"),
			},
		},
	})
	_ = fmt.Sprintf
}
