// Package app holds lclaw's use cases and the ports they consume. Ports are
// named for the role they play, not the vendor that fills them, and live
// beside their consumers as Go convention prefers.
package app

import (
	"context"
	"errors"
	"fmt"
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

// Keychain is the host's secret store. Every method names the keychain
// file, because its path is configuration loaded at run time. Get returns
// the value; Describe returns everything but the value. Put refuses an
// existing item unless replace is set. A missing item wraps
// ErrSecretNotFound, and every method refuses to touch a keychain whose
// file does not exist.
type Keychain interface {
	Exists(ctx context.Context, path string) (bool, error)
	Create(ctx context.Context, path string) error
	Get(ctx context.Context, path, name string) ([]byte, error)
	Describe(ctx context.Context, path, name string) (domain.SecretItem, error)
	Put(ctx context.Context, path, name string, value []byte, source domain.Source, replace bool) error
	Delete(ctx context.Context, path, name string) error
}

// SecretTarget is a machine's Podman secret store. StoreSecret replaces an
// existing secret of the same name. ListSecrets returns only the secrets
// lclaw created, found by label. RemoveVolume succeeds when no such volume
// exists.
type SecretTarget interface {
	MachineRunning(ctx context.Context, role domain.Role) (bool, error)
	StoreSecret(ctx context.Context, role domain.Role, name string, value []byte) error
	SecretExists(ctx context.Context, role domain.Role, name string) (bool, error)
	RemoveSecret(ctx context.Context, role domain.Role, name string) error
	ListSecrets(ctx context.Context, role domain.Role) ([]string, error)
	RemoveVolume(ctx context.Context, role domain.Role, name string) error
}

// KeyMinter obtains a secret from a running service and revokes it again.
// Only the agent's LiteLLM virtual key is minted. The adapter belongs to
// the lifecycle design; until it lands, UnavailableMinter fills the port.
type KeyMinter interface {
	Mint(ctx context.Context, name string) ([]byte, error)
	Revoke(ctx context.Context, name string) error
}

var (
	// ErrSecretNotFound is the domain's value, wrapped by the keychain
	// adapter when an item is absent.
	ErrSecretNotFound = domain.ErrSecretNotFound
	// ErrSecretExists is wrapped when Put without replace hits an item.
	ErrSecretExists = errors.New("secret already exists")
	// ErrUnknownSecret is wrapped when a name is not in the catalogue.
	ErrUnknownSecret = errors.New("unknown secret")
	// ErrKeychainMissing is wrapped when the keychain file does not exist.
	ErrKeychainMissing = errors.New("keychain not found")
	// ErrKeychainLocked is wrapped when the keychain is locked and could
	// not be unlocked.
	ErrKeychainLocked = errors.New("keychain is locked")
	// ErrWrongPassword is wrapped when unlocking fails on the password.
	ErrWrongPassword = errors.New("wrong keychain password")
	// ErrNotGeneratable is wrapped when --generate is refused.
	ErrNotGeneratable = errors.New("secret cannot be generated")
	// ErrMinterUnavailable is what UnavailableMinter returns.
	ErrMinterUnavailable = errors.New("minting needs the services machine up, which the lifecycle commands will provide")
)

// UnavailableMinter fills the KeyMinter port until the lifecycle design
// lands an adapter that talks to LiteLLM.
type UnavailableMinter struct{}

// Mint implements KeyMinter.
func (UnavailableMinter) Mint(context.Context, string) ([]byte, error) {
	return nil, ErrMinterUnavailable
}

// Revoke implements KeyMinter.
func (UnavailableMinter) Revoke(context.Context, string) error { return ErrMinterUnavailable }

// FindingsError carries lclaw.toml findings out of a use case so the
// presentation layer can render them the way doctor renders its report.
type FindingsError struct {
	Findings []domain.Finding
}

func (e *FindingsError) Error() string {
	return fmt.Sprintf("%s has %d problem(s)", domain.TopologyFile, len(e.Findings))
}
