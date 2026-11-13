package client

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// ClusterSpec is what a local Kubernetes cluster is created with.
type ClusterSpec struct {
	Name             string
	NodeImage        string // empty for the runtime's kindest/node
	CPUs             int64  // control plane, or the single node; 0 for the default
	Memory           string // likewise, e.g. "4G"
	Workers          []WorkerGroup
	CNI              string // manifest path, "NONE", or empty for kindnet
	SharedImageStore bool
	Scheme           string // registry scheme for pulling the node image; not recorded
}

// WorkerGroup is a number of equally sized worker nodes. Zero sizes are the
// runtime's defaults.
type WorkerGroup struct {
	Count  int64
	CPUs   int64
	Memory string
}

// Cluster is a cluster as the runtime reports it.
type Cluster struct {
	Name              string
	CNI               string // "NONE", a manifest path, or empty for kindnet
	KubernetesVersion string
	SharedImageStore  bool
	State             string // e.g. "running"
	Nodes             []ClusterNode
}

// ClusterNode is a node container of a cluster.
type ClusterNode struct {
	Name        string
	Role        string // "control-plane" or "worker"
	Address     string
	State       string // e.g. "running", "stopped"
	Image       string
	CPUs        int64
	MemoryBytes int64
}

// clusterJSON is one entry of `container cluster list --format json`
// (container 1.7.1).
type clusterJSON struct {
	ID            string `json:"id"`
	Configuration struct {
		CNI               *string `json:"cni"`
		KubernetesVersion string  `json:"kubernetesVersion"`
		SharedImageStore  bool    `json:"sharedImageStore"`
		Nodes             []struct {
			Name  string   `json:"name"`
			Roles []string `json:"roles"`
		} `json:"nodes"`
	} `json:"configuration"`
	Status struct {
		State string `json:"state"`
		Nodes []struct {
			Name      string   `json:"name"`
			Addresses []string `json:"addresses"`
			Status    string   `json:"status"`
		} `json:"nodes"`
	} `json:"status"`
}

// nodeJSON is the part of `container inspect` a node needs; node containers
// are not in `container list`.
type nodeJSON struct {
	Configuration struct {
		ID    string `json:"id"`
		Image struct {
			Reference string `json:"reference"`
		} `json:"image"`
		Resources struct {
			CPUs          int64 `json:"cpus"`
			MemoryInBytes int64 `json:"memoryInBytes"`
		} `json:"resources"`
	} `json:"configuration"`
}

// CreateCluster creates the cluster and waits until it is ready, which it
// never is if the CNI does not come up: give ctx a deadline.
func (c CLI) CreateCluster(ctx context.Context, spec ClusterSpec) error {
	args := []string{"cluster", "create", "--name", spec.Name}
	if spec.CPUs != 0 {
		args = append(args, "--cpus", strconv.FormatInt(spec.CPUs, 10))
	}
	for _, f := range [][2]string{{"--memory", spec.Memory}, {"--node-image", spec.NodeImage}, {"--cni", spec.CNI}, {"--scheme", spec.Scheme}} {
		if f[1] != "" {
			args = append(args, f[0], f[1])
		}
	}
	for _, w := range spec.Workers {
		group := "count=" + strconv.FormatInt(w.Count, 10)
		if w.CPUs != 0 {
			group += ",cpus=" + strconv.FormatInt(w.CPUs, 10)
		}
		if w.Memory != "" {
			group += ",memory=" + w.Memory
		}
		args = append(args, "--workers", group)
	}
	if spec.SharedImageStore {
		args = append(args, "--shared-image-store")
	}
	return withScratchKubeconfig(ctx, func(ctx context.Context, _ string) error {
		_, err := c.Run(ctx, args...)
		return err
	})
}

// Cluster returns the named cluster, or nil if it does not exist.
func (c CLI) Cluster(ctx context.Context, name string) (*Cluster, error) {
	var list []clusterJSON
	if err := c.json(ctx, &list, "cluster", "list", "--format", "json"); err != nil {
		return nil, err
	}
	for _, j := range list {
		if j.ID == name {
			return c.cluster(ctx, j)
		}
	}
	return nil, nil
}

func (c CLI) cluster(ctx context.Context, j clusterJSON) (*Cluster, error) {
	cl := &Cluster{
		Name:              j.ID,
		KubernetesVersion: j.Configuration.KubernetesVersion,
		SharedImageStore:  j.Configuration.SharedImageStore,
		State:             j.Status.State,
	}
	if j.Configuration.CNI != nil {
		cl.CNI = *j.Configuration.CNI
	}
	roles := map[string]string{}
	for _, n := range j.Configuration.Nodes {
		if len(n.Roles) > 0 {
			roles[n.Name] = n.Roles[0]
		}
	}
	names := make([]string, 0, len(j.Status.Nodes))
	for _, n := range j.Status.Nodes {
		node := ClusterNode{Name: n.Name, Role: roles[n.Name], State: n.Status}
		if len(n.Addresses) > 0 {
			node.Address = n.Addresses[0]
		}
		cl.Nodes = append(cl.Nodes, node)
		names = append(names, n.Name)
	}
	if len(names) == 0 {
		return cl, nil
	}
	var inspected []nodeJSON
	if err := c.json(ctx, &inspected, append([]string{"inspect"}, names...)...); err != nil {
		return nil, err
	}
	for _, in := range inspected {
		for i := range cl.Nodes {
			if n := &cl.Nodes[i]; n.Name == in.Configuration.ID {
				n.Image = in.Configuration.Image.Reference
				n.CPUs = in.Configuration.Resources.CPUs
				n.MemoryBytes = in.Configuration.Resources.MemoryInBytes
			}
		}
	}
	return cl, nil
}

// ClusterKubeconfig returns a kubeconfig for the cluster, with its admin
// credentials.
func (c CLI) ClusterKubeconfig(ctx context.Context, name string) ([]byte, error) {
	var config []byte
	err := withScratchKubeconfig(ctx, func(ctx context.Context, path string) error {
		if _, err := c.Run(ctx, "cluster", "write-config", "--name", name, "--kubeconfig", path); err != nil {
			return err
		}
		var err error
		config, err = os.ReadFile(path)
		return err
	})
	return config, err
}

func (c CLI) DeleteCluster(ctx context.Context, name string) error {
	return withScratchKubeconfig(ctx, func(ctx context.Context, _ string) error {
		if _, err := c.Run(ctx, "cluster", "delete", "--name", name); err != nil {
			return fmt.Errorf("deleting cluster %s: %w", name, err)
		}
		return nil
	})
}

// withScratchKubeconfig runs fn with KUBECONFIG set to a file in a new
// temporary directory, so that creating and deleting clusters leaves the
// user's kubeconfig alone: by default the CLI adds and removes the
// cluster's entries in ~/.kube/config.
func withScratchKubeconfig(ctx context.Context, fn func(ctx context.Context, path string) error) error {
	dir, err := os.MkdirTemp("", "espalier-kubeconfig-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	path := filepath.Join(dir, "config")
	return fn(WithEnv(ctx, "KUBECONFIG="+path), path)
}
