// SPDX-License-Identifier: MPL-2.0

package acctest

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
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
