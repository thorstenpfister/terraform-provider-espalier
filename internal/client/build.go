package client

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// BuildSpec is what an image is built with.
type BuildSpec struct {
	Context    string // build context directory
	Dockerfile string // empty for <Context>/Dockerfile
	Tags       []string
	BuildArgs  map[string]string
	Target     string
	Platforms  []string // os/arch[/variant]; empty for the host's
	Labels     map[string]string
	Secrets    []BuildSecret
	SSH        bool // forward the SSH agent
	NoCache    bool
	Pull       bool // pull base images even if present
}

// BuildSecret makes a host environment variable or file available to the
// build as secret ID; exactly one of Env and File is set.
type BuildSecret struct {
	ID, Env, File string
}

// BuilderSettings size the builder container; zero values leave the
// runtime's defaults (2 CPUs, 2 GiB).
type BuilderSettings struct {
	CPUs   int64
	Memory string // e.g. "4G"
}

// builderJSON is one entry of `container builder status --format json`
// (container 1.7.1): the builder container, or none if it was deleted.
type builderJSON struct {
	Status struct {
		State string `json:"state"`
	} `json:"status"`
}

// buildErrorLines is how much of a failed build's output its error keeps:
// the failing step and BuildKit's message are at the end.
const buildErrorLines = 20

// BuildImage builds an image with the builder, which the runtime starts if
// it is not running.
func (c CLI) BuildImage(ctx context.Context, spec BuildSpec) error {
	// --quiet hangs when the output is not a terminal (1.7.1).
	args := []string{"build", "--progress", "plain"}
	if spec.Dockerfile != "" {
		args = append(args, "--file", spec.Dockerfile)
	}
	for _, t := range spec.Tags {
		args = append(args, "--tag", t)
	}
	args = append(args, keyValueArgs("--build-arg", spec.BuildArgs)...)
	args = append(args, keyValueArgs("--label", spec.Labels)...)
	if spec.Target != "" {
		args = append(args, "--target", spec.Target)
	}
	if len(spec.Platforms) > 0 {
		args = append(args, "--platform", strings.Join(spec.Platforms, ","))
	}
	for _, s := range spec.Secrets {
		source := "env=" + s.Env
		if s.File != "" {
			source = "src=" + s.File
		}
		args = append(args, "--secret", "id="+s.ID+","+source)
	}
	if spec.SSH {
		args = append(args, "--ssh", "default")
	}
	if spec.NoCache {
		args = append(args, "--no-cache")
	}
	if spec.Pull {
		args = append(args, "--pull")
	}
	_, err := c.Run(ctx, append(args, spec.Context)...)
	var exit *ExitError
	if errors.As(err, &exit) {
		lines := strings.Split(exit.Stderr, "\n")
		exit.Stderr = strings.Join(lines[max(0, len(lines)-buildErrorLines):], "\n")
	}
	return err
}

// keyValueArgs repeats flag for each key=value, ordered by key.
func keyValueArgs(flag string, m map[string]string) []string {
	var args []string
	for _, k := range slices.Sorted(maps.Keys(m)) {
		args = append(args, flag, k+"="+m[k])
	}
	return args
}

// BuilderRunning reports whether the builder container runs.
func (c CLI) BuilderRunning(ctx context.Context) (bool, error) {
	var list []builderJSON
	if err := c.json(ctx, &list, "builder", "status", "--format", "json"); err != nil {
		return false, err
	}
	return len(list) == 1 && list[0].Status.State == "running", nil
}

// StartBuilder starts the builder with s. A running builder with other
// settings is recreated, one with the same settings is left alone; zero
// settings mean the defaults.
func (c CLI) StartBuilder(ctx context.Context, s BuilderSettings) error {
	args := []string{"builder", "start"}
	if s.CPUs != 0 {
		args = append(args, "--cpus", strconv.FormatInt(s.CPUs, 10))
	}
	if s.Memory != "" {
		args = append(args, "--memory", s.Memory)
	}
	_, err := c.Run(ctx, args...)
	return err
}

func (c CLI) StopBuilder(ctx context.Context) error {
	_, err := c.Run(ctx, "builder", "stop")
	return err
}
