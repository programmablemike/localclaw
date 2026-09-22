package domain

import (
	"errors"
	"fmt"
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
