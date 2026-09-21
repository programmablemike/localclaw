package main

import (
	"bytes"
	"encoding/json"
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
