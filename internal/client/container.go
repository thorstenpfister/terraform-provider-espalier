package client

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// ContainerSpec is what a container is created with. Zero values leave the
// choice to the image or the runtime's defaults.
type ContainerSpec struct {
	Name  string
	Image string
	// Entrypoint replaces the image's entrypoint, and then its command too;
	// nil keeps it. Command is the arguments to the entrypoint; nil keeps
	// the image's.
	Entrypoint []string
	Command    []string
	Env        map[string]string // on top of the image's environment
	Workdir    string
	CPUs       int64
	Memory     string // e.g. "512M"
	Labels     map[string]string
	Networks   []NetworkAttachment // nil attaches the runtime's default network
	Mounts     []Mount
	Ports      []Port
	CapAdd     []string
	CapDrop    []string

	Platform       string // e.g. "linux/arm64"; empty for the image's
	User           string // name|uid[:gid]; empty for the image's
	ReadOnly       bool   // read-only root filesystem
	DNS            DNS
	NoDNS          bool // no resolver configuration at all; DNS must be empty
	Init           bool // run an init process that reaps zombies and forwards signals
	Rosetta        bool
	SSH            bool   // forward the host's SSH agent
	TTY            bool   // give the process a terminal, which keeps e.g. a shell running
	ShmSize        string // e.g. "64M"; empty for the runtime's default
	Ulimits        []Ulimit
	PublishSockets []Socket
	Scheme         string // registry scheme for pulling the image, "http" or "https"; not recorded

	// Kernel, KernelArgs and InitImage are not reported.
	Kernel         string   // host path of a custom kernel
	KernelArgs     []string // appended to the kernel command line
	InitImage      string   // custom init (vminit) image
	Runtime        string   // runtime handler; empty for container-runtime-<os>
	Virtualization bool     // expose virtualization to the container
	// ReadOnlyPaths and MaskedPaths are added to the runtime's defaults, or
	// replace them after a "NONE". Reported: the effective paths, nil while
	// the defaults apply.
	ReadOnlyPaths []string
	MaskedPaths   []string
}

// DNS is a container's resolver configuration. Zero values are the
// runtime's defaults.
type DNS struct {
	Nameservers []string
	Domain      string
	Search      []string
	Options     []string
}

// Ulimit is a resource limit of the container's process.
type Ulimit struct {
	Name       string // as the CLI takes it, e.g. "nofile"
	Soft, Hard int64
}

// Socket publishes a socket in the container on the host.
type Socket struct {
	HostPath, ContainerPath string
}

// NetworkAttachment attaches a container to a network.
type NetworkAttachment struct {
	Network string
	MAC     string   // empty: assigned by the runtime when the container starts
	MTU     int64    // 0: the runtime's default
	Aliases []string // fully qualified DNS names, e.g. "api.example.test"
}

// Mount types.
const (
	MountBind   = "bind"   // a host directory, shared with virtiofs
	MountVolume = "volume" // a runtime volume, by name
	MountTmpfs  = "tmpfs"
)

// Mount is a filesystem mounted into a container.
type Mount struct {
	Type     string // MountBind, MountVolume or MountTmpfs
	Source   string // host path or volume name; empty for tmpfs
	Target   string
	ReadOnly bool
}

// Port publishes a container port on the host.
type Port struct {
	HostIP        string // empty: all addresses
	HostPort      int64
	ContainerPort int64
	Protocol      string // "tcp" or "udp"
}

// Container is a container as the runtime reports it. Command, Workdir,
// CPUs and Networks' MTUs are the effective values, so they are set even if
// the spec left them to defaults; Env excludes the image's own environment.
type Container struct {
	ContainerSpec
	MemoryBytes  int64
	ShmSizeBytes int64  // 0 for the runtime's default
	State        string // "running", "stopped", …
	Addresses    []Address
}

// Address is a running container's address on one network.
type Address struct {
	Network     string
	IPv4        string // with prefix length, e.g. "192.168.64.2/24"
	IPv4Gateway string
	IPv6        string
	MAC         string
}

