package main

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/programmablemike/localclaw"
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

func TestRunInitWritesThenSkipsThenForces(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "lclaw")
	paths := scaffoldPaths(t)
	if len(paths) < 4 {
		t.Fatalf("embedded scaffold holds %d files: %v", len(paths), paths)
	}

	var stdout, stderr bytes.Buffer
	if code := run([]string{"lclaw", "init", "--dir", dir}, &stdout, &stderr); code != 0 {
		t.Fatalf("first init: exit %d, stderr %q", code, stderr.String())
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
	if code := run([]string{"lclaw", "--output", "json", "init", "--dir", dir}, &stdout, &stderr); code != 0 {
		t.Fatalf("second init: exit %d, stderr %q", code, stderr.String())
	}
	var second struct{ Written, Skipped []string }
	if err := json.Unmarshal(stdout.Bytes(), &second); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, stdout.String())
	}
	if len(second.Written) != 0 || len(second.Skipped) != len(paths) {
		t.Fatalf("second init wrote %v, skipped %d of %d", second.Written, len(second.Skipped), len(paths))
	}

	tomlPath := filepath.Join(dir, "lclaw.toml")
	if err := os.WriteFile(tomlPath, []byte("schema = 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	if code := run([]string{"lclaw", "init", "--dir", dir, "--force"}, &stdout, &stderr); code != 0 {
		t.Fatalf("forced init: exit %d, stderr %q", code, stderr.String())
	}
	got, _ := os.ReadFile(tomlPath)
	want, _ := fs.ReadFile(scaffoldFS(), "lclaw.toml")
	if !bytes.Equal(got, want) {
		t.Fatal("--force did not restore lclaw.toml")
	}
}

func TestRunInitReportsAWriteFailure(t *testing.T) {
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
