package domain

import (
	"fmt"
	"path"
	"slices"
	"sort"
	"strings"
)

// TopologyFile is the name of the topology file at the root of the scaffold
// directory.
const TopologyFile = "lclaw.toml"

// PlaybookFile is the first-boot playbook, relative to the scaffold root.
const PlaybookFile = "machine/playbook.yaml"

// Schema is the lclaw.toml format version this build reads.
const Schema = 2

// DefaultKeychainPath is the keychain holding LocalClaw's secrets when
// lclaw.toml has no [keychain] table. The loader expands the tilde; the
// domain never touches the file system.
const DefaultKeychainPath = "~/Library/Keychains/lclaw.keychain-db"

// Providers are the Podman machine providers a topology may name.
var Providers = []string{"libkrun", "applehv"}

// Topology is what lclaw.toml describes: the one machine and its size, the
// zones and what runs on each, and where the keychain holding their
// secrets lives. Zones keep the order the loader supplies so findings come
// out in a stable order.
type Topology struct {
	Schema       int
	Provider     string
	KeychainPath string // the [keychain] path, already expanded by the loader
	Machine      MachineSpec
	Zones        []ZoneSpec
}

// MachineSpec is the [machine] table.
type MachineSpec struct {
	CPUs      int
	MemoryMiB int
	DiskGiB   int
	Volumes   []string // "host:guest", both absolute
}

// ZoneSpec is one [zones.<name>] table. Name is the table key; it should
// be a role name, and Validate reports it when it is not. Bridges maps a
// workload of this zone to the other zones whose networks it also joins.
type ZoneSpec struct {
	Name      string
	Internal  bool
	Workloads []Workload
	Secrets   []string // keychain item names the zone's pods consume
	Bridges   map[Workload][]string
}

// Workload names one directory under workloads/ in the scaffold.
type Workload string

// Dir is the workload's directory, slash-separated and relative to the
// scaffold root.
func (w Workload) Dir() string { return "workloads/" + string(w) }

// PodFile is the workload's Pod file, relative to the scaffold root.
func (w Workload) PodFile() string { return w.Dir() + "/pod.yaml" }

// ImageTag is the image the workload's Containerfile builds.
func (w Workload) ImageTag() string { return "localhost/lclaw/" + string(w) + ":latest" }

// RequiredFiles lists the files every workload directory must contain,
// slash-separated and relative to the scaffold root.
func (w Workload) RequiredFiles() []string {
	return []string{w.Dir() + "/Containerfile", w.PodFile(), w.Dir() + "/.containerignore"}
}

// Workloads returns every workload in zone order, then list order.
func (t Topology) Workloads() []Workload {
	var out []Workload
	for _, z := range t.Zones {
		out = append(out, z.Workloads...)
	}
	return out
}

// Zone returns the zone for role r, if the file has one.
func (t Topology) Zone(r Role) (ZoneSpec, bool) {
	for _, z := range t.Zones {
		if z.Name == r.String() {
			return z, true
		}
	}
	return ZoneSpec{}, false
}

// Networks returns the networks a workload of zone z joins: the zone's
// own first, then each bridged zone's in role order.
func (z ZoneSpec) Networks(w Workload) []string {
	out := []string{"lclaw-" + z.Name}
	bridged := append([]string(nil), z.Bridges[w]...)
	sort.Slice(bridged, func(i, j int) bool { return roleRank(bridged[i]) < roleRank(bridged[j]) })
	for _, b := range bridged {
		out = append(out, "lclaw-"+b)
	}
	return out
}

// DeclaredSecrets returns the secret names each zone consumes, keyed by
// the zone name as written in the file, which is the shape NewCatalogue
// consumes. A zone that declares none is present with an empty list, so a
// caller can tell "declared nothing" from "not in the file".
func (t Topology) DeclaredSecrets() map[string][]string {
	out := make(map[string][]string, len(t.Zones))
	for _, z := range t.Zones {
		out[z.Name] = z.Secrets
	}
	return out
}

// Finding is one problem Validate found. Where is the TOML path of the
// offending table or key, such as "zones.agent.workloads".
type Finding struct {
	Where   string
	Message string
}

// String renders the finding as "where: message".
func (f Finding) String() string { return f.Where + ": " + f.Message }

