package domain

import (
	"reflect"
	"testing"
)

func names(specs []SecretSpec) []string {
	out := make([]string, 0, len(specs))
	for _, s := range specs {
		out = append(out, s.Name)
	}
	return out
}

func TestNewCatalogueMergesAndSorts(t *testing.T) {
	cat, findings := NewCatalogue(BuiltinSecrets(), map[string][]string{
		"services": {"anthropic-api-key", "shared-token"},
		"agent":    {"shared-token"},
	})
	if len(findings) != 0 {
		t.Fatalf("findings = %+v, want none", findings)
	}
	want := []string{"anthropic-api-key", "litellm-db-password", "litellm-master-key", "litellm-salt-key", "openclaw-gateway-token", "openclaw-litellm-key", "shared-token"}
	if got := names(cat.Entries()); !reflect.DeepEqual(got, want) {
		t.Fatalf("Entries() = %v, want %v", got, want)
	}
	key, ok := cat.Lookup("anthropic-api-key")
	if !ok || key.Source != User || !key.Rotatable || key.Generator.Bytes != 0 || !reflect.DeepEqual(key.Machines, []Role{Services}) {
		t.Fatalf("anthropic-api-key = %+v, %v", key, ok)
	}
	shared, _ := cat.Lookup("shared-token")
	if !reflect.DeepEqual(shared.Machines, []Role{Services, Agent}) {
		t.Fatalf("shared-token machines = %v, want services then agent", shared.Machines)
	}
	if _, ok := cat.Lookup("nope"); ok {
		t.Fatal("Lookup(nope) should fail")
	}
}

func TestCatalogueForMachine(t *testing.T) {
	cat, _ := NewCatalogue(BuiltinSecrets(), map[string][]string{"services": {"anthropic-api-key"}})
	want := []string{"anthropic-api-key", "litellm-db-password", "litellm-master-key", "litellm-salt-key"}
	if got := names(cat.ForMachine(Services)); !reflect.DeepEqual(got, want) {
		t.Fatalf("ForMachine(Services) = %v, want %v", got, want)
	}
	if got := names(cat.ForMachine(Infra)); len(got) != 0 {
		t.Fatalf("ForMachine(Infra) = %v, want none", got)
	}
}

func TestNewCatalogueFindings(t *testing.T) {
	_, findings := NewCatalogue(BuiltinSecrets(), map[string][]string{
		"services": {"Bad_Name", "litellm-master-key", "dup", "dup"},
	})
	want := []Finding{
		{Where: "machines.services.secrets", Message: `"Bad_Name" is not a valid secret name; use a lowercase DNS label of at most 63 characters`},
		{Where: "machines.services.secrets", Message: `"litellm-master-key" collides with a built-in secret; built-in secrets are always available, so remove it`},
		{Where: "machines.services.secrets", Message: `"dup" is listed twice`},
	}
	if !reflect.DeepEqual(findings, want) {
		t.Fatalf("findings =\n%+v\nwant\n%+v", findings, want)
	}
}

// An unknown machine is Validate's finding, not the catalogue's: the
// catalogue would otherwise report the same problem a second time. Its
// secrets are ignored here, and Validate names the machine.
func TestNewCatalogueIgnoresUnknownMachine(t *testing.T) {
	cat, findings := NewCatalogue(BuiltinSecrets(), map[string][]string{"laptop": {"whatever"}})
	if len(findings) != 0 {
		t.Fatalf("findings = %+v, want none", findings)
	}
	if _, ok := cat.Lookup("whatever"); ok {
		t.Fatal("a secret under an unknown machine must not enter the catalogue")
	}
}

func TestNewCatalogueBuiltinOnly(t *testing.T) {
	cat, findings := NewCatalogue(BuiltinSecrets(), nil)
	if len(findings) != 0 || len(cat.Entries()) != 5 {
		t.Fatalf("findings=%v entries=%d", findings, len(cat.Entries()))
	}
}
