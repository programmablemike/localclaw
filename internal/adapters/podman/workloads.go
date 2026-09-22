package podman

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/programmablemike/localclaw/internal/domain"
)

// NetworkExists implements app.WorkloadRuntime with network exists, which
// exits 0 when present and 1 when absent.
func (c *Client) NetworkExists(ctx context.Context, name string) (bool, error) {
	_, err := c.run(ctx, connection("network", "exists", name)...)
	return existsResult("network exists "+name, err)
}

// CreateNetwork implements app.WorkloadRuntime. An internal network has no
// route out of the machine.
func (c *Client) CreateNetwork(ctx context.Context, name string, internal bool) error {
	args := []string{"network", "create"}
	if internal {
		args = append(args, "--internal")
	}
	args = append(args, name)
	if _, err := c.run(ctx, connection(args...)...); err != nil {
		return wrap("create network "+name, err)
	}
	return nil
}

// RemoveNetwork implements app.WorkloadRuntime.
func (c *Client) RemoveNetwork(ctx context.Context, name string) error {
	if _, err := c.run(ctx, connection("network", "rm", name)...); err != nil {
		return wrap("remove network "+name, err)
	}
	return nil
}

// Build implements app.WorkloadRuntime. The build is quiet, because a
// successful build's log is noise; on failure the runner's error carries
// the tail of stderr, which is the log that matters.
func (c *Client) Build(ctx context.Context, tag, contextDir string) error {
	if _, err := c.run(ctx, connection("build", "--quiet", "--tag", tag, contextDir)...); err != nil {
		return wrap("build "+tag, err)
	}
	return nil
}

// Play implements app.WorkloadRuntime with kube play --replace, one
// --network per network and --userns when set.
func (c *Client) Play(ctx context.Context, file string, networks []string, userns string) error {
	args := []string{"kube", "play", "--replace"}
	for _, n := range networks {
		args = append(args, "--network", n)
	}
	if userns != "" {
		args = append(args, "--userns", userns)
	}
	args = append(args, file)
	if _, err := c.run(ctx, connection(args...)...); err != nil {
		return wrap("kube play "+file, err)
	}
	return nil
}

// Down implements app.WorkloadRuntime. Volumes survive: kube down removes
// them only with --force, which is not passed.
func (c *Client) Down(ctx context.Context, file string) error {
	if _, err := c.run(ctx, connection("kube", "down", file)...); err != nil {
		return wrap("kube down "+file, err)
	}
	return nil
}

// ListPods implements app.WorkloadRuntime from `pod ps --format json`,
// filtered to the pods lclaw played by label. Only the pod name and each
// container's name and state are decoded.
func (c *Client) ListPods(ctx context.Context) ([]domain.Pod, error) {
	res, err := c.run(ctx, connection("pod", "ps", "--filter", "label="+partOfKey+"="+partOfValue, "--format", "json")...)
	if err != nil {
		return nil, wrap("list pods", err)
	}
	out := bytes.TrimSpace(res.Stdout)
	if len(out) == 0 {
		return []domain.Pod{}, nil
	}
	var rows []struct {
		Name       string `json:"Name"`
		Containers []struct {
			Names  string `json:"Names"`
			Status string `json:"Status"`
		} `json:"Containers"`
	}
	if err := json.Unmarshal(out, &rows); err != nil {
		return nil, fmt.Errorf("podman: list pods: decode: %w", err)
	}
	pods := make([]domain.Pod, 0, len(rows))
	for _, r := range rows {
		p := domain.Pod{Name: r.Name}
		for _, ct := range r.Containers {
			// The pod's infra container is Podman's, not the workload's.
			if len(ct.Names) > 6 && ct.Names[len(ct.Names)-6:] == "-infra" {
				continue
			}
			p.Containers = append(p.Containers, domain.Container{Name: ct.Names, State: ct.Status})
		}
		pods = append(pods, p)
	}
	return pods, nil
}
