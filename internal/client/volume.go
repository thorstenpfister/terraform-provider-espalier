package client

import (
	"context"
	"fmt"
	"maps"
	"slices"
)

// VolumeSpec is what a volume is created with.
type VolumeSpec struct {
	Name   string
	Size   string            // e.g. "2G"; empty for the runtime's default (a sparse 512 GiB image)
	Labels map[string]string // may be nil
	// Options are driver-specific; the runtime keeps the requested size
	// among them as "size", which Volume leaves out.
	Options map[string]string
}

// Volume is a volume as the runtime reports it.
type Volume struct {
	VolumeSpec
	SizeBytes int64
	Format    string // filesystem, e.g. "ext4"
	Source    string // host path of the disk image
}

// volumeJSON is one entry of `container volume list --format json`
// (container 1.7.1). A requested size is kept verbatim in options.size.
type volumeJSON struct {
	ID            string `json:"id"`
	Configuration struct {
		Format      string            `json:"format"`
		Labels      map[string]string `json:"labels"`
		Options     map[string]string `json:"options"`
		SizeInBytes int64             `json:"sizeInBytes"`
		Source      string            `json:"source"`
	} `json:"configuration"`
}

func (c CLI) CreateVolume(ctx context.Context, spec VolumeSpec) error {
	args := []string{"volume", "create"}
	if spec.Size != "" {
		args = append(args, "-s", spec.Size)
	}
	for _, k := range slices.Sorted(maps.Keys(spec.Labels)) {
		args = append(args, "--label", k+"="+spec.Labels[k])
	}
	for _, k := range slices.Sorted(maps.Keys(spec.Options)) {
		args = append(args, "--opt", k+"="+spec.Options[k])
	}
	_, err := c.Run(ctx, append(args, spec.Name)...)
	return err
}

// Volume returns the named volume, or nil if it does not exist.
func (c CLI) Volume(ctx context.Context, name string) (*Volume, error) {
	volumes, err := c.Volumes(ctx)
	if err != nil {
		return nil, err
	}
	for _, v := range volumes {
		if v.Name == name {
			return &v, nil
		}
	}
	return nil, nil
}

// Volumes returns all volumes.
func (c CLI) Volumes(ctx context.Context) ([]Volume, error) {
	var list []volumeJSON
	if err := c.json(ctx, &list, "volume", "list", "--format", "json"); err != nil {
		return nil, err
	}
	volumes := make([]Volume, 0, len(list))
	for _, v := range list {
		cfg := v.Configuration
		opts := maps.Clone(cfg.Options)
		delete(opts, "size")
		if len(opts) == 0 {
			opts = nil
		}
		volumes = append(volumes, Volume{
			VolumeSpec: VolumeSpec{Name: v.ID, Size: cfg.Options["size"], Labels: cfg.Labels, Options: opts},
			SizeBytes:  cfg.SizeInBytes,
			Format:     cfg.Format,
			Source:     cfg.Source,
		})
	}
	return volumes, nil
}

func (c CLI) DeleteVolume(ctx context.Context, name string) error {
	if _, err := c.Run(ctx, "volume", "delete", name); err != nil {
		return fmt.Errorf("deleting volume %s: %w", name, err)
	}
	return nil
}
