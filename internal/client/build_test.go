package client

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestBuildImage(t *testing.T) {
	f := fakeRunner{
		"build --progress plain --file /src/app/Containerfile --tag app:1 --tag app:latest " +
			"--build-arg A=1 --build-arg B=2 --label team=x --target final --platform linux/arm64,linux/amd64 " +
			"--secret id=tok,env=TOK --secret id=key,src=/run/key --ssh default --no-cache --pull /src/app": {},
	}
	err := CLI{Run: f.run}.BuildImage(context.Background(), BuildSpec{
		Context:    "/src/app",
		Dockerfile: "/src/app/Containerfile",
		Tags:       []string{"app:1", "app:latest"},
		BuildArgs:  map[string]string{"B": "2", "A": "1"},
		Target:     "final",
		Platforms:  []string{"linux/arm64", "linux/amd64"},
		Labels:     map[string]string{"team": "x"},
		Secrets:    []BuildSecret{{ID: "tok", Env: "TOK"}, {ID: "key", File: "/run/key"}},
		SSH:        true,
		NoCache:    true,
		Pull:       true,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestBuildImageErrorKeepsTheEnd(t *testing.T) {
	var log []string
	for i := range 50 {
		log = append(log, fmt.Sprintf("#%d step", i))
	}
	log = append(log, `Error: unknown: "failed to solve: exit code: 3"`)
	f := fakeRunner{"build --progress plain --tag app:1 /src": {err: &ExitError{Code: 1, Stderr: strings.Join(log, "\n")}}}
	err := CLI{Run: f.run}.BuildImage(context.Background(), BuildSpec{Context: "/src", Tags: []string{"app:1"}})
	var exit *ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("got %v, want an *ExitError", err)
	}
	lines := strings.Split(exit.Stderr, "\n")
	if len(lines) != buildErrorLines || !strings.Contains(lines[len(lines)-1], "failed to solve") {
		t.Errorf("got %d lines ending in %q", len(lines), lines[len(lines)-1])
	}
}

func TestBuilderRunning(t *testing.T) {
	for _, c := range []struct {
		out  string
		want bool
	}{
		{`[{"id":"buildkit","status":{"state":"running"}}]`, true},
		{`[{"id":"buildkit","status":{"state":"stopped"}}]`, false},
		{`[]`, false}, // deleted
	} {
		f := fakeRunner{"builder status --format json": {out: c.out}}
		got, err := CLI{Run: f.run}.BuilderRunning(context.Background())
		if err != nil || got != c.want {
			t.Errorf("%s: got %v, %v; want %v", c.out, got, err, c.want)
		}
	}
}

func TestStartBuilder(t *testing.T) {
	f := fakeRunner{"builder start": {}, "builder start --cpus 4 --memory 4G": {}}
	c := CLI{Run: f.run}
	if err := c.StartBuilder(context.Background(), BuilderSettings{}); err != nil {
		t.Error(err)
	}
	if err := c.StartBuilder(context.Background(), BuilderSettings{CPUs: 4, Memory: "4G"}); err != nil {
		t.Error(err)
	}
}
