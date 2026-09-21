package exec

import (
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
	res, err := newSystem().Run(context.Background(), "sh", "-c", "echo out; echo err >&2")
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
	res, err := newSystem().Run(context.Background(), "sh", "-c", "echo partial; echo boom >&2; exit 3")
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
	_, err := newSystem().Run(context.Background(), "lclaw-no-such-binary-4f9c")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestSystemRunHonoursCancellation(t *testing.T) {
	requireSh(t)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := newSystem().Run(ctx, "sh", "-c", "sleep 5")
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
