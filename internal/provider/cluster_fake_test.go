package provider

import (
	"context"
	"encoding/base64"
	"fmt"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"

	"github.com/thorstenpfister/terraform-provider-espalier/internal/client"
)

// fakeClusterClient keeps clusters in memory, as the runtime reports them;
// the rest of client.Client is not used.
type fakeClusterClient struct {
	client.Client
	mu       sync.Mutex
	clusters map[string]*client.Cluster
	created  int
}

func (f *fakeClusterClient) Version(context.Context) (string, error) {
	return testedCLIVersion, nil
}

func (f *fakeClusterClient) CreateCluster(_ context.Context, spec client.ClusterSpec) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.clusters[spec.Name]; ok {
		return fmt.Errorf("cluster %s already exists", spec.Name)
	}
	cl := &client.Cluster{Name: spec.Name, KubernetesVersion: "v1.35.5", State: "running", CNI: spec.CNI}
	node := func(name, role string) client.ClusterNode {
		return client.ClusterNode{Name: name, Role: role, State: "running", Address: "192.168.64.2", Image: "docker.io/kindest/node@sha256:ce97", CPUs: 2, MemoryBytes: 2 << 30}
	}
	cl.Nodes = append(cl.Nodes, node(spec.Name, "control-plane"))
	for _, w := range spec.Workers {
		for range w.Count {
			cl.Nodes = append(cl.Nodes, node(fmt.Sprintf("%s-worker-%d", spec.Name, len(cl.Nodes)), "worker"))
		}
	}
	f.clusters[spec.Name] = cl
	f.created++
	return nil
}

func (f *fakeClusterClient) Cluster(_ context.Context, name string) (*client.Cluster, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cl, ok := f.clusters[name]
	if !ok {
		return nil, nil
	}
	copied := *cl
	copied.Nodes = append([]client.ClusterNode(nil), cl.Nodes...)
	return &copied, nil
}

func (f *fakeClusterClient) ClusterKubeconfig(_ context.Context, name string) ([]byte, error) {
	data := base64.StdEncoding.EncodeToString([]byte("PEM of " + name))
	return fmt.Appendf(nil, `apiVersion: v1
kind: Config
clusters:
- name: %[1]s
  cluster:
    server: https://127.0.0.1:6445
    certificate-authority-data: %[2]s
users:
- name: %[1]s
  user:
    client-certificate-data: %[2]s
    client-key-data: %[2]s
`, name, data), nil
}

func (f *fakeClusterClient) DeleteCluster(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.clusters, name)
	return nil
}

// change edits a cluster the way the runtime would report it after
// something happened to it.
func (f *fakeClusterClient) change(name string, fn func(*client.Cluster)) func() {
	return func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		fn(f.clusters[name])
	}
}

// TestClusterDegradedIsReplaced plans and applies against a fake runtime:
// a cluster with a stopped or missing node is replaced, a healthy one is
// left alone.
func TestClusterDegradedIsReplaced(t *testing.T) {
	fake := &fakeClusterClient{clusters: map[string]*client.Cluster{}}
	factories := map[string]func() (tfprotov6.ProviderServer, error){
		"espalier": providerserver.NewProtocol6WithError(&espalierProvider{version: "test", client: fake}),
	}
	addr := "espalier_cluster.test"
	config := `
resource "espalier_cluster" "test" {
  name    = "fake"
  cni     = "NONE"
  workers = [{ count = 1 }]
}`
	replaced := resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
		plancheck.ExpectResourceAction(addr, plancheck.ResourceActionDestroyBeforeCreate),
	}}
	running := []statecheck.StateCheck{statecheck.ExpectKnownValue(addr, tfjsonpath.New("state"), knownvalue.StringExact("running"))}
	stopWorker := fake.change("fake", func(cl *client.Cluster) {
		cl.State = "degraded" // as the runtime reports it
		cl.Nodes[1].State = "stopped"
	})
	dropWorker := fake.change("fake", func(cl *client.Cluster) { cl.Nodes = cl.Nodes[:1] })

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: config, ConfigStateChecks: running},
			{PreConfig: stopWorker, Config: config, ConfigPlanChecks: replaced, ConfigStateChecks: running},
			{PreConfig: dropWorker, Config: config, ConfigPlanChecks: replaced, ConfigStateChecks: running},
			// Healthy: nothing to do.
			{Config: config, ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}},
		},
	})
	if fake.created != 3 {
		t.Errorf("created %d clusters, want 3 (the first and two replacements)", fake.created)
	}
}

// No workers, written as an empty list or left out, is the same cluster.
func TestClusterEmptyWorkers(t *testing.T) {
	fake := &fakeClusterClient{clusters: map[string]*client.Cluster{}}
	factories := map[string]func() (tfprotov6.ProviderServer, error){
		"espalier": providerserver.NewProtocol6WithError(&espalierProvider{version: "test", client: fake}),
	}
	config := func(workers string) string {
		return fmt.Sprintf("resource \"espalier_cluster\" \"test\" {\n  name = \"fake\"\n  %s\n}", workers)
	}
	inPlace := resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
		plancheck.ExpectResourceAction("espalier_cluster.test", plancheck.ResourceActionUpdate),
	}}
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: config("workers = []")},
			{Config: config(""), ConfigPlanChecks: inPlace},
			{Config: config("workers = []"), ConfigPlanChecks: inPlace},
		},
	})
	if fake.created != 1 {
		t.Errorf("created %d clusters, want 1", fake.created)
	}
}

// Sizes taken from other resources are unknown while planning: creating
// works, and a change of them, unknown until applied, replaces the cluster.
func TestClusterUnknownValues(t *testing.T) {
	fake := &fakeClusterClient{clusters: map[string]*client.Cluster{}}
	factories := map[string]func() (tfprotov6.ProviderServer, error){
		"espalier": providerserver.NewProtocol6WithError(&espalierProvider{version: "test", client: fake}),
	}
	config := func(memory string) string {
		return fmt.Sprintf(`
resource "terraform_data" "v" {
  input = { name = "fake", cpus = 2, workers = 1 }
}

resource "terraform_data" "memory" {
  input = %q
}

resource "espalier_cluster" "test" {
  name    = terraform_data.v.output.name
  cpus    = terraform_data.v.output.cpus
  memory  = terraform_data.memory.output
  workers = [{ count = terraform_data.v.output.workers, cpus = 2, memory = terraform_data.memory.output }]
  cni     = "NONE"
}`, memory)
	}
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: config("2G")},
			{
				Config: config("2048M"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("espalier_cluster.test", plancheck.ResourceActionDestroyBeforeCreate),
				}},
			},
		},
	})
}
