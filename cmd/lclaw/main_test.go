package main

import (
	"bytes"
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
