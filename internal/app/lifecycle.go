package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"

	"github.com/programmablemike/localclaw/internal/domain"
)

// Lifecycle is the use case behind `lclaw up`, `lclaw down` and `lclaw
// status`. It composes the machine, the zone networks, the secrets steps
// and the per-workload apply sequence, in the order the lifecycle design
// fixes, and reports every step as a check.
type Lifecycle struct {
	Runtime   MachineRuntime
	Workloads WorkloadRuntime
	Scaffold  DirOpener
	Topology  TopologyLoader
	Keychain  Keychain
	Target    SecretTarget
	Minter    KeyMinter
	Random    io.Reader // crypto/rand.Reader in production
	Progress  Progress  // nil means NoProgress
}

func (l *Lifecycle) progress() Progress {
	if l.Progress == nil {
		return NoProgress{}
	}
	return l.Progress
}

// load reads and validates the topology. Findings come back as a
// FindingsError so the presentation layer renders them like doctor does.
func (l *Lifecycle) load(dir string) (domain.Topology, domain.Catalogue, error) {
	fsys, err := l.Scaffold.OpenDir(dir)
	if err != nil {
		return domain.Topology{}, domain.Catalogue{}, fmt.Errorf("lifecycle: %w", err)
	}
	top, err := l.Topology.Load(fsys)
	if err != nil {
		return domain.Topology{}, domain.Catalogue{}, fmt.Errorf("lifecycle: %w", err)
	}
	exists := func(p string) bool {
		_, err := fs.Stat(fsys, p)
		return err == nil
	}
	if findings := domain.Validate(top, exists); len(findings) > 0 {
		return domain.Topology{}, domain.Catalogue{}, &FindingsError{Findings: findings}
	}
	cat, _ := domain.NewCatalogue(domain.BuiltinSecrets(), top.DeclaredSecrets())
	return top, cat, nil
}

// pass, warn and fail build checks.
func pass(name, summary string) domain.Check {
	return domain.Check{Name: name, Status: domain.Pass, Summary: summary}
}

func warn(name, summary, hint string) domain.Check {
	return domain.Check{Name: name, Status: domain.Warn, Summary: summary, Hint: hint}
}

func fail(name string, err error) domain.Check {
	return domain.Check{Name: name, Status: domain.Fail, Summary: err.Error()}
}

