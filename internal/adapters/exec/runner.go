// Package exec is the single seam between lclaw and the operating system.
// Every external command runs through a Runner, and the Fake in this package
// is the test double every other adapter uses.
package exec

import (
	"context"
	"fmt"
	osexec "os/exec"
)

// Result carries what a command wrote.
type Result struct {
	Stdout, Stderr []byte
}

// Runner runs one command to completion.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) (Result, error)
}

// ErrNotFound is returned, wrapped, when the executable is not on PATH.
// It is the same value as os/exec.ErrNotFound so errors.Is works on either.
var ErrNotFound = osexec.ErrNotFound

// ExitError reports a command that ran and exited non-zero. Stderr holds the
// trimmed tail of what it wrote to standard error.
type ExitError struct {
	Code   int
	Stderr string
}

func (e *ExitError) Error() string {
	if e.Stderr == "" {
		return fmt.Sprintf("exit status %d", e.Code)
	}
	return fmt.Sprintf("exit status %d: %s", e.Code, e.Stderr)
}
