package client

import (
	"cmp"
	"context"
	"fmt"
	"strings"
)

// ImageSpec is what an image is pulled with.
type ImageSpec struct {
	Reference string // e.g. "busybox:1.37" or "ghcr.io/org/app@sha256:…"
	Platform  string // os/arch[/variant]; empty pulls every platform
}

// Image is a local image.
type Image struct {
	Name      string   // fully qualified, e.g. "docker.io/library/busybox:1.37"
	Digest    string   // digest of the image index or manifest
	Platforms []string // os/arch[/variant] of the image's variants
}

// imageJSON is one entry of `container image list --format json`
// (container 1.7.1).
type imageJSON struct {
	Configuration struct {
		Name       string `json:"name"`
		Descriptor struct {
			Digest string `json:"digest"`
		} `json:"descriptor"`
	} `json:"configuration"`
	Variants []struct {
		Platform struct {
			OS           string `json:"os"`
			Architecture string `json:"architecture"`
			Variant      string `json:"variant"`
		} `json:"platform"`
	} `json:"variants"`
}

func (c CLI) PullImage(ctx context.Context, spec ImageSpec) error {
	args := []string{"image", "pull", "--progress", "none"}
	if spec.Platform != "" {
		args = append(args, "--platform", spec.Platform)
	}
	_, err := c.Run(ctx, append(args, spec.Reference)...)
	return err
}

// Image returns the local image a reference resolves to, or nil if it has
// not been pulled.
func (c CLI) Image(ctx context.Context, reference string) (*Image, error) {
	domain, err := c.defaultRegistry(ctx)
	if err != nil {
		return nil, err
	}
	name := NormalizeReference(reference, domain)
	var list []imageJSON
	if err := c.json(ctx, &list, "image", "list", "--format", "json"); err != nil {
		return nil, err
	}
	for _, i := range list {
		if i.Configuration.Name != name {
			continue
		}
		img := &Image{Name: name, Digest: i.Configuration.Descriptor.Digest}
		for _, v := range i.Variants {
			p := v.Platform
			if p.OS == "unknown" { // attestation manifests
				continue
			}
			platform := p.OS + "/" + p.Architecture
			if p.Variant != "" {
				platform += "/" + p.Variant
			}
			img.Platforms = append(img.Platforms, platform)
		}
		return img, nil
	}
	return nil, nil
}

func (c CLI) DeleteImage(ctx context.Context, name string) error {
	if _, err := c.Run(ctx, "image", "delete", name); err != nil {
		return fmt.Errorf("deleting image %s: %w", name, err)
	}
	return nil
}

// defaultRegistry is the registry short references resolve to (the
// runtime's registry.domain property, docker.io unless changed).
func (c CLI) defaultRegistry(ctx context.Context) (string, error) {
	var props struct {
		Registry struct {
			Domain string `json:"domain"`
		} `json:"registry"`
	}
	if err := c.json(ctx, &props, "system", "property", "list", "--format", "json"); err != nil {
		return "", err
	}
	return cmp.Or(props.Registry.Domain, "docker.io"), nil
}

// NormalizeReference qualifies an image reference the way the runtime stores
// it: a missing registry becomes domain (with library/ for single names on
// docker.io), and a missing tag and digest becomes :latest.
func NormalizeReference(ref, domain string) string {
	name, digest, hasDigest := strings.Cut(ref, "@")
	// Like Docker: the first component is a registry if it looks like a host.
	first, _, nested := strings.Cut(name, "/")
	hasRegistry := nested && (strings.ContainsAny(first, ".:") || first == "localhost")
	if !hasRegistry {
		if !nested && domain == "docker.io" {
			name = "library/" + name
		}
		name = domain + "/" + name
	}
	if hasDigest {
		return name + "@" + digest
	}
	if !strings.Contains(name[strings.LastIndex(name, "/")+1:], ":") {
		name += ":latest"
	}
	return name
}
