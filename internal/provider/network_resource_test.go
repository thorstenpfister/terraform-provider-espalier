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

	"github.com/thorstenpfister/terraform-provider-espalier/internal/client"
)

// testClient talks to the local runtime, for setup and checks outside
// Terraform.
var testClient client.Client = client.CLI{Run: client.ExecRunner(defaultCLIPath)}

func TestAccNetwork(t *testing.T) {
	name := acctest.RandomWithPrefix("espalier-test")
	addr := "espalier_network.test"
	cidr := knownvalue.StringRegexp(regexp.MustCompile(`^[0-9.]+/\d+$`))

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviders,
		CheckDestroy:             checkNetworksGone(name),
		Steps: []resource.TestStep{
			// The runtime chooses the subnets.
			{
				Config: fmt.Sprintf(`resource "espalier_network" "test" { name = %q }`, name),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("id"), knownvalue.StringExact(name)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("internal"), knownvalue.Bool(false)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("subnet"), cidr),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("plugin"), knownvalue.StringExact("container-network-vmnet")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("options"), knownvalue.Null()),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("gateway"), knownvalue.StringRegexp(regexp.MustCompile(`^[0-9.]+$`))),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("labels"), knownvalue.Null()),
				},
			},
			{
				ResourceName:      addr,
				ImportState:       true,
				ImportStateId:     name,
				ImportStateVerify: true,
			},
			// Deleted outside Terraform: planned again and recreated.
			{
				PreConfig: func() {
					if err := testClient.DeleteNetwork(context.Background(), name); err != nil {
						t.Fatal(err)
					}
				},
				Config: fmt.Sprintf(`resource "espalier_network" "test" { name = %q }`, name),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionCreate)},
				},
			},
			// Any change replaces the network.
			{
				Config: fmt.Sprintf(`
resource "espalier_network" "test" {
  name     = %q
  internal = true
  subnet   = "192.168.231.0/24"
  labels   = { purpose = "espalier-test" }
  plugin   = "container-network-vmnet"
  options  = { "espalier.test" = "yes" }
}`, name),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionReplace)},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("internal"), knownvalue.Bool(true)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("subnet"), knownvalue.StringExact("192.168.231.0/24")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("gateway"), knownvalue.StringExact("192.168.231.1")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("labels"),
						knownvalue.MapExact(map[string]knownvalue.Check{"purpose": knownvalue.StringExact("espalier-test")})),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("options"),
						knownvalue.MapExact(map[string]knownvalue.Check{"espalier.test": knownvalue.StringExact("yes")})),
				},
			},
			{
				ResourceName:      addr,
				ImportState:       true,
				ImportStateId:     name,
				ImportStateVerify: true,
			},
		},
	})
}

func checkNetworksGone(names ...string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		for _, name := range names {
			n, err := testClient.Network(context.Background(), name)
			if err != nil {
				return err
			}
			if n != nil {
				return fmt.Errorf("network %s still exists", name)
			}
		}
		return nil
	}
}
