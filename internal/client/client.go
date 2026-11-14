// Package client talks to Apple's container runtime. The provider depends
// only on the Client interface; CLI implements it with the `container`
// command line tool and its JSON output, so a native API client can replace
// it later without touching the provider.
package client

import "context"

// Client is everything the provider needs from the runtime.
type Client interface {
	// System reports whether the runtime is running, its versions and the
	// default kernel. A stopped runtime is not an error: Running is false.
	System(ctx context.Context) (System, error)
	// Version returns the version of the command line tool, also while the
	// runtime is stopped.
	Version(ctx context.Context) (string, error)

	CreateNetwork(ctx context.Context, spec NetworkSpec) error
	// Network returns nil, without an error, if the network does not exist.
	Network(ctx context.Context, name string) (*Network, error)
	DeleteNetwork(ctx context.Context, name string) error
	Networks(ctx context.Context) ([]Network, error)

	CreateVolume(ctx context.Context, spec VolumeSpec) error
	// Volume returns nil, without an error, if the volume does not exist.
	Volume(ctx context.Context, name string) (*Volume, error)
	DeleteVolume(ctx context.Context, name string) error
	Volumes(ctx context.Context) ([]Volume, error)

	PullImage(ctx context.Context, spec ImageSpec) error
	// Image resolves a reference to a local image; nil, without an error, if
	// it has not been pulled.
	Image(ctx context.Context, reference string) (*Image, error)
	DeleteImage(ctx context.Context, name string) error
	Images(ctx context.Context) ([]Image, error)
	TagImage(ctx context.Context, source, target string) error
	// ImageName returns the fully qualified name a reference resolves to.
	ImageName(ctx context.Context, reference string) (string, error)

	BuildImage(ctx context.Context, spec BuildSpec) error
	BuilderRunning(ctx context.Context) (bool, error)
	// StartBuilder starts the builder, or recreates a running one whose
	// settings differ.
	StartBuilder(ctx context.Context, settings BuilderSettings) error
	StopBuilder(ctx context.Context) error

	CreateContainer(ctx context.Context, spec ContainerSpec) error
	StartContainer(ctx context.Context, name string) error
	StopContainer(ctx context.Context, name string) error
	// Container returns nil, without an error, if the container does not exist.
	Container(ctx context.Context, name string) (*Container, error)
	// DeleteContainer deletes the container, stopping it first if it runs.
	DeleteContainer(ctx context.Context, name string) error
	Containers(ctx context.Context) ([]Container, error)

	CreateMachine(ctx context.Context, spec MachineSpec) error
	// SetMachine changes settings, effective when the machine next starts.
	SetMachine(ctx context.Context, name string, settings MachineSettings) error
	StartMachine(ctx context.Context, name string) error
	StopMachine(ctx context.Context, name string) error
	SetDefaultMachine(ctx context.Context, name string) error
	// Machine returns nil, without an error, if the machine does not exist.
	Machine(ctx context.Context, name string) (*Machine, error)
	Machines(ctx context.Context) ([]Machine, error)
	DeleteMachine(ctx context.Context, name string) error

	// CreateCluster waits until the cluster is ready, or ctx is done.
	CreateCluster(ctx context.Context, spec ClusterSpec) error
	// Cluster returns nil, without an error, if the cluster does not exist.
	Cluster(ctx context.Context, name string) (*Cluster, error)
	ClusterKubeconfig(ctx context.Context, name string) ([]byte, error)
	DeleteCluster(ctx context.Context, name string) error
}

// System describes the runtime on this host.
type System struct {
	Running       bool
	Status        string // as reported by `container system status`: "running", "unregistered", …
	ClientVersion string
	ServerVersion string     // empty unless running
	Kernel        Kernel     // zero unless running
	Properties    Properties // zero unless running
}

// Properties are the runtime's defaults from its config.toml, as `container
// system property list` reports them merged with the built-in ones.
// Memory sizes are in bytes; unset values are zero.
type Properties struct {
	RegistryDomain  string
	DNSDomain       string // local DNS domain appended to container names
	VMInitImage     string
	NetworkSubnet   string
	NetworkSubnetV6 string
	Container       struct {
		CPUs        int64
		MemoryBytes int64
		DNS         DNS
	}
	Builder struct {
		CPUs        int64
		MemoryBytes int64
		Rosetta     bool
		Image       string
	}
	Machine struct {
		CPUs           int64
		MemoryBytes    int64
		HomeMount      string
		Virtualization bool
	}
}

// Kernel is the runtime's default kernel for new containers.
type Kernel struct {
	BinaryPath string
	URL        string
	Digest     string
}
