package domain

import (
	"fmt"
	"sort"
	"strconv"
)

// PortBinding is one published port of a pod: the port inside the pod, the
// port on the host's loopback that forwards to it, and the protocol as
// Podman words it ("tcp", "udp", "sctp"). A container port the machine
// does not publish has no binding at all.
type PortBinding struct {
	ContainerPort int
	HostPort      int
	Protocol      string
}

// addressable reports whether the binding can carry an http:// URL. Only
// TCP can, and an endpoint list is a list of addresses to open, so a UDP
// publication is not one of them.
func (b PortBinding) addressable() bool { return b.Protocol == "tcp" }

// EndpointSpec names a port a workload serves something a person opens,
// and the path to open. The runtime knows which ports are published; it
// cannot know that 5681 is a dashboard or that Kuma's GUI lives under
// /gui, so that knowledge lives here.
type EndpointSpec struct {
	Workload      Workload
	ContainerPort int
	Label         string
	Path          string // begins with "/"
}

// KnownEndpoints is the catalogue of human-facing ports the scaffold's
// workloads serve, in the order they are reported. A workload absent from
// this list still has its published ports reported, just without a label.
func KnownEndpoints() []EndpointSpec {
	return []EndpointSpec{
		{Workload: "kuma-cp", ContainerPort: 5681, Label: "Kuma dashboard", Path: "/gui"},
		{Workload: "gateway", ContainerPort: 8080, Label: "LocalClaw gateway", Path: "/"},
		{Workload: "litellm", ContainerPort: 4000, Label: "LiteLLM admin UI", Path: "/ui"},
		{Workload: "agentgateway", ContainerPort: 15000, Label: "Agent Gateway admin UI", Path: "/"},
		{Workload: "agentgateway", ContainerPort: 3000, Label: "Agent Gateway proxy", Path: "/"},
		{Workload: "openclaw", ContainerPort: 18789, Label: "OpenClaw Control UI", Path: "/"},
	}
}

// Endpoint is one address the catalogue describes, resolved against what
// the machine is actually running and publishing. URL is set when the
// endpoint can be opened on the host; otherwise Reason says why not, so
// the list never offers an address that would refuse the connection.
type Endpoint struct {
	Workload Workload
	Label    string
	URL      string
	Reason   string
}

// Reachable reports whether the endpoint has a URL to open.
func (e Endpoint) Reachable() bool { return e.URL != "" }

// Name is how the endpoint is identified in output: the label when the
// catalogue has one, else the workload and its container port.
func (e Endpoint) Name() string { return e.Label }

// EvaluateEndpoints resolves every endpoint of the selected zones against
// the pods the runtime reported. Each workload of the topology is visited
// in zone order; for each, the catalogue's entries come first in
// catalogue order, then any other port the pod publishes, so a user who
// adds a hostPort to a workload sees it without editing this table.
//
// A pod that is absent, degraded, or serving a port the machine does not
// publish yields an Endpoint with a Reason instead of a URL. That is the
// point of the list: it reports what is reachable, not what is configured.
func EvaluateEndpoints(t Topology, roles []Role, pods []Pod) []Endpoint {
	byName := make(map[string]Pod, len(pods))
	for _, p := range pods {
		byName[p.Name] = p
	}
	var out []Endpoint
	for _, r := range roles {
		z, ok := t.Zone(r)
		if !ok {
			continue
		}
		for _, w := range z.Workloads {
			pod, present := byName[string(w)]
			out = append(out, workloadEndpoints(z, w, pod, present)...)
		}
	}
	return out
}

// workloadEndpoints resolves one workload's endpoints. pod is the zero Pod
// when the runtime reported none, which present distinguishes from a pod
// that exists but has no containers.
func workloadEndpoints(z ZoneSpec, w Workload, pod Pod, present bool) []Endpoint {
	// Why nothing this workload serves can be reached, or "" when it can.
	reason := ""
	switch {
	case !present:
		reason = "the pod is not running"
	case !pod.Running():
		reason = "the pod is degraded"
	}

	var out []Endpoint
	claimed := map[int]bool{}
	for _, spec := range KnownEndpoints() {
		if spec.Workload != w {
			continue
		}
		claimed[spec.ContainerPort] = true
		e := Endpoint{Workload: w, Label: spec.Label}
		host, published := pod.HostPortFor(spec.ContainerPort)
		switch {
		case reason != "":
			e.Reason = reason
		case !published:
			e.Reason = unpublished(z, spec.ContainerPort)
		default:
			e.URL = "http://localhost:" + strconv.Itoa(host) + spec.Path
		}
		out = append(out, e)
	}

	// Ports the pod publishes that the catalogue does not describe. Only
	// published ones: an undescribed, unpublished port is not an endpoint,
	// it is an implementation detail.
	var extra []PortBinding
	for _, b := range pod.Ports {
		if !claimed[b.ContainerPort] && b.addressable() {
			extra = append(extra, b)
		}
	}
	sort.Slice(extra, func(i, j int) bool { return extra[i].ContainerPort < extra[j].ContainerPort })
	for _, b := range extra {
		out = append(out, Endpoint{
			Workload: w,
			Label:    fmt.Sprintf("%s port %d", w, b.ContainerPort),
			URL:      "http://localhost:" + strconv.Itoa(b.HostPort) + "/",
		})
	}
	return out
}

// unpublished explains why a running workload's port cannot be opened. An
// internal zone is the interesting case: it is deliberate, not a mistake,
// so the reason says so rather than suggesting the user fix it.
func unpublished(z ZoneSpec, port int) string {
	if z.Internal {
		return fmt.Sprintf("port %d is not published; zones.%s is internal, so the host reaches it only through the gateway", port, z.Name)
	}
	return fmt.Sprintf("port %d is not published to the host; add a hostPort to the pod", port)
}

// HostPortFor returns the host port the pod publishes for the given
// container port over TCP, and whether it publishes one at all.
func (p Pod) HostPortFor(containerPort int) (int, bool) {
	for _, b := range p.Ports {
		if b.ContainerPort == containerPort && b.addressable() {
			return b.HostPort, true
		}
	}
	return 0, false
}
