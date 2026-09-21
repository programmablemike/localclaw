package tty

import (
	"bytes"
	"os"
	"testing"
)

func TestIsTerminalFalseForPipe(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	if IsTerminal(r) {
		t.Fatal("a pipe is not a terminal")
	}
}

func TestReadPasswordWritesPromptAndFailsOffTerminal(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	var out bytes.Buffer
	if _, err := ReadPassword(r, &out, "value: "); err == nil {
		t.Fatal("want an error reading a password from a pipe")
	}
	// The newline ends the prompt line whether or not the read succeeded,
	// so a failed prompt does not leave the cursor mid-line.
	if got := out.String(); got != "value: \n" {
		t.Fatalf("prompt output = %q", got)
	}
}
