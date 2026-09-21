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
	"strings"

	"github.com/programmablemike/localclaw/internal/adapters/exec"
	"github.com/programmablemike/localclaw/internal/domain"
)

// Client satisfies app.MachineRuntime.
type Client struct {
	Runner exec.Runner
}

// run executes podman with args.
func (c *Client) run(ctx context.Context, args ...string) (exec.Result, error) {
	return c.Runner.Run(ctx, exec.Command{Name: "podman", Args: args})
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
// Name and Running.
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

// wrap prefixes err with the adapter and operation, mapping a missing
// executable to domain.ErrToolNotFound.
func wrap(op string, err error) error {
	if errors.Is(err, exec.ErrNotFound) {
		return fmt.Errorf("podman: %s: %w", op, domain.ErrToolNotFound)
	}
	return fmt.Errorf("podman: %s: %w", op, err)
}
