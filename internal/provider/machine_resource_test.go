package provider

import (
	"context"
	"fmt"
	"os/user"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

const testMachine = "espalier-test-machine"

func machineConfig(image string, cpus int, memory, account string, running bool) string {
	acct := ""
	if account != "" {
		acct = fmt.Sprintf("user = %q", account)
	}
	return fmt.Sprintf(`
resource "espalier_machine" "test" {
  name       = %q
  image      = %q
  home_mount = "none"
  cpus       = %d
  memory     = %q
  running    = %t
  %s
}`, testMachine, image, cpus, memory, running, acct)
}

func TestAccMachine(t *testing.T) {
	addr := "espalier_machine.test"
	me, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	expect := func(action plancheck.ResourceActionType) resource.ConfigPlanChecks {
		return resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, action)}}
	}
	status := func(s string) []statecheck.StateCheck {
		return []statecheck.StateCheck{statecheck.ExpectKnownValue(addr, tfjsonpath.New("status"), knownvalue.StringExact(s))}
	}

	pulled := newImages(t, "alpine:3.22")
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviders,
		CheckDestroy: func(*terraform.State) error {
			m, err := testClient.Machine(context.Background(), testMachine)
			if err != nil {
				return err
			}
			if m != nil {
				return fmt.Errorf("machine %s still exists", testMachine)
			}
			return deleteImages(pulled)
		},
		Steps: []resource.TestStep{
			{
				Config: machineConfig("alpine:3.22", 1, "1G", "", true) + `
data "espalier_machine" "test" {
  name = espalier_machine.test.name
}

data "espalier_machines" "all" {
  depends_on = [espalier_machine.test]
}`,
				ConfigStateChecks: append(status("running"),
					statecheck.ExpectKnownValue("data.espalier_machine.test", tfjsonpath.New("image"), knownvalue.StringExact("docker.io/library/alpine:3.22")),
					statecheck.ExpectKnownValue("data.espalier_machine.test", tfjsonpath.New("memory"), knownvalue.StringExact("1G")),
					statecheck.ExpectKnownValue("data.espalier_machine.test", tfjsonpath.New("kernel"), knownvalue.Null()),
					statecheck.ExpectKnownValue("data.espalier_machines.all", tfjsonpath.New("machines"), knownvalue.ListPartial(map[int]knownvalue.Check{
						0: knownvalue.ObjectPartial(map[string]knownvalue.Check{"name": knownvalue.StringExact(testMachine)}),
					})),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("cpus"), knownvalue.Int64Exact(1)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("home_mount"), knownvalue.StringExact("none")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("user"), knownvalue.StringExact(me.Username)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("home"), knownvalue.StringExact("/home/"+me.Username)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("platform"), knownvalue.StringExact("linux/arm64")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("ip_address"), knownvalue.NotNull()),
				),
			},
			{
				ResourceName:            addr,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"image"}, // imported fully qualified
			},
			// Settings change in place.
			{
				Config:            machineConfig("alpine:3.22", 2, "1G", "", true),
				ConfigPlanChecks:  expect(plancheck.ResourceActionUpdate),
				ConfigStateChecks: append(status("running"), statecheck.ExpectKnownValue(addr, tfjsonpath.New("cpus"), knownvalue.Int64Exact(2))),
			},
			// The same image and memory, written differently: no replacement.
			{
				Config:           machineConfig("docker.io/library/alpine:3.22", 2, "1024M", "", true),
				ConfigPlanChecks: expect(plancheck.ResourceActionUpdate),
			},
			// Stopped and started in place.
			{
				Config:            machineConfig("alpine:3.22", 2, "1G", "", false),
				ConfigPlanChecks:  expect(plancheck.ResourceActionUpdate),
				ConfigStateChecks: append(status("stopped"), statecheck.ExpectKnownValue(addr, tfjsonpath.New("ip_address"), knownvalue.Null())),
			},
			{
				Config:            machineConfig("alpine:3.22", 2, "1G", "", true),
				ConfigPlanChecks:  expect(plancheck.ResourceActionUpdate),
				ConfigStateChecks: status("running"),
			},
			// Stopped outside Terraform: started again.
			{
				PreConfig: func() {
					if err := testClient.StopMachine(context.Background(), testMachine); err != nil {
						t.Fatal(err)
					}
				},
				Config:            machineConfig("alpine:3.22", 2, "1G", "", true),
				ConfigPlanChecks:  expect(plancheck.ResourceActionUpdate),
				ConfigStateChecks: status("running"),
			},
			// Deleted outside Terraform: created again.
			{
				PreConfig: func() {
					if err := testClient.DeleteMachine(context.Background(), testMachine); err != nil {
						t.Fatal(err)
					}
				},
				Config:           machineConfig("alpine:3.22", 2, "1G", "", true),
				ConfigPlanChecks: expect(plancheck.ResourceActionCreate),
			},
			// The account replaces the machine.
			{
				Config:           machineConfig("alpine:3.22", 2, "1G", "espalier", true),
				ConfigPlanChecks: expect(plancheck.ResourceActionDestroyBeforeCreate),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("user"), knownvalue.StringExact("espalier")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("home"), knownvalue.StringExact("/home/espalier")),
				},
			},
		},
	})
}

