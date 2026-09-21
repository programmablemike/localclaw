package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"strings"
	"testing"

	"github.com/programmablemike/localclaw/internal/domain"
)

type fakeDoctor struct {
	report domain.Report
	err    error
}

func (f *fakeDoctor) Run(ctx context.Context) (domain.Report, error) { return f.report, f.err }

type run struct {
	stdout, stderr bytes.Buffer
	err            error
	level          *slog.LevelVar
}

// execute runs the whole command tree with buffers as writers.
func execute(t *testing.T, doc DoctorRunner, args ...string) *run {
	t.Helper()
	r := &run{level: new(slog.LevelVar)}
	root := New(Deps{
		Doctor: doc,
		Build:  BuildInfo{Version: "0.1.0-dev", Commit: "abc1234", Date: "2026-09-20T12:00:00Z", Modified: true},
		Level:  r.level,
		Stdout: &r.stdout,
		Stderr: &r.stderr,
	})
	r.err = root.Run(context.Background(), append([]string{"lclaw"}, args...))
	return r
}

func TestVersionText(t *testing.T) {
	r := execute(t, &fakeDoctor{}, "version")
	if r.err != nil {
		t.Fatal(r.err)
	}
	want := "lclaw 0.1.0-dev\n" +
		"commit:   abc1234 (modified)\n" +
		"built:    2026-09-20T12:00:00Z\n" +
		"go:       " + runtime.Version() + " " + runtime.GOOS + "/" + runtime.GOARCH + "\n"
	if got := r.stdout.String(); got != want {
		t.Fatalf("stdout =\n%q\nwant\n%q", got, want)
	}
	if r.stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", r.stderr.String())
	}
}

func TestVersionJSON(t *testing.T) {
	r := execute(t, &fakeDoctor{}, "--output", "json", "version")
	if r.err != nil {
		t.Fatal(r.err)
	}
	var got map[string]any
	if err := json.Unmarshal(r.stdout.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, r.stdout.String())
	}
	if got["version"] != "0.1.0-dev" || got["commit"] != "abc1234" || got["modified"] != true ||
		got["date"] != "2026-09-20T12:00:00Z" || got["go"] != runtime.Version() ||
		got["platform"] != runtime.GOOS+"/"+runtime.GOARCH {
		t.Fatalf("got %v", got)
	}
}

func TestOutputFromEnvironment(t *testing.T) {
	t.Setenv("LCLAW_OUTPUT", "json")
	r := execute(t, &fakeDoctor{}, "version")
	if r.err != nil {
		t.Fatal(r.err)
	}
	if !strings.HasPrefix(r.stdout.String(), "{") {
		t.Fatalf("stdout = %q, want JSON", r.stdout.String())
	}
}

func TestBadOutputIsUsageError(t *testing.T) {
	r := execute(t, &fakeDoctor{}, "--output", "yaml", "version")
	if got := ExitCode(r.err); got != 2 {
		t.Fatalf("exit code = %d (err %v), want 2", got, r.err)
	}
	if !strings.Contains(r.stderr.String(), `invalid value "yaml" for --output`) {
		t.Fatalf("stderr = %q", r.stderr.String())
	}
	if r.stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", r.stdout.String())
	}
}

func TestUnknownFlagIsUsageError(t *testing.T) {
	r := execute(t, &fakeDoctor{}, "--bogus")
	if got := ExitCode(r.err); got != 2 {
		t.Fatalf("exit code = %d (err %v), want 2", got, r.err)
	}
	if !strings.Contains(r.stderr.String(), "Run 'lclaw --help' for usage.") {
		t.Fatalf("stderr = %q", r.stderr.String())
	}
}

func TestVerboseRaisesLogLevel(t *testing.T) {
	r := execute(t, &fakeDoctor{}, "--verbose", "version")
	if r.err != nil {
		t.Fatal(r.err)
	}
	if r.level.Level() != slog.LevelDebug {
		t.Fatalf("level = %v, want DEBUG", r.level.Level())
	}
}

func TestDefaultLogLevelIsNotDebug(t *testing.T) {
	r := execute(t, &fakeDoctor{}, "version")
	if r.level.Level() == slog.LevelDebug {
		t.Fatal("level is DEBUG without --verbose")
	}
}

func TestHelpListsCommandsOnStdout(t *testing.T) {
	r := execute(t, &fakeDoctor{}, "--help")
	if r.err != nil {
		t.Fatal(r.err)
	}
	out := r.stdout.String()
	if !strings.Contains(out, "doctor") || !strings.Contains(out, "version") || !strings.Contains(out, "--output") {
		t.Fatalf("help = %q", out)
	}
}

func TestSubcommandUnknownFlagIsUsageError(t *testing.T) {
	for _, args := range [][]string{{"doctor", "--bogus"}, {"version", "--bogus"}} {
		r := execute(t, &fakeDoctor{}, args...)
		if got := ExitCode(r.err); got != 2 {
			t.Fatalf("%v: exit code = %d (err %v), want 2", args, got, r.err)
		}
		if r.stdout.Len() != 0 {
			t.Fatalf("%v: stdout = %q, want empty", args, r.stdout.String())
		}
		if strings.Count(r.stderr.String(), "lclaw:") != 1 || !strings.Contains(r.stderr.String(), "Run 'lclaw --help' for usage.") {
			t.Fatalf("%v: stderr = %q", args, r.stderr.String())
		}
	}
}

func TestUnknownCommandIsUsageError(t *testing.T) {
	for _, args := range [][]string{{"bogus"}, {"help"}} {
		r := execute(t, &fakeDoctor{}, args...)
		if got := ExitCode(r.err); got != 2 {
			t.Fatalf("%v: exit code = %d (err %v), want 2", args, got, r.err)
		}
		if r.stdout.Len() != 0 {
			t.Fatalf("%v: stdout = %q, want empty", args, r.stdout.String())
		}
		if !strings.Contains(r.stderr.String(), `unknown command "`+args[0]+`"`) {
			t.Fatalf("%v: stderr = %q", args, r.stderr.String())
		}
	}
}

func TestNoArgumentsShowsHelp(t *testing.T) {
	r := execute(t, &fakeDoctor{})
	if r.err != nil {
		t.Fatal(r.err)
	}
	if !strings.Contains(r.stdout.String(), "doctor") || !strings.Contains(r.stdout.String(), "version") {
		t.Fatalf("stdout = %q, want help", r.stdout.String())
	}
}

func TestGlobalFlagAfterCommand(t *testing.T) {
	r := execute(t, &fakeDoctor{}, "version", "--output", "json")
	if r.err != nil {
		t.Fatal(r.err)
	}
	if !strings.HasPrefix(r.stdout.String(), "{") {
		t.Fatalf("stdout = %q, want JSON", r.stdout.String())
	}
}

func TestExitCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, 0},
		{"checks failed", ErrChecksFailed, 1},
		{"wrapped checks failed", fmt.Errorf("x: %w", ErrChecksFailed), 1},
		{"other", errors.New("boom"), 1},
		{"usage", &usageError{err: errors.New("bad flag")}, 2},
		{"cancelled", fmt.Errorf("podman: version: %w", context.Canceled), 130},
		{"deadline", context.DeadlineExceeded, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExitCode(tt.err); got != tt.want {
				t.Fatalf("ExitCode(%v) = %d, want %d", tt.err, got, tt.want)
			}
		})
	}
}
