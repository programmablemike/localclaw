package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/programmablemike/localclaw/internal/domain"
)

// Secrets is the use case behind the `lclaw secrets` command group. Every
// method loads the topology from dir, builds the catalogue, and refuses to
// go on when the keychain file is missing, because `security` would
// otherwise fall back to the login keychain. The commands write the
// keychain and, where a machine that uses the secret is running, that
// machine's store. They never apply pods.
type Secrets struct {
	Scaffold DirOpener
	Topology TopologyLoader
	Keychain Keychain
	Target   SecretTarget
	Minter   KeyMinter
	Random   io.Reader // crypto/rand.Reader in production
}

// SecretStatus is one row of `secrets list`.
type SecretStatus struct {
	Spec   domain.SecretSpec
	Source domain.Source // the stored source when set, else the default
	Set    bool
}

// StoreState is what happened, or what is true, in the machine's store.
// The strings are part of the CLI's interface.
type StoreState string

const (
	StoreStored  StoreState = "stored"
	StoreRemoved StoreState = "removed"
	StoreAbsent  StoreState = "absent"
	StoreStopped StoreState = "stopped"
	StoreFailed  StoreState = "failed"
)

// StoreOutcome is the store's row in an Outcome or a SecretDetail.
type StoreOutcome struct {
	State StoreState
	Err   error
}

// SecretDetail is what `secrets describe` shows. Never the value.
type SecretDetail struct {
	SecretStatus
	Created  time.Time
	Modified time.Time
	Store    StoreOutcome
}

// Outcome is what set, update and delete report: the keychain succeeded,
// and here is what happened in the machine's store.
type Outcome struct {
	Name    string
	Store   StoreOutcome
	Warning string
}

// Failed reports whether the store failed.
func (o Outcome) Failed() bool { return o.Store.State == StoreFailed }

// prepare loads the topology from the scaffold directory and builds the
// catalogue, exactly as Doctor reaches it: open the directory, then decode
// the file inside it.
func (s *Secrets) prepare(dir string) (domain.Topology, domain.Catalogue, error) {
	fsys, err := s.Scaffold.OpenDir(dir)
	if err != nil {
		return domain.Topology{}, domain.Catalogue{}, fmt.Errorf("secrets: %w", err)
	}
	topo, err := s.Topology.Load(fsys)
	if err != nil {
		return domain.Topology{}, domain.Catalogue{}, fmt.Errorf("secrets: %w", err)
	}
	cat, findings := domain.NewCatalogue(domain.BuiltinSecrets(), topo.DeclaredSecrets())
	if len(findings) > 0 {
		return domain.Topology{}, domain.Catalogue{}, &FindingsError{Findings: findings}
	}
	return topo, cat, nil
}

// requireKeychain fails when the keychain file is absent. Every write path
// depends on this check: `security add-generic-password` with a missing
// keychain path silently writes to the default keychain instead.
func requireKeychain(ctx context.Context, kc Keychain, path string) error {
	exists, err := kc.Exists(ctx, path)
	if err != nil {
		return fmt.Errorf("secrets: %w", err)
	}
	if !exists {
		return fmt.Errorf("%w at %s; run `lclaw init` to create it", ErrKeychainMissing, path)
	}
	return nil
}

func lookup(cat domain.Catalogue, dir, name string) (domain.SecretSpec, error) {
	spec, ok := cat.Lookup(name)
	if !ok {
		return domain.SecretSpec{}, fmt.Errorf("%w: %q is not in the catalogue; declare it under zones.<role>.secrets in %s",
			ErrUnknownSecret, name, filepath.Join(dir, domain.TopologyFile))
	}
	return spec, nil
}

// List returns one status per catalogue entry, in name order.
func (s *Secrets) List(ctx context.Context, dir string) ([]SecretStatus, error) {
	topo, cat, err := s.prepare(dir)
	if err != nil {
		return nil, err
	}
	if err := requireKeychain(ctx, s.Keychain, topo.KeychainPath); err != nil {
		return nil, err
	}
	entries := cat.Entries()
	out := make([]SecretStatus, 0, len(entries))
	for _, spec := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		st := SecretStatus{Spec: spec, Source: spec.Source}
		item, err := s.Keychain.Describe(ctx, topo.KeychainPath, spec.Name)
		switch {
		case errors.Is(err, ErrSecretNotFound):
		case err != nil:
			return nil, fmt.Errorf("secrets: describe %s: %w", spec.Name, err)
		default:
			st.Set, st.Source = true, item.Source
		}
		out = append(out, st)
	}
	return out, nil
}

