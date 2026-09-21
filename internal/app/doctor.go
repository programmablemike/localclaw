package app

import (
	"context"

	"github.com/programmablemike/localclaw/internal/domain"
)

// Doctor checks that the host can run LocalClaw.
type Doctor struct {
	Runtime MachineRuntime
	Envs    EnvironmentManager
}

// Run executes the checks in a fixed order: flox version, podman version,
// then the machine list. Every problem a user can fix is a Check in the
// report; the returned error is non-nil only when the context ends.
func (d *Doctor) Run(ctx context.Context) (domain.Report, error) {
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
