package flox

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/programmablemike/localclaw/internal/adapters/exec"
	"github.com/programmablemike/localclaw/internal/domain"
)

func TestVersion(t *testing.T) {
	f := exec.NewFake()
	f.Script("flox", []string{"--version"}, exec.Response{Stdout: "1.13.1-g684cdfb\n"})
	got, err := (&Client{Runner: f}).Version(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := domain.Version{Major: 1, Minor: 13, Patch: 1, Raw: "1.13.1-g684cdfb"}
	if got != want {
		t.Fatalf("Version() = %+v, want %+v", got, want)
	}
}

func TestVersionNotFound(t *testing.T) {
	f := exec.NewFake()
	f.Script("flox", []string{"--version"}, exec.Response{Err: exec.ErrNotFound})
	_, err := (&Client{Runner: f}).Version(context.Background())
	if !errors.Is(err, domain.ErrToolNotFound) {
		t.Fatalf("err = %v, want domain.ErrToolNotFound", err)
	}
}

func TestVersionExitError(t *testing.T) {
	f := exec.NewFake()
	f.Script("flox", []string{"--version"}, exec.Response{Err: &exec.ExitError{Code: 1, Stderr: "broken"}})
	_, err := (&Client{Runner: f}).Version(context.Background())
	if err == nil || !strings.HasPrefix(err.Error(), "flox: version: exit status 1: broken") {
		t.Fatalf("err = %v", err)
	}
}

func TestVersionEmpty(t *testing.T) {
	f := exec.NewFake()
	f.Script("flox", []string{"--version"}, exec.Response{Stdout: "\n"})
	_, err := (&Client{Runner: f}).Version(context.Background())
	if err == nil || !strings.HasPrefix(err.Error(), "flox: version: ") {
		t.Fatalf("err = %v", err)
	}
}
