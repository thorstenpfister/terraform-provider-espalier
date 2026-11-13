package provider

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"

	"github.com/thorstenpfister/terraform-provider-espalier/internal/client"
)

func optionsConfig(dir, platform, shm string) string {
	return fmt.Sprintf(`
resource "espalier_container" "test" {
  name      = "espalier-test-options"
  image     = "busybox:1.37"
  command   = ["sleep", "3600"]
  scheme    = "https"
  platform  = %q
  user      = "1000:1000"
  read_only = true
  init      = true
  shm_size  = %q

  dns = {
    nameservers    = ["1.1.1.1"]
    domain         = "espalier.test"
    search_domains = ["a.espalier.test"]
    options        = ["ndots:2"]
  }
  ulimits         = [{ name = "nofile", soft = 1024, hard = 2048 }]
  publish_sockets = [{ host_path = "%s/s.sock", container_path = "/run/s.sock" }]
}

data "espalier_container" "test" {
  name = espalier_container.test.name
}`, platform, shm, dir)
}

func TestAccContainerOptions(t *testing.T) {
	addr, data := "espalier_container.test", "data.espalier_container.test"
	dir := t.TempDir()
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviders,
		CheckDestroy:             checkContainersGone("espalier-test-options"),
		Steps: []resource.TestStep{
			{
				Config: optionsConfig(dir, "linux/arm64", "128M"),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("state"), knownvalue.StringExact("running")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("platform"), knownvalue.StringExact("linux/arm64")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("no_dns"), knownvalue.Bool(false)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("dns").AtMapKey("domain"), knownvalue.StringExact("espalier.test")),
					// As the runtime reports it.
					statecheck.ExpectKnownValue(data, tfjsonpath.New("platform"), knownvalue.StringExact("linux/arm64/v8")),
					statecheck.ExpectKnownValue(data, tfjsonpath.New("shm_size"), knownvalue.StringExact("128M")),
					statecheck.ExpectKnownValue(data, tfjsonpath.New("ulimits"), knownvalue.ListExact([]knownvalue.Check{
						knownvalue.ObjectExact(map[string]knownvalue.Check{
							"name": knownvalue.StringExact("nofile"), "soft": knownvalue.Int64Exact(1024), "hard": knownvalue.Int64Exact(2048),
						}),
					})),
					statecheck.ExpectKnownValue(data, tfjsonpath.New("user"), knownvalue.StringExact("1000:1000")),
					statecheck.ExpectKnownValue(data, tfjsonpath.New("read_only"), knownvalue.Bool(true)),
					statecheck.ExpectKnownValue(data, tfjsonpath.New("init"), knownvalue.Bool(true)),
					statecheck.ExpectKnownValue(data, tfjsonpath.New("scheme"), knownvalue.Null()),
				},
			},
			{
				ResourceName:            addr,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"image", "platform", "scheme"}, // as the runtime reports them
			},
			// The same platform and size, written differently: no replacement.
			{
				Config: optionsConfig(dir, "linux/arm64/v8", "131072K"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate),
				}},
			},
			{
				Config: optionsConfig(dir, "linux/arm64/v8", "256M"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(addr, plancheck.ResourceActionDestroyBeforeCreate),
				}},
			},
		},
	})
}

func TestAccContainerNoDNS(t *testing.T) {
	addr := "espalier_container.test"
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviders,
		CheckDestroy:             checkContainersGone("espalier-test-no-dns"),
		Steps: []resource.TestStep{
			{
				Config: `
resource "espalier_container" "test" {
  name    = "espalier-test-no-dns"
  image   = "busybox:1.37"
  command = ["sleep", "3600"]
  no_dns  = true
}`,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("no_dns"), knownvalue.Bool(true)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("dns"), knownvalue.Null()),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("user"), knownvalue.StringExact("0:0")),
				},
			},
			{ResourceName: addr, ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"image"}},
		},
	})
}

func vmConfig(kernel, initImage, kernelArg string) string {
	return fmt.Sprintf(`
resource "espalier_container" "test" {
  name            = "espalier-test-vm"
  image           = "busybox:1.37"
  command         = ["sleep", "3600"]
  kernel          = %q
  kernel_args     = [%q]
  init_image      = %q
  runtime         = "container-runtime-linux"
  virtualization  = true
  read_only_paths = ["/opt"]
  masked_paths    = ["NONE", "/srv"]
}

data "espalier_container" "test" {
  name = espalier_container.test.name
}`, kernel, kernelArg, initImage)
}

