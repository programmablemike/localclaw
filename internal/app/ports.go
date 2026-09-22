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

// MachineInit is what creating the machine needs. Provider is passed to
// Podman as CONTAINERS_MACHINE_PROVIDER; Playbook is the first-boot
// playbook path on the host; Volumes are "host:guest" mounts, and none
// means nothing from the host is mounted.
type MachineInit struct {
	Name      string
	Provider  string
	CPUs      int
	MemoryMiB int
	DiskGiB   int
	Volumes   []string
	Playbook  string
}

// MachineRuntime is what the use cases need from Podman about machines.
// Every call that names a machine also names the provider, because Podman
// creates and drives machines on the provider the environment selects.
type MachineRuntime interface {
	Version(ctx context.Context) (domain.Version, error)
	ListMachines(ctx context.Context) ([]domain.Machine, error)
	InitMachine(ctx context.Context, m MachineInit) error
	StartMachine(ctx context.Context, provider, name string) error
	StopMachine(ctx context.Context, provider, name string) error
	RemoveMachine(ctx context.Context, provider, name string) error
}

// WorkloadRuntime is what the use cases need from Podman inside the
// machine: networks, images and pods, all through the remote connection.
// Build's context and Play's and Down's file are host paths.
type WorkloadRuntime interface {
	NetworkExists(ctx context.Context, name string) (bool, error)
	CreateNetwork(ctx context.Context, name string, internal bool) error
	RemoveNetwork(ctx context.Context, name string) error
	Build(ctx context.Context, tag, contextDir string) error
	Play(ctx context.Context, file string, networks []string, userns string) error
	Down(ctx context.Context, file string) error
	ListPods(ctx context.Context) ([]domain.Pod, error)
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

// SecretTarget is the machine's Podman secret store. StoreSecret replaces
// an existing secret of the same name. ListSecrets returns only the secrets
// lclaw created, found by label. RemoveVolume succeeds when no such volume
// exists.
type SecretTarget interface {
	MachineRunning(ctx context.Context) (bool, error)
	StoreSecret(ctx context.Context, name string, value []byte) error
	SecretExists(ctx context.Context, name string) (bool, error)
	RemoveSecret(ctx context.Context, name string) error
	ListSecrets(ctx context.Context) ([]string, error)
	RemoveVolume(ctx context.Context, name string) error
}

// KeyMinter obtains a secret from a running service and revokes it again.
// Only the agent's LiteLLM virtual key is minted. Ready blocks until the
// service can serve or the context ends.
type KeyMinter interface {
	Ready(ctx context.Context) error
	Mint(ctx context.Context, name string) ([]byte, error)
	Revoke(ctx context.Context, name string, value []byte) error
}

// Progress hears about each lifecycle step as it begins, so the
// presentation layer can show that something is happening during a boot or
// a build. Implementations must not block.
type Progress interface {
	Step(name, detail string)
}

// NoProgress discards every step.
type NoProgress struct{}

// Step implements Progress.
func (NoProgress) Step(string, string) {}

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
	// ErrMinterUnavailable is wrapped when the minter cannot reach LiteLLM,
	// which means the services zone is not up.
	ErrMinterUnavailable = errors.New("minting needs the services zone up; run `lclaw up services` first")
)

// FindingsError carries lclaw.toml findings out of a use case so the
// presentation layer can render them the way doctor renders its report.
type FindingsError struct {
	Findings []domain.Finding
}

func (e *FindingsError) Error() string {
	return fmt.Sprintf("%s has %d problem(s)", domain.TopologyFile, len(e.Findings))
}

// UnavailableMinter fills the KeyMinter port where no LiteLLM can be
// reached, such as in tests; every call reports ErrMinterUnavailable.
type UnavailableMinter struct{}

// Ready implements KeyMinter.
func (UnavailableMinter) Ready(context.Context) error { return ErrMinterUnavailable }

// Mint implements KeyMinter.
func (UnavailableMinter) Mint(context.Context, string) ([]byte, error) {
	return nil, ErrMinterUnavailable
}

// Revoke implements KeyMinter.
func (UnavailableMinter) Revoke(context.Context, string, []byte) error { return ErrMinterUnavailable }