// containerJSON is one entry of `container list --all --format json`
// (container 1.7.1).
type containerJSON struct {
	ID            string `json:"id"`
	Configuration struct {
		Image struct {
			Reference string `json:"reference"`
		} `json:"image"`
		InitProcess struct {
			Executable       string   `json:"executable"`
			Arguments        []string `json:"arguments"`
			Environment      []string `json:"environment"`
			WorkingDirectory string   `json:"workingDirectory"`
			Terminal         bool     `json:"terminal"`
			User             struct {
				Raw *struct {
					UserString string `json:"userString"`
				} `json:"raw"`
				ID *struct {
					UID int64 `json:"uid"`
					GID int64 `json:"gid"`
				} `json:"id"`
			} `json:"user"`
			Rlimits []struct {
				Limit string `json:"limit"`
				Soft  int64  `json:"soft"`
				Hard  int64  `json:"hard"`
			} `json:"rlimits"`
		} `json:"initProcess"`
		Resources struct {
			CPUs          int64 `json:"cpus"`
			MemoryInBytes int64 `json:"memoryInBytes"`
		} `json:"resources"`
		Labels   map[string]string `json:"labels"`
		Platform platformJSON      `json:"platform"`
		Networks []struct {
			Network string `json:"network"`
			Options struct {
				Aliases    []string `json:"aliases"`
				MacAddress string   `json:"macAddress"`
				MTU        int64    `json:"mtu"`
			} `json:"options"`
		} `json:"networks"`
		Mounts []struct {
			Destination string   `json:"destination"`
			Options     []string `json:"options"`
			Source      string   `json:"source"`
			Type        struct {
				Volume *struct {
					Name string `json:"name"`
				} `json:"volume"`
				Virtiofs *struct{} `json:"virtiofs"`
				Tmpfs    *struct{} `json:"tmpfs"`
			} `json:"type"`
		} `json:"mounts"`
		PublishedPorts []struct {
			ContainerPort int64  `json:"containerPort"`
			HostAddress   string `json:"hostAddress"`
			HostPort      int64  `json:"hostPort"`
			Proto         string `json:"proto"`
		} `json:"publishedPorts"`
		CapAdd  []string `json:"capAdd"`
		CapDrop []string `json:"capDrop"`
		DNS     *struct {
			Nameservers   []string `json:"nameservers"`
			Domain        string   `json:"domain"`
			SearchDomains []string `json:"searchDomains"`
			Options       []string `json:"options"`
		} `json:"dns"`
		ReadOnly         bool     `json:"readOnly"`
		UseInit          bool     `json:"useInit"`
		Rosetta          bool     `json:"rosetta"`
		SSH              bool     `json:"ssh"`
		ShmSize          int64    `json:"shmSize"`
		RuntimeHandler   string   `json:"runtimeHandler"`
		Virtualization   bool     `json:"virtualization"`
		ReadonlyPaths    []string `json:"readonlyPaths"`
		MaskedPaths      []string `json:"maskedPaths"`
		PublishedSockets []struct {
			ContainerPath string `json:"containerPath"`
			HostPath      string `json:"hostPath"`
		} `json:"publishedSockets"`
	} `json:"configuration"`
	Status struct {
		State    string `json:"state"`
		Networks []struct {
			Network     string `json:"network"`
			IPv4Address string `json:"ipv4Address"`
			IPv4Gateway string `json:"ipv4Gateway"`
			IPv6Address string `json:"ipv6Address"`
			MacAddress  string `json:"macAddress"`
		} `json:"networks"`
	} `json:"status"`
}

func (c CLI) CreateContainer(ctx context.Context, spec ContainerSpec) error {
	args := []string{"create", "--name", spec.Name}
	for _, k := range slices.Sorted(maps.Keys(spec.Env)) {
		args = append(args, "--env", k+"="+spec.Env[k])
	}
	if spec.Workdir != "" {
		args = append(args, "--workdir", spec.Workdir)
	}
	if spec.CPUs != 0 {
		args = append(args, "--cpus", strconv.FormatInt(spec.CPUs, 10))
	}
	if spec.Memory != "" {
		args = append(args, "--memory", spec.Memory)
	}
	for _, k := range slices.Sorted(maps.Keys(spec.Labels)) {
		args = append(args, "--label", k+"="+spec.Labels[k])
	}
	for _, n := range spec.Networks {
		opt := n.Network
		if n.MAC != "" {
			opt += ",mac=" + n.MAC
		}
		if n.MTU != 0 {
			opt += ",mtu=" + strconv.FormatInt(n.MTU, 10)
		}
		for _, a := range n.Aliases {
			opt += ",alias=" + a
		}
		args = append(args, "--network", opt)
	}
	for _, m := range spec.Mounts {
		opt := "type=" + m.Type + ",target=" + m.Target
		if m.Source != "" {
			opt += ",source=" + m.Source
		}
		if m.ReadOnly {
			opt += ",readonly"
		}
		args = append(args, "--mount", opt)
	}
	for _, p := range spec.Ports {
		publish := fmt.Sprintf("%d:%d/%s", p.HostPort, p.ContainerPort, p.Protocol)
		if p.HostIP != "" {
			publish = p.HostIP + ":" + publish
		}
		args = append(args, "--publish", publish)
	}
	for _, cap := range spec.CapAdd {
		args = append(args, "--cap-add", cap)
	}
	for _, cap := range spec.CapDrop {
		args = append(args, "--cap-drop", cap)
	}
	args = append(args, spec.optionArgs()...)
	if len(spec.Entrypoint) > 0 {
		args = append(args, "--entrypoint", spec.Entrypoint[0])
	}
	args = append(args, spec.Image)
	if len(spec.Entrypoint) > 1 {
		args = append(args, spec.Entrypoint[1:]...)
	}
	args = append(args, spec.Command...)
	_, err := c.Run(ctx, args...)
	return err
}

