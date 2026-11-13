package client

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

// Trimmed from container 1.7.1: the probe container from
// TestCreateContainerArgs, running.
const containersJSON = `[{"id":"probe","configuration":{
"capAdd":["CAP_NET_RAW"],"capDrop":["CAP_MKNOD"],"id":"probe",
"image":{"reference":"docker.io/library/busybox:1.37"},
"initProcess":{"arguments":["-c","sleep 3600"],"environment":["PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin","FOO=bar"],"executable":"/bin/sh","workingDirectory":"/tmp",
 "user":{"raw":{"userString":"1000:1000"}},"rlimits":[{"hard":2048,"limit":"RLIMIT_NOFILE","soft":1024}]},
"labels":{"a":"b"},
"mounts":[
 {"destination":"/data","options":[],"source":"/vols/v/volume.img","type":{"volume":{"cache":{"on":{}},"format":"ext4","name":"v","sync":{"fsync":{}}}}},
 {"destination":"/bind","options":["ro"],"source":"/host/dir","type":{"virtiofs":{}}},
 {"destination":"/scratch","options":[],"source":"tmpfs","type":{"tmpfs":{}}}],
"networks":[
 {"network":"net","options":{"aliases":["probe.espalier.test"],"hostname":"probe","macAddress":"02:42:ac:11:00:02","mtu":1400}},
 {"network":"default","options":{"hostname":"probe","mtu":1280}}],
"platform":{"architecture":"arm64","os":"linux","variant":"v8"},
"publishedPorts":[{"containerPort":80,"count":1,"hostAddress":"127.0.0.1","hostPort":18081,"proto":"tcp"}],
"resources":{"cpuOverhead":1,"cpus":2,"memoryInBytes":268435456},
"dns":{"domain":"espalier.test","nameservers":["1.1.1.1"],"options":["ndots:2"],"searchDomains":["a.test"]},
"readOnly":true,"useInit":true,"rosetta":true,"ssh":true,"shmSize":134217728,
"publishedSockets":[{"containerPath":"/run/s.sock","hostPath":"/host/s.sock"}],
"runtimeHandler":"container-runtime-linux","virtualization":true,
"readonlyPaths":["/proc/bus","/opt"],"maskedPaths":[]},
"status":{"state":"running","networks":[
 {"network":"net","ipv4Address":"192.168.232.2/24","ipv4Gateway":"192.168.232.1","ipv6Address":"fd6a::2/64","macAddress":"02:42:ac:11:00:02"},
 {"network":"default","ipv4Address":"192.168.64.2/24","ipv4Gateway":"192.168.64.1","ipv6Address":"fd55::2/64","macAddress":"f2:9f:c7:e6:b6:97"}]}}]`

const busyboxImageJSON = `[{"configuration":{"descriptor":{"digest":"sha256:a7db"},"name":"docker.io/library/busybox:1.37"},
"variants":[{"platform":{"architecture":"arm64","os":"linux","variant":"v8"},"config":{"config":{"Env":["PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"],"Cmd":["sh"]}}}]}]`

