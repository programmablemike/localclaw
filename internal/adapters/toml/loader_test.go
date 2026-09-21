package toml

import (
	"errors"
	"io/fs"
	"os"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/programmablemike/localclaw/internal/domain"
)

// scaffold returns a file system holding the named fixture as lclaw.toml.
func scaffold(t *testing.T, fixture string) fs.FS {
	t.Helper()
	data, err := os.ReadFile("testdata/" + fixture)
	if err != nil {
		t.Fatal(err)
	}
	return fstest.MapFS{domain.TopologyFile: {Data: data}}
}

func TestLoadValid(t *testing.T) {
	got, err := (Loader{}).Load(scaffold(t, "valid.toml"))
	if err != nil {
		t.Fatal(err)
	}
	want := domain.Topology{
		Schema:   1,
		Provider: "libkrun",
		Machines: []domain.MachineSpec{
			{Name: "infra", CPUs: 1, MemoryMiB: 1024, DiskGiB: 10, Workloads: []domain.Workload{"wireguard", "kuma-cp", "gateway"}},
			{Name: "services", CPUs: 2, MemoryMiB: 4096, DiskGiB: 30, Workloads: []domain.Workload{"litellm-db", "litellm", "agentgateway"}},
			{Name: "agent", CPUs: 2, MemoryMiB: 4096, DiskGiB: 30, Workloads: []domain.Workload{"openclaw"}, Volumes: []string{"/Users/me/workspace:/mnt/workspace"}},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load() =\n%+v\nwant\n%+v", got, want)
	}
}

func TestLoadOrdersRolesThenUnknownNames(t *testing.T) {
	src := "schema = 1\nprovider = \"libkrun\"\n" +
		"[machines.zeta]\ncpus = 1\nmemory-mib = 1\ndisk-gib = 1\nworkloads = []\n" +
		"[machines.agent]\ncpus = 1\nmemory-mib = 1\ndisk-gib = 1\nworkloads = []\n" +
		"[machines.alpha]\ncpus = 1\nmemory-mib = 1\ndisk-gib = 1\nworkloads = []\n" +
		"[machines.infra]\ncpus = 1\nmemory-mib = 1\ndisk-gib = 1\nworkloads = []\n"
	got, err := (Loader{}).Load(fstest.MapFS{domain.TopologyFile: {Data: []byte(src)}})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, m := range got.Machines {
		names = append(names, m.Name)
	}
	if want := []string{"infra", "agent", "alpha", "zeta"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("machine order = %v, want %v", names, want)
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := (Loader{}).Load(fstest.MapFS{})
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want fs.ErrNotExist", err)
	}
	if !strings.HasPrefix(err.Error(), "toml: read lclaw.toml: ") {
		t.Errorf("err = %q", err)
	}
}

func TestLoadRejectsUnknownKeys(t *testing.T) {
	_, err := (Loader{}).Load(scaffold(t, "unknown-key.toml"))
	if err == nil {
		t.Fatal("expected an error for unknown keys")
	}
	if got, want := err.Error(), "lclaw.toml: unknown keys: colour, machines.infra.cpu"; got != want {
		t.Fatalf("err = %q, want %q", got, want)
	}
}

func TestLoadMalformed(t *testing.T) {
	_, err := (Loader{}).Load(scaffold(t, "malformed.toml"))
	if err == nil {
		t.Fatal("expected a parse error")
	}
	if !strings.HasPrefix(err.Error(), "lclaw.toml: toml: line 2") {
		t.Fatalf("err = %q, want a line-numbered parse error", err)
	}
}

func TestLoadWrongType(t *testing.T) {
	_, err := (Loader{}).Load(scaffold(t, "wrong-type.toml"))
	if err == nil {
		t.Fatal("expected a type error")
	}
	if !strings.HasPrefix(err.Error(), "lclaw.toml: ") || !strings.Contains(err.Error(), "machines.infra.cpus") {
		t.Fatalf("err = %q, want the file name and the offending key", err)
	}
}