func (c CLI) StartContainer(ctx context.Context, name string) error {
	_, err := c.Run(ctx, "start", name)
	return err
}

func (c CLI) StopContainer(ctx context.Context, name string) error {
	_, err := c.Run(ctx, "stop", name)
	return err
}

// optionArgs are the flags for the less common options.
func (spec ContainerSpec) optionArgs() []string {
	var args []string
	flag := func(name, value string) {
		if value != "" {
			args = append(args, name, value)
		}
	}
	flag("--platform", spec.Platform)
	flag("--user", spec.User)
	flag("--shm-size", spec.ShmSize)
	flag("--scheme", spec.Scheme)
	flag("--kernel", spec.Kernel)
	flag("--init-image", spec.InitImage)
	flag("--runtime", spec.Runtime)
	for _, f := range []struct {
		name string
		on   bool
	}{
		{"--read-only", spec.ReadOnly}, {"--init", spec.Init}, {"--rosetta", spec.Rosetta}, {"--ssh", spec.SSH},
		{"--no-dns", spec.NoDNS}, {"--virtualization", spec.Virtualization}, {"--tty", spec.TTY},
	} {
		if f.on {
			args = append(args, f.name)
		}
	}
	for _, ns := range spec.DNS.Nameservers {
		args = append(args, "--dns", ns)
	}
	flag("--dns-domain", spec.DNS.Domain)
	for _, s := range spec.DNS.Search {
		args = append(args, "--dns-search", s)
	}
	for _, o := range spec.DNS.Options {
		args = append(args, "--dns-option", o)
	}
	for _, u := range spec.Ulimits {
		args = append(args, "--ulimit", fmt.Sprintf("%s=%d:%d", u.Name, u.Soft, u.Hard))
	}
	for _, s := range spec.PublishSockets {
		args = append(args, "--publish-socket", s.HostPath+":"+s.ContainerPath)
	}
	for _, list := range []struct {
		flag   string
		values []string
	}{{"--kernel-arg", spec.KernelArgs}, {"--read-only-path", spec.ReadOnlyPaths}, {"--masked-path", spec.MaskedPaths}} {
		for _, v := range list.values {
			args = append(args, list.flag, v)
		}
	}
	return args
}

// Container returns the named container, or nil if it does not exist.
func (c CLI) Container(ctx context.Context, name string) (*Container, error) {
	containers, err := c.Containers(ctx)
	if err != nil {
		return nil, err
	}
	for _, ctr := range containers {
		if ctr.Name == name {
			return &ctr, nil
		}
	}
	return nil, nil
}

// Containers returns all containers, running or not.
func (c CLI) Containers(ctx context.Context) ([]Container, error) {
	var list []containerJSON
	if err := c.json(ctx, &list, "list", "--all", "--format", "json"); err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, nil
	}
	var images []imageJSON
	if err := c.json(ctx, &images, "image", "list", "--format", "json"); err != nil {
		return nil, err
	}
	containers := make([]Container, 0, len(list))
	for _, j := range list {
		containers = append(containers, j.container(images))
	}
	return containers, nil
}

