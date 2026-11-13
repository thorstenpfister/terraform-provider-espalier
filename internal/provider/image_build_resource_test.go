package provider

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/compare"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

const (
	builtTag      = "espalier-test/built:1"
	builtTagOther = "espalier-test/built:2"
)

const buildDockerfile = `FROM docker.io/library/busybox:1.37
ARG GREETING=hello
COPY file.txt /file.txt
RUN --mount=type=secret,id=tok test -s /run/secrets/tok && echo "$GREETING" > /greeting
`

func TestAccImageBuild(t *testing.T) {
	dir := buildContext(t, buildDockerfile)
	t.Setenv("ESPALIER_TEST_TOKEN", "secret")
	addr := "espalier_image_build.test"
	config := func(provider, extra, tag string) string {
		return fmt.Sprintf(`
provider "espalier" {
  %s
}

resource "espalier_image_build" "test" {
  context    = %q
  tags       = [%q]
  build_args = { GREETING = "hi" }
  secrets    = { tok = { env = "ESPALIER_TEST_TOKEN" } }
  %s
}

data "espalier_image" "built" {
  name       = espalier_image_build.test.id
  depends_on = [espalier_image_build.test]
}`, provider, dir, tag, extra)
	}
	rebuilds := resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate)}}
	digests := statecheck.CompareValue(compare.ValuesDiffer())

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviders,
		PreCheck:                 func() { stopBuilder(t) },
		CheckDestroy: func(s *terraform.State) error {
			stopBuilder(t)
			return checkImagesGone(builtTag, builtTagOther)(s)
		},
		Steps: []resource.TestStep{
			{
				Config: config("", "", builtTag),
				Check:  checkBuilderRunning(false),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("id"), knownvalue.StringExact("docker.io/"+builtTag)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("context_hash"), knownvalue.StringRegexp(regexp.MustCompile(`^sha256:[0-9a-f]{64}$`))),
					statecheck.CompareValuePairs(addr, tfjsonpath.New("digest"), "data.espalier_image.built", tfjsonpath.New("digest"), compare.ValuesSame()),
					digests.AddStateValue(addr, tfjsonpath.New("digest")),
				},
			},
			// An ignored file changes nothing.
			{
				PreConfig: func() { writeFile(t, dir, "ignored.txt", "changed") },
				Config:    config("", "", builtTag),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			// A file the build reads rebuilds it, in place.
			{
				PreConfig:        func() { writeFile(t, dir, "file.txt", "changed") },
				Config:           config("", "", builtTag),
				ConfigPlanChecks: rebuilds,
				ConfigStateChecks: []statecheck.StateCheck{
					digests.AddStateValue(addr, tfjsonpath.New("digest")),
				},
			},
			{
				Config:           config("", `triggers = { rebuild = "1" }`, builtTag),
				ConfigPlanChecks: rebuilds,
			},
			// Removed outside Terraform: built again.
			{
				PreConfig: func() {
					if err := testClient.DeleteImage(context.Background(), "docker.io/"+builtTag); err != nil {
						t.Fatal(err)
					}
				},
				Config:           config("", `triggers = { rebuild = "1" }`, builtTag),
				ConfigPlanChecks: rebuilds,
			},
			// Only how it is kept: no rebuild.
			{
				Config: config("", `triggers = { rebuild = "1" }
  keep_locally = false
  no_cache     = true`, builtTag),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate),
						plancheck.ExpectKnownValue(addr, tfjsonpath.New("digest"), knownvalue.NotNull()),
					},
				},
			},
			// A dropped tag is removed; the builder is kept running if asked.
			{
				Config:           config("builder = { keep_running = true }", "", builtTagOther),
				ConfigPlanChecks: rebuilds,
				Check:            resource.ComposeTestCheckFunc(checkImagesGone(builtTag), checkBuilderRunning(true)),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("id"), knownvalue.StringExact("docker.io/"+builtTagOther)),
				},
			},
		},
	})
}

func TestAccImageBuildFails(t *testing.T) {
	dir := buildContext(t, "FROM docker.io/library/busybox:1.37\nRUN exit 3\n")
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviders,
		PreCheck:                 func() { stopBuilder(t) },
		// The failed build stopped the builder again.
		CheckDestroy: resource.ComposeTestCheckFunc(checkImagesGone(builtTag), checkBuilderRunning(false)),
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "espalier_image_build" "test" {
  context = %q
  tags    = [%q]
}`, dir, builtTag),
				ExpectError: regexp.MustCompile(`exit code: 3`),
			},
		},
	})
}

func TestAccImageBuildSecretNeedsOneSource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviders,
		Steps: []resource.TestStep{{
			Config: `
resource "espalier_image_build" "test" {
  context = "/nonexistent"
  tags    = ["espalier-test/never:1"]
  secrets = { tok = { env = "A", file = "/b" } }
}`,
			ExpectError: regexp.MustCompile(`Invalid Attribute Combination`),
		}},
	})
}

// buildContext is a build context with the given Dockerfile, a file.txt and
// an ignored.txt that .dockerignore excludes.
func buildContext(t *testing.T, dockerfile string) string {
	dir := t.TempDir()
	writeFile(t, dir, "Dockerfile", dockerfile)
	writeFile(t, dir, "file.txt", "content")
	writeFile(t, dir, "ignored.txt", "noise")
	writeFile(t, dir, ".dockerignore", "ignored.txt\n")
	return dir
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func stopBuilder(t *testing.T) {
	ctx := context.Background()
	if running, err := testClient.BuilderRunning(ctx); err != nil {
		t.Fatal(err)
	} else if running {
		if err := testClient.StopBuilder(ctx); err != nil {
			t.Fatal(err)
		}
	}
}

func checkBuilderRunning(want bool) resource.TestCheckFunc {
	return func(*terraform.State) error {
		running, err := testClient.BuilderRunning(context.Background())
		if err != nil {
			return err
		}
		if running != want {
			return fmt.Errorf("builder running = %v, want %v", running, want)
		}
		return nil
	}
}
