package domain

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestEvaluateTool(t *testing.T) {
	req := Requirement{Tool: "podman", Min: Version{Major: 5}, InstallHint: "install Podman 5"}
	tests := []struct {
		name  string
		found Version
		err   error
		want  Check
	}{
		{
			name:  "pass",
			found: Version{Major: 5, Minor: 8, Patch: 4, Raw: "5.8.4"},
			want:  Check{Name: "podman", Status: Pass, Summary: "5.8.4 (minimum 5.0.0)"},
		},
		{
			name:  "too old",
			found: Version{Major: 4, Minor: 9, Patch: 3, Raw: "4.9.3"},
			want:  Check{Name: "podman", Status: Fail, Summary: "4.9.3 is older than the minimum 5.0.0", Hint: "install Podman 5"},
		},
		{
			name: "not found",
			err:  fmt.Errorf("podman: version: %w", ErrToolNotFound),
			want: Check{Name: "podman", Status: Fail, Summary: "not found", Hint: "install Podman 5"},
		},
		{
			name: "other error",
			err:  errors.New("podman: version: exit status 125: boom"),
			want: Check{Name: "podman", Status: Fail, Summary: "podman: version: exit status 125: boom"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EvaluateTool(req, tt.found, tt.err)
			if got != tt.want {
				t.Fatalf("EvaluateTool() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestEvaluateMachines(t *testing.T) {
	found := []Machine{
		{Name: "lclaw-infra", Running: true},
		{Name: "podman-machine-default", Running: false},
		{Name: "lclaw-agent", Running: false},
	}
	got := EvaluateMachines(Roles(), found)
	want := []Check{
		{Name: "lclaw-infra", Status: Pass, Summary: "running"},
		{Name: "lclaw-services", Status: Warn, Summary: "not created", Hint: "run `lclaw up` once it is available"},
		{Name: "lclaw-agent", Status: Pass, Summary: "stopped"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("EvaluateMachines() =\n%+v\nwant\n%+v", got, want)
	}
}

func TestEvaluateMachinesNoneCreated(t *testing.T) {
	got := EvaluateMachines(Roles(), nil)
	if len(got) != 3 {
		t.Fatalf("got %d checks, want 3", len(got))
	}
	for _, c := range got {
		if c.Status != Warn {
			t.Errorf("%s: status %v, want Warn", c.Name, c.Status)
		}
	}
}

func TestRequirementsDefaults(t *testing.T) {
	if PodmanRequirement.Tool != "podman" || PodmanRequirement.Min != (Version{Major: 5, Minor: 8}) {
		t.Errorf("PodmanRequirement = %+v, want podman with minimum 5.8.0", PodmanRequirement)
	}
	if !(Version{Major: 5, Minor: 8, Patch: 4}).AtLeast(PodmanRequirement.Min) {
		t.Error("5.8.4 must satisfy the podman minimum")
	}
	if (Version{Major: 5, Minor: 7, Patch: 9}).AtLeast(PodmanRequirement.Min) {
		t.Error("5.7.9 must not satisfy the podman minimum: --playbook needs 5.8")
	}
	if !strings.Contains(PodmanRequirement.InstallHint, "5.8") {
		t.Errorf("InstallHint %q should name 5.8", PodmanRequirement.InstallHint)
	}
	if FloxRequirement.Tool != "flox" || !(Version{Major: 1}).AtLeast(FloxRequirement.Min) {
		t.Errorf("FloxRequirement = %+v", FloxRequirement)
	}
	if PodmanRequirement.InstallHint == "" || FloxRequirement.InstallHint == "" {
		t.Error("install hints must not be empty")
	}
}
