package provider

import (
	"context"
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

// The tests run busybox:1.37 and leave it in place: the runtime pulls it on
// first use.
func containerConfig(image, memory, foo string, running bool) string {
	return fmt.Sprintf(`
resource "espalier_container" "test" {
  name    = "espalier-test-ctr"
  image   = %q
  command = ["sleep", "3600"]
  env     = { FOO = %q }
  workdir = "/tmp"
  cpus    = 1
  memory  = %q
  labels  = { "espalier.test" = "yes" }
  running = %t
}`, image, foo, memory, running)
}

func TestAccContainer(t *testing.T) {
	addr := "espalier_container.test"
	expect := func(action plancheck.ResourceActionType) resource.ConfigPlanChecks {
		return resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, action)}}
	}
	ipv4 := knownvalue.StringRegexp(regexp.MustCompile(`^[0-9.]+/[0-9]+$`))
	running := []statecheck.StateCheck{
		statecheck.ExpectKnownValue(addr, tfjsonpath.New("state"), knownvalue.StringExact("running")),
		statecheck.ExpectKnownValue(addr, tfjsonpath.New("addresses").AtSliceIndex(0).AtMapKey("network"), knownvalue.StringExact("default")),
		statecheck.ExpectKnownValue(addr, tfjsonpath.New("addresses").AtSliceIndex(0).AtMapKey("ipv4_address"), ipv4),
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviders,
		CheckDestroy:             checkContainersGone("espalier-test-ctr"),
		Steps: []resource.TestStep{
			{
				Config: containerConfig("busybox:1.37", "256M", "bar", true),
				ConfigStateChecks: append(running,
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("id"), knownvalue.StringExact("espalier-test-ctr")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("image"), knownvalue.StringExact("busybox:1.37")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("memory"), knownvalue.StringExact("256M")),
					// Only what the configuration adds, not the image's PATH.
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("env"), knownvalue.MapExact(map[string]knownvalue.Check{
						"FOO": knownvalue.StringExact("bar"),
					})),
				),
			},
			{
				ResourceName:            addr,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"image"}, // imported fully qualified
			},
			// Stopped and started in place.
			{
				Config:           containerConfig("busybox:1.37", "256M", "bar", false),
				ConfigPlanChecks: expect(plancheck.ResourceActionUpdate),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("state"), knownvalue.StringExact("stopped")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("addresses"), knownvalue.ListExact(nil)),
				},
			},
			{
				Config:            containerConfig("busybox:1.37", "256M", "bar", true),
				ConfigPlanChecks:  expect(plancheck.ResourceActionUpdate),
				ConfigStateChecks: running,
			},
			// Stopped outside Terraform: started again.
			{
				PreConfig: func() {
					if err := testClient.StopContainer(context.Background(), "espalier-test-ctr"); err != nil {
						t.Fatal(err)
					}
				},
				Config:           containerConfig("busybox:1.37", "256M", "bar", true),
				ConfigPlanChecks: expect(plancheck.ResourceActionUpdate),
			},
			// The same image and memory, written differently: no replacement.
			{
				Config:           containerConfig("docker.io/library/busybox:1.37", "262144K", "bar", true),
				ConfigPlanChecks: expect(plancheck.ResourceActionUpdate),
			},
			// Deleted outside Terraform: created again.
			{
				PreConfig: func() {
					if err := testClient.DeleteContainer(context.Background(), "espalier-test-ctr"); err != nil {
						t.Fatal(err)
					}
				},
				Config:           containerConfig("busybox:1.37", "256M", "bar", true),
				ConfigPlanChecks: expect(plancheck.ResourceActionCreate),
			},
			// Anything else replaces the container.
			{
				Config:           containerConfig("busybox:1.37", "256M", "baz", true),
				ConfigPlanChecks: expect(plancheck.ResourceActionDestroyBeforeCreate),
			},
			{
				Config:           containerConfig("busybox:1.37", "512M", "baz", true),
				ConfigPlanChecks: expect(plancheck.ResourceActionDestroyBeforeCreate),
			},
		},
	})
}

// Unset arguments take the image's and runtime's defaults.
func TestAccContainerDefaults(t *testing.T) {
	addr := "espalier_container.test"
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviders,
		CheckDestroy:             checkContainersGone("espalier-test-defaults"),
		Steps: []resource.TestStep{{
			// busybox's default shell exits at once without a terminal.
			Config: `
resource "espalier_container" "test" {
  name    = "espalier-test-defaults"
  image   = "busybox:1.37"
  running = false
}`,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("command"), knownvalue.ListExact([]knownvalue.Check{knownvalue.StringExact("sh")})),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("workdir"), knownvalue.StringExact("/")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("cpus"), knownvalue.NotNull()),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("memory"), knownvalue.NotNull()),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("env"), knownvalue.Null()),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("labels"), knownvalue.Null()),
			},
		}},
	})
}

func checkContainersGone(names ...string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		for _, name := range names {
			c, err := testClient.Container(context.Background(), name)
			if err != nil {
				return err
			}
			if c != nil {
				return fmt.Errorf("container %s still exists", name)
			}
		}
		return nil
	}
}

// An image with an entrypoint, which command passes arguments to.
const entrypointImage = "registry.k8s.io/pause:3.10"

func entrypointConfig(extra string) string {
	return fmt.Sprintf(`
resource "espalier_container" "test" {
  name    = "espalier-test-entrypoint"
  image   = %q
  running = false
  %s
}`, entrypointImage, extra)
}

func TestAccContainerEntrypoint(t *testing.T) {
	addr := "espalier_container.test"
	pulled := newImages(t, entrypointImage)
	list := func(values ...string) knownvalue.Check {
		checks := make([]knownvalue.Check, len(values))
		for i, v := range values {
			checks[i] = knownvalue.StringExact(v)
		}
		return knownvalue.ListExact(checks)
	}
	process := func(entrypoint, command knownvalue.Check) []statecheck.StateCheck {
		return []statecheck.StateCheck{
			statecheck.ExpectKnownValue(addr, tfjsonpath.New("entrypoint"), entrypoint),
			statecheck.ExpectKnownValue(addr, tfjsonpath.New("command"), command),
		}
	}
	replaces := resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
		plancheck.ExpectResourceAction(addr, plancheck.ResourceActionDestroyBeforeCreate),
	}}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviders,
		CheckDestroy: func(s *terraform.State) error {
			if err := checkContainersGone("espalier-test-entrypoint")(s); err != nil {
				return err
			}
			return deleteImages(pulled)
		},
		Steps: []resource.TestStep{
			// The image's entrypoint, which has no command.
			{Config: entrypointConfig(""), ConfigStateChecks: process(list("/pause"), knownvalue.Null())},
			// Arguments go to the image's entrypoint.
			{
				Config:            entrypointConfig(`command = ["-v"]`),
				ConfigPlanChecks:  replaces,
				ConfigStateChecks: process(list("/pause"), list("-v")),
			},
			{ResourceName: addr, ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"image"}},
			// A replaced entrypoint is kept as written.
			{
				Config: entrypointConfig(`entrypoint = ["/bin/sh", "-c"]
  command    = ["echo hi"]`),
				ConfigPlanChecks:  replaces,
				ConfigStateChecks: process(list("/bin/sh", "-c"), list("echo hi")),
			},
		},
	})
}
