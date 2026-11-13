package client

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

// Captured from container 1.7.1: a host-only network with a requested subnet
// and label, one with a requested IPv6 prefix, and one left to the runtime.
const networksJSON = `[
{"configuration":{"ipv4Subnet":"192.168.200.0/24","labels":{"a":"b"},"mode":"hostOnly","name":"fixed","options":{},"plugin":"container-network-vmnet"},"id":"fixed","status":{"ipv4Gateway":"192.168.200.1","ipv4Subnet":"192.168.200.0/24","ipv6Subnet":"fdf5:4514:5137:7b::/64"}},
{"configuration":{"ipv6Subnet":"fd00:abcd:1::/64","labels":{},"mode":"nat","name":"v6","options":{},"plugin":"container-network-vmnet"},"id":"v6","status":{"ipv4Gateway":"192.168.65.1","ipv4Subnet":"192.168.65.0/24","ipv6Subnet":"fd00:abcd:1::1/64"}},
{"configuration":{"labels":{},"mode":"nat","name":"chosen","options":{},"plugin":"container-network-vmnet"},"id":"chosen","status":{"ipv4Gateway":"192.168.66.1","ipv4Subnet":"192.168.66.0/24","ipv6Subnet":"fdb0:d1d7:b13:19c::/64"}}
]`

func TestNetwork(t *testing.T) {
	f := fakeRunner{"network list --format json": {out: networksJSON}}
	c := CLI{Run: f.run}
	for name, want := range map[string]*Network{
		"fixed": {NetworkSpec: NetworkSpec{Name: "fixed", Internal: true, Subnet: "192.168.200.0/24",
			SubnetV6: "fdf5:4514:5137:7b::/64", Labels: map[string]string{"a": "b"}}, Gateway: "192.168.200.1"},
		// The requested prefix, not the status' gateway form (::1/64).
		"v6": {NetworkSpec: NetworkSpec{Name: "v6", Subnet: "192.168.65.0/24",
			SubnetV6: "fd00:abcd:1::/64", Labels: map[string]string{}}, Gateway: "192.168.65.1"},
		"chosen": {NetworkSpec: NetworkSpec{Name: "chosen", Subnet: "192.168.66.0/24",
			SubnetV6: "fdb0:d1d7:b13:19c::/64", Labels: map[string]string{}}, Gateway: "192.168.66.1"},
		"missing": nil,
	} {
		got, err := c.Network(context.Background(), name)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: got %+v, want %+v", name, got, want)
		}
	}
}

func TestCreateNetworkArgs(t *testing.T) {
	var got string
	run := func(_ context.Context, args ...string) ([]byte, error) {
		got = strings.Join(args, " ")
		return nil, nil
	}
	err := CLI{Run: run}.CreateNetwork(context.Background(), NetworkSpec{
		Name: "n", Internal: true, Subnet: "10.1.0.0/24", Labels: map[string]string{"z": "1", "a": "2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "network create --internal --subnet 10.1.0.0/24 --label a=2 --label z=1 n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
