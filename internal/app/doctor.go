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

	r.Checks = append(r.Checks, d.scaffoldChecks(dir)...)
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
// per validation finding, so every problem is visible in one run.
func (d *Doctor) scaffoldChecks(dir string) []domain.Check {
	fsys, err := d.Scaffold.OpenDir(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return []domain.Check{
			{Name: "scaffold", Status: domain.Warn, Summary: dir + " not found", Hint: "run `lclaw init` to write the default files"},
			{Name: "topology", Status: domain.Warn, Summary: "skipped because the scaffold check failed", Hint: "run `lclaw init`, then `lclaw doctor` again"},
		}
	case err != nil:
		return []domain.Check{
			{Name: "scaffold", Status: domain.Fail, Summary: err.Error()},
			{Name: "topology", Status: domain.Warn, Summary: "skipped because the scaffold check failed", Hint: "fix the scaffold directory, then run `lclaw doctor` again"},
		}
	}
	checks := []domain.Check{{Name: "scaffold", Status: domain.Pass, Summary: dir}}

	top, err := d.Topology.Load(fsys)
	if err != nil {
		c := domain.Check{Name: "topology", Status: domain.Fail, Summary: err.Error()}
		if errors.Is(err, fs.ErrNotExist) {
			c.Hint = "run `lclaw init` to write the default " + domain.TopologyFile
		}
		return append(checks, c)
	}
	exists := func(p string) bool {
		_, err := fs.Stat(fsys, p)
		return err == nil
	}
	findings := domain.Validate(top, exists)
	if len(findings) == 0 {
		summary := fmt.Sprintf("%d machines, %d workloads", len(top.Machines), len(top.Workloads()))
		return append(checks, domain.Check{Name: "topology", Status: domain.Pass, Summary: summary})
	}
	for _, f := range findings {
		checks = append(checks, domain.Check{Name: "topology", Status: domain.Fail, Summary: f.String()})
	}
	return checks
}
