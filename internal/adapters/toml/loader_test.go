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
	t.Setenv("HOME", "/home/tester")
	got, err := (Loader{}).Load(scaffold(t, "valid.toml"))
	if err != nil {
		t.Fatal(err)
	}
	want := domain.Topology{
		Schema:       2,
		Provider:     "libkrun",
		KeychainPath: "/home/tester/Library/Keychains/lclaw.keychain-db",
		Machine:      domain.MachineSpec{CPUs: 4, MemoryMiB: 8192, DiskGiB: 60, Volumes: []string{"/Users/me/workspace:/mnt/workspace"}},
		Zones: []domain.ZoneSpec{
			{Name: "infra", Workloads: []domain.Workload{"wireguard", "kuma-cp", "gateway"}, Bridges: map[domain.Workload][]string{"kuma-cp": {"services", "agent"}}},
			{Name: "services", Workloads: []domain.Workload{"litellm-db", "litellm", "agentgateway"}, Secrets: []string{"anthropic-api-key"}, Bridges: map[domain.Workload][]string{"agentgateway": {"agent"}}},
			{Name: "agent", Internal: true, Workloads: []domain.Workload{"openclaw"}},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load() =\n%+v\nwant\n%+v", got, want)
	}
}

func TestLoadOrdersRolesThenUnknownNames(t *testing.T) {
	src := "schema = 2\nprovider = \"libkrun\"\n" +
		"[zones.zeta]\nworkloads = []\n" +
		"[zones.agent]\nworkloads = []\n" +
		"[zones.alpha]\nworkloads = []\n" +
		"[zones.infra]\nworkloads = []\n"
	got, err := (Loader{}).Load(fstest.MapFS{domain.TopologyFile: {Data: []byte(src)}})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, z := range got.Zones {
		names = append(names, z.Name)
	}
	if want := []string{"infra", "agent", "alpha", "zeta"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("zone order = %v, want %v", names, want)
	}
}

// A schema 1 file decodes rather than failing on unknown keys, so that
// validation can report the schema with a migration hint.
func TestLoadSchema1DecodesForValidation(t *testing.T) {
	src := "schema = 1\nprovider = \"libkrun\"\n" +
		"[machines.infra]\ncpus = 1\nmemory-mib = 1024\ndisk-gib = 10\nworkloads = [\"wireguard\"]\nsecrets = [\"x\"]\n"
	got, err := (Loader{}).Load(fstest.MapFS{domain.TopologyFile: {Data: []byte(src)}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Schema != 1 || len(got.Zones) != 0 {
		t.Fatalf("Load() = %+v, want schema 1 and no zones", got)
	}
	findings := domain.Validate(got, func(string) bool { return false })
	if len(findings) == 0 || findings[0].Where != "schema" || !strings.Contains(findings[0].Message, "init --force") {
		t.Fatalf("findings = %+v, want the schema finding first with the migration hint", findings)
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
	if got, want := err.Error(), "lclaw.toml: unknown keys: colour, machine.cpu"; got != want {
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
	if !strings.HasPrefix(err.Error(), "lclaw.toml: ") || !strings.Contains(err.Error(), "machine.cpus") {
		t.Fatalf("err = %q, want the file name and the offending key", err)
	}
}

func TestLoadKeychainPathExpandsHome(t *testing.T) {
	t.Setenv("HOME", "/home/tester")
	top, err := Loader{}.Load(scaffold(t, "valid.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := top.KeychainPath, "/home/tester/Library/Keychains/lclaw.keychain-db"; got != want {
		t.Fatalf("KeychainPath = %q, want %q", got, want)
	}
}

func TestLoadKeychainPathDefaultsWhenTableAbsent(t *testing.T) {
	t.Setenv("HOME", "/home/tester")
	data := "schema = 2\nprovider = \"libkrun\"\n"
	top, err := Loader{}.Load(fstest.MapFS{domain.TopologyFile: {Data: []byte(data)}})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := top.KeychainPath, "/home/tester/Library/Keychains/lclaw.keychain-db"; got != want {
		t.Fatalf("KeychainPath = %q, want %q", got, want)
	}
}

func TestLoadKeychainPathAbsoluteIsKept(t *testing.T) {
	data := "schema = 2\n\n[keychain]\npath = \"/Volumes/Secure/lclaw.keychain-db\"\n"
	top, err := Loader{}.Load(fstest.MapFS{domain.TopologyFile: {Data: []byte(data)}})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := top.KeychainPath, "/Volumes/Secure/lclaw.keychain-db"; got != want {
		t.Fatalf("KeychainPath = %q, want %q", got, want)
	}
}

func TestLoadRelativeKeychainPathIsAnError(t *testing.T) {
	_, err := Loader{}.Load(scaffold(t, "relative-keychain.toml"))
	if err == nil || !strings.Contains(err.Error(), "must be absolute or start with ~/") {
		t.Fatalf("err = %v, want a message about an absolute path", err)
	}
}

func TestLoadSecrets(t *testing.T) {
	t.Setenv("HOME", "/home/tester")
	top, err := Loader{}.Load(scaffold(t, "valid.toml"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{"infra": nil, "services": {"anthropic-api-key"}, "agent": nil}
	if got := top.DeclaredSecrets(); !reflect.DeepEqual(got, want) {
		t.Fatalf("DeclaredSecrets() = %v, want %v", got, want)
	}
}
