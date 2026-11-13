package provider

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

// Arguments taken from other resources are unknown while planning, both on
// create and when what they come from changes. terraform_data's output is
// unknown until it is applied, so every argument below is.
func unknownValuesConfig(memory string) string {
	return fmt.Sprintf(`
resource "terraform_data" "v" {
  input = {
    name       = "espalier-test-unknown"
    image      = "busybox:1.37"
    platform   = "linux/arm64"
    cpus       = 1
    command    = ["sleep", "3600"]
    target     = "/data"
    port       = 18089
    label      = "unknown"
    user       = "1000:1000"
    workdir    = "/tmp"
    shm_size   = "64M"
    nameserver = "192.168.64.1"
    size       = "512M"
  }
}

resource "terraform_data" "memory" {
  input = %q
}

locals {
  v = terraform_data.v.output
}

resource "espalier_network" "test" {
  name   = local.v.name
  labels = { test = local.v.label }
}

resource "espalier_volume" "test" {
  name   = local.v.name
  size   = local.v.size
  labels = { test = local.v.label }
}

resource "espalier_image" "test" {
  name     = local.v.image
  platform = local.v.platform
}

resource "espalier_container" "test" {
  name     = local.v.name
  image    = espalier_image.test.name
  platform = local.v.platform
  command  = local.v.command
  cpus     = local.v.cpus
  memory   = terraform_data.memory.output
  user     = local.v.user
  workdir  = local.v.workdir
  shm_size = local.v.shm_size
  env      = { LABEL = local.v.label }
  labels   = { test = local.v.label }
  dns      = { nameservers = [local.v.nameserver] }
  networks = [{ network = espalier_network.test.name, aliases = ["${local.v.name}.espalier.test"] }]
  mounts   = [{ type = "volume", source = espalier_volume.test.name, target = local.v.target }]
  ports    = [{ host_ip = "127.0.0.1", host_port = local.v.port, container_port = 80 }]
}`, memory)
}

func TestAccUnknownValues(t *testing.T) {
	addr := "espalier_container.test"
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviders,
		CheckDestroy: resource.ComposeTestCheckFunc(
			checkContainersGone("espalier-test-unknown"),
			checkNetworksGone("espalier-test-unknown"),
			checkVolumesGone("espalier-test-unknown"),
		),
		Steps: []resource.TestStep{
			{
				Config: unknownValuesConfig("256M"),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("memory"), knownvalue.StringExact("256M")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("state"), knownvalue.StringExact("running")),
				},
			},
			// The memory changes, unknown until applied: the container is
			// replaced, what does not depend on it stays.
			{
				Config: unknownValuesConfig("512M"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(addr, plancheck.ResourceActionDestroyBeforeCreate),
					plancheck.ExpectResourceAction("espalier_network.test", plancheck.ResourceActionNoop),
					plancheck.ExpectResourceAction("espalier_volume.test", plancheck.ResourceActionNoop),
				}},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("memory"), knownvalue.StringExact("512M")),
				},
			},
		},
	})
}

func TestAccMachineUnknownValues(t *testing.T) {
	pulled := newImages(t, "alpine:3.22")
	config := func(cpus int) string {
		return fmt.Sprintf(`
resource "terraform_data" "v" {
  input = { name = %q, image = "alpine:3.22", memory = "1G", home_mount = "none" }
}

resource "terraform_data" "cpus" {
  input = %d
}

resource "espalier_machine" "test" {
  name       = terraform_data.v.output.name
  image      = terraform_data.v.output.image
  memory     = terraform_data.v.output.memory
  home_mount = terraform_data.v.output.home_mount
  cpus       = terraform_data.cpus.output
  running    = false
}`, testMachine, cpus)
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviders,
		CheckDestroy: func(*terraform.State) error {
			if m, err := testClient.Machine(context.Background(), testMachine); err != nil || m != nil {
				return fmt.Errorf("machine %s still exists (%v)", testMachine, err)
			}
			return deleteImages(pulled)
		},
		Steps: []resource.TestStep{
			{Config: config(1)},
			// Settings change in place, even unknown while planning.
			{
				Config: config(2),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("espalier_machine.test", plancheck.ResourceActionUpdate),
				}},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("espalier_machine.test", tfjsonpath.New("cpus"), knownvalue.Int64Exact(2)),
				},
			},
		},
	})
}
