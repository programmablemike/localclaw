package exec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	osexec "os/exec"
	"strings"
	"time"
)

// System runs commands with os/exec. A child that ignores cancellation is
// killed after WaitDelay. Every command is logged at debug level with its
// duration; a non-sensitive command also logs the tail of its stderr.
type System struct {
	Log       *slog.Logger  // nil discards
	WaitDelay time.Duration // zero means 5s
}

const maxStderrTail = 1024

func (s *System) settings() (*slog.Logger, time.Duration) {
	log := s.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	delay := s.WaitDelay
	if delay == 0 {
		delay = 5 * time.Second
	}
	return log, delay
}

// Run implements Runner.
func (s *System) Run(ctx context.Context, c Command) (Result, error) {
	log, delay := s.settings()

	cmd := osexec.CommandContext(ctx, c.Name, c.Args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdin = c.Stdin
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.WaitDelay = delay

	start := time.Now()
	err := cmd.Run()
	res := Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	attrs := []any{"name", c.Name, "args", c.Args, "duration", time.Since(start), "err", err}
	if c.Sensitive {
		attrs = append(attrs, "sensitive", true)
	} else {
		attrs = append(attrs, "stderr", tail(stderr.String()))
	}
	log.Debug("exec", attrs...)

	return res, classify(ctx, c.Name, err, tail(stderr.String()))
}

// Interactive implements Runner. The child inherits this process's standard
// streams, so it can prompt on the terminal; nothing is captured.
func (s *System) Interactive(ctx context.Context, c Command) error {
	log, delay := s.settings()

	cmd := osexec.CommandContext(ctx, c.Name, c.Args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.WaitDelay = delay

	start := time.Now()
	err := cmd.Run()
	log.Debug("exec interactive", "name", c.Name, "args", c.Args, "duration", time.Since(start), "err", err)
	return classify(ctx, c.Name, err, "")
}

// classify turns what os/exec reported into this package's error values.
func classify(ctx context.Context, name string, err error, stderrTail string) error {
	if err == nil {
		return nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("%s: %w", name, ctxErr)
	}
	var ee *osexec.ExitError
	if errors.As(err, &ee) {
		return &ExitError{Code: ee.ExitCode(), Stderr: stderrTail}
	}
	// Not found, permission denied, and anything else os/exec reports.
	// os/exec wraps ErrNotFound so errors.Is(err, ErrNotFound) holds.
	return fmt.Errorf("%s: %w", name, err)
}

// tail returns the trimmed last maxStderrTail bytes of s.
func tail(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > maxStderrTail {
		s = s[len(s)-maxStderrTail:]
	}
	return s
}