// Describe returns everything about one secret except its value.
func (s *Secrets) Describe(ctx context.Context, dir, name string) (SecretDetail, error) {
	topo, cat, err := s.prepare(dir)
	if err != nil {
		return SecretDetail{}, err
	}
	spec, err := lookup(cat, dir, name)
	if err != nil {
		return SecretDetail{}, err
	}
	if err := requireKeychain(ctx, s.Keychain, topo.KeychainPath); err != nil {
		return SecretDetail{}, err
	}
	d := SecretDetail{SecretStatus: SecretStatus{Spec: spec, Source: spec.Source}}
	item, err := s.Keychain.Describe(ctx, topo.KeychainPath, name)
	switch {
	case errors.Is(err, ErrSecretNotFound):
	case err != nil:
		return SecretDetail{}, fmt.Errorf("secrets: describe %s: %w", name, err)
	default:
		d.Set, d.Source, d.Created, d.Modified = true, item.Source, item.Created, item.Modified
	}
	d.Store = s.inspect(ctx, name)
	return d, nil
}

// inspect reports what the machine's store holds for name.
func (s *Secrets) inspect(ctx context.Context, name string) StoreOutcome {
	var so StoreOutcome
	running, err := s.Target.MachineRunning(ctx)
	switch {
	case err != nil:
		so.State, so.Err = StoreFailed, err
	case !running:
		so.State = StoreStopped
	default:
		exists, err := s.Target.SecretExists(ctx, name)
		switch {
		case err != nil:
			so.State, so.Err = StoreFailed, err
		case exists:
			so.State = StoreStored
		default:
			so.State = StoreAbsent
		}
	}
	return so
}

// Set creates a secret with source user and pushes it to the machine's
// store when the machine is running.
func (s *Secrets) Set(ctx context.Context, dir, name string, value []byte) (Outcome, error) {
	topo, cat, err := s.prepare(dir)
	if err != nil {
		return Outcome{}, err
	}
	spec, err := lookup(cat, dir, name)
	if err != nil {
		return Outcome{}, err
	}
	if err := domain.ValidateValue(value); err != nil {
		return Outcome{}, err
	}
	if err := requireKeychain(ctx, s.Keychain, topo.KeychainPath); err != nil {
		return Outcome{}, err
	}
	if err := s.Keychain.Put(ctx, topo.KeychainPath, name, value, domain.User, false); err != nil {
		if errors.Is(err, ErrSecretExists) {
			return Outcome{}, fmt.Errorf("%w: %q; run `lclaw secrets update %s` to replace it", ErrSecretExists, name, name)
		}
		return Outcome{}, fmt.Errorf("secrets: set %s: %w", name, err)
	}
	return s.push(ctx, spec, value)
}

// Update replaces an existing secret. With generate it re-runs the
// generator or the minter and restores the default source; without it the
// source becomes user. Refused with ErrNotGeneratable for user-declared
// names and for entries that may not be rotated.
func (s *Secrets) Update(ctx context.Context, dir, name string, value []byte, generate bool) (Outcome, error) {
	topo, cat, err := s.prepare(dir)
	if err != nil {
		return Outcome{}, err
	}
	spec, err := lookup(cat, dir, name)
	if err != nil {
		return Outcome{}, err
	}
	source := domain.User
	if generate {
		switch {
		case spec.Source == domain.User:
			return Outcome{}, fmt.Errorf("%w: %q is user-supplied and has nothing to generate", ErrNotGeneratable, name)
		case !spec.Rotatable:
			return Outcome{}, fmt.Errorf("%w: %q may not be rotated", ErrNotGeneratable, name)
		}
		source = spec.Source
	} else if err := domain.ValidateValue(value); err != nil {
		return Outcome{}, err
	}
	if err := requireKeychain(ctx, s.Keychain, topo.KeychainPath); err != nil {
		return Outcome{}, err
	}
	if _, err := s.Keychain.Describe(ctx, topo.KeychainPath, name); err != nil {
		if errors.Is(err, ErrSecretNotFound) {
			return Outcome{}, fmt.Errorf("%w: %q is not set; run `lclaw secrets set %s` first", ErrSecretNotFound, name, name)
		}
		return Outcome{}, fmt.Errorf("secrets: update %s: %w", name, err)
	}
	if generate {
		value, err = s.produce(ctx, spec)
		if err != nil {
			return Outcome{}, fmt.Errorf("secrets: update %s: %w", name, err)
		}
	}
	if err := s.Keychain.Put(ctx, topo.KeychainPath, name, value, source, true); err != nil {
		return Outcome{}, fmt.Errorf("secrets: update %s: %w", name, err)
	}
	return s.push(ctx, spec, value)
}

