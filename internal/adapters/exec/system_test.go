package exec

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	osexec "os/exec"
	"strings"
	"testing"
	"time"
)

func requireSh(t *testing.T) {
	t.Helper()
	if _, err := osexec.LookPath("sh"); err != nil {
		t.Skip("sh not on PATH")
	}
}

func newSystem() *System {
	return &System{Log: slog.New(slog.DiscardHandler), WaitDelay: 200 * time.Millisecond}
}

func TestSystemRunCapturesStdoutAndStderr(t *testing.T) {
	requireSh(t)
	res, err := newSystem().Run(context.Background(), Command{Name: "sh", Args: []string{"-c", "echo out; echo err >&2"}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := string(res.Stdout); got != "out\n" {
		t.Errorf("stdout = %q, want %q", got, "out\n")
	}
	if got := string(res.Stderr); got != "err\n" {
		t.Errorf("stderr = %q, want %q", got, "err\n")
	}
}

func TestSystemRunNonZeroExitIsExitError(t *testing.T) {
	requireSh(t)
	res, err := newSystem().Run(context.Background(), Command{Name: "sh", Args: []string{"-c", "echo partial; echo boom >&2; exit 3"}})
	var ee *ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("err = %v (%T), want *ExitError", err, err)
	}
	if ee.Code != 3 {
		t.Errorf("Code = %d, want 3", ee.Code)
	}
	if ee.Stderr != "boom" {
		t.Errorf("Stderr = %q, want %q", ee.Stderr, "boom")
	}
	if got := string(res.Stdout); got != "partial\n" {
		t.Errorf("stdout = %q, want %q", got, "partial\n")
	}
	if !strings.Contains(err.Error(), "exit status 3") || !strings.Contains(err.Error(), "boom") {
		t.Errorf("Error() = %q, want exit status and stderr", err.Error())
	}
}

func TestSystemRunNotFound(t *testing.T) {
	_, err := newSystem().Run(context.Background(), Command{Name: "lclaw-no-such-binary-4f9c"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestSystemRunHonoursCancellation(t *testing.T) {
	requireSh(t)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := newSystem().Run(ctx, Command{Name: "sh", Args: []string{"-c", "sleep 5"}})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("Run took %v, want well under 2s", elapsed)
	}
}

func TestExitErrorMessage(t *testing.T) {
	if got := (&ExitError{Code: 125}).Error(); got != "exit status 125" {
		t.Errorf("Error() = %q", got)
	}
	if got := (&ExitError{Code: 1, Stderr: "bad"}).Error(); got != "exit status 1: bad" {
		t.Errorf("Error() = %q", got)
	}
}

func TestSystemRunPassesStdin(t *testing.T) {
	requireSh(t)
	res, err := newSystem().Run(context.Background(), Command{Name: "sh", Args: []string{"-c", "cat"}, Stdin: strings.NewReader("from stdin")})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(res.Stdout); got != "from stdin" {
		t.Fatalf("stdout = %q", got)
	}
}

func TestSystemInteractiveReportsExitStatus(t *testing.T) {
	requireSh(t)
	if err := newSystem().Interactive(context.Background(), Command{Name: "sh", Args: []string{"-c", "exit 0"}}); err != nil {
		t.Fatalf("exit 0: %v", err)
	}
	err := newSystem().Interactive(context.Background(), Command{Name: "sh", Args: []string{"-c", "exit 3"}})
	var ee *ExitError
	if !errors.As(err, &ee) || ee.Code != 3 || ee.Stderr != "" {
		t.Fatalf("err = %v (%T), want ExitError{Code: 3}", err, err)
	}
}

func TestSystemInteractiveNotFound(t *testing.T) {
	err := newSystem().Interactive(context.Background(), Command{Name: "lclaw-no-such-binary-4f9c"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestSystemSensitiveDoesNotLogOutput(t *testing.T) {
	requireSh(t)
	var buf bytes.Buffer
	sys := &System{Log: slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))}
	// The secret travels over stdin, as a real sensitive command's would
	// (see the "security -i" fake test); if it were baked into Args instead,
	// the always-logged "args" attribute would leak it regardless of
	// Sensitive, which is not what this test is checking.
	_, err := sys.Run(context.Background(), Command{
		Name:      "sh",
		Args:      []string{"-c", "cat >&2; exit 1"},
		Stdin:     strings.NewReader("topsecret"),
		Sensitive: true,
	})
	if err == nil {
		t.Fatal("want exit error")
	}
	if strings.Contains(buf.String(), "topsecret") {
		t.Fatalf("log leaked stderr of a sensitive command:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "sensitive=true") {
		t.Fatalf("log should mark the command sensitive:\n%s", buf.String())
	}
}
