package cli

import (
	"context"
	"errors"

	"github.com/programmablemike/localclaw/internal/app"
	"github.com/programmablemike/localclaw/internal/domain"
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
//	1   failed checks, failed init writes, a failed keychain step, a
//	    secret that already exists or is not set, or any other
//	    unexpected error
//	2   usage error (unknown flag, bad --output value, an unknown secret
//	    name, an invalid secret value, or a refused --generate)
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
	case errors.Is(err, app.ErrUnknownSecret),
		errors.Is(err, domain.ErrInvalidValue),
		errors.Is(err, app.ErrNotGeneratable),
		errors.Is(err, app.ErrMinterUnavailable):
		return 2
	default:
		return 1
	}
}

// Silent reports whether err's message has already been printed by the cli
// package, so the composition root must not print it again: a usage error
// prints itself via usage(), and ErrChecksFailed/ErrInitFailed follow a
// report that already named what failed. Every other error — including the
// secrets command group's sentinel errors — has not been printed anywhere
// yet, and the composition root must print it.
func Silent(err error) bool {
	var ue *usageError
	switch {
	case err == nil:
		return true
	case errors.As(err, &ue):
		return true
	case errors.Is(err, ErrChecksFailed):
		return true
	case errors.Is(err, ErrInitFailed):
		return true
	default:
		return false
	}
}
