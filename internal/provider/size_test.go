package provider

import "testing"

func TestSize(t *testing.T) {
	for in, want := range map[string]int64{"0": 0, "512": 512, "1K": 1 << 10, "256M": 256 << 20, "262144K": 256 << 20, "2G": 2 << 30} {
		if got, ok := parseSize(in); !ok || got != want {
			t.Errorf("parseSize(%q) = %d, %t; want %d", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "1.5G", "1g", "M", "-1"} {
		if _, ok := parseSize(in); ok {
			t.Errorf("parseSize(%q) accepted", in)
		}
	}
	for in, want := range map[int64]string{0: "0", 1000: "1000", 1 << 10: "1K", 1536 << 20: "1536M", 1 << 30: "1G"} {
		if got := formatSize(in); got != want {
			t.Errorf("formatSize(%d) = %q, want %q", in, got, want)
		}
	}
}
