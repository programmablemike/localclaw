package domain

import (
	"reflect"
	"testing"
)

// tcp is a published TCP binding.
func tcp(container, host int) PortBinding {
	return PortBinding{ContainerPort: container, HostPort: host, Protocol: "tcp"}
}

func TestEvaluateEndpointsRunningSystem(t *testing.T) {
	top := validTopology()
	pods := []Pod{
		{
			Name:       "kuma-cp",
			Containers: []Container{{Name: "kuma-cp-kuma-cp", State: "running"}},
			Ports:      []PortBinding{tcp(5681, 5681)},
		},
		{
			Name:       "gateway",
			Containers: []Container{{Name: "gateway-gateway", State: "running"}},
			Ports:      []PortBinding{tcp(8080, 8080)},
		},
		{
			Name:       "litellm-db",
			Containers: []Container{{Name: "litellm-db-litellm-db", State: "running"}},
		},
		{
			Name:       "litellm",
			Containers: []Container{{Name: "litellm-litellm", State: "running"}},
		},
		{
			Name:       "agentgateway",
			Containers: []Container{{Name: "agentgateway-agentgateway", State: "running"}},
		},
		{
			Name:       "openclaw",
			Containers: []Container{{Name: "openclaw-openclaw", State: "running"}},
		},
	}
	got := EvaluateEndpoints(top, Roles(), pods)
	want := []Endpoint{
		{Workload: "kuma-cp", Label: "Kuma dashboard", URL: "http://localhost:5681/gui"},
		{Workload: "gateway", Label: "LocalClaw gateway", URL: "http://localhost:8080/"},
		{Workload: "litellm", Label: "LiteLLM admin UI",
			Reason: "port 4000 is not published to the host; add a hostPort to the pod"},
		{Workload: "agentgateway", Label: "Agent Gateway admin UI",
			Reason: "port 15000 is not published to the host; add a hostPort to the pod"},
		{Workload: "agentgateway", Label: "Agent Gateway proxy",
			Reason: "port 3000 is not published to the host; add a hostPort to the pod"},
		// The agent zone is internal, so the reason says so rather than
		// telling the user to publish a port they must not publish.
		{Workload: "openclaw", Label: "OpenClaw Control UI",
			Reason: "port 18789 is not published; zones.agent is internal, so the host reaches it only through the gateway"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("EvaluateEndpoints() =\n%+v\nwant\n%+v", got, want)
	}
}

func TestEvaluateEndpointsPodNotRunning(t *testing.T) {
	top := validTopology()
	pods := []Pod{
		// Present but degraded: a URL would only time out.
		{
			Name:       "kuma-cp",
			Containers: []Container{{Name: "kuma-cp-kuma-cp", State: "exited"}},
			Ports:      []PortBinding{tcp(5681, 5681)},
		},
		// gateway is absent from the list entirely.
	}
	got := EvaluateEndpoints(top, []Role{Infra}, pods)
	want := []Endpoint{
		{Workload: "kuma-cp", Label: "Kuma dashboard", Reason: "the pod is degraded"},
		{Workload: "gateway", Label: "LocalClaw gateway", Reason: "the pod is not running"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("EvaluateEndpoints() =\n%+v\nwant\n%+v", got, want)
	}
	for _, e := range got {
		if e.Reachable() {
			t.Errorf("%s is reachable with no URL", e.Label)
		}
	}
}

func TestEvaluateEndpointsRemappedAndExtraPorts(t *testing.T) {
	top := validTopology()
	pods := []Pod{
		{
			Name:       "kuma-cp",
			Containers: []Container{{Name: "kuma-cp-kuma-cp", State: "running"}},
			Ports: []PortBinding{
				// The host port need not equal the container port.
				tcp(5681, 15681),
				// Not in the catalogue: reported anyway, so a user who
				// publishes a port sees it without editing the table.
				tcp(5678, 5678),
				// UDP carries no http:// URL, so it is not an endpoint.
				{ContainerPort: 9090, HostPort: 9090, Protocol: "udp"},
			},
		},
	}
	got := EvaluateEndpoints(top, []Role{Infra}, pods)
	want := []Endpoint{
		{Workload: "kuma-cp", Label: "Kuma dashboard", URL: "http://localhost:15681/gui"},
		{Workload: "kuma-cp", Label: "kuma-cp port 5678", URL: "http://localhost:5678/"},
		{Workload: "gateway", Label: "LocalClaw gateway", Reason: "the pod is not running"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("EvaluateEndpoints() =\n%+v\nwant\n%+v", got, want)
	}
}

func TestEvaluateEndpointsNoZones(t *testing.T) {
	if got := EvaluateEndpoints(Topology{}, Roles(), nil); len(got) != 0 {
		t.Fatalf("EvaluateEndpoints() = %+v, want none", got)
	}
}

func TestHostPortFor(t *testing.T) {
	p := Pod{Ports: []PortBinding{
		tcp(5681, 15681),
		{ContainerPort: 9090, HostPort: 9090, Protocol: "udp"},
	}}
	if host, ok := p.HostPortFor(5681); !ok || host != 15681 {
		t.Errorf("HostPortFor(5681) = %d, %v; want 15681, true", host, ok)
	}
	if _, ok := p.HostPortFor(9090); ok {
		t.Error("HostPortFor(9090) found a UDP binding; only TCP carries a URL")
	}
	if _, ok := p.HostPortFor(1); ok {
		t.Error("HostPortFor(1) found a binding that does not exist")
	}
}

// TestKnownEndpointsMatchTheScaffold guards the catalogue against the
// topology drifting away from it: every workload named here must still be
// one the default topology places somewhere, or the endpoint is dead code
// that will never be reported.
func TestKnownEndpointsMatchTheScaffold(t *testing.T) {
	placed := map[Workload]bool{}
	for _, w := range validTopology().Workloads() {
		placed[w] = true
	}
	seen := map[EndpointSpec]bool{}
	for _, spec := range KnownEndpoints() {
		if !placed[spec.Workload] {
			t.Errorf("%s is in the endpoint catalogue but in no zone", spec.Workload)
		}
		if spec.Label == "" {
			t.Errorf("%s port %d has no label", spec.Workload, spec.ContainerPort)
		}
		if len(spec.Path) == 0 || spec.Path[0] != '/' {
			t.Errorf("%s path %q must begin with a slash", spec.Workload, spec.Path)
		}
		if seen[spec] {
			t.Errorf("%s port %d is listed twice", spec.Workload, spec.ContainerPort)
		}
		seen[spec] = true
	}
}