// TestAccContainerVM runs a container with the runtime's own kernel and
// init image, named explicitly.
func TestAccContainerVM(t *testing.T) {
	addr, data := "espalier_container.test", "data.espalier_container.test"
	kernel, initImage := runtimeKernelAndInit(t)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviders,
		CheckDestroy:             checkContainersGone("espalier-test-vm"),
		Steps: []resource.TestStep{
			{
				Config: vmConfig(kernel, initImage, "quiet"),
				Check:  checkKernelArg("espalier-test-vm", "quiet"),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("state"), knownvalue.StringExact("running")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("kernel"), knownvalue.StringExact(kernel)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("masked_paths"), knownvalue.ListExact([]knownvalue.Check{
						knownvalue.StringExact("NONE"), knownvalue.StringExact("/srv"),
					})),
					// As the runtime reports them: with the defaults, or without after NONE.
					statecheck.ExpectKnownValue(data, tfjsonpath.New("virtualization"), knownvalue.Bool(true)),
					statecheck.ExpectKnownValue(data, tfjsonpath.New("runtime"), knownvalue.StringExact("container-runtime-linux")),
					statecheck.ExpectKnownValue(data, tfjsonpath.New("read_only_paths"), knownvalue.ListPartial(map[int]knownvalue.Check{
						0: knownvalue.StringExact("/proc/bus"),
					})),
					statecheck.ExpectKnownValue(data, tfjsonpath.New("masked_paths"), knownvalue.ListExact([]knownvalue.Check{
						knownvalue.StringExact("/srv"),
					})),
					statecheck.ExpectKnownValue(data, tfjsonpath.New("kernel"), knownvalue.Null()),
				},
			},
			{
				ResourceName:      addr,
				ImportState:       true,
				ImportStateVerify: true,
				// Not reported, or reported with the defaults.
				ImportStateVerifyIgnore: []string{"image", "kernel", "kernel_args", "init_image", "read_only_paths", "masked_paths"},
			},
			{
				Config: vmConfig(kernel, initImage, "loglevel=3"),
				Check:  checkKernelArg("espalier-test-vm", "loglevel=3"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(addr, plancheck.ResourceActionDestroyBeforeCreate),
				}},
			},
		},
	})
}

// runtimeKernelAndInit returns the path of the runtime's default kernel and
// a local vminit image.
func runtimeKernelAndInit(t *testing.T) (string, string) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	kernel, err := filepath.EvalSymlinks(filepath.Join(home, "Library/Application Support/com.apple.container/kernels/default.kernel-arm64"))
	if err != nil {
		t.Skipf("no default kernel: %v", err)
	}
	images, err := testClient.Images(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(images, func(img client.Image) bool {
		return strings.HasPrefix(img.Name, "ghcr.io/apple/containerization/vminit:")
	})
	if i < 0 {
		t.Skip("no local vminit image")
	}
	return kernel, images[i].Name
}

// checkKernelArg checks the container's kernel command line.
func checkKernelArg(name, arg string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		out, err := testClient.(client.CLI).Run(context.Background(), "exec", name, "cat", "/proc/cmdline")
		if err != nil {
			return err
		}
		if !slices.Contains(strings.Fields(string(out)), arg) {
			return fmt.Errorf("kernel command line %q lacks %q", out, arg)
		}
		return nil
	}
}

// With a terminal, sh keeps running instead of exiting at once.
func TestAccContainerTTY(t *testing.T) {
	addr := "espalier_container.test"
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviders,
		CheckDestroy:             checkContainersGone("espalier-test-tty"),
		Steps: []resource.TestStep{
			{
				Config: `
resource "espalier_container" "test" {
  name    = "espalier-test-tty"
  image   = "busybox:1.37"
  command = ["sh"]
  tty     = true
}`,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("tty"), knownvalue.Bool(true)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("state"), knownvalue.StringExact("running")),
				},
			},
			{ResourceName: addr, ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"image"}},
		},
	})
}

func TestAccContainerOptionsInvalid(t *testing.T) {
	var steps []resource.TestStep
	for extra, msg := range map[string]string{
		`no_dns = true
  dns = { domain = "x.test" }`: "either no_dns or dns",
		`ulimits = [{ name = "nofile", soft = 2, hard = 1 }]`:                          "exceeds its hard",
		`ulimits = [{ name = "RLIMIT_NOFILE", soft = 1, hard = 1 }]`:                   "limit name in lower",
		`publish_sockets = [{ host_path = "s.sock", container_path = "/run/s.sock" }]`: "absolute path",
		`scheme = "ftp"`:                     "value must be one of",
		`shm_size = "64MB"`:                  "number of bytes",
		`kernel = "vmlinux"`:                 "absolute path",
		`masked_paths = ["proc/kcore"]`:      "absolute path or NONE",
		`read_only_paths = ["/opt", "none"]`: "absolute path or NONE",
	} {
		steps = append(steps, resource.TestStep{
			Config: fmt.Sprintf(`
resource "espalier_container" "test" {
  name  = "espalier-test-invalid"
  image = "busybox:1.37"
  %s
}`, extra),
			PlanOnly:    true,
			ExpectError: regexp.MustCompile(msg),
		})
	}
	resource.Test(t, resource.TestCase{ProtoV6ProviderFactories: testAccProviders, Steps: steps})
}
