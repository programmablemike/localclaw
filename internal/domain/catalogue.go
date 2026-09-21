package domain

import (
	"fmt"
	"sort"
)

// Catalogue is every secret lclaw knows about: the built-in entries merged
// with the names declared per machine in lclaw.toml. Entries are kept
// sorted by name so every listing has the same order.
type Catalogue struct {
	entries []SecretSpec
}

// NewCatalogue merges builtin with declared, a map from machine name (as
// written in lclaw.toml) to secret names. Any built-in entry a user sets by
// hand becomes source "user" at set time, not here. Problems are returned as
// findings, all of them, the way topology validation reports: a malformed
// name, a name that collides with a built-in entry, a name listed twice
// under one machine, or an unknown machine. A name listed under two
// machines is one entry received by both.
func NewCatalogue(builtin []SecretSpec, declared map[string][]string) (Catalogue, []Check) {
	byName := make(map[string]int, len(builtin)) // name -> index in entries
	entries := append([]SecretSpec(nil), builtin...)
	for i := range entries {
		byName[entries[i].Name] = i
	}

	var findings []Check
	fail := func(where, summary, hint string) {
		findings = append(findings, Check{Name: where, Status: Fail, Summary: summary, Hint: hint})
	}

	for _, role := range Roles() {
		list, ok := declared[role.String()]
		if !ok {
			continue
		}
		where := "machines." + role.String() + ".secrets"
		seen := map[string]bool{}
		for _, name := range list {
			switch {
			case ValidateName(name) != nil:
				fail(where, fmt.Sprintf("%q is not a valid secret name", name), "use a lowercase DNS label of at most 63 characters")
				continue
			case isBuiltin(builtin, name):
				fail(where, fmt.Sprintf("%q collides with a built-in secret", name), "remove it from lclaw.toml; built-in secrets are always available")
				continue
			case seen[name]:
				fail(where, fmt.Sprintf("%q is listed twice", name), "remove the duplicate from lclaw.toml")
				continue
			}
			seen[name] = true
			if i, ok := byName[name]; ok {
				entries[i].Machines = append(entries[i].Machines, role)
				continue
			}
			entries = append(entries, SecretSpec{Name: name, Source: User, Machines: []Role{role}, Rotatable: true})
			byName[name] = len(entries) - 1
		}
	}

	unknown := make([]string, 0)
	for machine := range declared {
		if _, ok := ParseRole(machine); !ok {
			unknown = append(unknown, machine)
		}
	}
	sort.Strings(unknown)
	for _, machine := range unknown {
		fail("machines."+machine, "unknown machine", "machines are infra, services and agent")
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return Catalogue{entries: entries}, findings
}

func isBuiltin(builtin []SecretSpec, name string) bool {
	for _, b := range builtin {
		if b.Name == name {
			return true
		}
	}
	return false
}

// Entries returns every entry sorted by name.
func (c Catalogue) Entries() []SecretSpec {
	return append([]SecretSpec(nil), c.entries...)
}

// Lookup returns the entry named name.
func (c Catalogue) Lookup(name string) (SecretSpec, bool) {
	for _, e := range c.entries {
		if e.Name == name {
			return e, true
		}
	}
	return SecretSpec{}, false
}

// ForMachine returns the entries the machine with role r receives, in name
// order.
func (c Catalogue) ForMachine(r Role) []SecretSpec {
	var out []SecretSpec
	for _, e := range c.entries {
		if e.UsedBy(r) {
			out = append(out, e)
		}
	}
	return out
}
