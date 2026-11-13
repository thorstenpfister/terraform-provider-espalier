package provider

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/thorstenpfister/terraform-provider-espalier/internal/client"
)

// fakeBuilderClient records the builder calls; the rest of client.Client
// is not used.
type fakeBuilderClient struct {
	client.Client
	running bool
	calls   []string
}

func (f *fakeBuilderClient) BuilderRunning(context.Context) (bool, error) { return f.running, nil }

func (f *fakeBuilderClient) StartBuilder(_ context.Context, s client.BuilderSettings) error {
	f.running = true
	f.calls = append(f.calls, "start")
	return nil
}

func (f *fakeBuilderClient) StopBuilder(context.Context) error {
	f.running = false
	f.calls = append(f.calls, "stop")
	return nil
}

func TestBuilder(t *testing.T) {
	ctx := context.Background()
	for _, c := range []struct {
		name    string
		running bool
		b       *builder
		want    string
	}{
		{"stopped: started and stopped once for nested builds", false, &builder{}, "start build build stop"},
		{"already running: left running", true, &builder{}, "build build"},
		{"keep running", false, &builder{keepRunning: true}, "start build build"},
		{"settings applied to a running builder", true, &builder{settings: client.BuilderSettings{CPUs: 4}}, "start build build"},
	} {
		f := &fakeBuilderClient{running: c.running}
		buildFn := func() error { f.calls = append(f.calls, "build"); return nil }
		// The second build begins while the first is in progress.
		err := c.b.build(ctx, f, func() error {
			if err := c.b.build(ctx, f, buildFn); err != nil {
				return err
			}
			return buildFn()
		})
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(f.calls, " "); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}

	f := &fakeBuilderClient{}
	failed := errors.New("failed")
	if err := (&builder{}).build(ctx, f, func() error { return failed }); !errors.Is(err, failed) || f.running {
		t.Errorf("failed build: got %v, running %v; want the build's error and a stopped builder", err, f.running)
	}
}