// Up brings the selected zones up: check user secrets, create and start
// the machine, create the networks, then for each zone resolve and inject
// its secrets and build and play each workload. It stops the zone at the
// first failure, skips the zones after it, and returns the report. The
// error is non-nil only for cancellation, a broken keychain, or an invalid
// topology.
func (l *Lifecycle) Up(ctx context.Context, dir string, roles []domain.Role) (domain.Report, error) {
	var r domain.Report
	top, cat, err := l.load(dir)
	if err != nil {
		return r, err
	}
	p := l.progress()

	// User secrets first: a missing API key costs nothing to detect and a
	// boot costs a minute. Values are never read here.
	p.Step("secrets", "checking the keychain")
	if err := requireKeychain(ctx, l.Keychain, top.KeychainPath); err != nil {
		return r, err
	}
	missing := 0
	for _, role := range roles {
		for _, spec := range cat.ForZone(role) {
			if spec.Source != domain.User {
				continue
			}
			if err := ctx.Err(); err != nil {
				return r, err
			}
			_, err := l.Keychain.Describe(ctx, top.KeychainPath, spec.Name)
			switch {
			case errors.Is(err, ErrSecretNotFound):
				missing++
				r.Checks = append(r.Checks, domain.EvaluateSecret(spec, err))
			case err != nil:
				return r, fmt.Errorf("lifecycle: %w", err)
			}
		}
	}
	if missing > 0 {
		return r, nil
	}

	// The machine.
	p.Step("machine", "inspecting "+domain.MachineName)
	machineCheck, err := l.ensureMachine(ctx, top, p)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return r, ctxErr
	}
	r.Checks = append(r.Checks, machineCheck)
	if err != nil {
		return r, nil
	}

	// The networks, before any pod is played.
	for _, role := range roles {
		if err := ctx.Err(); err != nil {
			return r, err
		}
		z, _ := top.Zone(role)
		name := "network/" + role.String()
		exists, err := l.Workloads.NetworkExists(ctx, role.NetworkName())
		if err != nil {
			r.Checks = append(r.Checks, fail(name, err))
			return r, nil
		}
		if exists {
			r.Checks = append(r.Checks, pass(name, "present"))
			continue
		}
		p.Step(name, "creating "+role.NetworkName())
		if err := l.Workloads.CreateNetwork(ctx, role.NetworkName(), z.Internal); err != nil {
			r.Checks = append(r.Checks, fail(name, err))
			return r, nil
		}
		summary := "created"
		if z.Internal {
			summary += " (internal)"
		}
		r.Checks = append(r.Checks, pass(name, summary))
	}

	// The zones, in order.
	resolve := &ResolveSecrets{Keychain: l.Keychain, Minter: l.Minter, Random: l.Random}
	inject := &InjectSecrets{Target: l.Target}
	failed := ""
	for _, role := range roles {
		if err := ctx.Err(); err != nil {
			return r, err
		}
		if failed != "" {
			r.Checks = append(r.Checks, warn(role.String(), "skipped because "+failed+" failed", "fix it, then run `lclaw up` again"))
			continue
		}
		z, _ := top.Zone(role)
		name := role.String() + "/secrets"
		p.Step(name, "resolving secrets")
		resolved, findings, err := resolve.Run(ctx, top.KeychainPath, cat, role)
		if err != nil {
			return r, err
		}
		if len(findings) > 0 {
			r.Checks = append(r.Checks, findings...)
			failed = name
			continue
		}
		if err := inject.Run(ctx, resolved); err != nil {
			if ctx.Err() != nil {
				return r, ctx.Err()
			}
			r.Checks = append(r.Checks, fail(name, err))
			failed = name
			continue
		}
		r.Checks = append(r.Checks, pass(name, fmt.Sprintf("%d injected", len(resolved))))

		for _, w := range z.Workloads {
			if err := ctx.Err(); err != nil {
				return r, err
			}
			name := role.String() + "/" + string(w)
			p.Step(name, "building "+w.ImageTag())
			if err := l.Workloads.Build(ctx, w.ImageTag(), filepath.Join(dir, filepath.FromSlash(w.Dir()))); err != nil {
				if ctx.Err() != nil {
					return r, ctx.Err()
				}
				r.Checks = append(r.Checks, fail(name, err))
				failed = name
				break
			}
			p.Step(name, "playing "+w.PodFile())
			userns := ""
			if z.Internal {
				userns = "auto"
			}
			if err := l.Workloads.Play(ctx, filepath.Join(dir, filepath.FromSlash(w.PodFile())), z.Networks(w), userns); err != nil {
				if ctx.Err() != nil {
					return r, ctx.Err()
				}
				r.Checks = append(r.Checks, fail(name, err))
				failed = name
				break
			}
			r.Checks = append(r.Checks, pass(name, "built and playing"))
		}
		if failed != "" {
			continue
		}
		if role == domain.Services && l.Minter != nil {
			name := role.String() + "/ready"
			p.Step(name, "waiting for LiteLLM")
			if err := l.Minter.Ready(ctx); err != nil {
				if ctx.Err() != nil {
					return r, ctx.Err()
				}
				r.Checks = append(r.Checks, fail(name, err))
				failed = name
				continue
			}
			r.Checks = append(r.Checks, pass(name, "LiteLLM is ready"))
		}
	}
	return r, nil
}