// container converts the JSON; images is the image list, for the
// environment the container inherits from its image.
func (j containerJSON) container(images []imageJSON) Container {
	cfg := j.Configuration
	defaults := imageConfig(images, cfg.Image.Reference, cfg.Platform)
	ctr := Container{
		ContainerSpec: ContainerSpec{
			Name:    j.ID,
			Image:   cfg.Image.Reference,
			Env:     map[string]string{},
			Workdir: cfg.InitProcess.WorkingDirectory,
			CPUs:    cfg.Resources.CPUs,
			Labels:  cfg.Labels,
			CapAdd:  cfg.CapAdd,
			CapDrop: cfg.CapDrop,
		},
		MemoryBytes:  cfg.Resources.MemoryInBytes,
		ShmSizeBytes: cfg.ShmSize,
		State:        j.Status.State,
	}
	ctr.Entrypoint, ctr.Command = SplitProcess(append([]string{cfg.InitProcess.Executable}, cfg.InitProcess.Arguments...), defaults.Entrypoint)
	ctr.Platform = cfg.Platform.String()
	ctr.ReadOnly, ctr.Init, ctr.Rosetta, ctr.SSH = cfg.ReadOnly, cfg.UseInit, cfg.Rosetta, cfg.SSH
	ctr.TTY = cfg.InitProcess.Terminal
	ctr.Runtime, ctr.Virtualization = cfg.RuntimeHandler, cfg.Virtualization
	ctr.ReadOnlyPaths, ctr.MaskedPaths = cfg.ReadonlyPaths, cfg.MaskedPaths
	switch u := cfg.InitProcess.User; {
	case u.Raw != nil:
		ctr.User = u.Raw.UserString
	case u.ID != nil:
		ctr.User = fmt.Sprintf("%d:%d", u.ID.UID, u.ID.GID)
	}
	if d := cfg.DNS; d != nil {
		ctr.DNS = DNS{Nameservers: d.Nameservers, Domain: d.Domain, Search: d.SearchDomains, Options: d.Options}
	} else {
		ctr.NoDNS = true
	}
	for _, l := range cfg.InitProcess.Rlimits {
		ctr.Ulimits = append(ctr.Ulimits, Ulimit{Name: strings.ToLower(strings.TrimPrefix(l.Limit, "RLIMIT_")), Soft: l.Soft, Hard: l.Hard})
	}
	for _, s := range cfg.PublishedSockets {
		ctr.PublishSockets = append(ctr.PublishSockets, Socket{HostPath: s.HostPath, ContainerPath: s.ContainerPath})
	}
	// The runtime merges the image's environment in; keep only what the
	// container adds or overrides.
	for _, kv := range cfg.InitProcess.Environment {
		if !slices.Contains(defaults.Env, kv) {
			k, v, _ := strings.Cut(kv, "=")
			ctr.Env[k] = v
		}
	}
	for _, n := range cfg.Networks {
		ctr.Networks = append(ctr.Networks, NetworkAttachment{
			Network: n.Network, MAC: n.Options.MacAddress, MTU: n.Options.MTU, Aliases: n.Options.Aliases,
		})
	}
	for _, m := range cfg.Mounts {
		mount := Mount{Target: m.Destination, ReadOnly: slices.Contains(m.Options, "ro")}
		switch {
		case m.Type.Volume != nil:
			mount.Type, mount.Source = MountVolume, m.Type.Volume.Name
		case m.Type.Tmpfs != nil:
			mount.Type = MountTmpfs
		default:
			mount.Type, mount.Source = MountBind, m.Source
		}
		ctr.Mounts = append(ctr.Mounts, mount)
	}
	for _, p := range cfg.PublishedPorts {
		ctr.Ports = append(ctr.Ports, Port{HostIP: p.HostAddress, HostPort: p.HostPort, ContainerPort: p.ContainerPort, Protocol: p.Proto})
	}
	for _, n := range j.Status.Networks {
		ctr.Addresses = append(ctr.Addresses, Address{
			Network: n.Network, IPv4: n.IPv4Address, IPv4Gateway: n.IPv4Gateway, IPv6: n.IPv6Address, MAC: n.MacAddress,
		})
	}
	return ctr
}

func (c CLI) DeleteContainer(ctx context.Context, name string) error {
	if _, err := c.Run(ctx, "delete", "--force", name); err != nil {
		return fmt.Errorf("deleting container %s: %w", name, err)
	}
	return nil
}

type platformJSON struct {
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
	Variant      string `json:"variant"`
}

func (p platformJSON) String() string {
	s := p.OS + "/" + p.Architecture
	if p.Variant != "" {
		s += "/" + p.Variant
	}
	return s
}

// imageDefaults is the part of an image's configuration a container
// inherits.
type imageDefaults struct {
	Env        []string `json:"Env"`
	Entrypoint []string `json:"Entrypoint"`
}

// SplitProcess splits a process's argument list into the entrypoint and its
// arguments: the given entrypoint if the process starts with it, else the
// executable. Without an entrypoint the arguments are the whole list, as for
// an image without one.
func SplitProcess(argv, entrypoint []string) (ep, args []string) {
	switch {
	case len(entrypoint) == 0:
		return nil, argv
	case len(argv) >= len(entrypoint) && slices.Equal(argv[:len(entrypoint)], entrypoint):
		return argv[:len(entrypoint)], argv[len(entrypoint):]
	default:
		return argv[:1], argv[1:]
	}
}

// imageConfig returns the configuration of the image variant a container
// runs. An image deleted since the container was created has none.
func imageConfig(images []imageJSON, reference string, platform platformJSON) imageDefaults {
	for _, i := range images {
		if i.Configuration.Name != reference {
			continue
		}
		for _, v := range i.Variants {
			if v.Platform == platform {
				return v.Config.Config
			}
		}
	}
	return imageDefaults{}
}