func TestAccMachineInvalid(t *testing.T) {
	var steps []resource.TestStep
	for extra, msg := range map[string]string{
		`memory = "512M"`:    "at least 1G",
		`cpus = 0`:           "at least 1",
		`default = false`:    `must be "true"`,
		`home_mount = "yes"`: "value must be one of",
		`kernel = "vmlinux"`: "absolute path",
	} {
		steps = append(steps, resource.TestStep{
			Config: fmt.Sprintf(`
resource "espalier_machine" "test" {
  name  = "espalier-test-invalid"
  image = "alpine:3.22"
  %s
}`, extra),
			PlanOnly:    true,
			ExpectError: regexp.MustCompile(msg),
		})
	}
	resource.Test(t, resource.TestCase{ProtoV6ProviderFactories: testAccProviders, Steps: steps})
}

// Created stopped, with its settings: it never boots.
func TestAccMachineCreatedStopped(t *testing.T) {
	addr := "espalier_machine.test"
	pulled := newImages(t, "alpine:3.22")
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviders,
		CheckDestroy: func(*terraform.State) error {
			if m, err := testClient.Machine(context.Background(), testMachine); err != nil || m != nil {
				return fmt.Errorf("machine %s still exists (%v)", testMachine, err)
			}
			return deleteImages(pulled)
		},
		Steps: []resource.TestStep{{
			Config: machineConfig("alpine:3.22", 1, "1G", "", false),
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("status"), knownvalue.StringExact("stopped")),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("cpus"), knownvalue.Int64Exact(1)),
				statecheck.ExpectKnownValue(addr, tfjsonpath.New("home_mount"), knownvalue.StringExact("none")),
			},
		}},
	})
}

func defaultMachinesConfig(cpus int, makeDefault bool) string {
	def := ""
	if makeDefault {
		def = "default = true"
	}
	return fmt.Sprintf(`
resource "espalier_machine" "first" {
  name       = "%[1]s-1"
  image      = "alpine:3.22"
  home_mount = "none"
  running    = false
}

resource "espalier_machine" "second" {
  name       = "%[1]s-2"
  image      = "alpine:3.22"
  home_mount = "none"
  cpus       = %[2]d
  memory     = "1G"
  running    = false
  %[3]s

  depends_on = [espalier_machine.first]
}

data "espalier_machines" "all" {
  depends_on = [espalier_machine.first, espalier_machine.second]
}`, testMachine, cpus, def)
}

// Becoming the default machine, and a settings change of a stopped machine:
// both in place, and the machine stays stopped.
func TestAccMachineDefaultAndStoppedSettings(t *testing.T) {
	requireNoMachines(t)
	second := "espalier_machine.second"
	pulled := newImages(t, "alpine:3.22")
	isDefault := func(i int, want bool) statecheck.StateCheck {
		return statecheck.ExpectKnownValue("data.espalier_machines.all", tfjsonpath.New("machines").AtSliceIndex(i).AtMapKey("default"), knownvalue.Bool(want))
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviders,
		CheckDestroy: func(*terraform.State) error {
			if ms, err := testClient.Machines(context.Background()); err != nil || len(ms) != 0 {
				return fmt.Errorf("machines left: %d (%v)", len(ms), err)
			}
			return deleteImages(pulled)
		},
		Steps: []resource.TestStep{
			// The first machine on a host becomes its default.
			{
				Config:            defaultMachinesConfig(1, false),
				ConfigStateChecks: []statecheck.StateCheck{isDefault(0, true), isDefault(1, false)},
			},
			{
				Config: defaultMachinesConfig(2, true),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(second, plancheck.ResourceActionUpdate),
				}},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(second, tfjsonpath.New("default"), knownvalue.Bool(true)),
					statecheck.ExpectKnownValue(second, tfjsonpath.New("cpus"), knownvalue.Int64Exact(2)),
					statecheck.ExpectKnownValue(second, tfjsonpath.New("status"), knownvalue.StringExact("stopped")),
				},
			},
			// The data source read before the change was applied; read again.
			{
				Config:            defaultMachinesConfig(2, true),
				ConfigStateChecks: []statecheck.StateCheck{isDefault(0, false), isDefault(1, true)},
			},
		},
	})
}
