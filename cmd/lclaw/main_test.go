package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"os"
	osexec "os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/programmablemike/localclaw"
	"github.com/programmablemike/localclaw/internal/adapters/exec"
)

func TestRunVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"lclaw", "version"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr %q", code, stderr.String())
	}
	want := "lclaw " + strings.TrimSpace(localclaw.Version) + "\n"
	if !strings.HasPrefix(stdout.String(), want) {
		t.Fatalf("stdout = %q, want prefix %q", stdout.String(), want)
	}
}

func TestRunUsageErrorExits2(t *testing.T) {
	for _, args := range [][]string{
		{"lclaw", "--output", "yaml", "version"},
		{"lclaw", "doctor", "--bogus"},
		{"lclaw", "init", "--bogus"},
	} {
		var stdout, stderr bytes.Buffer
		code := run(args, &stdout, &stderr)
		if code != 2 {
			t.Fatalf("%v: exit code = %d, want 2", args, code)
		}
		if stdout.Len() != 0 {
			t.Fatalf("%v: stdout = %q, want empty", args, stdout.String())
		}
		if strings.Count(stderr.String(), "lclaw:") != 1 {
			t.Fatalf("%v: stderr should carry exactly one lclaw: line, got %q", args, stderr.String())
		}
	}
}

func TestBuildInfoVersion(t *testing.T) {
	bi := buildInfo()
	if bi.Version != strings.TrimSpace(localclaw.Version) {
		t.Fatalf("Version = %q, want %q", bi.Version, strings.TrimSpace(localclaw.Version))
	}
	if bi.Commit == "" || bi.Date == "" {
		t.Fatalf("Commit and Date must never be empty: %+v", bi)
	}
}

func TestDefaultDirIsUnderHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	if got, want := defaultDir(), filepath.Join(home, ".config", "lclaw"); got != want {
		t.Fatalf("defaultDir() = %q, want %q", got, want)
	}
}

// TestRunDoctorReportsAMissingScaffold runs the real doctor, so it also
// executes flox and podman if they are installed; their verdicts are not
// asserted, only the scaffold check against a directory that does not exist.
func TestRunDoctorReportsAMissingScaffold(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "absent")
	var stdout, stderr bytes.Buffer
	code := run([]string{"lclaw", "--output", "json", "doctor", "--dir", dir}, &stdout, &stderr)
	if code != 0 && code != 1 {
		t.Fatalf("exit code = %d, want 0 or 1; stderr %q", code, stderr.String())
	}
	var got struct {
		Checks []struct{ Name, Status, Summary string }
	}
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, stdout.String())
	}
	for _, c := range got.Checks {
		if c.Name == "scaffold" {
			if c.Status != "warn" || c.Summary != dir+" not found" {
				t.Fatalf("scaffold check = %+v", c)
			}
			return
		}
	}
	t.Fatalf("no scaffold check in %s", stdout.String())
}

// scaffoldPaths lists every embedded default, slash-separated.
func scaffoldPaths(t *testing.T) []string {
	t.Helper()
	var paths []string
	err := fs.WalkDir(scaffoldFS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			paths = append(paths, p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return paths
}

// wantInitExit is the exit code `init` must produce when it reaches the
// keychain step. lclaw's keychain needs macOS's security tool; elsewhere
// init still writes the files and reports every other step, but the
// keychain step itself fails and init exits 1.
func wantInitExit() int {
	if runtime.GOOS != "darwin" {
		return 1
	}
	if _, err := osexec.LookPath("security"); err != nil {
		return 1
	}
	return 0
}

// precreateKeychain creates the empty keychain at the path the scaffold's
// lclaw.toml names (~/Library/Keychains/lclaw.keychain-db under home), the
// same way internal/adapters/keychain/real_test.go's TestRealKeychain does.
// This makes init find the keychain already there and report "skipped", so
// no test ever drives the interactive `security create-keychain` prompt
// itself or leaves behind an empty-password keychain. It is a no-op off
// macOS or without security on PATH, matching wantInitExit's exit-1 case.
func precreateKeychain(t *testing.T, home string) {
	t.Helper()
	if runtime.GOOS != "darwin" {
		return
	}
	if _, err := osexec.LookPath("security"); err != nil {
		return
	}
	ctx := context.Background()
	sys := &exec.System{}
	path := filepath.Join(home, "Library", "Keychains", "lclaw.keychain-db")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := sys.Run(ctx, exec.Command{Name: "security", Args: []string{"create-keychain", path}, Stdin: strings.NewReader("throwaway\nthrowaway\n")}); err != nil {
		t.Fatalf("create-keychain: %v", err)
	}
	t.Cleanup(func() {
		_, _ = sys.Run(ctx, exec.Command{Name: "security", Args: []string{"delete-keychain", path}})
	})
}

func TestRunInitWritesThenSkipsThenForces(t *testing.T) {
	// The scaffold's lclaw.toml points the keychain path at
	// ~/Library/Keychains/lclaw.keychain-db, so HOME must be a throwaway
	// directory for the whole test: this must never touch the developer's
	// real keychains.
	home := t.TempDir()
	t.Setenv("HOME", home)
	precreateKeychain(t, home)

	dir := filepath.Join(t.TempDir(), "lclaw")
	paths := scaffoldPaths(t)
	if len(paths) < 4 {
		t.Fatalf("embedded scaffold holds %d files: %v", len(paths), paths)
	}

	var stdout, stderr bytes.Buffer
	if code := run([]string{"lclaw", "init", "--dir", dir}, &stdout, &stderr); code != wantInitExit() {
		t.Fatalf("first init: exit %d, want %d, stderr %q", code, wantInitExit(), stderr.String())
	}
	for _, p := range paths {
		want, err := fs.ReadFile(scaffoldFS(), p)
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(p)))
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s: content differs from the embedded default", p)
		}
	}

	stdout.Reset()
	if code := run([]string{"lclaw", "--output", "json", "init", "--dir", dir}, &stdout, &stderr); code != wantInitExit() {
		t.Fatalf("second init: exit %d, want %d, stderr %q", code, wantInitExit(), stderr.String())
	}
	var second struct {
		Written, Skipped []string
		Keychain         struct{ State string }
	}
	if err := json.Unmarshal(stdout.Bytes(), &second); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, stdout.String())
	}
	if len(second.Written) != 0 || len(second.Skipped) != len(paths) {
		t.Fatalf("second init wrote %v, skipped %d of %d", second.Written, len(second.Skipped), len(paths))
	}
	if wantInitExit() == 0 {
		if second.Keychain.State != "skipped" {
			t.Fatalf("second init keychain.state = %q, want skipped", second.Keychain.State)
		}
	} else {
		if second.Keychain.State != "failed" {
			t.Fatalf("second init keychain.state = %q, want failed", second.Keychain.State)
		}
	}

	tomlPath := filepath.Join(dir, "lclaw.toml")
	if err := os.WriteFile(tomlPath, []byte("schema = 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	if code := run([]string{"lclaw", "init", "--dir", dir, "--force"}, &stdout, &stderr); code != wantInitExit() {
		t.Fatalf("forced init: exit %d, want %d, stderr %q", code, wantInitExit(), stderr.String())
	}
	got, _ := os.ReadFile(tomlPath)
	want, _ := fs.ReadFile(scaffoldFS(), "lclaw.toml")
	if !bytes.Equal(got, want) {
		t.Fatal("--force did not restore lclaw.toml")
	}
}

