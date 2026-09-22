package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/programmablemike/localclaw/internal/domain"
)

// Doctor checks that the host can run LocalClaw.
type Doctor struct {
	Runtime  MachineRuntime
	Envs     EnvironmentManager
	Scaffold DirOpener
	Topology TopologyLoader
	Keychain Keychain
}

// Run executes the checks in a fixed order: flox version, podman version,
// the scaffold directory at dir, its topology, then the machine list. Every
// problem a user can fix is a Check in the report; the returned error is
// non-nil only when the context ends.
func (d *Doctor) Run(ctx context.Context, dir string) (domain.Report, error) {
	var r domain.Report

	fv, err := d.Envs.Version(ctx)
	r.Checks = append(r.Checks, domain.EvaluateTool(domain.FloxRequirement, fv, err))
	if err := ctx.Err(); err != nil {
		return r, err
	}

	pv, err := d.Runtime.Version(ctx)
	podman := domain.EvaluateTool(domain.PodmanRequirement, pv, err)
	r.Checks = append(r.Checks, podman)
	if err := ctx.Err(); err != nil {
		return r, err
	}

	scaffold, top, topologyOK := d.scaffoldChecks(dir)
	r.Checks = append(r.Checks, scaffold...)
	r.Checks = append(r.Checks, d.secretChecks(ctx, top, topologyOK)...)
	if err := ctx.Err(); err != nil {
		return r, err
	}

	if podman.Status == domain.Fail {
		r.Checks = append(r.Checks, domain.Check{
			Name:    "machines",
			Status:  domain.Warn,
			Summary: "skipped because the podman check failed",
			Hint:    "fix podman, then run `lclaw doctor` again",
		})
		return r, nil
	}

	machines, err := d.Runtime.ListMachines(ctx)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return r, ctxErr
	}
	if err != nil {
		r.Checks = append(r.Checks, domain.Check{Name: "machines", Status: domain.Fail, Summary: err.Error()})
		return r, nil
	}
	r.Checks = append(r.Checks, domain.EvaluateMachines(domain.Roles(), machines)...)
	return r, nil
}

// scaffoldChecks reports whether dir exists and whether its topology is
// valid: one check named scaffold, then either one topology check or one
// per validation finding, so every problem is visible in one run. It also
// returns the loaded topology and whether it is usable, so the caller can
// check the keychain and every secret it declares.
func (d *Doctor) scaffoldChecks(dir string) ([]domain.Check, domain.Topology, bool) {
	fsys, err := d.Scaffold.OpenDir(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return []domain.Check{
			{Name: "scaffold", Status: domain.Warn, Summary: dir + " not found", Hint: "run `lclaw init` to write the default files"},
			{Name: "topology", Status: domain.Warn, Summary: "skipped because the scaffold check failed", Hint: "run `lclaw init`, then `lclaw doctor` again"},
		}, domain.Topology{}, false
	case err != nil:
		return []domain.Check{
			{Name: "scaffold", Status: domain.Fail, Summary: err.Error()},
			{Name: "topology", Status: domain.Warn, Summary: "skipped because the scaffold check failed", Hint: "fix the scaffold directory, then run `lclaw doctor` again"},
		}, domain.Topology{}, false
	}
	checks := []domain.Check{{Name: "scaffold", Status: domain.Pass, Summary: dir}}

	top, err := d.Topology.Load(fsys)
	if err != nil {
		c := domain.Check{Name: "topology", Status: domain.Fail, Summary: err.Error()}
		if errors.Is(err, fs.ErrNotExist) {
			c.Hint = "run `lclaw init` to write the default " + domain.TopologyFile
		}
		return append(checks, c), domain.Topology{}, false
	}
	exists := func(p string) bool {
		_, err := fs.Stat(fsys, p)
		return err == nil
	}
	findings := domain.Validate(top, exists)
	if len(findings) == 0 {
		summary := fmt.Sprintf("%d machines, %d workloads", len(top.Machines), len(top.Workloads()))
		return append(checks, domain.Check{Name: "topology", Status: domain.Pass, Summary: summary}), top, true
	}
	for _, f := range findings {
		checks = append(checks, domain.Check{Name: "topology", Status: domain.Fail, Summary: f.String()})
	}
	return checks, top, true
}

// secretChecks reports whether the keychain exists and whether every
// secret declared in lclaw.toml is set. Values are never read: Describe
// returns the item's attributes only. When the topology could not be read
// there is no keychain path to check, and when the keychain is missing
// there is nothing to ask it, so each case reports one skipped check
// rather than a cascade of failures.
func (d *Doctor) secretChecks(ctx context.Context, top domain.Topology, topologyOK bool) []domain.Check {
	if !topologyOK {
		return []domain.Check{{
			Name:    "keychain",
			Status:  domain.Warn,
			Summary: "skipped because the topology check failed",
			Hint:    "fix " + domain.TopologyFile + ", then run `lclaw doctor` again",
		}}
	}

	exists, err := d.Keychain.Exists(ctx, top.KeychainPath)
	keychain := domain.EvaluateKeychain(top.KeychainPath, exists, err)
	checks := []domain.Check{keychain}

	cat, _ := domain.NewCatalogue(domain.BuiltinSecrets(), top.DeclaredSecrets())
	var declared []domain.SecretSpec
	for _, spec := range cat.Entries() {
		if spec.Source == domain.User {
			declared = append(declared, spec)
		}
	}
	if len(declared) == 0 {
		return checks
	}
	if keychain.Status == domain.Fail {
		return append(checks, domain.Check{
			Name:    "secrets",
			Status:  domain.Warn,
			Summary: "skipped because the keychain check failed",
			Hint:    "run `lclaw init`, then `lclaw doctor` again",
		})
	}
	for _, spec := range declared {
		if ctx.Err() != nil {
			return checks
		}
		_, err := d.Keychain.Describe(ctx, top.KeychainPath, spec.Name)
		checks = append(checks, domain.EvaluateSecret(spec, err))
	}
	return checks
}
