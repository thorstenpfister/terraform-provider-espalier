package client

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// fakeRunner answers CLI invocations from a table keyed by the joined args.
type fakeRunner map[string]struct {
	out string
	err error
}

func (f fakeRunner) run(_ context.Context, args ...string) ([]byte, error) {
	r, ok := f[strings.Join(args, " ")]
	if !ok {
		return nil, errors.New("unexpected invocation: " + strings.Join(args, " "))
	}
	return []byte(r.out), r.err
}

// Captured from container 1.7.1.
const (
	versionJSON = `[{"appName":"container","buildType":"release","commit":"unspecified","version":"1.7.1"},{"appName":"container-apiserver","buildType":"release","commit":"unspecified","version":"1.7.1"}]`
	statusJSON  = `{"client":{"appName":"container","version":"1.7.1"},"server":{"appName":"container-apiserver","version":"1.7.1"},"status":"running"}`
	propsJSON   = `{"build":{"cpus":2,"image":"ghcr.io/apple/container-builder-shim/builder:0.13.3","memory":"2048mb","rosetta":true},"container":{"cpus":4,"memory":"1gb","dns":{"nameservers":["1.1.1.1"]}},"dns":{"domain":"test"},"kernel":{"binaryPath":"opt/kata/share/kata-containers/vmlinux-6.18.56-208-debug","digest":"sha256:2c5b","url":"https://github.com/kata-containers/kata-containers/releases/download/4.3.1/kata-static-4.3.1-arm64.tar.zst"},"machine":{"cpus":5,"homeMount":"rw","memory":"12gb","virtualization":false},"network":{"subnet":"192.168.100.0/24"},"registry":{"domain":"docker.io"},"vminit":{"image":"ghcr.io/apple/containerization/vminit:0.54.0"}}`
)

func TestSystemRunning(t *testing.T) {
	f := fakeRunner{
		"system version --format json":       {out: versionJSON},
		"system status --format json":        {out: statusJSON},
		"system property list --format json": {out: propsJSON},
	}
	got, err := CLI{Run: f.run}.System(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := System{
		Running:       true,
		Status:        "running",
		ClientVersion: "1.7.1",
		ServerVersion: "1.7.1",
		Kernel: Kernel{
			BinaryPath: "opt/kata/share/kata-containers/vmlinux-6.18.56-208-debug",
			URL:        "https://github.com/kata-containers/kata-containers/releases/download/4.3.1/kata-static-4.3.1-arm64.tar.zst",
			Digest:     "sha256:2c5b",
		},
	}
	p := &want.Properties
	p.RegistryDomain, p.DNSDomain, p.VMInitImage, p.NetworkSubnet = "docker.io", "test", "ghcr.io/apple/containerization/vminit:0.54.0", "192.168.100.0/24"
	p.Container.CPUs, p.Container.MemoryBytes, p.Container.DNS = 4, 1<<30, DNS{Nameservers: []string{"1.1.1.1"}}
	p.Builder.CPUs, p.Builder.MemoryBytes, p.Builder.Rosetta, p.Builder.Image = 2, 2048<<20, true, "ghcr.io/apple/container-builder-shim/builder:0.13.3"
	p.Machine.CPUs, p.Machine.MemoryBytes, p.Machine.HomeMount = 5, 12<<30, "rw"
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestSystemStopped(t *testing.T) {
	f := fakeRunner{
		"system version --format json": {out: versionJSON},
		// Captured from container 1.7.1 after `container system stop`.
		"system status --format json": {out: `{"status":"unregistered"}`, err: &ExitError{Code: 1}},
	}
	got, err := CLI{Run: f.run}.System(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := System{Status: "unregistered", ClientVersion: "1.7.1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestSystemStatusUnreadable(t *testing.T) {
	f := fakeRunner{
		"system version --format json": {out: versionJSON},
		"system status --format json":  {err: &ExitError{Code: 1, Stderr: "boom"}},
	}
	if _, err := (CLI{Run: f.run}).System(context.Background()); err == nil {
		t.Fatal("want an error when status prints nothing usable")
	}
}

func TestSystemCLIMissing(t *testing.T) {
	_, err := CLI{Run: ExecRunner("/nonexistent/container")}.System(context.Background())
	if err == nil {
		t.Fatal("want an error for a missing CLI")
	}
}

func TestParseMemorySize(t *testing.T) {
	for in, want := range map[string]int64{"": 0, "2048mb": 2048 << 20, "1gb": 1 << 30, "1g": 1 << 30, "12GB": 12 << 30, "512": 512, "4GiB": 4 << 30} {
		if got, err := ParseMemorySize(in); err != nil || got != want {
			t.Errorf("ParseMemorySize(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	if _, err := ParseMemorySize("lots"); err == nil {
		t.Error(`ParseMemorySize("lots"): no error`)
	}
}
