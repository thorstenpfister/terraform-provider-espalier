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

	CreateNetwork(ctx context.Context, spec NetworkSpec) error
	// Network returns nil, without an error, if the network does not exist.
	Network(ctx context.Context, name string) (*Network, error)
	DeleteNetwork(ctx context.Context, name string) error

	CreateVolume(ctx context.Context, spec VolumeSpec) error
	// Volume returns nil, without an error, if the volume does not exist.
	Volume(ctx context.Context, name string) (*Volume, error)
	DeleteVolume(ctx context.Context, name string) error
}

// System describes the runtime on this host.
type System struct {
	Running       bool
	Status        string // as reported by `container system status`: "running", "unregistered", …
	ClientVersion string
	ServerVersion string // empty unless running
	Kernel        Kernel // zero unless running
}

// Kernel is the runtime's default kernel for new containers.
type Kernel struct {
	BinaryPath string
	URL        string
	Digest     string
}
