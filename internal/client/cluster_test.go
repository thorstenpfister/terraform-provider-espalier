package client

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"
)

// Trimmed from container 1.7.1: a cluster with one worker and no CNI.
const (
	clustersJSON = `[{"id":"lab","configuration":{"cni":"NONE","kubernetesVersion":"v1.35.5","labels":{},"name":"lab",
"nodes":[{"kind":"linux","name":"lab","roles":["control-plane"]},{"kind":"linux","name":"lab-worker-1","roles":["worker"]}],
"remove":false,"sharedImageStore":false},
"status":{"nodes":[{"addresses":["192.168.64.69"],"name":"lab","ports":["6445->6443"],"status":"running"},
{"addresses":["192.168.64.70"],"name":"lab-worker-1","status":"stopped"}],"state":"running"}},
{"id":"other","configuration":{"cni":null,"nodes":[]},"status":{"nodes":[],"state":"running"}}]`
	nodesJSON = `[{"configuration":{"id":"lab","image":{"reference":"docker.io/kindest/node@sha256:ce97"},"resources":{"cpus":4,"memoryInBytes":4294967296}}},
{"configuration":{"id":"lab-worker-1","image":{"reference":"docker.io/kindest/node@sha256:ce97"},"resources":{"cpus":2,"memoryInBytes":2147483648}}}]`
)

func TestCluster(t *testing.T) {
	f := fakeRunner{
		"cluster list --format json": {out: clustersJSON},
		"inspect lab lab-worker-1":   {out: nodesJSON},
	}
	c := CLI{Run: f.run}
	got, err := c.Cluster(context.Background(), "lab")
	if err != nil {
		t.Fatal(err)
	}
	want := &Cluster{
		Name: "lab", CNI: "NONE", KubernetesVersion: "v1.35.5", State: "running",
		Nodes: []ClusterNode{
			{Name: "lab", Role: "control-plane", Address: "192.168.64.69", State: "running", Image: "docker.io/kindest/node@sha256:ce97", CPUs: 4, MemoryBytes: 4 << 30},
			{Name: "lab-worker-1", Role: "worker", Address: "192.168.64.70", State: "stopped", Image: "docker.io/kindest/node@sha256:ce97", CPUs: 2, MemoryBytes: 2 << 30},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
	if got, err := c.Cluster(context.Background(), "other"); err != nil || got.CNI != "" || len(got.Nodes) != 0 {
		t.Errorf("other: got %+v, %v", got, err)
	}
	if got, _ := c.Cluster(context.Background(), "missing"); got != nil {
		t.Errorf("missing: got %+v, want nil", got)
	}
}

// Creating and deleting run with a scratch KUBECONFIG, never the user's.
func TestCreateAndDeleteClusterKeepKubeconfig(t *testing.T) {
	var calls []string
	run := func(ctx context.Context, args ...string) ([]byte, error) {
		env, _ := ctx.Value(envKey{}).([]string)
		if len(env) != 1 || !strings.HasPrefix(env[0], "KUBECONFIG="+os.TempDir()) {
			t.Errorf("%v: env %v, want a scratch KUBECONFIG", args, env)
		}
		calls = append(calls, strings.Join(args, " "))
		return nil, nil
	}
	c := CLI{Run: run}
	err := c.CreateCluster(context.Background(), ClusterSpec{
		Name: "lab", NodeImage: "kindest/node:v1", CPUs: 4, Memory: "4G", CNI: "NONE", Scheme: "https", SharedImageStore: true,
		Workers: []WorkerGroup{{Count: 2, CPUs: 2, Memory: "2G"}, {Count: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteCluster(context.Background(), "lab"); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"cluster create --name lab --cpus 4 --memory 4G --node-image kindest/node:v1 --cni NONE --scheme https" +
			" --workers count=2,cpus=2,memory=2G --workers count=1 --shared-image-store",
		"cluster delete --name lab",
	}
	if !reflect.DeepEqual(calls, want) {
		t.Errorf("got  %q\nwant %q", calls, want)
	}
}

func TestClusterKubeconfig(t *testing.T) {
	for _, tc := range []struct{ written, want string }{
		// As write-config writes a new file: the only context is selected.
		{"contexts:\n- name: lab\n", "contexts:\n- name: lab\ncurrent-context: \"lab\"\n"},
		{"contexts:\n- name: lab\ncurrent-context: other\n", "contexts:\n- name: lab\ncurrent-context: other\n"},
		{"apiVersion: v1\n", "apiVersion: v1\n"},
	} {
		run := func(_ context.Context, args ...string) ([]byte, error) {
			return nil, os.WriteFile(args[len(args)-1], []byte(tc.written), 0o600)
		}
		got, err := CLI{Run: run}.ClusterKubeconfig(context.Background(), "lab")
		if err != nil || string(got) != tc.want {
			t.Errorf("%q: got %q, %v; want %q", tc.written, got, err, tc.want)
		}
	}
}
