package exec

import (
	"errors"
	"strings"
	"testing"
)

func TestRedactDropsTheLineCarryingTheSecret(t *testing.T) {
	secret := strings.Repeat("0123456789abcdef", 8) // 128 characters
	err := Redact(&ExitError{Code: 1, Stderr: "tool: echoing " + secret + "\ntool: real diagnostic"}, secret)
	msg := err.Error()
	if strings.Contains(msg, secret) {
		t.Fatalf("err = %v, must not carry the secret", msg)
	}
	if !strings.Contains(msg, "real diagnostic") {
		t.Fatalf("err = %v, must keep the diagnostic", msg)
	}
	var ee *ExitError
	if !errors.As(err, &ee) || ee.Code != 1 {
		t.Fatalf("errors.As = %v, %v, want an *ExitError{Code: 1}", ee, err)
	}
}

// TestRedactDropsATruncatedSecret is the case a whole-string match misses:
// tail() keeps only the last maxStderrTail bytes, so a long value reaches
// Redact as a fragment of itself.
func TestRedactDropsATruncatedSecret(t *testing.T) {
	secret := strings.Repeat("0123456789abcdef", 200) // 3200 characters
	fragment := secret[1700:2600]
	err := Redact(&ExitError{Code: 1, Stderr: fragment + "\ntool: real diagnostic"}, secret)
	msg := err.Error()
	if strings.Contains(msg, fragment[:FragmentLen]) {
		t.Fatalf("err = %v, must not carry a fragment of the secret", msg)
	}
	if !strings.Contains(msg, "real diagnostic") {
		t.Fatalf("err = %v, must keep the diagnostic", msg)
	}
}

// TestRedactDropsAWrappedSecret covers a value the tool broke across lines,
// which was never wholly on any one of them.
func TestRedactDropsAWrappedSecret(t *testing.T) {
	secret := strings.Repeat("0123456789abcdef", 20) // 320 characters
	var b strings.Builder
	for i := 0; i < len(secret); i += 80 {
		b.WriteString(secret[i:i+80] + "\n")
	}
	b.WriteString("tool: real diagnostic")
	err := Redact(&ExitError{Code: 1, Stderr: b.String()}, secret)
	if got := err.Error(); strings.Contains(got, secret[:FragmentLen]) {
		t.Fatalf("err = %v, must not carry a wrapped line of the secret", got)
	}
}

// TestRedactMatchesAShortSecretWhole is the fallback for a value with no
// FragmentLen-character window.
func TestRedactMatchesAShortSecretWhole(t *testing.T) {
	err := Redact(&ExitError{Code: 1, Stderr: "tool: echoing abc123\ntool: real diagnostic"}, "abc123")
	msg := err.Error()
	if strings.Contains(msg, "abc123") {
		t.Fatalf("err = %v, must not carry the secret", msg)
	}
	if !strings.Contains(msg, "real diagnostic") {
		t.Fatalf("err = %v, must keep the diagnostic", msg)
	}
}

func TestRedactPassesOtherErrorsThrough(t *testing.T) {
	want := errors.New("security: executable file not found in $PATH")
	if got := Redact(want, "abc123"); !errors.Is(got, want) {
		t.Fatalf("Redact() = %v, want %v", got, want)
	}
	if got := Redact(&ExitError{Code: 1, Stderr: "tool: real diagnostic"}, ""); !strings.Contains(got.Error(), "real diagnostic") {
		t.Fatalf("Redact() with no secret = %v", got)
	}
}
