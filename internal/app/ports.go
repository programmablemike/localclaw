// Package app holds lclaw's use cases and the ports they consume. Ports are
// named for the role they play, not the vendor that fills them, and live
// beside their consumers as Go convention prefers.
package app

import (
	"context"
	"io/fs"

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

// FileWriter is the write side of the scaffold directory.
type FileWriter interface {
	// WriteFile writes data to path, creating parent directories as needed.
	// When overwrite is false and path already exists it returns an error
	// wrapping fs.ErrExist and leaves the file alone. A successful write is
	// atomic: the file is complete or absent, never half-written.
	WriteFile(path string, data []byte, overwrite bool) error
}

// DirOpener is the read side of the scaffold directory.
type DirOpener interface {
	// OpenDir returns dir as a file system rooted there. A missing directory
	// is an error wrapping fs.ErrNotExist.
	OpenDir(dir string) (fs.FS, error)
}

// TopologyLoader reads and decodes lclaw.toml at the root of a scaffold.
type TopologyLoader interface {
	// Load decodes domain.TopologyFile from scaffold. A missing file is an
	// error wrapping fs.ErrNotExist; malformed input and unknown keys are
	// errors naming the line or the keys.
	Load(scaffold fs.FS) (domain.Topology, error)
}
