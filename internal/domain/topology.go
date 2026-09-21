package domain

import (
	"fmt"
	"path"
	"slices"
	"strings"
)

// TopologyFile is the name of the topology file at the root of the scaffold
// directory.
const TopologyFile = "lclaw.toml"

// DefaultKeychainPath is the keychain holding LocalClaw's secrets when
// lclaw.toml has no [keychain] table. The loader expands the tilde; the
// domain never touches the file system.
const DefaultKeychainPath = "~/Library/Keychains/lclaw.keychain-db"

// Providers are the Podman machine providers a topology may name.
var Providers = []string{"libkrun", "applehv"}

// Topology is what lclaw.toml describes: which machines exist, how big they
// are, what runs on each, and where the keychain holding their secrets
// lives. Machines keep the order the loader supplies so findings come out
// in a stable order.
type Topology struct {
	Schema       int
	Provider     string
	KeychainPath string // the [keychain] path, already expanded by the loader
	Machines     []MachineSpec
}

// MachineSpec is one [machines.<name>] table. Name is the table key; it
// should be a role name, and Validate reports it when it is not.
type MachineSpec struct {
	Name      string
	CPUs      int
	MemoryMiB int
	DiskGiB   int
	Workloads []Workload
	Secrets   []string // keychain item names the machine receives on up
	Volumes   []string // "host:guest", both absolute
}

// Workload names one directory under workloads/ in the scaffold.
type Workload string

// Dir is the workload's directory, slash-separated and relative to the
// scaffold root.
func (w Workload) Dir() string { return "workloads/" + string(w) }

// RequiredFiles lists the files every workload directory must contain,
// slash-separated and relative to the scaffold root.
func (w Workload) RequiredFiles() []string {
	return []string{w.Dir() + "/Containerfile", w.Dir() + "/pod.yaml", w.Dir() + "/.containerignore"}
}

// Workloads returns every workload in machine order, then list order.
func (t Topology) Workloads() []Workload {
	var out []Workload
	for _, m := range t.Machines {
		out = append(out, m.Workloads...)
	}
	return out
}

// DeclaredSecrets returns the secret names each machine receives, keyed by
// the machine name as written in the file, which is the shape NewCatalogue
// consumes. A machine that declares none is present with an empty list, so
// a caller can tell "declared nothing" from "not in the file".
func (t Topology) DeclaredSecrets() map[string][]string {
	out := make(map[string][]string, len(t.Machines))
	for _, m := range t.Machines {
		out[m.Name] = m.Secrets
	}
	return out
}

// Finding is one problem Validate found. Where is the TOML path of the
// offending table or key, such as "machines.agent.workloads".
type Finding struct {
	Where   string
	Message string
}

// String renders the finding as "where: message".
func (f Finding) String() string { return f.Where + ": " + f.Message }

// Validate checks t against the deployment design's rules and reports every
// problem, in file order, rather than stopping at the first. It never
// returns an error: an invalid topology is a verdict, not a failure of the
// program. exists reports whether a slash-separated path relative to the
// scaffold root exists; it is the only question Validate asks about the
// outside world, so callers pass a closure over a file system or a map.
func Validate(t Topology, exists func(path string) bool) []Finding {
	var out []Finding
	add := func(where, format string, args ...any) {
		out = append(out, Finding{Where: where, Message: fmt.Sprintf(format, args...)})
	}

	if t.Schema != 1 {
		add("schema", "must be 1, got %d", t.Schema)
	}
	if !slices.Contains(Providers, t.Provider) {
		add("provider", "must be one of %s, got %q", strings.Join(Providers, ", "), t.Provider)
	}

	seen := map[Workload]string{} // workload -> the machine that listed it first
	count := map[string]int{}
	for _, m := range t.Machines {
		count[m.Name]++
		where := "machines." + m.Name
		if !isRole(m.Name) {
			add(where, "unknown machine; the roles are %s", roleNames())
		}
		if m.CPUs <= 0 {
			add(where+".cpus", "must be positive, got %d", m.CPUs)
		}
		if m.MemoryMiB <= 0 {
			add(where+".memory-mib", "must be positive, got %d", m.MemoryMiB)
		}
		if m.DiskGiB <= 0 {
			add(where+".disk-gib", "must be positive, got %d", m.DiskGiB)
		}
		for _, w := range m.Workloads {
			ww := where + ".workloads"
			if !validWorkloadName(string(w)) {
				add(ww, "%q is not a valid workload name; use lowercase letters, digits and hyphens", string(w))
				continue
			}
			if first, dup := seen[w]; dup {
				if first == m.Name {
					add(ww, "%q is listed twice", string(w))
				} else {
					add(ww, "%q is also listed under machines.%s", string(w), first)
				}
				continue
			}
			seen[w] = m.Name
			var missing []string
			for _, f := range w.RequiredFiles() {
				if !exists(f) {
					missing = append(missing, path.Base(f))
				}
			}
			if len(missing) > 0 {
				add(ww, "%s is missing %s", w.Dir(), strings.Join(missing, ", "))
			}
		}
		for i, v := range m.Volumes {
			host, guest, ok := strings.Cut(v, ":")
			if !ok || !strings.HasPrefix(host, "/") || !strings.HasPrefix(guest, "/") {
				add(fmt.Sprintf("%s.volumes[%d]", where, i), "%q must be two absolute paths joined by a colon", v)
			}
		}
	}

	for _, r := range Roles() {
		switch n := count[r.String()]; {
		case n == 0:
			add("machines", "machines.%s is missing", r)
		case n > 1:
			add("machines", "machines.%s appears %d times", r, n)
		}
	}
	return out
}

// ParseRole is the inverse of Role.String.
func ParseRole(s string) (Role, bool) {
	for _, r := range Roles() {
		if r.String() == s {
			return r, true
		}
	}
	return 0, false
}

func isRole(name string) bool {
	_, ok := ParseRole(name)
	return ok
}

func roleNames() string {
	names := make([]string, 0, len(Roles()))
	for _, r := range Roles() {
		names = append(names, r.String())
	}
	return strings.Join(names, ", ")
}

// validWorkloadName accepts lowercase letters, digits and hyphens, not
// starting with a hyphen: the name becomes a directory and an image tag.
func validWorkloadName(s string) bool {
	if s == "" {
		return false
	}
	for i, c := range s {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		case c == '-' && i > 0:
		default:
			return false
		}
	}
	return true
}
