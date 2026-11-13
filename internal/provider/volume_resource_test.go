package provider

import (
	"context"
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

func TestAccVolume(t *testing.T) {
	name := acctest.RandomWithPrefix("espalier-test")
	addr := "espalier_volume.test"
	plain := fmt.Sprintf(`resource "espalier_volume" "test" { name = %q }`, name)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviders,
		CheckDestroy:             checkVolumesGone(name),
		Steps: []resource.TestStep{
			{
				Config: `resource "espalier_volume" "test" {
  name = "espalier-test-invalid"
  size = "2 GB"
}`,
				ExpectError: regexp.MustCompile(`must be a number of bytes`),
			},
			// The runtime's default size.
			{
				Config: plain,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("id"), knownvalue.StringExact(name)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("size"), knownvalue.Null()),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("size_bytes"), knownvalue.Int64Exact(512<<30)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("format"), knownvalue.StringExact("ext4")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("source"), knownvalue.StringRegexp(regexp.MustCompile(`/volume\.img$`))),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("labels"), knownvalue.Null()),
				},
			},
			{ResourceName: addr, ImportState: true, ImportStateId: name, ImportStateVerify: true},
			// Deleted outside Terraform: planned again and recreated.
			{
				PreConfig: func() {
					if err := testClient.DeleteVolume(context.Background(), name); err != nil {
						t.Fatal(err)
					}
				},
				Config: plain,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionCreate)},
				},
			},
			// Any change replaces the volume.
			{
				Config: fmt.Sprintf(`
resource "espalier_volume" "test" {
  name   = %q
  size   = "64M"
  labels = { purpose = "espalier-test" }
}`, name),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionReplace)},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("size"), knownvalue.StringExact("64M")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("size_bytes"), knownvalue.Int64Exact(64<<20)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("labels"),
						knownvalue.MapExact(map[string]knownvalue.Check{"purpose": knownvalue.StringExact("espalier-test")})),
				},
			},
			{ResourceName: addr, ImportState: true, ImportStateId: name, ImportStateVerify: true},
		},
	})
}

func checkVolumesGone(names ...string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		for _, name := range names {
			v, err := testClient.Volume(context.Background(), name)
			if err != nil {
				return err
			}
			if v != nil {
				return fmt.Errorf("volume %s still exists", name)
			}
		}
		return nil
	}
}
