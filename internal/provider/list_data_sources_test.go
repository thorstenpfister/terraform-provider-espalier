package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

const listConfig = `
locals {
  labels = { "espalier.test/list" = "yes" }
}

# Created in reverse order, to see the list sorted.
resource "espalier_network" "b" {
  name   = "espalier-test-list-b"
  labels = local.labels
}

resource "espalier_network" "a" {
  name   = "espalier-test-list-a"
  labels = local.labels
}

resource "espalier_volume" "test" {
  name   = "espalier-test-list"
  size   = "64M"
  labels = local.labels
}

resource "espalier_container" "test" {
  name    = "espalier-test-list"
  image   = "busybox:1.37"
  labels  = local.labels
  running = false
}
`

const listDataConfig = listConfig + `
data "espalier_networks" "test" {
  labels = local.labels
}

data "espalier_volumes" "test" {
  labels = local.labels
}

data "espalier_containers" "test" {
  labels = local.labels
}

data "espalier_networks" "none" {
  labels = { "espalier.test/list" = "no" }
}

data "espalier_images" "all" {}
`

func TestAccListDataSources(t *testing.T) {
	name := func(n string) knownvalue.Check {
		return knownvalue.ObjectPartial(map[string]knownvalue.Check{"name": knownvalue.StringExact(n)})
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviders,
		CheckDestroy:             checkContainersGone("espalier-test-list"),
		Steps: []resource.TestStep{
			// Data sources read after the resources exist.
			{Config: listConfig},
			{
				Config: listDataConfig,
				ConfigStateChecks: []statecheck.StateCheck{
					// Only the labelled ones, ordered by name.
					statecheck.ExpectKnownValue("data.espalier_networks.test", tfjsonpath.New("networks"),
						knownvalue.ListExact([]knownvalue.Check{name("espalier-test-list-a"), name("espalier-test-list-b")})),
					statecheck.ExpectKnownValue("data.espalier_volumes.test", tfjsonpath.New("volumes"),
						knownvalue.ListExact([]knownvalue.Check{knownvalue.ObjectPartial(map[string]knownvalue.Check{
							"name": knownvalue.StringExact("espalier-test-list"), "size_bytes": knownvalue.Int64Exact(64 << 20),
						})})),
					statecheck.ExpectKnownValue("data.espalier_containers.test", tfjsonpath.New("containers"),
						knownvalue.ListExact([]knownvalue.Check{knownvalue.ObjectPartial(map[string]knownvalue.Check{
							"name":  knownvalue.StringExact("espalier-test-list"),
							"image": knownvalue.StringExact("docker.io/library/busybox:1.37"),
							"state": knownvalue.StringExact("stopped"),
						})})),
					statecheck.ExpectKnownValue("data.espalier_networks.none", tfjsonpath.New("networks"), knownvalue.ListExact(nil)),
				},
				Check: resource.TestCheckTypeSetElemNestedAttrs("data.espalier_images.all", "images.*", map[string]string{
					"name": "docker.io/library/busybox:1.37",
				}),
			},
		},
	})
}
