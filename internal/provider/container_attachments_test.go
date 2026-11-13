package provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/compare"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

func attachedConfig(dir string, hostPort int) string {
	return fmt.Sprintf(`
resource "espalier_network" "test" {
  name   = "espalier-test-ctr-net"
  subnet = "192.168.233.0/24"
}

resource "espalier_volume" "test" {
  name = "espalier-test-ctr-vol"
  size = "64M"
}

resource "espalier_container" "test" {
  name    = "espalier-test-attached"
  image   = "busybox:1.37"
  command = ["sleep", "3600"]

  networks = [
    {
      network     = espalier_network.test.name
      mac_address = "02:42:c0:a8:e9:10"
      mtu         = 1400
      aliases     = ["attached.espalier.test"]
    },
    { network = "default" },
  ]
  mounts = [
    { type = "volume", source = espalier_volume.test.name, target = "/data" },
    { type = "bind", source = %q, target = "/host", read_only = true },
    { type = "tmpfs", target = "/scratch" },
  ]
  ports = [
    { host_ip = "127.0.0.1", host_port = %d, container_port = 80 },
    { host_port = %d, container_port = 53, protocol = "udp" },
  ]
  cap_add  = ["CAP_NET_RAW"]
  cap_drop = ["CAP_MKNOD"]
}

data "espalier_container" "test" {
  name = espalier_container.test.name
}`, dir, hostPort, hostPort)
}

func TestAccContainerAttachments(t *testing.T) {
	addr, data := "espalier_container.test", "data.espalier_container.test"
	dir := t.TempDir()
	net := func(i int, key string) tfjsonpath.Path {
		return tfjsonpath.New("networks").AtSliceIndex(i).AtMapKey(key)
	}
	addr0 := func(key string) tfjsonpath.Path { return tfjsonpath.New("addresses").AtSliceIndex(0).AtMapKey(key) }

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviders,
		CheckDestroy:             checkContainersGone("espalier-test-attached"),
		Steps: []resource.TestStep{
			{
				Config: attachedConfig(dir, 18091),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, net(0, "mtu"), knownvalue.Int64Exact(1400)),
					statecheck.ExpectKnownValue(addr, net(1, "network"), knownvalue.StringExact("default")),
					statecheck.ExpectKnownValue(addr, net(1, "mtu"), knownvalue.NotNull()), // the runtime's default
					statecheck.ExpectKnownValue(addr, net(1, "mac_address"), knownvalue.Null()),
					// The primary network, with the requested MAC.
					statecheck.ExpectKnownValue(addr, addr0("network"), knownvalue.StringExact("espalier-test-ctr-net")),
					statecheck.ExpectKnownValue(addr, addr0("mac_address"), knownvalue.StringExact("02:42:c0:a8:e9:10")),
					statecheck.ExpectKnownValue(addr, addr0("ipv4_address"), knownvalue.StringRegexp(regexp.MustCompile(`^192\.168\.233\.`))),
					// The data source sees the same container, with the image fully qualified.
					statecheck.CompareValuePairs(addr, tfjsonpath.New("mounts"), data, tfjsonpath.New("mounts"), compare.ValuesSame()),
					statecheck.CompareValuePairs(addr, tfjsonpath.New("networks"), data, tfjsonpath.New("networks"), compare.ValuesSame()),
					statecheck.CompareValuePairs(addr, tfjsonpath.New("addresses"), data, tfjsonpath.New("addresses"), compare.ValuesSame()),
					statecheck.ExpectKnownValue(data, tfjsonpath.New("image"), knownvalue.StringExact("docker.io/library/busybox:1.37")),
					statecheck.ExpectKnownValue(data, tfjsonpath.New("cap_add"), knownvalue.SetExact([]knownvalue.Check{knownvalue.StringExact("CAP_NET_RAW")})),
					statecheck.ExpectKnownValue(data, net(0, "aliases"), knownvalue.ListExact([]knownvalue.Check{knownvalue.StringExact("attached.espalier.test")})),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("ports"), knownvalue.ListExact([]knownvalue.Check{
						knownvalue.ObjectExact(map[string]knownvalue.Check{
							"host_ip": knownvalue.StringExact("127.0.0.1"), "host_port": knownvalue.Int64Exact(18091),
							"container_port": knownvalue.Int64Exact(80), "protocol": knownvalue.StringExact("tcp"),
						}),
						knownvalue.ObjectExact(map[string]knownvalue.Check{
							"host_ip": knownvalue.StringExact("0.0.0.0"), "host_port": knownvalue.Int64Exact(18091),
							"container_port": knownvalue.Int64Exact(53), "protocol": knownvalue.StringExact("udp"),
						}),
					})),
				},
			},
			{
				ResourceName:      addr,
				ImportState:       true,
				ImportStateVerify: true,
				// imported fully qualified
				ImportStateVerifyIgnore: []string{"image"},
			},
			// Unchanged: no diff, even with the runtime's defaults filled in.
			{Config: attachedConfig(dir, 18091), PlanOnly: true},
			{
				Config: attachedConfig(dir, 18092),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(addr, plancheck.ResourceActionDestroyBeforeCreate),
				}},
			},
		},
	})
}

func TestAccContainerInvalid(t *testing.T) {
	config := func(extra string) string {
		return fmt.Sprintf(`
resource "espalier_container" "test" {
  name  = "espalier-test-invalid"
  image = "busybox:1.37"
  %s
}`, extra)
	}
	var steps []resource.TestStep
	for extra, msg := range map[string]string{
		`mounts = [{ type = "tmpfs", source = "x", target = "/t" }]`:              "takes no source",
		`mounts = [{ type = "volume", target = "/t" }]`:                           "needs a source",
		`mounts = [{ type = "bind", source = "rel", target = "/t" }]`:             "absolute host",
		`cap_add = ["net_raw"]`:                                                   "as the runtime",
		`networks = [{ network = "default", mac_address = "02:42:AC:11:00:02" }]`: "MAC address in lower",
		`memory = "1.5G"`: "number of bytes",
	} {
		steps = append(steps, resource.TestStep{Config: config(extra), PlanOnly: true, ExpectError: regexp.MustCompile(msg)})
	}
	resource.Test(t, resource.TestCase{ProtoV6ProviderFactories: testAccProviders, Steps: steps})
}

func TestAccContainerDataSourceMissing(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviders,
		Steps: []resource.TestStep{{
			Config:      `data "espalier_container" "test" { name = "espalier-test-never-created" }`,
			ExpectError: regexp.MustCompile(`does not exist`),
		}},
	})
}
