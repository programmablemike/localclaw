// Package toml decodes lclaw.toml. It is the only place the TOML library is
// imported, and it rejects keys the schema does not define so a typo cannot
// silently become a default. Loader satisfies app.TopologyLoader.
package toml

import (
	"fmt"
	"io/fs"
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
	Machines map[string]machine `toml:"machines"`
}

type machine struct {
	CPUs      int      `toml:"cpus"`
	MemoryMiB int      `toml:"memory-mib"`
	DiskGiB   int      `toml:"disk-gib"`
	Workloads []string `toml:"workloads"`
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
	return toDomain(f), nil
}

func toDomain(f file) domain.Topology {
	t := domain.Topology{Schema: f.Schema, Provider: f.Provider}
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
		spec := domain.MachineSpec{Name: name, CPUs: m.CPUs, MemoryMiB: m.MemoryMiB, DiskGiB: m.DiskGiB, Volumes: m.Volumes}
		for _, w := range m.Workloads {
			spec.Workloads = append(spec.Workloads, domain.Workload(w))
		}
		t.Machines = append(t.Machines, spec)
	}
	return t
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
