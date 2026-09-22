package domain

import (
	"reflect"
	"testing"
)

func TestEvaluateMachine(t *testing.T) {
	tests := []struct {
		name    string
		found   []Machine
		want    Check
		running bool
	}{
		{
			name:    "running",
			found:   []Machine{{Name: "podman-machine-default"}, {Name: "lclaw", Running: true}},
			want:    Check{Name: "machine", Status: Pass, Summary: "running"},
			running: true,
		},
		{
			name:  "stopped",
			found: []Machine{{Name: "lclaw"}},
			want:  Check{Name: "machine", Status: Pass, Summary: "stopped", Hint: "run `lclaw up`"},
		},
		{
			name:  "not created",
			found: []Machine{{Name: "podman-machine-default", Running: true}},
			want:  Check{Name: "machine", Status: Warn, Summary: "not created", Hint: "run `lclaw up`"},
		},
		{
			name: "none",
			want: Check{Name: "machine", Status: Warn, Summary: "not created", Hint: "run `lclaw up`"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, running := EvaluateMachine(tt.found)
			if got != tt.want || running != tt.running {
				t.Fatalf("EvaluateMachine() = %+v, %v; want %+v, %v", got, running, tt.want, tt.running)
			}
		})
	}
}

func TestEvaluateNetworks(t *testing.T) {
	top := validTopology()
	got := EvaluateNetworks(top, Roles(), map[string]bool{"lclaw-infra": true, "lclaw-agent": true})
	want := []Check{
		{Name: "network/infra", Status: Pass, Summary: "present"},
		{Name: "network/services", Status: Warn, Summary: "missing", Hint: "run `lclaw up services`"},
		{Name: "network/agent", Status: Pass, Summary: "present (internal)"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("EvaluateNetworks() =\n%+v\nwant\n%+v", got, want)
	}
}

func TestPodRunning(t *testing.T) {
	if (Pod{}).Running() {
		t.Error("a pod with no containers is not running")
	}
	if !(Pod{Containers: []Container{{Name: "a", State: "running"}}}).Running() {
		t.Error("one running container is running")
	}
	if (Pod{Containers: []Container{{Name: "a", State: "running"}, {Name: "b", State: "exited"}}}).Running() {
		t.Error("an exited container makes the pod not running")
	}
}

func TestEvaluateWorkloads(t *testing.T) {
	top := validTopology()
	pods := []Pod{
		{Name: "litellm-db", Containers: []Container{{Name: "litellm-db-litellm-db", State: "running"}}},
		{Name: "litellm", Containers: []Container{{Name: "litellm-litellm", State: "exited"}}},
		{Name: "openclaw", Containers: []Container{{Name: "openclaw-openclaw", State: "running"}}},
		{Name: "stranger"},
	}
	got := EvaluateWorkloads(top, []Role{Services, Agent}, pods)
	want := []Check{
		{Name: "services/litellm-db", Status: Pass, Summary: "running"},
		{Name: "services/litellm", Status: Fail, Summary: "degraded: container litellm-litellm exited", Hint: "podman --connection lclaw pod logs litellm"},
		{Name: "services/agentgateway", Status: Warn, Summary: "absent", Hint: "run `lclaw up services`"},
		{Name: "agent/openclaw", Status: Pass, Summary: "running"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("EvaluateWorkloads() =\n%+v\nwant\n%+v", got, want)
	}
	if got := EvaluateWorkloads(top, []Role{Infra}, []Pod{{Name: "kuma-cp"}}); got[1].Summary != "degraded: no containers" {
		t.Fatalf("a pod with no containers should be degraded: %+v", got[1])
	}
}
