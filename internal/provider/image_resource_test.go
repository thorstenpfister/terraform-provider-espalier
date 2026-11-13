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

// Small images that nothing else on the host is likely to use, so the
// tests may delete them.
const (
	testImage     = "busybox:1.36"
	testImageFull = "docker.io/library/busybox:1.36"
	keptImage     = "busybox:1.35"
)

func imageConfig(keep bool) string {
	return fmt.Sprintf(`
resource "espalier_image" "test" {
  name         = %q
  platform     = "linux/arm64"
  keep_locally = %t
  scheme       = "https"
}

data "espalier_image" "test" {
  name = espalier_image.test.id
}`, testImage, keep)
}

func TestAccImage(t *testing.T) {
	addr := "espalier_image.test"
	digest := knownvalue.StringRegexp(regexp.MustCompile(`^sha256:[0-9a-f]{64}$`))
	updates := resource.ConfigPlanChecks{
		PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate)},
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviders,
		CheckDestroy:             checkImagesGone(testImage),
		Steps: []resource.TestStep{
			{
				Config: imageConfig(false),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("id"), knownvalue.StringExact(testImageFull)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("name"), knownvalue.StringExact(testImage)),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("digest"), digest),
					statecheck.ExpectKnownValue("data.espalier_image.test", tfjsonpath.New("digest"), digest),
					// Only the platform that was pulled.
					statecheck.ExpectKnownValue("data.espalier_image.test", tfjsonpath.New("platforms"),
						knownvalue.ListExact([]knownvalue.Check{knownvalue.StringExact("linux/arm64/v8")})),
				},
			},
			{
				ResourceName:            addr,
				ImportState:             true,
				ImportStateId:           testImage,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"platform", "scheme"}, // the runtime does not record them
			},
			// Removed outside Terraform: pulled again.
			{
				PreConfig: func() {
					if err := testClient.DeleteImage(context.Background(), testImageFull); err != nil {
						t.Fatal(err)
					}
				},
				Config: imageConfig(false),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(addr, plancheck.ResourceActionCreate)},
				},
			},
			// keep_locally changes in place, without pulling again.
			{Config: imageConfig(true), ConfigPlanChecks: updates},
			{Config: imageConfig(false), ConfigPlanChecks: updates},
		},
	})
}

func TestAccImageKeepLocally(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviders,
		CheckDestroy: func(*terraform.State) error {
			img, err := testClient.Image(context.Background(), keptImage)
			if err != nil {
				return err
			}
			if img == nil {
				return fmt.Errorf("image %s was deleted despite keep_locally", keptImage)
			}
			return testClient.DeleteImage(context.Background(), img.Name)
		},
		Steps: []resource.TestStep{{
			Config: fmt.Sprintf(`
resource "espalier_image" "test" {
  name         = %q
  platform     = "linux/arm64"
  keep_locally = true
}`, keptImage),
		}},
	})
}

func TestAccImageDataSourceMissing(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviders,
		Steps: []resource.TestStep{{
			Config:      `data "espalier_image" "test" { name = "espalier-test/never-pulled:0" }`,
			ExpectError: regexp.MustCompile(`is not present in the container runtime`),
		}},
	})
}

func checkImagesGone(refs ...string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		for _, ref := range refs {
			img, err := testClient.Image(context.Background(), ref)
			if err != nil {
				return err
			}
			if img != nil {
				return fmt.Errorf("image %s still present", img.Name)
			}
		}
		return nil
	}
}
