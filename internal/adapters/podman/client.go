// Package podman wraps the podman command line. It reads only the fields it
// needs from --format json output so Podman upgrades do not break it, and it
// never links Podman's Go bindings.
package podman

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/programmablemike/localclaw/internal/adapters/exec"
	"github.com/programmablemike/localclaw/internal/app"
	"github.com/programmablemike/localclaw/internal/domain"
)

// providerEnv is the environment variable that selects the machine
// provider for every podman machine command.
const providerEnv = "CONTAINERS_MACHINE_PROVIDER"

// Client satisfies app.MachineRuntime, app.WorkloadRuntime and
// app.SecretTarget.
type Client struct {
	Runner exec.Runner
}

// run executes podman with args.
func (c *Client) run(ctx context.Context, args ...string) (exec.Result, error) {
	return c.Runner.Run(ctx, exec.Command{Name: "podman", Args: args})
}

// runMachine executes a podman machine command with the provider set.
func (c *Client) runMachine(ctx context.Context, provider string, args ...string) (exec.Result, error) {
	cmd := exec.Command{Name: "podman", Args: append([]string{"machine"}, args...)}
	if provider != "" {
		cmd.Env = []string{providerEnv + "=" + provider}
	}
	return c.Runner.Run(ctx, cmd)
}

// connection prefixes args with the remote connection for the machine.
func connection(args ...string) []string {
	return append([]string{"--connection", domain.MachineName}, args...)
}

// Version parses the last field of `podman --version`.
func (c *Client) Version(ctx context.Context) (domain.Version, error) {
	res, err := c.run(ctx, "--version")
	if err != nil {
		return domain.Version{}, wrap("version", err)
	}
	fields := strings.Fields(string(res.Stdout))
	if len(fields) == 0 {
		return domain.Version{}, errors.New("podman: version: empty output")
	}
	v, err := domain.ParseVersion(fields[len(fields)-1])
	if err != nil {
		return domain.Version{}, fmt.Errorf("podman: version: %w", err)
	}
	return v, nil
}

// ListMachines decodes `podman machine list --format json`, keeping only
// Name and Running. Podman lists every provider's machines.
func (c *Client) ListMachines(ctx context.Context) ([]domain.Machine, error) {
	res, err := c.run(ctx, "machine", "list", "--format", "json")
	if err != nil {
		return nil, wrap("list machines", err)
	}
	out := bytes.TrimSpace(res.Stdout)
	if len(out) == 0 {
		return []domain.Machine{}, nil
	}
	var rows []struct {
		Name    string `json:"Name"`
		Running bool   `json:"Running"`
	}
	if err := json.Unmarshal(out, &rows); err != nil {
		return nil, fmt.Errorf("podman: list machines: decode: %w", err)
	}
	machines := make([]domain.Machine, 0, len(rows))
	for _, r := range rows {
		machines = append(machines, domain.Machine{Name: r.Name, Running: r.Running})
	}
	return machines, nil
}

// InitMachine implements app.MachineRuntime with `podman machine init`.
// No --now: creating and starting are separate, reportable steps. With no
// volumes it passes --volume "" so nothing from the host is mounted.
func (c *Client) InitMachine(ctx context.Context, m app.MachineInit) error {
	args := []string{
		"init", m.Name,
		"--cpus", strconv.Itoa(m.CPUs),
		"--memory", strconv.Itoa(m.MemoryMiB),
		"--disk-size", strconv.Itoa(m.DiskGiB),
		"--playbook", m.Playbook,
	}
	if len(m.Volumes) == 0 {
		args = append(args, "--volume", "")
	}
	for _, v := range m.Volumes {
		args = append(args, "--volume", v)
	}
	if _, err := c.runMachine(ctx, m.Provider, args...); err != nil {
		return wrap("init machine "+m.Name, err)
	}
	return nil
}

// StartMachine implements app.MachineRuntime. Podman refuses to start a
// machine that is already running, so callers check first.
func (c *Client) StartMachine(ctx context.Context, provider, name string) error {
	if _, err := c.runMachine(ctx, provider, "start", "--no-info", name); err != nil {
		return wrap("start machine "+name, err)
	}
	return nil
}

// StopMachine implements app.MachineRuntime. Stopping a stopped machine
// is not an error to Podman either.
func (c *Client) StopMachine(ctx context.Context, provider, name string) error {
	if _, err := c.runMachine(ctx, provider, "stop", name); err != nil {
		return wrap("stop machine "+name, err)
	}
	return nil
}

// RemoveMachine implements app.MachineRuntime with `machine rm --force`,
// which removes the disk and everything on it without a prompt.
func (c *Client) RemoveMachine(ctx context.Context, provider, name string) error {
	if _, err := c.runMachine(ctx, provider, "rm", "--force", name); err != nil {
		return wrap("remove machine "+name, err)
	}
	return nil
}

// wrap prefixes err with the adapter and operation, mapping a missing
// executable to domain.ErrToolNotFound.
func wrap(op string, err error) error {
	if errors.Is(err, exec.ErrNotFound) {
		return fmt.Errorf("podman: %s: %w", op, domain.ErrToolNotFound)
	}
	return fmt.Errorf("podman: %s: %w", op, err)
}
