package provider

import (
	"context"
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/compare"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"

	"github.com/thorstenpfister/terraform-provider-espalier/internal/client"
)

const tagTarget = "espalier-test/tagged:1"

func TestAccImageTag(t *testing.T) {
	addr := "espalier_image_tag.test"
	config := fmt.Sprintf(`
resource "espalier_image_tag" "test" {
  source = "busybox:1.37"
  target = %q
}

data "espalier_image" "source" {
  name = "busybox:1.37"
}`, tagTarget)
	creates := resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionCreate)}}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviders,
		CheckDestroy: func(s *terraform.State) error {
			if err := checkImagesGone(tagTarget, "busybox:1.35")(s); err != nil {
				return err
			}
			if img, err := testClient.Image(context.Background(), "busybox:1.37"); err != nil || img == nil {
				return fmt.Errorf("source image gone with its tag (%v)", err)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: config,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("id"), knownvalue.StringExact("docker.io/"+tagTarget)),
					statecheck.CompareValuePairs(addr, tfjsonpath.New("digest"), "data.espalier_image.source", tfjsonpath.New("digest"), compare.ValuesSame()),
				},
			},
			{
				ResourceName:      addr,
				ImportState:       true,
				ImportStateId:     "busybox:1.37," + tagTarget,
				ImportStateVerify: true,
			},
			// Removed outside Terraform: tagged again.
			{
				PreConfig: func() {
					if err := testClient.DeleteImage(context.Background(), "docker.io/"+tagTarget); err != nil {
						t.Fatal(err)
					}
				},
				Config:           config,
				ConfigPlanChecks: creates,
			},
			// Pointed at another image outside Terraform: tagged again.
			{
				PreConfig: func() {
					ctx := context.Background()
					if err := testClient.PullImage(ctx, client.ImageSpec{Reference: "busybox:1.35", Platform: "linux/arm64"}); err != nil {
						t.Fatal(err)
					}
					if err := testClient.TagImage(ctx, "busybox:1.35", tagTarget); err != nil {
						t.Fatal(err)
					}
					if err := testClient.DeleteImage(ctx, "docker.io/library/busybox:1.35"); err != nil {
						t.Fatal(err)
					}
				},
				Config:           config,
				ConfigPlanChecks: creates,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.CompareValuePairs(addr, tfjsonpath.New("digest"), "data.espalier_image.source", tfjsonpath.New("digest"), compare.ValuesSame()),
				},
			},
		},
	})
}

func TestAccImageTagSelf(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviders,
		Steps: []resource.TestStep{{
			Config: `
resource "espalier_image_tag" "test" {
  source = "busybox:1.37"
  target = "docker.io/library/busybox:1.37"
}`,
			ExpectError: regexp.MustCompile(`names the source image itself`),
		}},
	})
}
