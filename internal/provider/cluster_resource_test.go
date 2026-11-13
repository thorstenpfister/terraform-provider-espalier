package provider

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"

	"github.com/thorstenpfister/terraform-provider-espalier/internal/client"
)

func TestClusterHealthy(t *testing.T) {
	node := func(name, role, state string) client.ClusterNode {
		return client.ClusterNode{Name: name, Role: role, State: state}
	}
	cp, w1 := node("c", "control-plane", "running"), node("c-worker-1", "worker", "running")
	for _, c := range []struct {
		name    string
		cluster client.Cluster
		workers int64
		want    bool
	}{
		{"running", client.Cluster{State: "running", Nodes: []client.ClusterNode{cp, w1}}, 1, true},
		{"stopped worker", client.Cluster{State: "running", Nodes: []client.ClusterNode{cp, node("c-worker-1", "worker", "stopped")}}, 1, false},
		{"missing worker", client.Cluster{State: "running", Nodes: []client.ClusterNode{cp}}, 1, false},
		{"no control plane", client.Cluster{State: "running", Nodes: []client.ClusterNode{w1}}, 1, false},
		{"cluster stopped", client.Cluster{State: "stopped", Nodes: []client.ClusterNode{cp}}, 0, false},
	} {
		if got := clusterHealthy(&c.cluster, c.workers); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

const testCluster = "espalier-test-cluster"

func clusterConfig(memory string) string {
	return fmt.Sprintf(`
resource "espalier_cluster" "test" {
  name    = %q
  cpus    = 2
  memory  = %q
  cni     = "NONE"
  workers = [{ cpus = 2, memory = "2G" }]
}`, testCluster, memory)
}

func TestAccCluster(t *testing.T) {
	addr := "espalier_cluster.test"
	home, _ := os.UserHomeDir()
	kubeconfigBefore, _ := os.ReadFile(filepath.Join(home, ".kube", "config"))
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviders,
		CheckDestroy: func(s *terraform.State) error {
			if cl, err := testClient.Cluster(context.Background(), testCluster); err != nil || cl != nil {
				return fmt.Errorf("cluster still present (%v)", err)
			}
			if after, _ := os.ReadFile(filepath.Join(home, ".kube", "config")); string(after) != string(kubeconfigBefore) {
				return fmt.Errorf("~/.kube/config changed")
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: clusterConfig("2G"),
				Check:  checkAPIServerReady(addr),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("state"), knownvalue.StringExact("running")),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("api_server"), knownvalue.StringRegexp(regexp.MustCompile(`^https://127\.0\.0\.1:\d+$`))),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("kubernetes_version"), knownvalue.StringRegexp(regexp.MustCompile(`^v1\.`))),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("node_image"), knownvalue.StringRegexp(regexp.MustCompile(`kindest/node`))),
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("nodes"), knownvalue.ListExact([]knownvalue.Check{
						knownvalue.ObjectPartial(map[string]knownvalue.Check{
							"name": knownvalue.StringExact(testCluster), "role": knownvalue.StringExact("control-plane"), "state": knownvalue.StringExact("running"),
						}),
						knownvalue.ObjectPartial(map[string]knownvalue.Check{
							"name": knownvalue.StringExact(testCluster + "-worker-1"), "role": knownvalue.StringExact("worker"),
						}),
					})),
				},
			},
			{
				ResourceName:      addr,
				ImportState:       true,
				ImportStateVerify: true,
				// The import makes the worker sizes explicit; they are the same.
				ImportStateVerifyIgnore: []string{"timeouts"},
			},
			// The same size, written differently: no replacement.
			{
				Config: clusterConfig("2048M"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(addr, plancheck.ResourceActionUpdate),
				}},
			},
		},
	})
}

// A CNI that never gets the nodes ready runs into the create timeout, and
// the half-made cluster is deleted.
func TestAccClusterCreateTimeout(t *testing.T) {
	manifest := filepath.Join(t.TempDir(), "cni.yaml")
	writeFile(t, filepath.Dir(manifest), "cni.yaml", "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: not-a-cni\n  namespace: kube-system\n")
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviders,
		CheckDestroy: func(*terraform.State) error {
			if cl, err := testClient.Cluster(context.Background(), testCluster); err != nil || cl != nil {
				return fmt.Errorf("half-made cluster still present (%v)", err)
			}
			return nil
		},
		Steps: []resource.TestStep{{
			Config: fmt.Sprintf(`
resource "espalier_cluster" "test" {
  name   = %q
  cpus   = 2
  memory = "2G"
  cni    = %q

  timeouts {
    create = "90s"
  }
}`, testCluster, manifest),
			ExpectError: regexp.MustCompile(`not ready after 1m30s`),
		}},
	})
}

func TestAccClusterInvalid(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviders,
		Steps: []resource.TestStep{{
			Config: `
resource "espalier_cluster" "test" {
  name    = "espalier-test-invalid"
  workers = [{ count = 0 }]
}`,
			PlanOnly:    true,
			ExpectError: regexp.MustCompile(`must be at least 1`),
		}},
	})
}

// checkAPIServerReady calls the API server's /readyz with the cluster's
// admin credentials from the state.
func checkAPIServerReady(addr string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		attrs := s.RootModule().Resources[addr].Primary.Attributes
		cert, err := tls.X509KeyPair([]byte(attrs["client_certificate"]), []byte(attrs["client_key"]))
		if err != nil {
			return err
		}
		ca := x509.NewCertPool()
		if !ca.AppendCertsFromPEM([]byte(attrs["cluster_ca_certificate"])) {
			return fmt.Errorf("no CA certificate")
		}
		c := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{Certificates: []tls.Certificate{cert}, RootCAs: ca}}}
		resp, err := c.Get(attrs["api_server"] + "/readyz")
		if err != nil {
			return err
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("/readyz: %s", resp.Status)
		}
		return nil
	}
}
