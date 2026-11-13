package client

import (
	"context"
	"fmt"
	"slices"
	"strconv"
)

// MachineSpec is what a container machine is created with: a long-lived
// VM made from an image, with an account for a host user.
type MachineSpec struct {
	Name       string
	Image      string
	Platform   string // e.g. "linux/arm64"; empty for the runtime's default
	User       string // account name; empty for the host user's
	UID, GID   *int64 // nil for the host user's
	Home       string // empty for /home/<user>
	Scheme     string // registry scheme for pulling the image; not recorded
	SetDefault bool   // make it the default machine
}

// MachineSettings can change after creation; they take effect when the
// machine next starts. Zero values leave a setting as it is.
type MachineSettings struct {
	CPUs           int64
	Memory         string // e.g. "4G"
	HomeMount      string // "rw", "ro" or "none"
	Virtualization *bool  // nested virtualization
	Kernel         string // path of a custom kernel
}

// Machine is a container machine as the runtime reports it.
// Virtualization and Kernel are not reported.
type Machine struct {
	Name        string
	Image       string
	Digest      string
	Platform    string
	User        string
	UID, GID    int64
	Home        string
	CPUs        int64
	MemoryBytes int64
	HomeMount   string
	Default     bool
	Status      string // "running", "stopped", …
	IPAddress   string
	DiskSize    int64
}

// machineJSON is the output of `container machine inspect` (container
// 1.7.1); `machine list` has only a summary, but the default flag.
type machineJSON struct {
	ID    string `json:"id"`
	Image struct {
		Reference  string `json:"reference"`
		Descriptor struct {
			Digest string `json:"digest"`
		} `json:"descriptor"`
	} `json:"image"`
	Platform  platformJSON `json:"platform"`
	CPUs      int64        `json:"cpus"`
	Memory    int64        `json:"memory"`
	HomeMount string       `json:"homeMount"`
	Status    string       `json:"status"`
	IPAddress string       `json:"ipAddress"`
	DiskSize  int64        `json:"diskSize"`
	UserSetup struct {
		Username string `json:"username"`
		UID      int64  `json:"uid"`
		GID      int64  `json:"gid"`
		Home     string `json:"home"`
	} `json:"userSetup"`
}

type machineSummaryJSON struct {
	ID      string `json:"id"`
	Default bool   `json:"default"`
}

func (c CLI) CreateMachine(ctx context.Context, spec MachineSpec) error {
	args := []string{"machine", "create", "--name", spec.Name, "--progress", "none"}
	for _, f := range [][2]string{{"--platform", spec.Platform}, {"--user", spec.User}, {"--home", spec.Home}, {"--scheme", spec.Scheme}} {
		if f[1] != "" {
			args = append(args, f[0], f[1])
		}
	}
	if spec.UID != nil {
		args = append(args, "--uid", strconv.FormatInt(*spec.UID, 10))
	}
	if spec.GID != nil {
		args = append(args, "--gid", strconv.FormatInt(*spec.GID, 10))
	}
	if spec.SetDefault {
		args = append(args, "--set-default")
	}
	_, err := c.Run(ctx, append(args, spec.Image)...)
	return err
}

// SetMachine changes settings; they take effect when the machine next
// starts. It does nothing if s sets nothing.
func (c CLI) SetMachine(ctx context.Context, name string, s MachineSettings) error {
	var settings []string
	if s.CPUs != 0 {
		settings = append(settings, "cpus="+strconv.FormatInt(s.CPUs, 10))
	}
	for _, kv := range [][2]string{{"memory", s.Memory}, {"home-mount", s.HomeMount}, {"kernel", s.Kernel}} {
		if kv[1] != "" {
			settings = append(settings, kv[0]+"="+kv[1])
		}
	}
	if s.Virtualization != nil {
		settings = append(settings, "virtualization="+strconv.FormatBool(*s.Virtualization))
	}
	if len(settings) == 0 {
		return nil
	}
	_, err := c.Run(ctx, append([]string{"machine", "set", "--name", name}, settings...)...)
	return err
}

// StartMachine starts a stopped machine. There is no start command: running
// a command in a machine starts it.
func (c CLI) StartMachine(ctx context.Context, name string) error {
	_, err := c.Run(ctx, "machine", "run", "--name", name, "true")
	return err
}

func (c CLI) StopMachine(ctx context.Context, name string) error {
	_, err := c.Run(ctx, "machine", "stop", name)
	return err
}

func (c CLI) SetDefaultMachine(ctx context.Context, name string) error {
	_, err := c.Run(ctx, "machine", "set-default", name)
	return err
}

// Machine returns the named machine, or nil if it does not exist.
func (c CLI) Machine(ctx context.Context, name string) (*Machine, error) {
	list, err := c.machineList(ctx)
	if err != nil {
		return nil, err
	}
	i := slices.IndexFunc(list, func(m machineSummaryJSON) bool { return m.ID == name })
	if i < 0 {
		return nil, nil
	}
	m, err := c.inspectMachine(ctx, list[i])
	return &m, err
}

// Machines returns all machines.
func (c CLI) Machines(ctx context.Context) ([]Machine, error) {
	list, err := c.machineList(ctx)
	if err != nil {
		return nil, err
	}
	machines := make([]Machine, 0, len(list))
	for _, summary := range list {
		m, err := c.inspectMachine(ctx, summary)
		if err != nil {
			return nil, err
		}
		machines = append(machines, m)
	}
	return machines, nil
}

func (c CLI) machineList(ctx context.Context) ([]machineSummaryJSON, error) {
	var list []machineSummaryJSON
	err := c.json(ctx, &list, "machine", "list", "--format", "json")
	return list, err
}

// inspectMachine adds the details `machine inspect` has to a list entry;
// it takes one machine at a time.
func (c CLI) inspectMachine(ctx context.Context, summary machineSummaryJSON) (Machine, error) {
	var inspected []machineJSON
	if err := c.json(ctx, &inspected, "machine", "inspect", summary.ID); err != nil {
		return Machine{}, err
	}
	if len(inspected) != 1 {
		return Machine{}, fmt.Errorf("inspecting machine %s: got %d results", summary.ID, len(inspected))
	}
	j := inspected[0]
	return Machine{
		Name:        j.ID,
		Image:       j.Image.Reference,
		Digest:      j.Image.Descriptor.Digest,
		Platform:    j.Platform.String(),
		User:        j.UserSetup.Username,
		UID:         j.UserSetup.UID,
		GID:         j.UserSetup.GID,
		Home:        j.UserSetup.Home,
		CPUs:        j.CPUs,
		MemoryBytes: j.Memory,
		HomeMount:   j.HomeMount,
		Default:     summary.Default,
		Status:      j.Status,
		IPAddress:   j.IPAddress,
		DiskSize:    j.DiskSize,
	}, nil
}

func (c CLI) DeleteMachine(ctx context.Context, name string) error {
	if _, err := c.Run(ctx, "machine", "delete", name); err != nil {
		return fmt.Errorf("deleting machine %s: %w", name, err)
	}
	return nil
}
