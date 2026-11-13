package client

import (
	"context"
	"errors"
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
	propsJSON   = `{"kernel":{"binaryPath":"opt/kata/share/kata-containers/vmlinux-6.18.56-208-debug","digest":"sha256:2c5b","url":"https://github.com/kata-containers/kata-containers/releases/download/4.3.1/kata-static-4.3.1-arm64.tar.zst"}}`
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
	if got != want {
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
	if got != want {
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
