// Package exec is the single seam between lclaw and the operating system.
// Every external command runs through a Runner, and the Fake in this package
// is the test double every other adapter uses.
package exec

import (
	"context"
	"fmt"
	"io"
	osexec "os/exec"
)

// Command is one external command. Stdin, when non-nil, is written to the
// child's standard input. Sensitive marks a command whose output must never
// be logged; its name, arguments, duration and exit status still are. The
// returned Result and any ExitError still carry the output, because adapters
// read values from it (the keychain adapter reads a password line from
// stderr) and are responsible for stripping values before wrapping errors.
type Command struct {
	Name      string
	Args      []string
	Stdin     io.Reader
	Sensitive bool
}

// Result carries what a command wrote.
type Result struct {
	Stdout, Stderr []byte
}

// Runner runs external commands. Run captures both output streams.
// Interactive inherits the terminal and captures nothing; it exists for
// commands that must prompt the user themselves, such as
// `security create-keychain`.
type Runner interface {
	Run(ctx context.Context, c Command) (Result, error)
	Interactive(ctx context.Context, c Command) error
}

// ErrNotFound is returned, wrapped, when the executable is not on PATH.
// It is the same value as os/exec.ErrNotFound so errors.Is works on either.
var ErrNotFound = osexec.ErrNotFound

// ExitError reports a command that ran and exited non-zero. Stderr holds the
// trimmed tail of what it wrote to standard error, or "" for an interactive
// command.
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
