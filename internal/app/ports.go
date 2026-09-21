// Package app holds lclaw's use cases and the ports they consume. Ports are
// named for the role they play, not the vendor that fills them, and live
// beside their consumers as Go convention prefers.
package app

import (
	"context"

	"github.com/programmablemike/localclaw/internal/domain"
)

// MachineRuntime is what the use cases need from Podman.
type MachineRuntime interface {
	Version(ctx context.Context) (domain.Version, error)
	ListMachines(ctx context.Context) ([]domain.Machine, error)
}

// EnvironmentManager is what the use cases need from Flox.
type EnvironmentManager interface {
	Version(ctx context.Context) (domain.Version, error)
}

// ErrToolNotFound is the sentinel adapters wrap when an executable is
// absent. It is the domain's value so the evaluation rules can recognise it.
var ErrToolNotFound = domain.ErrToolNotFound