func TestContainer(t *testing.T) {
	f := fakeRunner{
		"list --all --format json": {out: containersJSON},
		"image list --format json": {out: busyboxImageJSON},
	}
	c := CLI{Run: f.run}
	got, err := c.Container(context.Background(), "probe")
	if err != nil {
		t.Fatal(err)
	}
	want := &Container{
		ContainerSpec: ContainerSpec{
			Name:    "probe",
			Image:   "docker.io/library/busybox:1.37",
			Command: []string{"/bin/sh", "-c", "sleep 3600"},
			Env:     map[string]string{"FOO": "bar"}, // without the image's PATH
			Workdir: "/tmp",
			CPUs:    2,
			Labels:  map[string]string{"a": "b"},
			Networks: []NetworkAttachment{
				{Network: "net", MAC: "02:42:ac:11:00:02", MTU: 1400, Aliases: []string{"probe.espalier.test"}},
				{Network: "default", MTU: 1280},
			},
			Mounts: []Mount{
				{Type: MountVolume, Source: "v", Target: "/data"},
				{Type: MountBind, Source: "/host/dir", Target: "/bind", ReadOnly: true},
				{Type: MountTmpfs, Target: "/scratch"},
			},
			Ports:   []Port{{HostIP: "127.0.0.1", HostPort: 18081, ContainerPort: 80, Protocol: "tcp"}},
			CapAdd:  []string{"CAP_NET_RAW"},
			CapDrop: []string{"CAP_MKNOD"},

			Platform:       "linux/arm64/v8",
			User:           "1000:1000",
			ReadOnly:       true,
			DNS:            DNS{Nameservers: []string{"1.1.1.1"}, Domain: "espalier.test", Search: []string{"a.test"}, Options: []string{"ndots:2"}},
			Init:           true,
			Rosetta:        true,
			SSH:            true,
			Ulimits:        []Ulimit{{Name: "nofile", Soft: 1024, Hard: 2048}},
			PublishSockets: []Socket{{HostPath: "/host/s.sock", ContainerPath: "/run/s.sock"}},
			Runtime:        "container-runtime-linux",
			Virtualization: true,
			ReadOnlyPaths:  []string{"/proc/bus", "/opt"},
			MaskedPaths:    []string{}, // NONE: no defaults either
		},
		MemoryBytes:  256 << 20,
		ShmSizeBytes: 128 << 20,
		State:        "running",
		Addresses: []Address{
			{Network: "net", IPv4: "192.168.232.2/24", IPv4Gateway: "192.168.232.1", IPv6: "fd6a::2/64", MAC: "02:42:ac:11:00:02"},
			{Network: "default", IPv4: "192.168.64.2/24", IPv4Gateway: "192.168.64.1", IPv6: "fd55::2/64", MAC: "f2:9f:c7:e6:b6:97"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
	if got, _ := c.Container(context.Background(), "missing"); got != nil {
		t.Errorf("missing: got %+v, want nil", got)
	}
}

func TestCreateContainerArgs(t *testing.T) {
	var got string
	run := func(_ context.Context, args ...string) ([]byte, error) {
		got = strings.Join(args, " ")
		return nil, nil
	}
	err := CLI{Run: run}.CreateContainer(context.Background(), ContainerSpec{
		Name:     "probe",
		Image:    "docker.io/library/busybox:1.37",
		Command:  []string{"/bin/sh", "-c", "sleep 3600"},
		Env:      map[string]string{"FOO": "bar"},
		Workdir:  "/tmp",
		CPUs:     2,
		Memory:   "256M",
		Labels:   map[string]string{"a": "b"},
		Networks: []NetworkAttachment{{Network: "net", MAC: "02:42:ac:11:00:02", MTU: 1400, Aliases: []string{"probe.espalier.test"}}, {Network: "default"}},
		Mounts:   []Mount{{Type: MountVolume, Source: "v", Target: "/data"}, {Type: MountBind, Source: "/host/dir", Target: "/bind", ReadOnly: true}, {Type: MountTmpfs, Target: "/scratch"}},
		Ports:    []Port{{HostIP: "127.0.0.1", HostPort: 18081, ContainerPort: 80, Protocol: "tcp"}, {HostPort: 5353, ContainerPort: 53, Protocol: "udp"}},
		CapAdd:   []string{"CAP_NET_RAW"},
		CapDrop:  []string{"CAP_MKNOD"},

		Platform:       "linux/arm64",
		User:           "1000:1000",
		ShmSize:        "128M",
		Scheme:         "http",
		ReadOnly:       true,
		Init:           true,
		NoDNS:          true,
		Ulimits:        []Ulimit{{Name: "nofile", Soft: 1024, Hard: 2048}},
		PublishSockets: []Socket{{HostPath: "/host/s.sock", ContainerPath: "/run/s.sock"}},
		Kernel:         "/k/vmlinux",
		KernelArgs:     []string{"quiet", "loglevel=3"},
		InitImage:      "vminit:1",
		Runtime:        "container-runtime-linux",
		Virtualization: true,
		ReadOnlyPaths:  []string{"/opt"},
		MaskedPaths:    []string{"NONE", "/secret"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "create --name probe --env FOO=bar --workdir /tmp --cpus 2 --memory 256M --label a=b" +
		" --network net,mac=02:42:ac:11:00:02,mtu=1400,alias=probe.espalier.test --network default" +
		" --mount type=volume,target=/data,source=v --mount type=bind,target=/bind,source=/host/dir,readonly --mount type=tmpfs,target=/scratch" +
		" --publish 127.0.0.1:18081:80/tcp --publish 5353:53/udp" +
		" --cap-add CAP_NET_RAW --cap-drop CAP_MKNOD" +
		" --platform linux/arm64 --user 1000:1000 --shm-size 128M --scheme http" +
		" --kernel /k/vmlinux --init-image vminit:1 --runtime container-runtime-linux --read-only --init --no-dns --virtualization" +
		" --ulimit nofile=1024:2048 --publish-socket /host/s.sock:/run/s.sock" +
		" --kernel-arg quiet --kernel-arg loglevel=3 --read-only-path /opt --masked-path NONE --masked-path /secret" +
		" --entrypoint /bin/sh docker.io/library/busybox:1.37 -c sleep 3600"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

// A container created with --no-dns has no DNS configuration; numeric
// users are recorded as ids.
func TestContainerNoDNS(t *testing.T) {
	j := strings.NewReplacer(
		`"dns":{"domain":"espalier.test","nameservers":["1.1.1.1"],"options":["ndots:2"],"searchDomains":["a.test"]}`, `"dns":null`,
		`{"raw":{"userString":"1000:1000"}}`, `{"id":{"gid":100,"uid":1000}}`,
	).Replace(containersJSON)
	f := fakeRunner{"list --all --format json": {out: j}, "image list --format json": {out: busyboxImageJSON}}
	got, err := CLI{Run: f.run}.Container(context.Background(), "probe")
	if err != nil {
		t.Fatal(err)
	}
	if !got.NoDNS || !reflect.DeepEqual(got.DNS, DNS{}) || got.User != "1000:100" {
		t.Errorf("got NoDNS %t, DNS %+v, User %q; want true, empty, 1000:100", got.NoDNS, got.DNS, got.User)
	}
}