// produce makes a value the way the entry's default source says.
func (s *Secrets) produce(ctx context.Context, spec domain.SecretSpec) ([]byte, error) {
	if spec.Source == domain.Minted {
		return s.Minter.Mint(ctx, spec.Name)
	}
	return spec.Generator.Generate(s.Random)
}

// Delete removes the keychain item and the secret from the machine's store
// when the machine is running. Deleting an entry that may not be rotated
// sets a warning, because the next up generates a new value.
func (s *Secrets) Delete(ctx context.Context, dir, name string) (Outcome, error) {
	topo, cat, err := s.prepare(dir)
	if err != nil {
		return Outcome{}, err
	}
	spec, err := lookup(cat, dir, name)
	if err != nil {
		return Outcome{}, err
	}
	if err := requireKeychain(ctx, s.Keychain, topo.KeychainPath); err != nil {
		return Outcome{}, err
	}
	if err := s.Keychain.Delete(ctx, topo.KeychainPath, name); err != nil {
		if errors.Is(err, ErrSecretNotFound) {
			return Outcome{}, fmt.Errorf("%w: %q is not set", ErrSecretNotFound, name)
		}
		return Outcome{}, fmt.Errorf("secrets: delete %s: %w", name, err)
	}
	o := Outcome{Name: name}
	if !spec.Rotatable {
		o.Warning = fmt.Sprintf("%s may not be rotated: the next `lclaw up` generates a new value and anything encrypted with the old one becomes unreadable", name)
	}
	so := s.inspect(ctx, name)
	if so.State == StoreStored {
		if err := s.Target.RemoveSecret(ctx, name); err != nil {
			so.State, so.Err = StoreFailed, err
		} else {
			so.State = StoreRemoved
		}
	}
	o.Store = so
	return o, nil
}

// Get returns the value. It is the only method that does.
func (s *Secrets) Get(ctx context.Context, dir, name string) ([]byte, error) {
	topo, cat, err := s.prepare(dir)
	if err != nil {
		return nil, err
	}
	if _, err := lookup(cat, dir, name); err != nil {
		return nil, err
	}
	if err := requireKeychain(ctx, s.Keychain, topo.KeychainPath); err != nil {
		return nil, err
	}
	value, err := s.Keychain.Get(ctx, topo.KeychainPath, name)
	if err != nil {
		if errors.Is(err, ErrSecretNotFound) {
			return nil, fmt.Errorf("%w: %q is not set", ErrSecretNotFound, name)
		}
		return nil, fmt.Errorf("secrets: get %s: %w", name, err)
	}
	return value, nil
}

// push stores value in the machine's store when the machine is running and
// reports what happened. A store failure does not roll back: the keychain
// is the source of truth and the next up reconciles.
func (s *Secrets) push(ctx context.Context, spec domain.SecretSpec, value []byte) (Outcome, error) {
	o := Outcome{Name: spec.Name}
	if err := ctx.Err(); err != nil {
		return o, err
	}
	var so StoreOutcome
	running, err := s.Target.MachineRunning(ctx)
	switch {
	case err != nil:
		so.State, so.Err = StoreFailed, err
	case !running:
		so.State = StoreStopped
	default:
		if err := s.Target.StoreSecret(ctx, spec.Name, value); err != nil {
			so.State, so.Err = StoreFailed, err
		} else {
			so.State = StoreStored
		}
	}
	o.Store = so
	return o, nil
}