// ensureMachine creates the machine if it does not exist and starts it if
// it is not running. A missing tool or a failed command is a failed check.
func (l *Lifecycle) ensureMachine(ctx context.Context, top domain.Topology, p Progress) (domain.Check, error) {
	machines, err := l.Runtime.ListMachines(ctx)
	if err != nil {
		return fail("machine", err), err
	}
	var found *domain.Machine
	for i := range machines {
		if machines[i].Name == domain.MachineName {
			found = &machines[i]
			break
		}
	}
	created := false
	if found == nil {
		p.Step("machine", "creating "+domain.MachineName)
		err := l.Runtime.InitMachine(ctx, MachineInit{
			Name:      domain.MachineName,
			Provider:  top.Provider,
			CPUs:      top.Machine.CPUs,
			MemoryMiB: top.Machine.MemoryMiB,
			DiskGiB:   top.Machine.DiskGiB,
			Volumes:   top.Machine.Volumes,
			Playbook:  domain.PlaybookFile,
		})
		if err != nil {
			return fail("machine", err), err
		}
		created = true
	}
	if found != nil && found.Running {
		return pass("machine", "running"), nil
	}
	p.Step("machine", "starting "+domain.MachineName)
	if err := l.Runtime.StartMachine(ctx, top.Provider, domain.MachineName); err != nil {
		return fail("machine", err), err
	}
	if created {
		return pass("machine", "created and started"), nil
	}
	return pass("machine", "started"), nil
}

// Down takes the selected zones down in reverse order, removes their
// networks, and when no LocalClaw pod remains purges the secrets and stops
// the machine. With destroy, and only with every zone selected, it then
// removes the machine and revokes the agent's minted key. It continues
// past failures so the report is complete.
func (l *Lifecycle) Down(ctx context.Context, dir string, roles []domain.Role, destroy bool) (domain.Report, error) {
	var r domain.Report
	top, cat, err := l.load(dir)
	if err != nil {
		return r, err
	}
	if destroy && len(roles) != len(domain.Roles()) {
		return r, errors.New("lifecycle: --destroy applies to the whole machine; name no zone")
	}
	p := l.progress()

	machines, err := l.Runtime.ListMachines(ctx)
	if err != nil {
		r.Checks = append(r.Checks, fail("machine", err))
		return r, nil
	}
	var found *domain.Machine
	for i := range machines {
		if machines[i].Name == domain.MachineName {
			found = &machines[i]
		}
	}
	if found == nil {
		r.Checks = append(r.Checks, warn("machine", "not created", "nothing to take down"))
		return r, nil
	}
	if !found.Running {
		if destroy {
			return l.destroy(ctx, top, cat, r, p)
		}
		r.Checks = append(r.Checks, warn("machine", "stopped", "nothing to take down"))
		return r, nil
	}

	pods, err := l.Workloads.ListPods(ctx)
	if err != nil {
		r.Checks = append(r.Checks, fail("pods", err))
		return r, nil
	}
	present := map[string]bool{}
	for _, pod := range pods {
		present[pod.Name] = true
	}

	// Zones in reverse, workloads in reverse.
	for i := len(roles) - 1; i >= 0; i-- {
		role := roles[i]
		z, _ := top.Zone(role)
		zoneOK := true
		for j := len(z.Workloads) - 1; j >= 0; j-- {
			if err := ctx.Err(); err != nil {
				return r, err
			}
			w := z.Workloads[j]
			name := role.String() + "/" + string(w)
			if !present[string(w)] {
				r.Checks = append(r.Checks, warn(name, "absent", ""))
				continue
			}
			p.Step(name, "kube down "+w.PodFile())
			if err := l.Workloads.Down(ctx, filepath.Join(dir, filepath.FromSlash(w.PodFile()))); err != nil {
				if ctx.Err() != nil {
					return r, ctx.Err()
				}
				r.Checks = append(r.Checks, fail(name, err))
				zoneOK = false
				continue
			}
			delete(present, string(w))
			r.Checks = append(r.Checks, pass(name, "stopped"))
		}
		name := "network/" + role.String()
		if !zoneOK {
			r.Checks = append(r.Checks, warn(name, "kept because a pod could not be stopped", ""))
			continue
		}
		exists, err := l.Workloads.NetworkExists(ctx, role.NetworkName())
		switch {
		case ctx.Err() != nil:
			return r, ctx.Err()
		case err != nil:
			r.Checks = append(r.Checks, fail(name, err))
		case !exists:
			r.Checks = append(r.Checks, warn(name, "absent", ""))
		default:
			p.Step(name, "removing "+role.NetworkName())
			if err := l.Workloads.RemoveNetwork(ctx, role.NetworkName()); err != nil {
				r.Checks = append(r.Checks, fail(name, err))
			} else {
				r.Checks = append(r.Checks, pass(name, "removed"))
			}
		}
	}

	// The store is shared, so purge and stop only when nothing is left.
	if len(present) > 0 {
		hint := "run `lclaw down` with no zone to stop the machine"
		r.Checks = append(r.Checks, warn("secrets", fmt.Sprintf("kept: %d pod(s) still running", len(present)), hint))
		r.Checks = append(r.Checks, warn("machine", "running", hint))
		return r, nil
	}
	p.Step("secrets", "purging the secret store")
	purged, err := (&PurgeSecrets{Target: l.Target}).Run(ctx)
	switch {
	case ctx.Err() != nil:
		return r, ctx.Err()
	case err != nil:
		r.Checks = append(r.Checks, fail("secrets", err))
	default:
		r.Checks = append(r.Checks, pass("secrets", fmt.Sprintf("%d purged", len(purged))))
	}
	p.Step("machine", "stopping "+domain.MachineName)
	if err := l.Runtime.StopMachine(ctx, top.Provider, domain.MachineName); err != nil {
		if ctx.Err() != nil {
			return r, ctx.Err()
		}
		r.Checks = append(r.Checks, fail("machine", err))
		return r, nil
	}
	r.Checks = append(r.Checks, pass("machine", "stopped"))
	if destroy {
		return l.destroy(ctx, top, cat, r, p)
	}
	return r, nil
}

