package provider

import (
	"os"
	"path/filepath"
	"testing"
)

func TestContextHash(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("Dockerfile", "FROM scratch\n")
	write("app/main.go", "package main\n")
	write("logs/a.log", "noise")
	write("cache/keep.txt", "kept")
	write("cache/drop.txt", "dropped")
	write(".dockerignore", "logs\ncache\n!cache/keep.txt\n")
	dockerfile := filepath.Join(dir, "Dockerfile")

	hash := func() string {
		t.Helper()
		h, err := contextHash(dir, dockerfile)
		if err != nil {
			t.Fatal(err)
		}
		return h
	}
	base := hash()
	for _, c := range []struct {
		name    string
		change  func()
		changes bool
	}{
		{"ignored file", func() { write("logs/b.log", "more noise") }, false},
		{"ignored file in an excluded directory with exceptions", func() { write("cache/drop.txt", "changed") }, false},
		{"exception", func() { write("cache/keep.txt", "changed") }, true},
		{"content", func() { write("app/main.go", "package main // changed\n") }, true},
		{"new file", func() { write("app/util.go", "package main\n") }, true},
		{"Dockerfile", func() { write("Dockerfile", "FROM busybox\n") }, true},
		{"mode", func() {
			if err := os.Chmod(filepath.Join(dir, "app/main.go"), 0o755); err != nil {
				t.Fatal(err)
			}
		}, true},
	} {
		c.change()
		got := hash()
		if changed := got != base; changed != c.changes {
			t.Errorf("%s: hash changed = %v, want %v", c.name, changed, c.changes)
		}
		base = got
	}

	if _, err := contextHash(dir, filepath.Join(dir, "Missing.Dockerfile")); err == nil {
		t.Error("missing Dockerfile: no error")
	}
}
