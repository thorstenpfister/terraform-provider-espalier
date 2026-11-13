package client

import (
	"context"
	"reflect"
	"testing"
)

func TestNormalizeReference(t *testing.T) {
	for _, c := range []struct{ ref, domain, want string }{
		{"busybox", "docker.io", "docker.io/library/busybox:latest"},
		{"busybox:1.37", "docker.io", "docker.io/library/busybox:1.37"},
		{"grafana/grafana:13.3.1", "docker.io", "docker.io/grafana/grafana:13.3.1"},
		{"docker.io/library/busybox:1.37", "docker.io", "docker.io/library/busybox:1.37"},
		{"ghcr.io/org/app", "docker.io", "ghcr.io/org/app:latest"},
		{"localhost/app:1", "docker.io", "localhost/app:1"},
		{"zot.lab:5000/team/app:2", "docker.io", "zot.lab:5000/team/app:2"},
		{"busybox@sha256:abc", "docker.io", "docker.io/library/busybox@sha256:abc"},
		{"app:1", "ghcr.io", "ghcr.io/app:1"},
	} {
		if got := NormalizeReference(c.ref, c.domain); got != c.want {
			t.Errorf("NormalizeReference(%q, %q) = %q, want %q", c.ref, c.domain, got, c.want)
		}
	}
}

// Trimmed from container 1.7.1: an index with two platforms and an
// attestation manifest.
const imagesJSON = `[{"configuration":{"descriptor":{"digest":"sha256:a7db","mediaType":"application/vnd.oci.image.index.v1+json","size":9535},"name":"docker.io/library/busybox:1.37"},"id":"a7db",
"variants":[{"platform":{"architecture":"amd64","os":"linux"},"size":2212583},{"platform":{"architecture":"unknown","os":"unknown"},"size":2752},{"platform":{"architecture":"arm64","os":"linux","variant":"v8"},"size":1901860}]}]`

func TestImage(t *testing.T) {
	f := fakeRunner{
		"system property list --format json": {out: `{"registry":{"domain":"docker.io"}}`},
		"image list --format json":           {out: imagesJSON},
	}
	c := CLI{Run: f.run}
	got, err := c.Image(context.Background(), "busybox:1.37")
	if err != nil {
		t.Fatal(err)
	}
	want := &Image{Name: "docker.io/library/busybox:1.37", Digest: "sha256:a7db", Platforms: []string{"linux/amd64", "linux/arm64/v8"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if got, _ := c.Image(context.Background(), "busybox:1.36"); got != nil {
		t.Errorf("busybox:1.36: got %+v, want nil", got)
	}
}
