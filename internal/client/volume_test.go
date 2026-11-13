package client

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

// Captured from container 1.7.1: a default volume and one with a size and label.
const volumesJSON = `[
{"configuration":{"driver":"local","format":"ext4","labels":{},"name":"plain","options":{},"sizeInBytes":549755813888,"source":"/vols/plain/volume.img"},"id":"plain"},
{"configuration":{"driver":"local","format":"ext4","labels":{"a":"b"},"name":"sized","options":{"size":"2G"},"sizeInBytes":2147483648,"source":"/vols/sized/volume.img"},"id":"sized"}
]`

func TestVolume(t *testing.T) {
	f := fakeRunner{"volume list --format json": {out: volumesJSON}}
	c := CLI{Run: f.run}
	for name, want := range map[string]*Volume{
		"plain": {VolumeSpec: VolumeSpec{Name: "plain", Labels: map[string]string{}},
			SizeBytes: 549755813888, Format: "ext4", Source: "/vols/plain/volume.img"},
		"sized": {VolumeSpec: VolumeSpec{Name: "sized", Size: "2G", Labels: map[string]string{"a": "b"}},
			SizeBytes: 2147483648, Format: "ext4", Source: "/vols/sized/volume.img"},
		"missing": nil,
	} {
		got, err := c.Volume(context.Background(), name)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: got %+v, want %+v", name, got, want)
		}
	}
}

func TestCreateVolumeArgs(t *testing.T) {
	var got string
	run := func(_ context.Context, args ...string) ([]byte, error) {
		got = strings.Join(args, " ")
		return nil, nil
	}
	err := CLI{Run: run}.CreateVolume(context.Background(), VolumeSpec{Name: "v", Size: "2G", Labels: map[string]string{"a": "b"}})
	if err != nil {
		t.Fatal(err)
	}
	if want := "volume create -s 2G --label a=b v"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
