package app

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/programmablemike/localclaw/internal/domain"
)

// ResolvedSecret is one value ready to be injected.
type ResolvedSecret struct {
	Name  string
	Value []byte
}

// ResolveSecrets is the first of the two steps up runs per zone: read
// every catalogue entry the zone consumes from the keychain, generating or
// minting what is missing. A missing user secret is a finding, and findings
// are collected rather than thrown so up can report all of them and stop
// before touching Podman.
type ResolveSecrets struct {
	Keychain Keychain
	Minter   KeyMinter
	Random   io.Reader
}

// Run returns the resolved values in catalogue order, the findings, and an
// error only for cancellation or a broken keychain. It refuses when the
// keychain file is absent, before touching it any other way.
func (r *ResolveSecrets) Run(ctx context.Context, path string, cat domain.Catalogue, role domain.Role) ([]ResolvedSecret, []domain.Check, error) {
	if err := requireKeychain(ctx, r.Keychain, path); err != nil {
		return nil, nil, err
	}
	var resolved []ResolvedSecret
	var findings []domain.Check
	for _, spec := range cat.ForZone(role) {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		value, err := r.Keychain.Get(ctx, path, spec.Name)
		switch {
		case err == nil:
		case !errors.Is(err, ErrSecretNotFound):
			return nil, nil, fmt.Errorf("resolve secrets: %s: %w", spec.Name, err)
		case spec.Source == domain.User:
			findings = append(findings, domain.Check{Name: spec.Name, Status: domain.Fail, Summary: "unset", Hint: "run `lclaw secrets set " + spec.Name + "`"})
			continue
		case spec.Source == domain.Minted:
			value, err = r.Minter.Mint(ctx, spec.Name)
			if err != nil {
				findings = append(findings, domain.Check{Name: spec.Name, Status: domain.Fail, Summary: "not minted: " + err.Error(), Hint: "run `lclaw up services` first"})
				continue
			}
			if err := r.Keychain.Put(ctx, path, spec.Name, value, domain.Minted, false); err != nil {
				return nil, nil, fmt.Errorf("resolve secrets: store %s: %w", spec.Name, err)
			}
		default:
			value, err = spec.Generator.Generate(r.Random)
			if err != nil {
				return nil, nil, fmt.Errorf("resolve secrets: %s: %w", spec.Name, err)
			}
			if err := r.Keychain.Put(ctx, path, spec.Name, value, domain.Generated, false); err != nil {
				return nil, nil, fmt.Errorf("resolve secrets: store %s: %w", spec.Name, err)
			}
		}
		resolved = append(resolved, ResolvedSecret{Name: spec.Name, Value: value})
	}
	return resolved, findings, nil
}

// InjectSecrets is the second step: store each resolved value in the
// machine's Podman secret store. The target replaces existing secrets, so
// running up twice is safe.
type InjectSecrets struct {
	Target SecretTarget
}

// Run stores every secret, stopping at the first failure.
func (i *InjectSecrets) Run(ctx context.Context, secrets []ResolvedSecret) error {
	for _, s := range secrets {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := i.Target.StoreSecret(ctx, s.Name, s.Value); err != nil {
			return fmt.Errorf("inject secrets: %s: %w", s.Name, err)
		}
	}
	return nil
}

// PurgeSecrets is the step a full down runs after the last kube down and
// before the machine stops: remove every secret lclaw stored, found by
// label rather than by catalogue so entries dropped since the last up are
// cleaned up too, and the named volume of the same name if one exists.
type PurgeSecrets struct {
	Target SecretTarget
}

// Run returns the names it removed.
func (p *PurgeSecrets) Run(ctx context.Context) ([]string, error) {
	names, err := p.Target.ListSecrets(ctx)
	if err != nil {
		return nil, fmt.Errorf("purge secrets: %w", err)
	}
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := p.Target.RemoveSecret(ctx, name); err != nil {
			return nil, fmt.Errorf("purge secrets: remove %s: %w", name, err)
		}
		if err := p.Target.RemoveVolume(ctx, name); err != nil {
			return nil, fmt.Errorf("purge secrets: remove volume %s: %w", name, err)
		}
	}
	return names, nil
}
