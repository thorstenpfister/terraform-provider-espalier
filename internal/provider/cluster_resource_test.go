package provider

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
	"gopkg.in/yaml.v3"

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
			// A node that died degrades the cluster, which is replaced.
			{
				PreConfig: func() { killNode(t, testCluster+"-worker-1") },
				Config:    clusterConfig("2048M"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(addr, plancheck.ResourceActionDestroyBeforeCreate),
				}},
				Check: checkAPIServerReady(addr),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(addr, tfjsonpath.New("state"), knownvalue.StringExact("running")),
				},
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

// killNode kills the VM process of a cluster node, as a crash or a runtime
// restart would stop it: `container stop` does not see node containers.
// The runtime then reports the cluster as degraded (container 1.7.1).
func killNode(t *testing.T, node string) {
	t.Helper()
	out, err := exec.Command("pgrep", "-f", "container-runtime-linux start --root .*/containers/"+node+" ").Output()
	pids := strings.Fields(string(out))
	if err != nil || len(pids) != 1 {
		t.Fatalf("finding the process of node %s: %q, %v", node, out, err)
	}
	if err := exec.Command("kill", "-9", pids[0]).Run(); err != nil {
		t.Fatal(err)
	}
	for range 30 {
		cl, err := testClient.Cluster(context.Background(), testCluster)
		if err != nil {
			t.Fatal(err)
		}
		if cl != nil && cl.State != clusterRunning {
			return
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("cluster still running after killing node %s", node)
}

// checkAPIServerReady calls the API server's /readyz with the cluster's
// admin credentials from the state.
func checkAPIServerReady(addr string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		attrs := s.RootModule().Resources[addr].Primary.Attributes
		// Connect as kubectl would with the kubeconfig: through its current
		// context. It must agree with the separate attributes.
		var kc struct {
			CurrentContext string `yaml:"current-context"`
			Contexts       []struct {
				Name    string `yaml:"name"`
				Context struct{ Cluster, User string }
			}
			Clusters []struct {
				Name    string `yaml:"name"`
				Cluster struct {
					Server string `yaml:"server"`
					CA     string `yaml:"certificate-authority-data"`
				}
			}
			Users []struct {
				Name string `yaml:"name"`
				User struct {
					Cert string `yaml:"client-certificate-data"`
					Key  string `yaml:"client-key-data"`
				}
			}
		}
		if err := yaml.Unmarshal([]byte(attrs["kubeconfig"]), &kc); err != nil {
			return err
		}
		var server, caData, certData, keyData string
		for _, c := range kc.Contexts {
			if c.Name != kc.CurrentContext {
				continue
			}
			for _, cl := range kc.Clusters {
				if cl.Name == c.Context.Cluster {
					server, caData = cl.Cluster.Server, cl.Cluster.CA
				}
			}
			for _, u := range kc.Users {
				if u.Name == c.Context.User {
					certData, keyData = u.User.Cert, u.User.Key
				}
			}
		}
		decoded := map[string]string{}
		for name, data := range map[string]string{"cluster_ca_certificate": caData, "client_certificate": certData, "client_key": keyData} {
			b, err := base64.StdEncoding.DecodeString(data)
			if err != nil || len(b) == 0 {
				return fmt.Errorf("kubeconfig: no %s through current-context %q (%v)", name, kc.CurrentContext, err)
			}
			if string(b) != attrs[name] {
				return fmt.Errorf("kubeconfig and %s differ", name)
			}
			decoded[name] = string(b)
		}
		if server != attrs["api_server"] {
			return fmt.Errorf("kubeconfig server %q, api_server %q", server, attrs["api_server"])
		}
		cert, err := tls.X509KeyPair([]byte(decoded["client_certificate"]), []byte(decoded["client_key"]))
		if err != nil {
			return err
		}
		ca := x509.NewCertPool()
		if !ca.AppendCertsFromPEM([]byte(decoded["cluster_ca_certificate"])) {
			return fmt.Errorf("no CA certificate")
		}
		c := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{Certificates: []tls.Certificate{cert}, RootCAs: ca}}}
		resp, err := c.Get(server + "/readyz")
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
