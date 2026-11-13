package provider

import (
	"context"
	"sync"

	"github.com/thorstenpfister/terraform-provider-espalier/internal/client"
)

// builder runs builds on the runtime's single builder container. The
// builder keeps running after a build, so it stops it again after the last
// build of a run, unless it already ran before or keepRunning is set.
type builder struct {
	settings    client.BuilderSettings
	keepRunning bool

	mu      sync.Mutex
	active  int  // builds in progress
	started bool // the builder was started for the builds in progress
}

// build runs fn while the builder is up.
func (b *builder) build(ctx context.Context, c client.Client, fn func() error) error {
	if err := b.begin(ctx, c); err != nil {
		return err
	}
	err := fn()
	if stopErr := b.end(ctx, c); err == nil {
		err = stopErr
	}
	return err
}

// begin starts the builder for the first of concurrent builds, so they do
// not race to start it, and applies the settings to a running builder.
func (b *builder) begin(ctx context.Context, c client.Client) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.active == 0 {
		running, err := c.BuilderRunning(ctx)
		if err != nil {
			return err
		}
		if !running || b.settings != (client.BuilderSettings{}) {
			if err := c.StartBuilder(ctx, b.settings); err != nil {
				return err
			}
		}
		b.started = !running
	}
	b.active++
	return nil
}

func (b *builder) end(ctx context.Context, c client.Client) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.active--
	if b.active > 0 || !b.started || b.keepRunning {
		return nil
	}
	// The apply may have been cancelled: stop the builder regardless.
	return c.StopBuilder(context.WithoutCancel(ctx))
}
