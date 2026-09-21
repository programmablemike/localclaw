package cli

import (
	"context"
	"errors"
)

// ErrChecksFailed is returned by doctor when at least one check failed. The
// report has already said what failed, so the composition root prints
// nothing more for it.
var ErrChecksFailed = errors.New("checks failed")

// ErrInitFailed is returned by init when a file could not be written or
// the keychain could not be created. The report has already named what
// failed, so nothing more is printed for it.
var ErrInitFailed = errors.New("init did not complete")

// ExitCode maps the error returned by the root command to a process exit
// status. It is the only place that mapping lives.
//
//	0   success, including warnings
//	1   failed checks, failed init writes, a failed keychain step or any
//	    unexpected error
//	2   usage error (unknown flag, bad --output value)
//	130 interrupted
func ExitCode(err error) int {
	var ue *usageError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &ue):
		return 2
	case errors.Is(err, context.Canceled):
		return 130
	default:
		return 1
	}
}
