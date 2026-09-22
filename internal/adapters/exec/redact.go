package exec

import (
	"errors"
	"strings"
)

// FragmentLen is the shortest run of an encoded secret that Redact treats as
// a leak. Sixteen characters is eight bytes of a hexadecimal encoding and
// twelve of a base64 one: short enough that no useful part of a value
// survives, long enough that an ordinary diagnostic line does not collide
// with a high-entropy value by accident.
const FragmentLen = 16

// Redact returns err with every stderr line carrying any part of secret
// removed. Non-exit errors pass through untouched and the exit code is kept.
//
// A whole-string match is not enough. Stderr reaches ExitError as its
// trimmed last maxStderrTail bytes, so a command that echoes a long value
// back leaves only a fragment of it behind, and a value that wrapped across
// lines was never on one line to begin with. Redact therefore drops any line
// sharing a run of FragmentLen or more characters with secret, which covers
// both. A secret shorter than that is matched whole.
func Redact(err error, secret string) error {
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || secret == "" || exitErr.Stderr == "" {
		return err
	}
	lines := strings.Split(exitErr.Stderr, "\n")
	kept := lines[:0]
	fragments := fragmentSet(secret)
	for _, l := range lines {
		if !carries(l, secret, fragments) {
			kept = append(kept, l)
		}
	}
	return &ExitError{Code: exitErr.Code, Stderr: strings.TrimSpace(strings.Join(kept, "\n"))}
}

// fragmentSet returns every FragmentLen-character window of secret, or nil
// when secret is too short to have one.
func fragmentSet(secret string) map[string]struct{} {
	if len(secret) < FragmentLen {
		return nil
	}
	set := make(map[string]struct{}, len(secret)-FragmentLen+1)
	for i := 0; i+FragmentLen <= len(secret); i++ {
		set[secret[i:i+FragmentLen]] = struct{}{}
	}
	return set
}

// carries reports whether line shares a run of FragmentLen characters with
// secret. It walks line rather than secret, because stderr is bounded to
// maxStderrTail while a value is not.
func carries(line, secret string, fragments map[string]struct{}) bool {
	if fragments == nil {
		return strings.Contains(line, secret)
	}
	for i := 0; i+FragmentLen <= len(line); i++ {
		if _, ok := fragments[line[i:i+FragmentLen]]; ok {
			return true
		}
	}
	return false
}