// Validate checks t against the single-machine design's rules and reports
// every problem, in file order, rather than stopping at the first. It never
// returns an error: an invalid topology is a verdict, not a failure of the
// program. exists reports whether a slash-separated path relative to the
// scaffold root exists; it is the only question Validate asks about the
// outside world, so callers pass a closure over a file system or a map.
func Validate(t Topology, exists func(path string) bool) []Finding {
	var out []Finding
	add := func(where, format string, args ...any) {
		out = append(out, Finding{Where: where, Message: fmt.Sprintf(format, args...)})
	}

	switch {
	case t.Schema == 1:
		add("schema", "schema 1 described three machines and is no longer supported; run `lclaw init --force` to write the schema %d file", Schema)
	case t.Schema != Schema:
		add("schema", "must be %d, got %d", Schema, t.Schema)
	}
	if !slices.Contains(Providers, t.Provider) {
		add("provider", "must be one of %s, got %q", strings.Join(Providers, ", "), t.Provider)
	}

	if t.Machine.CPUs <= 0 {
		add("machine.cpus", "must be positive, got %d", t.Machine.CPUs)
	}
	if t.Machine.MemoryMiB <= 0 {
		add("machine.memory-mib", "must be positive, got %d", t.Machine.MemoryMiB)
	}
	if t.Machine.DiskGiB <= 0 {
		add("machine.disk-gib", "must be positive, got %d", t.Machine.DiskGiB)
	}
	for i, v := range t.Machine.Volumes {
		host, guest, ok := strings.Cut(v, ":")
		if !ok || !strings.HasPrefix(host, "/") || !strings.HasPrefix(guest, "/") {
			add(fmt.Sprintf("machine.volumes[%d]", i), "%q must be two absolute paths joined by a colon", v)
		}
	}

	seen := map[Workload]string{} // workload -> the zone that listed it first
	count := map[string]int{}
	bridgedInto := map[string]bool{}
	for _, z := range t.Zones {
		count[z.Name]++
		where := "zones." + z.Name
		if !isRole(z.Name) {
			add(where, "unknown zone; the zones are %s", roleNames())
		}
		own := map[Workload]bool{}
		for _, w := range z.Workloads {
			ww := where + ".workloads"
			if !validWorkloadName(string(w)) {
				add(ww, "%q is not a valid workload name; use lowercase letters, digits and hyphens", string(w))
				continue
			}
			if first, dup := seen[w]; dup {
				if first == z.Name {
					add(ww, "%q is listed twice", string(w))
				} else {
					add(ww, "%q is also listed under zones.%s", string(w), first)
				}
				continue
			}
			seen[w] = z.Name
			own[w] = true
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
		bridgeNames := make([]string, 0, len(z.Bridges))
		for w := range z.Bridges {
			bridgeNames = append(bridgeNames, string(w))
		}
		sort.Strings(bridgeNames)
		for _, name := range bridgeNames {
			w := Workload(name)
			bw := where + ".bridges." + name
			if !own[w] {
				add(bw, "%q is not a workload of this zone", name)
				continue
			}
			if z.Internal {
				add(bw, "an internal zone bridges nothing out")
				continue
			}
			targets := map[string]bool{}
			for _, target := range z.Bridges[w] {
				switch {
				case !isRole(target):
					add(bw, "unknown zone %q; the zones are %s", target, roleNames())
				case target == z.Name:
					add(bw, "a workload already joins its own zone")
				case targets[target]:
					add(bw, "%q is listed twice", target)
				default:
					targets[target] = true
					bridgedInto[target] = true
				}
			}
		}
	}

	for _, r := range Roles() {
		switch n := count[r.String()]; {
		case n == 0:
			add("zones", "zones.%s is missing", r)
		case n > 1:
			add("zones", "zones.%s appears %d times", r, n)
		}
	}
	for _, z := range t.Zones {
		if z.Internal && isRole(z.Name) && count[z.Name] == 1 && !bridgedInto[z.Name] {
			add("zones."+z.Name+".internal", "an internal zone must be the target of at least one bridge, or nothing can reach it")
		}
	}

	// Secret names are part of lclaw.toml, so they are validated here
	// rather than by a second entry point. The catalogue's findings are
	// appended as a group; each names the zone table it came from.
	_, secretFindings := NewCatalogue(BuiltinSecrets(), t.DeclaredSecrets())
	out = append(out, secretFindings...)

	return out
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

// roleRank orders known roles first, in Roles order.
func roleRank(name string) int {
	for i, r := range Roles() {
		if r.String() == name {
			return i
		}
	}
	return len(Roles())
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
