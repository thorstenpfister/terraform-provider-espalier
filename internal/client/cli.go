package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
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

// ExecRunner runs the binary at path (looked up on PATH if it has no slash).
func ExecRunner(path string) Runner {
	return func(ctx context.Context, args ...string) ([]byte, error) {
		var stdout, stderr bytes.Buffer
		cmd := exec.CommandContext(ctx, path, args...)
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

func (c CLI) System(ctx context.Context) (System, error) {
	var version []struct {
		AppName string `json:"appName"`
		Version string `json:"version"`
	}
	// `system version` reports the client even when the services are down.
	if err := c.json(ctx, &version, "system", "version", "--format", "json"); err != nil {
		return System{}, err
	}
	var sys System
	for _, v := range version {
		if v.AppName == "container" {
			sys.ClientVersion = v.Version
		}
	}

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

	var props struct {
		Kernel struct {
			BinaryPath string `json:"binaryPath"`
			URL        string `json:"url"`
			Digest     string `json:"digest"`
		} `json:"kernel"`
	}
	if err := c.json(ctx, &props, "system", "property", "list", "--format", "json"); err != nil {
		return System{}, err
	}
	sys.Kernel = Kernel(props.Kernel)
	return sys, nil
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
