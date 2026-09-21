package exec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	osexec "os/exec"
	"strings"
	"time"
)

// System runs commands with os/exec. A child that ignores cancellation is
// killed after WaitDelay. Every command is logged at debug level with its
// duration and the tail of its stderr.
type System struct {
	Log       *slog.Logger  // nil discards
	WaitDelay time.Duration // zero means 5s
}

const maxStderrTail = 1024

// Run implements Runner.
func (s *System) Run(ctx context.Context, name string, args ...string) (Result, error) {
	log := s.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	delay := s.WaitDelay
	if delay == 0 {
		delay = 5 * time.Second
	}

	cmd := osexec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.WaitDelay = delay

	start := time.Now()
	err := cmd.Run()
	res := Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	log.Debug("exec",
		"name", name,
		"args", args,
		"duration", time.Since(start),
		"err", err,
		"stderr", tail(stderr.String()))

	if err == nil {
		return res, nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return res, fmt.Errorf("%s: %w", name, ctxErr)
	}
	var ee *osexec.ExitError
	if errors.As(err, &ee) {
		return res, &ExitError{Code: ee.ExitCode(), Stderr: tail(stderr.String())}
	}
	// Not found, permission denied, and anything else os/exec reports.
	// os/exec wraps ErrNotFound so errors.Is(err, ErrNotFound) holds.
	return res, fmt.Errorf("%s: %w", name, err)
}

// tail returns the trimmed last maxStderrTail bytes of s.
func tail(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > maxStderrTail {
		s = s[len(s)-maxStderrTail:]
	}
	return s
}
