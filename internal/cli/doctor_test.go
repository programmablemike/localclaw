package cli

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/programmablemike/localclaw/internal/domain"
)

var update = flag.Bool("update", false, "rewrite golden files in testdata/")

func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v (run `go test ./internal/cli/ -update` to create it)", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("output differs from %s\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
	}
}

var mixed = domain.Report{Checks: []domain.Check{
	{Name: "flox", Status: domain.Pass, Summary: "1.13.1 (minimum 1.0.0)"},
	{Name: "podman", Status: domain.Pass, Summary: "5.8.4 (minimum 5.8.0)"},
	{Name: "lclaw-infra", Status: domain.Warn, Summary: "not created", Hint: "run `lclaw up` once it is available"},
	{Name: "lclaw-services", Status: domain.Warn, Summary: "not created", Hint: "run `lclaw up` once it is available"},
	{Name: "lclaw-agent", Status: domain.Pass, Summary: "running"},
}}

var failed = domain.Report{Checks: []domain.Check{
	{Name: "flox", Status: domain.Pass, Summary: "1.13.1 (minimum 1.0.0)"},
	{Name: "podman", Status: domain.Fail, Summary: "not found", Hint: domain.PodmanRequirement.InstallHint},
	{Name: "machines", Status: domain.Warn, Summary: "skipped because the podman check failed", Hint: "fix podman, then run `lclaw doctor` again"},
}}

func TestDoctorText(t *testing.T) {
	r := execute(t, &fakeDoctor{report: mixed}, "doctor")
	if r.err != nil {
		t.Fatal(r.err)
	}
	golden(t, "doctor-mixed.txt", r.stdout.Bytes())
	if r.stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", r.stderr.String())
	}
}

func TestDoctorJSON(t *testing.T) {
	r := execute(t, &fakeDoctor{report: mixed}, "--output", "json", "doctor")
	if r.err != nil {
		t.Fatal(r.err)
	}
	golden(t, "doctor-mixed.json", r.stdout.Bytes())
}

func TestDoctorFailedChecksExit1(t *testing.T) {
	r := execute(t, &fakeDoctor{report: failed}, "doctor")
	if !errors.Is(r.err, ErrChecksFailed) {
		t.Fatalf("err = %v, want ErrChecksFailed", r.err)
	}
	if got := ExitCode(r.err); got != 1 {
		t.Fatalf("exit code = %d, want 1", got)
	}
	golden(t, "doctor-fail.txt", r.stdout.Bytes())
}

func TestDoctorFailedChecksJSONStatus(t *testing.T) {
	r := execute(t, &fakeDoctor{report: failed}, "--output", "json", "doctor")
	if !errors.Is(r.err, ErrChecksFailed) {
		t.Fatalf("err = %v, want ErrChecksFailed", r.err)
	}
	if !bytes.Contains(r.stdout.Bytes(), []byte(`"status": "fail"`)) {
		t.Fatalf("stdout = %s", r.stdout.String())
	}
}

func TestDoctorInterrupted(t *testing.T) {
	r := execute(t, &fakeDoctor{err: fmt.Errorf("podman: version: %w", context.Canceled)}, "doctor")
	if got := ExitCode(r.err); got != 130 {
		t.Fatalf("exit code = %d (err %v), want 130", got, r.err)
	}
	if r.stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", r.stdout.String())
	}
}

func TestRenderJSONEmptyReport(t *testing.T) {
	var buf bytes.Buffer
	if err := renderReportJSON(&buf, domain.Report{}); err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"status\": \"pass\",\n  \"checks\": []\n}\n"
	if got := buf.String(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRenderTextEmptyReport(t *testing.T) {
	var buf bytes.Buffer
	renderReportText(&buf, domain.Report{})
	if got, want := buf.String(), "\n0 passed, 0 warnings, 0 failed\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestDoctorReceivesTheDefaultDir(t *testing.T) {
	unsetenv(t, "LCLAW_DIR")
	doc := &fakeDoctor{report: mixed}
	if r := execute(t, doc, "doctor"); r.err != nil {
		t.Fatal(r.err)
	}
	if doc.dir != testDefaultDir {
		t.Fatalf("dir = %q, want %q", doc.dir, testDefaultDir)
	}
}

func TestDirFlagAndEnvironment(t *testing.T) {
	tests := []struct {
		name string
		env  string
		args []string
		want string
	}{
		{"flag before the command", "", []string{"--dir", "/x", "doctor"}, "/x"},
		{"flag after the command", "", []string{"doctor", "--dir", "/y"}, "/y"},
		{"environment", "/z", []string{"doctor"}, "/z"},
		{"flag beats environment", "/z", []string{"--dir", "/x", "doctor"}, "/x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.env == "" {
				unsetenv(t, "LCLAW_DIR")
			} else {
				t.Setenv("LCLAW_DIR", tt.env)
			}
			doc := &fakeDoctor{report: mixed}
			if r := execute(t, doc, tt.args...); r.err != nil {
				t.Fatal(r.err)
			}
			if doc.dir != tt.want {
				t.Fatalf("dir = %q, want %q", doc.dir, tt.want)
			}
		})
	}
}

func TestNoDirIsUsageError(t *testing.T) {
	unsetenv(t, "LCLAW_DIR")
	doc := &fakeDoctor{report: mixed}
	r := executeDeps(t, Deps{Doctor: doc, DefaultDir: ""}, "doctor")
	if got := ExitCode(r.err); got != 2 {
		t.Fatalf("exit code = %d (err %v), want 2", got, r.err)
	}
	if !strings.Contains(r.stderr.String(), "no scaffold directory: pass --dir or set LCLAW_DIR") {
		t.Fatalf("stderr = %q", r.stderr.String())
	}
	if doc.dir != "" || r.stdout.Len() != 0 {
		t.Fatalf("doctor ran with dir %q, stdout %q", doc.dir, r.stdout.String())
	}
}
