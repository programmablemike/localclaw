package podman

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/programmablemike/localclaw/internal/adapters/exec"
	"github.com/programmablemike/localclaw/internal/domain"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestVersion(t *testing.T) {
	f := exec.NewFake()
	f.Script("podman", []string{"--version"}, exec.Response{Stdout: "podman version 5.8.4\n"})
	got, err := (&Client{Runner: f}).Version(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := domain.Version{Major: 5, Minor: 8, Patch: 4, Raw: "5.8.4"}
	if got != want {
		t.Fatalf("Version() = %+v, want %+v", got, want)
	}
}

func TestVersionNotFound(t *testing.T) {
	f := exec.NewFake()
	f.Script("podman", []string{"--version"}, exec.Response{Err: exec.ErrNotFound})
	_, err := (&Client{Runner: f}).Version(context.Background())
	if !errors.Is(err, domain.ErrToolNotFound) {
		t.Fatalf("err = %v, want domain.ErrToolNotFound", err)
	}
}

func TestVersionExitError(t *testing.T) {
	f := exec.NewFake()
	f.Script("podman", []string{"--version"}, exec.Response{Err: &exec.ExitError{Code: 125, Stderr: "boom"}})
	_, err := (&Client{Runner: f}).Version(context.Background())
	if err == nil || !strings.HasPrefix(err.Error(), "podman: version: exit status 125: boom") {
		t.Fatalf("err = %v", err)
	}
	if errors.Is(err, domain.ErrToolNotFound) {
		t.Fatal("exit error must not look like not-found")
	}
}

func TestVersionGarbage(t *testing.T) {
	f := exec.NewFake()
	f.Script("podman", []string{"--version"}, exec.Response{Stdout: "podman version banana\n"})
	_, err := (&Client{Runner: f}).Version(context.Background())
	if err == nil || !strings.HasPrefix(err.Error(), "podman: version: ") {
		t.Fatalf("err = %v", err)
	}
}

func TestListMachines(t *testing.T) {
	f := exec.NewFake()
	f.Script("podman", []string{"machine", "list", "--format", "json"}, exec.Response{Stdout: fixture(t, "machine-list.json")})
	got, err := (&Client{Runner: f}).ListMachines(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.Machine{
		{Name: "podman-machine-default", Running: false},
		{Name: "lclaw", Running: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListMachines() = %+v, want %+v", got, want)
	}
}

func TestListMachinesEmpty(t *testing.T) {
	for _, out := range []string{fixture(t, "machine-list-empty.json"), "", "\n"} {
		f := exec.NewFake()
		f.Script("podman", []string{"machine", "list", "--format", "json"}, exec.Response{Stdout: out})
		got, err := (&Client{Runner: f}).ListMachines(context.Background())
		if err != nil {
			t.Fatalf("output %q: %v", out, err)
		}
		if len(got) != 0 {
			t.Fatalf("output %q: got %+v, want none", out, got)
		}
	}
}

func TestListMachinesNotFound(t *testing.T) {
	f := exec.NewFake()
	f.Script("podman", []string{"machine", "list", "--format", "json"}, exec.Response{Err: exec.ErrNotFound})
	_, err := (&Client{Runner: f}).ListMachines(context.Background())
	if !errors.Is(err, domain.ErrToolNotFound) {
		t.Fatalf("err = %v, want domain.ErrToolNotFound", err)
	}
}

func TestListMachinesBadJSON(t *testing.T) {
	f := exec.NewFake()
	f.Script("podman", []string{"machine", "list", "--format", "json"}, exec.Response{Stdout: "{not json"})
	_, err := (&Client{Runner: f}).ListMachines(context.Background())
	if err == nil || !strings.HasPrefix(err.Error(), "podman: list machines: decode: ") {
		t.Fatalf("err = %v", err)
	}
}
