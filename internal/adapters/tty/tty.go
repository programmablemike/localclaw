// Package tty is the terminal seam: whether a stream is a terminal, and a
// prompt that reads without echo. It is the only package that imports
// golang.org/x/term.
package tty

import (
	"fmt"
	"io"
	"os"

	"golang.org/x/term"
)

// IsTerminal reports whether f is attached to a terminal. A nil f is never
// a terminal; this is checked explicitly rather than relying on os.File's
// own nil handling in Fd().
func IsTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

// ReadPassword writes prompt to w, reads one line from f with echo off,
// and ends the prompt line on w whether or not the read succeeded. The
// value is returned, never echoed and never logged.
func ReadPassword(f *os.File, w io.Writer, prompt string) ([]byte, error) {
	fmt.Fprint(w, prompt)
	defer fmt.Fprintln(w)
	b, err := term.ReadPassword(int(f.Fd()))
	if err != nil {
		return nil, fmt.Errorf("tty: read password: %w", err)
	}
	return b, nil
}
