// Package toml decodes lclaw.toml. It is the only place the TOML library is
// imported, and it rejects keys the schema does not define so a typo cannot
// silently become a default. Loader satisfies app.TopologyLoader.
package toml

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	burnt "github.com/BurntSushi/toml"

	"github.com/programmablemike/localclaw/internal/domain"
)

// Loader reads and decodes the topology file.
type Loader struct{}

// file mirrors the lclaw.toml schema. The struct tags are the schema:
// anything the decoder cannot place is reported as an unknown key.
type file struct {
	Schema   int                `toml:"schema"`
	Provider string             `toml:"provider"`
	Keychain keychain           `toml:"keychain"`
	Machines map[string]machine `toml:"machines"`
}

// keychain is the [keychain] table. An absent table means the default.
type keychain struct {
	Path string `toml:"path"`
}

type machine struct {
	CPUs      int      `toml:"cpus"`
	MemoryMiB int      `toml:"memory-mib"`
	DiskGiB   int      `toml:"disk-gib"`
	Workloads []string `toml:"workloads"`
	Secrets   []string `toml:"secrets"`
	Volumes   []string `toml:"volumes"`
}

// Load reads domain.TopologyFile from the root of scaffold. Machines come
// out in role order, then any other names alphabetically, so validation
// findings are stable.
func (Loader) Load(scaffold fs.FS) (domain.Topology, error) {
	data, err := fs.ReadFile(scaffold, domain.TopologyFile)
	if err != nil {
		return domain.Topology{}, fmt.Errorf("toml: read %s: %w", domain.TopologyFile, err)
	}
	var f file
	md, err := burnt.Decode(string(data), &f)
	if err != nil {
		return domain.Topology{}, fmt.Errorf("%s: %w", domain.TopologyFile, err)
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		keys := make([]string, len(undecoded))
		for i, k := range undecoded {
			keys[i] = k.String()
		}
		return domain.Topology{}, fmt.Errorf("%s: unknown keys: %s", domain.TopologyFile, strings.Join(keys, ", "))
	}
	return toDomain(f)
}

func toDomain(f file) (domain.Topology, error) {
	t := domain.Topology{Schema: f.Schema, Provider: f.Provider}
	path, err := keychainPath(f.Keychain.Path)
	if err != nil {
		return domain.Topology{}, err
	}
	t.KeychainPath = path
	names := make([]string, 0, len(f.Machines))
	for name := range f.Machines {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		ri, rj := roleRank(names[i]), roleRank(names[j])
		if ri != rj {
			return ri < rj
		}
		return names[i] < names[j]
	})
	for _, name := range names {
		m := f.Machines[name]
		spec := domain.MachineSpec{Name: name, CPUs: m.CPUs, MemoryMiB: m.MemoryMiB, DiskGiB: m.DiskGiB, Secrets: m.Secrets, Volumes: m.Volumes}
		for _, w := range m.Workloads {
			spec.Workloads = append(spec.Workloads, domain.Workload(w))
		}
		t.Machines = append(t.Machines, spec)
	}
	return t, nil
}

// keychainPath applies the default and makes the path absolute. It is the
// one value in the topology that names a file outside the scaffold, so it
// is resolved here rather than by every caller; the domain never touches
// the file system and fs.FS cannot reach the home directory.
func keychainPath(p string) (string, error) {
	if p == "" {
		p = domain.DefaultKeychainPath
	}
	raw := p
	if strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("%s: keychain path %q: %w", domain.TopologyFile, raw, err)
		}
		p = filepath.Join(home, p[2:])
	}
	if !filepath.IsAbs(p) {
		return "", fmt.Errorf("%s: keychain path %q must be absolute or start with ~/", domain.TopologyFile, raw)
	}
	return filepath.Clean(p), nil
}

// roleRank orders known roles first, in domain.Roles order.
func roleRank(name string) int {
	for i, r := range domain.Roles() {
		if r.String() == name {
			return i
		}
	}
	return len(domain.Roles())
}
