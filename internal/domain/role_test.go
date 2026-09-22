package domain

import (
	"reflect"
	"testing"
)

func TestRoles(t *testing.T) {
	want := []Role{Infra, Services, Agent}
	if got := Roles(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Roles() = %v, want %v", got, want)
	}
}

func TestRoleNames(t *testing.T) {
	tests := []struct {
		role    Role
		name    string
		network string
	}{
		{Infra, "infra", "lclaw-infra"},
		{Services, "services", "lclaw-services"},
		{Agent, "agent", "lclaw-agent"},
	}
	for _, tt := range tests {
		if got := tt.role.String(); got != tt.name {
			t.Errorf("Role(%d).String() = %q, want %q", int(tt.role), got, tt.name)
		}
		if got := tt.role.NetworkName(); got != tt.network {
			t.Errorf("Role(%d).NetworkName() = %q, want %q", int(tt.role), got, tt.network)
		}
	}
}