// destroy removes the machine and forgets the minted key. The machine's
// disk held the LiteLLM database that knew the key, so nothing is left to
// revoke; the keychain item is removed so the next up mints a fresh one.
func (l *Lifecycle) destroy(ctx context.Context, top domain.Topology, cat domain.Catalogue, r domain.Report, p Progress) (domain.Report, error) {
	p.Step("machine", "removing "+domain.MachineName)
	if err := l.Runtime.RemoveMachine(ctx, top.Provider, domain.MachineName); err != nil {
		if ctx.Err() != nil {
			return r, ctx.Err()
		}
		r.Checks = append(r.Checks, fail("destroy", err))
		return r, nil
	}
	r.Checks = append(r.Checks, pass("destroy", domain.MachineName+" removed with its images and volumes"))
	for _, spec := range cat.Entries() {
		if spec.Source != domain.Minted {
			continue
		}
		if err := ctx.Err(); err != nil {
			return r, err
		}
		_, err := l.Keychain.Describe(ctx, top.KeychainPath, spec.Name)
		if errors.Is(err, ErrSecretNotFound) {
			continue
		}
		if err != nil {
			r.Checks = append(r.Checks, fail("secrets/"+spec.Name, err))
			continue
		}
		if err := l.Keychain.Delete(ctx, top.KeychainPath, spec.Name); err != nil {
			r.Checks = append(r.Checks, fail("secrets/"+spec.Name, err))
			continue
		}
		r.Checks = append(r.Checks, pass("secrets/"+spec.Name, "forgotten; minted again on the next up"))
	}
	return r, nil
}

// Status reads the machine, the networks and the pods and reports one
// check each. It never writes.
func (l *Lifecycle) Status(ctx context.Context, dir string) (domain.Report, error) {
	var r domain.Report
	top, _, err := l.load(dir)
	if err != nil {
		return r, err
	}
	machines, err := l.Runtime.ListMachines(ctx)
	if err != nil {
		r.Checks = append(r.Checks, fail("machine", err))
		return r, nil
	}
	machine, running := domain.EvaluateMachine(machines)
	r.Checks = append(r.Checks, machine)
	if !running {
		return r, nil
	}
	present, err := networksPresent(ctx, l.Workloads, domain.Roles())
	if ctxErr := ctx.Err(); ctxErr != nil {
		return r, ctxErr
	}
	if err != nil {
		r.Checks = append(r.Checks, fail("networks", err))
		return r, nil
	}
	r.Checks = append(r.Checks, domain.EvaluateNetworks(top, domain.Roles(), present)...)
	pods, err := l.Workloads.ListPods(ctx)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return r, ctxErr
	}
	if err != nil {
		r.Checks = append(r.Checks, fail("pods", err))
		return r, nil
	}
	r.Checks = append(r.Checks, domain.EvaluateWorkloads(top, domain.Roles(), pods)...)
	return r, nil
}