func TestRunInitForceOverADirectoryFails(t *testing.T) {
	// The scaffold's lclaw.toml points the keychain path at
	// ~/Library/Keychains/lclaw.keychain-db, so HOME must be a throwaway
	// directory for the whole test: this must never touch the developer's
	// real keychains.
	home := t.TempDir()
	t.Setenv("HOME", home)
	precreateKeychain(t, home)

	dir := filepath.Join(t.TempDir(), "lclaw")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"lclaw", "init", "--dir", dir}, &stdout, &stderr); code != wantInitExit() {
		t.Fatalf("first init: exit %d, want %d, stderr %q", code, wantInitExit(), stderr.String())
	}

	tomlPath := filepath.Join(dir, "lclaw.toml")
	if err := os.Remove(tomlPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(tomlPath, 0o755); err != nil {
		t.Fatal(err)
	}

	stdout.Reset()
	stderr.Reset()
	code := run([]string{"lclaw", "init", "--dir", dir, "--force"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit %d, want 1; stdout %q stderr %q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "failed   lclaw.toml: ") {
		t.Fatalf("stdout = %q, want a failed line for lclaw.toml", stdout.String())
	}
	if !strings.Contains(stdout.String(), "is a directory") {
		t.Fatalf("stdout = %q, want the failure to say lclaw.toml is a directory", stdout.String())
	}
}

func TestRunSecretsUsageErrorsExit2(t *testing.T) {
	for _, args := range [][]string{
		{"lclaw", "secrets", "set"},
		{"lclaw", "secrets", "bogus"},
		{"lclaw", "secrets", "describe", "a", "b"},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 2 {
			t.Fatalf("%v: exit code = %d, want 2; stderr %q", args, code, stderr.String())
		}
	}
}

// A directory with no lclaw.toml and no keychain must fail with the init
// hint rather than reaching any real keychain on the developer's machine.
func TestRunSecretsListWithoutAScaffoldExits1(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	var stdout, stderr bytes.Buffer
	code := run([]string{"lclaw", "--dir", dir, "secrets", "list"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, stderr %q", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "lclaw:") {
		t.Fatalf("stderr = %q, want a diagnosed error", stderr.String())
	}
}

func TestRunInitReportsAWriteFailure(t *testing.T) {
	// The scaffold's lclaw.toml points the keychain path at
	// ~/Library/Keychains/lclaw.keychain-db, so HOME must be a throwaway
	// directory for the whole test: this must never touch the developer's
	// real keychains.
	t.Setenv("HOME", t.TempDir())
	if os.Getuid() == 0 {
		t.Skip("root can write anywhere")
	}
	dir := filepath.Join(t.TempDir(), "lclaw")
	if err := os.MkdirAll(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{"lclaw", "init", "--dir", dir}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit %d, want 1; stdout %q stderr %q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "failed   lclaw.toml: ") {
		t.Fatalf("stdout = %q, want a failed line for lclaw.toml", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty: the report already named the failures", stderr.String())
	}
}
