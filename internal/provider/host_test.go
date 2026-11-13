package provider

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/thorstenpfister/terraform-provider-espalier/internal/client"
)

// The acceptance tests run against the host's runtime. These helpers keep
// them from changing what was there before: images, the builder, the
// default machine.

// testedCLIVersion is the container version the provider was last verified
// against; see "Upgrading the runtime" in the README.
const testedCLIVersion = "1.7.1"

// skipUnlessAcc skips outside acceptance runs, which have no runtime to
// talk to; resource.Test does the same.
func skipUnlessAcc(t *testing.T) {
	t.Helper()
	if os.Getenv("TF_ACC") == "" {
		t.Skip("acceptance test: set TF_ACC=1")
	}
}

// requireImagesAbsent fails the test if an image it deletes is already on
// the host, rather than delete it.
func requireImagesAbsent(t *testing.T, refs ...string) {
	t.Helper()
	skipUnlessAcc(t)
	for _, ref := range refs {
		img, err := testClient.Image(context.Background(), ref)
		if err != nil {
			t.Fatal(err)
		}
		if img != nil {
			t.Fatalf("%s is on this host and the test would delete it; remove it first", img.Name)
		}
	}
}

// newImages returns which of refs are not on the host yet, so a test can
// delete only the images it pulled.
func newImages(t *testing.T, refs ...string) []string {
	t.Helper()
	skipUnlessAcc(t)
	var missing []string
	for _, ref := range refs {
		img, err := testClient.Image(context.Background(), ref)
		if err != nil {
			t.Fatal(err)
		}
		if img == nil {
			missing = append(missing, ref)
		}
	}
	return missing
}

func deleteImages(refs []string) error {
	ctx := context.Background()
	for _, ref := range refs {
		img, err := testClient.Image(ctx, ref)
		if err != nil {
			return err
		}
		if img != nil {
			if err := testClient.DeleteImage(ctx, img.Name); err != nil {
				return err
			}
		}
	}
	return nil
}

// stopBuilderUntilCleanup stops the builder for a test, and starts it again
// with its size afterwards if it was running.
func stopBuilderUntilCleanup(t *testing.T) {
	t.Helper()
	skipUnlessAcc(t)
	ctx := context.Background()
	out, err := testClient.(client.CLI).Run(ctx, "builder", "status", "--format", "json")
	if err != nil {
		t.Fatal(err)
	}
	var status []struct {
		Configuration struct {
			Resources struct {
				CPUs          int64 `json:"cpus"`
				MemoryInBytes int64 `json:"memoryInBytes"`
			} `json:"resources"`
		} `json:"configuration"`
		Status struct {
			State string `json:"state"`
		} `json:"status"`
	}
	if err := json.Unmarshal(out, &status); err != nil {
		t.Fatal(err)
	}
	if len(status) != 1 || status[0].Status.State != "running" {
		return
	}
	if err := testClient.StopBuilder(ctx); err != nil {
		t.Fatal(err)
	}
	r := status[0].Configuration.Resources
	t.Cleanup(func() {
		if err := testClient.StartBuilder(ctx, client.BuilderSettings{CPUs: r.CPUs, Memory: formatSize(r.MemoryInBytes)}); err != nil {
			t.Errorf("restarting the builder: %v", err)
		}
	})
}

// requireNoMachines skips a test that would change which machine is the
// default, if the host has machines of its own.
func requireNoMachines(t *testing.T) {
	t.Helper()
	skipUnlessAcc(t)
	machines, err := testClient.Machines(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(machines) > 0 {
		t.Skipf("this host has %d machines; the test would change the default machine", len(machines))
	}
}

// TestAccTestedVersion fails when the runtime is not the version the
// provider was verified against, so an upgrade gets a deliberate check.
func TestAccTestedVersion(t *testing.T) {
	skipUnlessAcc(t)
	sys, err := testClient.System(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if sys.ClientVersion != testedCLIVersion {
		t.Fatalf("container is %s, but the provider was verified against %s: work through \"Upgrading the runtime\" "+
			"in the README, then update testedCLIVersion", sys.ClientVersion, testedCLIVersion)
	}
}
