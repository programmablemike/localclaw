package podman

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

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
// filtered to the pods lclaw played by label. Only the pod name, each
// container's name and state, and the pod's published ports are decoded.
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
	names := make([]string, 0, len(rows))
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
		names = append(names, r.Name)
	}
	ports, err := c.podPorts(ctx, names)
	if err != nil {
		return nil, err
	}
	for i := range pods {
		pods[i].Ports = ports[pods[i].Name]
	}
	return pods, nil
}

// podPorts reads the published ports of the named pods with a single `pod
// inspect`. `pod ps` does not report them, and the bindings live on the
// pod's infra container, so the pod is the thing to ask.
//
// The shape is Podman's own: InfraConfig.PortBindings maps
// "<container port>/<protocol>" to the host bindings for it, and the host
// port is a string, not a number. Only published ports appear; unlike a
// container's, a pod's PortBindings has no entries for merely exposed
// ports, so everything here is genuinely reachable from the host.
func (c *Client) podPorts(ctx context.Context, names []string) (map[string][]domain.PortBinding, error) {
	out := map[string][]domain.PortBinding{}
	if len(names) == 0 {
		return out, nil
	}
	args := append([]string{"pod", "inspect"}, names...)
	res, err := c.run(ctx, connection(append(args, "--format", "json")...)...)
	if err != nil {
		return nil, wrap("inspect pods", err)
	}
	body := bytes.TrimSpace(res.Stdout)
	if len(body) == 0 {
		return out, nil
	}
	// `pod inspect` always renders an array, one entry per pod, even for a
	// single name.
	var rows []struct {
		Name        string `json:"Name"`
		InfraConfig *struct {
			PortBindings map[string][]struct {
				HostPort string `json:"HostPort"`
			} `json:"PortBindings"`
		} `json:"InfraConfig"`
	}
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, fmt.Errorf("podman: inspect pods: decode: %w", err)
	}
	for _, r := range rows {
		if r.InfraConfig == nil {
			continue
		}
		var bindings []domain.PortBinding
		for key, hosts := range r.InfraConfig.PortBindings {
			port, proto, ok := parsePortKey(key)
			if !ok {
				continue
			}
			for _, h := range hosts {
				hostPort, err := strconv.Atoi(h.HostPort)
				if err != nil {
					continue
				}
				bindings = append(bindings, domain.PortBinding{
					ContainerPort: port,
					HostPort:      hostPort,
					Protocol:      proto,
				})
			}
		}
		// The map's iteration order is random; the report must not be.
		sort.Slice(bindings, func(i, j int) bool {
			if bindings[i].ContainerPort != bindings[j].ContainerPort {
				return bindings[i].ContainerPort < bindings[j].ContainerPort
			}
			return bindings[i].Protocol < bindings[j].Protocol
		})
		out[r.Name] = bindings
	}
	return out, nil
}

// parsePortKey splits a PortBindings key, "5681/tcp", into the container
// port and the protocol. A key Podman words differently is skipped rather
// than guessed at.
func parsePortKey(key string) (int, string, bool) {
	portText, proto, ok := strings.Cut(key, "/")
	if !ok || proto == "" {
		return 0, "", false
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port <= 0 {
		return 0, "", false
	}
	return port, proto, true
}
