package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// Runner executes the container CLI with the given arguments and returns its
// standard output. A non-zero exit is returned as an *ExitError.
type Runner func(ctx context.Context, args ...string) ([]byte, error)

// ExitError is a CLI invocation that ran but failed.
type ExitError struct {
	Args   []string
	Code   int
	Stderr string
}

func (e *ExitError) Error() string {
	return fmt.Sprintf("container %s: exit %d: %s", strings.Join(e.Args, " "), e.Code, e.Stderr)
}

type envKey struct{}

// WithEnv returns a context whose CLI invocations get these environment
// variables (KEY=value) in addition to the provider's.
func WithEnv(ctx context.Context, env ...string) context.Context {
	prior, _ := ctx.Value(envKey{}).([]string)
	return context.WithValue(ctx, envKey{}, append(append([]string(nil), prior...), env...))
}

// ExecRunner runs the binary at path (looked up on PATH if it has no slash),
// with the environment from WithEnv.
func ExecRunner(path string) Runner {
	return func(ctx context.Context, args ...string) ([]byte, error) {
		var stdout, stderr bytes.Buffer
		cmd := exec.CommandContext(ctx, path, args...)
		if env, ok := ctx.Value(envKey{}).([]string); ok {
			cmd.Env = append(os.Environ(), env...)
		}
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return stdout.Bytes(), &ExitError{Args: args, Code: exit.ExitCode(), Stderr: strings.TrimSpace(stderr.String())}
		}
		return stdout.Bytes(), err
	}
}

// CLI implements Client with the container command line tool.
type CLI struct {
	Run Runner
}

var _ Client = CLI{}

func (c CLI) Version(ctx context.Context) (string, error) {
	var version []struct {
		AppName string `json:"appName"`
		Version string `json:"version"`
	}
	// `system version` reports the client even when the services are down.
	if err := c.json(ctx, &version, "system", "version", "--format", "json"); err != nil {
		return "", err
	}
	for _, v := range version {
		if v.AppName == "container" {
			return v.Version, nil
		}
	}
	return "", errors.New("container system version: no version for container")
}

func (c CLI) System(ctx context.Context) (System, error) {
	v, err := c.Version(ctx)
	if err != nil {
		return System{}, err
	}
	sys := System{ClientVersion: v}

	var status struct {
		Status string `json:"status"`
		Server struct {
			Version string `json:"version"`
		} `json:"server"`
	}
	// While the services are stopped the CLI exits non-zero but still prints
	// the status, e.g. {"status":"unregistered"} (1.7.1).
	out, err := c.Run(ctx, "system", "status", "--format", "json")
	var exit *ExitError
	if err != nil && !errors.As(err, &exit) {
		return System{}, err
	}
	if json.Unmarshal(out, &status) != nil || status.Status == "" {
		if exit != nil {
			return System{}, exit
		}
		return System{}, fmt.Errorf("container system status: unexpected output %q", out)
	}
	sys.Status = status.Status
	sys.Running = status.Status == "running"
	if !sys.Running {
		return sys, nil
	}
	sys.ServerVersion = status.Server.Version

	var props propertiesJSON
	if err := c.json(ctx, &props, "system", "property", "list", "--format", "json"); err != nil {
		return System{}, err
	}
	sys.Kernel = Kernel(props.Kernel)
	if sys.Properties, err = props.properties(); err != nil {
		return System{}, err
	}
	return sys, nil
}

// propertiesJSON is the output of `container system property list --format
// json` (container 1.7.1; keys from its config.toml reference).
type propertiesJSON struct {
	Build struct {
		CPUs    int64  `json:"cpus"`
		Memory  string `json:"memory"`
		Rosetta bool   `json:"rosetta"`
		Image   string `json:"image"`
	} `json:"build"`
	Container struct {
		CPUs   int64  `json:"cpus"`
		Memory string `json:"memory"`
		DNS    struct {
			Domain        string   `json:"domain"`
			Nameservers   []string `json:"nameservers"`
			SearchDomains []string `json:"searchDomains"`
			Options       []string `json:"options"`
		} `json:"dns"`
	} `json:"container"`
	DNS struct {
		Domain string `json:"domain"`
	} `json:"dns"`
	Kernel struct {
		BinaryPath string `json:"binaryPath"`
		URL        string `json:"url"`
		Digest     string `json:"digest"`
	} `json:"kernel"`
	Machine struct {
		CPUs           int64  `json:"cpus"`
		Memory         string `json:"memory"`
		HomeMount      string `json:"homeMount"`
		Virtualization bool   `json:"virtualization"`
	} `json:"machine"`
	Network struct {
		Subnet   string `json:"subnet"`
		SubnetV6 string `json:"subnetv6"`
	} `json:"network"`
	Registry struct {
		Domain string `json:"domain"`
	} `json:"registry"`
	VMInit struct {
		Image string `json:"image"`
	} `json:"vminit"`
}

func (j propertiesJSON) properties() (Properties, error) {
	p := Properties{
		RegistryDomain:  j.Registry.Domain,
		DNSDomain:       j.DNS.Domain,
		VMInitImage:     j.VMInit.Image,
		NetworkSubnet:   j.Network.Subnet,
		NetworkSubnetV6: j.Network.SubnetV6,
	}
	d := j.Container.DNS
	p.Container.CPUs = j.Container.CPUs
	p.Container.DNS = DNS{Nameservers: d.Nameservers, Domain: d.Domain, Search: d.SearchDomains, Options: d.Options}
	p.Builder.CPUs, p.Builder.Rosetta, p.Builder.Image = j.Build.CPUs, j.Build.Rosetta, j.Build.Image
	p.Machine.CPUs, p.Machine.HomeMount, p.Machine.Virtualization = j.Machine.CPUs, j.Machine.HomeMount, j.Machine.Virtualization
	for _, m := range []struct {
		value string
		bytes *int64
	}{{j.Container.Memory, &p.Container.MemoryBytes}, {j.Build.Memory, &p.Builder.MemoryBytes}, {j.Machine.Memory, &p.Machine.MemoryBytes}} {
		b, err := ParseMemorySize(m.value)
		if err != nil {
			return Properties{}, err
		}
		*m.bytes = b
	}
	return p, nil
}

var memorySizePattern = regexp.MustCompile(`^([0-9]+)\s*([kmgtp]?)(i?b)?$`)

// ParseMemorySize parses a size as config.toml writes it, e.g. "2048mb",
// "1g" or "12gb", in binary units; "" is 0.
func ParseMemorySize(s string) (int64, error) {
	if s == "" {
		return 0, nil
	}
	m := memorySizePattern.FindStringSubmatch(strings.ToLower(strings.TrimSpace(s)))
	if m == nil {
		return 0, fmt.Errorf("unexpected memory size %q", s)
	}
	n, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("unexpected memory size %q: %w", s, err)
	}
	return n << map[string]uint{"": 0, "k": 10, "m": 20, "g": 30, "t": 40, "p": 50}[m[2]], nil
}

// json runs the CLI and decodes its standard output into v.
func (c CLI) json(ctx context.Context, v any, args ...string) error {
	out, err := c.Run(ctx, args...)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(out, v); err != nil {
		return fmt.Errorf("container %s: decoding output: %w", strings.Join(args, " "), err)
	}
	return nil
}
