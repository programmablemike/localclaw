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
// hand becomes source "user" at set time, not here. A name listed under two
// machines is one entry received by both.
//
// Problems are returned as findings, all of them, in the shape topology
// validation uses: a malformed name, a name that collides with a built-in
// entry, or a name listed twice under one machine. A machine name that is
// not a role is not a finding here and its secrets are ignored, because
// Validate already reports the machine itself; reporting it again would
// show the same problem twice.
func NewCatalogue(builtin []SecretSpec, declared map[string][]string) (Catalogue, []Finding) {
	byName := make(map[string]int, len(builtin)) // name -> index in entries
	entries := append([]SecretSpec(nil), builtin...)
	for i := range entries {
		byName[entries[i].Name] = i
	}

	var findings []Finding
	fail := func(where, format string, args ...any) {
		findings = append(findings, Finding{Where: where, Message: fmt.Sprintf(format, args...)})
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
				fail(where, "%q is not a valid secret name; use a lowercase DNS label of at most 63 characters", name)
				continue
			case isBuiltin(builtin, name):
				fail(where, "%q collides with a built-in secret; built-in secrets are always available, so remove it", name)
				continue
			case seen[name]:
				fail(where, "%q is listed twice", name)
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
