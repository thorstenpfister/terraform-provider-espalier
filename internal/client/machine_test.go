package client

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

// From container 1.7.1.
const (
	machineListJSON    = `[{"memory":1073741824,"ipAddress":"192.168.64.120","cpus":2,"default":true,"id":"dev","status":"running","diskSize":43307008,"createdDate":"2026-11-13T10:59:52Z"}]`
	machineInspectJSON = `[{"containerId":"dev-b5a446","cpus":2,"createdDate":"2026-11-13T10:59:52Z","diskSize":43614208,"homeMount":"none","id":"dev",
"image":{"descriptor":{"digest":"sha256:bfe7","mediaType":"application/vnd.oci.image.index.v1+json","size":9218},"reference":"docker.io/library/alpine:3.22"},
"ipAddress":"192.168.64.120","memory":1073741824,"platform":{"architecture":"arm64","os":"linux"},"startedDate":"2026-11-13T10:59:52Z","status":"running",
"userSetup":{"gid":20,"home":"/home/me","uid":501,"username":"me"}}]`
)

func TestMachine(t *testing.T) {
	f := fakeRunner{
		"machine list --format json": {out: machineListJSON},
		"machine inspect dev":        {out: machineInspectJSON},
	}
	c := CLI{Run: f.run}
	got, err := c.Machine(context.Background(), "dev")
	if err != nil {
		t.Fatal(err)
	}
	want := &Machine{
		Name: "dev", Image: "docker.io/library/alpine:3.22", Digest: "sha256:bfe7", Platform: "linux/arm64",
		User: "me", UID: 501, GID: 20, Home: "/home/me", CPUs: 2, MemoryBytes: 1 << 30, HomeMount: "none",
		Default: true, Status: "running", IPAddress: "192.168.64.120", DiskSize: 43614208,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
	if got, err := c.Machine(context.Background(), "missing"); got != nil || err != nil {
		t.Errorf("missing: got %+v, %v; want nil, nil", got, err)
	}
	if all, err := c.Machines(context.Background()); err != nil || !reflect.DeepEqual(all, []Machine{*want}) {
		t.Errorf("Machines: got %+v, %v", all, err)
	}
}

func TestMachineArgs(t *testing.T) {
	var calls []string
	run := func(_ context.Context, args ...string) ([]byte, error) {
		calls = append(calls, strings.Join(args, " "))
		return nil, nil
	}
	c := CLI{Run: run}
	uid, gid, on := int64(1000), int64(0), true
	ctx := context.Background()
	if err := c.CreateMachine(ctx, MachineSpec{Name: "dev", Image: "alpine:3.22", Platform: "linux/arm64", User: "dev", UID: &uid, GID: &gid, Home: "/home/dev", Scheme: "http", SetDefault: true, Boot: true}); err != nil {
		t.Fatal(err)
	}
	if err := c.CreateMachine(ctx, MachineSpec{Name: "dev2", Image: "alpine:3.22", Settings: MachineSettings{CPUs: 2, Memory: "4G", HomeMount: "ro", Virtualization: &on, Kernel: "/k"}}); err != nil {
		t.Fatal(err)
	}
	if err := c.SetMachine(ctx, "dev", MachineSettings{CPUs: 2, Memory: "4G", HomeMount: "ro", Virtualization: &on, Kernel: "/k"}); err != nil {
		t.Fatal(err)
	}
	if err := c.SetMachine(ctx, "dev", MachineSettings{}); err != nil { // nothing to set: no call
		t.Fatal(err)
	}
	if err := c.StartMachine(ctx, "dev"); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"machine create --name dev --progress none --platform linux/arm64 --user dev --home /home/dev --scheme http --uid 1000 --gid 0 --set-default alpine:3.22",
		"machine create --name dev2 --progress none --cpus 2 --memory 4G --home-mount ro --kernel /k --virtualization --no-boot alpine:3.22",
		"machine set --name dev cpus=2 memory=4G home-mount=ro kernel=/k virtualization=true",
		"machine run --name dev true",
	}
	if !reflect.DeepEqual(calls, want) {
		t.Errorf("got  %q\nwant %q", calls, want)
	}
}
