package provider

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

// Needs a running runtime (`container system start`).
func TestAccSystemDataSource(t *testing.T) {
	version := knownvalue.StringRegexp(regexp.MustCompile(`^\d+\.\d+\.\d+`))
	size := knownvalue.StringRegexp(regexp.MustCompile(`^\d+[KMGTP]?$`))
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviders,
		Steps: []resource.TestStep{{
			Config: `data "espalier_system" "this" {}`,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue("data.espalier_system.this", tfjsonpath.New("running"), knownvalue.Bool(true)),
				statecheck.ExpectKnownValue("data.espalier_system.this", tfjsonpath.New("status"), knownvalue.StringExact("running")),
				statecheck.ExpectKnownValue("data.espalier_system.this", tfjsonpath.New("client_version"), version),
				statecheck.ExpectKnownValue("data.espalier_system.this", tfjsonpath.New("server_version"), version),
				statecheck.ExpectKnownValue("data.espalier_system.this", tfjsonpath.New("kernel_url"), knownvalue.StringRegexp(regexp.MustCompile(`^https://`))),
				statecheck.ExpectKnownValue("data.espalier_system.this", tfjsonpath.New("registry_domain"), knownvalue.NotNull()),
				statecheck.ExpectKnownValue("data.espalier_system.this", tfjsonpath.New("vminit_image"), knownvalue.StringRegexp(regexp.MustCompile(`vminit`))),
				statecheck.ExpectKnownValue("data.espalier_system.this", tfjsonpath.New("container_defaults").AtMapKey("memory"), size),
				statecheck.ExpectKnownValue("data.espalier_system.this", tfjsonpath.New("builder_defaults").AtMapKey("image"), knownvalue.StringRegexp(regexp.MustCompile(`builder`))),
				statecheck.ExpectKnownValue("data.espalier_system.this", tfjsonpath.New("machine_defaults").AtMapKey("memory"), size),
			},
		}},
	})
}

func TestAccSystemDataSourceMissingCLI(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviders,
		Steps: []resource.TestStep{{
			Config: `
provider "espalier" {
  cli_path = "/nonexistent/container"
}
data "espalier_system" "this" {}
`,
			ExpectError: regexp.MustCompile(`Reading the container runtime failed`),
		}},
	})
}
