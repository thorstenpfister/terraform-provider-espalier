package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

// Empty collections mean the same as unset ones (see empty.go): writing
// them is consistent after apply, switching between the two never replaces
// anything, and importing under a configuration that writes them only
// updates the state in place, as Terraform needs for the difference.
func emptyCollectionSteps(addr, id, empty, unset string) []resource.TestStep {
	inPlace := resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
		plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate),
	}}
	return []resource.TestStep{
		{Config: empty},
		{
			Config: empty, ResourceName: addr, ImportState: true, ImportStateKind: resource.ImportBlockWithID,
			ImportStateId: id, ExpectNonEmptyPlan: true,
			ImportPlanChecks: resource.ImportPlanChecks{PreApply: []plancheck.PlanCheck{
				plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate),
			}},
		},
		{Config: unset, ConfigPlanChecks: inPlace},
		{Config: empty, ConfigPlanChecks: inPlace},
	}
}

func TestAccContainerEmptyCollections(t *testing.T) {
	config := func(empty bool) string {
		collections := ""
		if empty {
			collections = `
  env             = {}
  labels          = {}
  mounts          = []
  ports           = []
  publish_sockets = []
  ulimits         = []
  cap_add         = []
  cap_drop        = []
  kernel_args     = []
  read_only_paths = []
  masked_paths    = []
  networks        = [{ network = "default", aliases = [] }]`
		}
		return fmt.Sprintf(`
resource "espalier_container" "test" {
  name    = "espalier-test-empty"
  image   = "busybox:1.37"
  command = ["sleep", "3600"]
  %s
}`, collections)
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviders,
		CheckDestroy:             checkContainersGone("espalier-test-empty"),
		Steps:                    emptyCollectionSteps("espalier_container.test", "espalier-test-empty", config(true), config(false)),
	})
}

func TestAccNetworkEmptyCollections(t *testing.T) {
	config := func(empty bool) string {
		collections := ""
		if empty {
			collections = "labels  = {}\n  options = {}"
		}
		return fmt.Sprintf("resource \"espalier_network\" \"test\" {\n  name = \"espalier-test-empty\"\n  %s\n}", collections)
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviders,
		CheckDestroy:             checkNetworksGone("espalier-test-empty"),
		Steps:                    emptyCollectionSteps("espalier_network.test", "espalier-test-empty", config(true), config(false)),
	})
}

func TestAccVolumeEmptyCollections(t *testing.T) {
	config := func(empty bool) string {
		collections := ""
		if empty {
			collections = "labels  = {}\n  options = {}"
		}
		return fmt.Sprintf("resource \"espalier_volume\" \"test\" {\n  name = \"espalier-test-empty\"\n  %s\n}", collections)
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviders,
		CheckDestroy:             checkVolumesGone("espalier-test-empty"),
		Steps:                    emptyCollectionSteps("espalier_volume.test", "espalier-test-empty", config(true), config(false)),
	})
}

// A dns block with nothing but empty lists is the same as none; one with
// some values keeps its empty lists as written.
func TestAccContainerEmptyDNS(t *testing.T) {
	config := func(dns string) string {
		return fmt.Sprintf(`
resource "espalier_container" "test" {
  name    = "espalier-test-empty-dns"
  image   = "busybox:1.37"
  command = ["sleep", "3600"]
  %s
}`, dns)
	}
	empty := config(`dns = { nameservers = [], search_domains = [], options = [] }`)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviders,
		CheckDestroy:             checkContainersGone("espalier-test-empty-dns"),
		Steps: append(emptyCollectionSteps("espalier_container.test", "espalier-test-empty-dns", empty, config("")),
			resource.TestStep{
				Config: config(`dns = { nameservers = ["192.168.64.1"], search_domains = [], options = [] }`),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("espalier_container.test", plancheck.ResourceActionDestroyBeforeCreate),
				}},
			}),
	})
}
