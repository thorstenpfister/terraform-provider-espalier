package client

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"
)

// NetworkSpec is what a network is created with.
type NetworkSpec struct {
	Name     string
	Internal bool              // host-only: no route off the host
	Subnet   string            // IPv4 CIDR; empty lets the runtime choose
	SubnetV6 string            // IPv6 prefix; empty lets the runtime choose
	Labels   map[string]string // may be nil
	Plugin   string            // empty for the runtime's default, container-network-vmnet
	Options  map[string]string // plugin-specific; may be nil
}

// Network is a network as the runtime reports it. Subnet and SubnetV6 are
// the requested values when one was given, else the ones the runtime chose
// (which may change when the runtime restarts).
type Network struct {
	NetworkSpec
	Gateway string // IPv4 gateway
}

// networkJSON is one entry of `container network list --format json`
// (container 1.7.1).
type networkJSON struct {
	ID            string `json:"id"`
	Configuration struct {
		Mode       string            `json:"mode"` // "nat" or "hostOnly"
		IPv4Subnet string            `json:"ipv4Subnet"`
		IPv6Subnet string            `json:"ipv6Subnet"`
		Labels     map[string]string `json:"labels"`
		Plugin     string            `json:"plugin"`
		Options    map[string]string `json:"options"`
	} `json:"configuration"`
	Status struct {
		IPv4Gateway string `json:"ipv4Gateway"`
		IPv4Subnet  string `json:"ipv4Subnet"`
		IPv6Subnet  string `json:"ipv6Subnet"`
	} `json:"status"`
}

func (n networkJSON) network() Network {
	c, s := n.Configuration, n.Status
	return Network{
		NetworkSpec: NetworkSpec{
			Name:     n.ID,
			Internal: c.Mode == "hostOnly",
			Subnet:   cmp.Or(c.IPv4Subnet, s.IPv4Subnet),
			SubnetV6: cmp.Or(c.IPv6Subnet, s.IPv6Subnet),
			Labels:   c.Labels,
			Plugin:   c.Plugin,
			Options:  c.Options,
		},
		Gateway: s.IPv4Gateway,
	}
}

func (c CLI) CreateNetwork(ctx context.Context, spec NetworkSpec) error {
	args := []string{"network", "create"}
	if spec.Internal {
		args = append(args, "--internal")
	}
	if spec.Subnet != "" {
		args = append(args, "--subnet", spec.Subnet)
	}
	if spec.SubnetV6 != "" {
		args = append(args, "--subnet-v6", spec.SubnetV6)
	}
	for _, k := range slices.Sorted(maps.Keys(spec.Labels)) {
		args = append(args, "--label", k+"="+spec.Labels[k])
	}
	if spec.Plugin != "" {
		args = append(args, "--plugin", spec.Plugin)
	}
	for _, k := range slices.Sorted(maps.Keys(spec.Options)) {
		args = append(args, "--option", k+"="+spec.Options[k])
	}
	_, err := c.Run(ctx, append(args, spec.Name)...)
	return err
}

// Network returns the named network, or nil if it does not exist. It reads
// the list rather than `network inspect`, whose not-found case is only an
// error message.
func (c CLI) Network(ctx context.Context, name string) (*Network, error) {
	var list []networkJSON
	if err := c.json(ctx, &list, "network", "list", "--format", "json"); err != nil {
		return nil, err
	}
	for _, n := range list {
		if n.ID == name {
			net := n.network()
			return &net, nil
		}
	}
	return nil, nil
}

func (c CLI) DeleteNetwork(ctx context.Context, name string) error {
	if _, err := c.Run(ctx, "network", "delete", name); err != nil {
		return fmt.Errorf("deleting network %s: %w", name, err)
	}
	return nil
}
