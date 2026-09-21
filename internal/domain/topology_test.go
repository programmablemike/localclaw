package domain

import (
	"reflect"
	"testing"
)

func validTopology() Topology {
	return Topology{
		Schema:   1,
		Provider: "libkrun",
		Machines: []MachineSpec{
			{Name: "infra", CPUs: 1, MemoryMiB: 1024, DiskGiB: 10, Workloads: []Workload{"wireguard", "kuma-cp", "gateway"}},
			{Name: "services", CPUs: 2, MemoryMiB: 4096, DiskGiB: 30, Workloads: []Workload{"litellm-db", "litellm", "agentgateway"}},
			{Name: "agent", CPUs: 2, MemoryMiB: 4096, DiskGiB: 30, Workloads: []Workload{"openclaw"}},
		},
	}
}

// present returns an exists function that knows every required file of t.
func present(t Topology) func(string) bool {
	files := map[string]bool{}
	for _, w := range t.Workloads() {
		for _, f := range w.RequiredFiles() {
			files[f] = true
		}
	}
	return func(p string) bool { return files[p] }
}

func nothing(string) bool { return false }

func TestWorkloadPaths(t *testing.T) {
	w := Workload("openclaw")
	if got := w.Dir(); got != "workloads/openclaw" {
		t.Errorf("Dir() = %q", got)
	}
	want := []string{"workloads/openclaw/Containerfile", "workloads/openclaw/pod.yaml", "workloads/openclaw/.containerignore"}
	if got := w.RequiredFiles(); !reflect.DeepEqual(got, want) {
		t.Errorf("RequiredFiles() = %v, want %v", got, want)
	}
}

func TestTopologyWorkloadsInOrder(t *testing.T) {
	want := []Workload{"wireguard", "kuma-cp", "gateway", "litellm-db", "litellm", "agentgateway", "openclaw"}
	if got := validTopology().Workloads(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Workloads() = %v, want %v", got, want)
	}
}

func TestFindingString(t *testing.T) {
	f := Finding{Where: "machines.infra.cpus", Message: "must be positive, got 0"}
	if got := f.String(); got != "machines.infra.cpus: must be positive, got 0" {
		t.Fatalf("String() = %q", got)
	}
}

func TestValidateAcceptsTheDefault(t *testing.T) {
	top := validTopology()
	if got := Validate(top, present(top)); len(got) != 0 {
		t.Fatalf("Validate() = %v, want no findings", got)
	}
}

func TestValidateEmptyTopology(t *testing.T) {
	got := Validate(Topology{}, nothing)
	want := []Finding{
		{"schema", "must be 1, got 0"},
		{"provider", `must be one of libkrun, applehv, got ""`},
		{"machines", "machines.infra is missing"},
		{"machines", "machines.services is missing"},
		{"machines", "machines.agent is missing"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Validate() =\n%+v\nwant\n%+v", got, want)
	}
}

func TestValidateRules(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Topology)
		exists func(Topology) func(string) bool
		want   []Finding
	}{
		{
			name:   "schema",
			mutate: func(t *Topology) { t.Schema = 2 },
			want:   []Finding{{"schema", "must be 1, got 2"}},
		},
		{
			name:   "provider",
			mutate: func(t *Topology) { t.Provider = "qemu" },
			want:   []Finding{{"provider", `must be one of libkrun, applehv, got "qemu"`}},
		},
		{
			name:   "applehv is allowed",
			mutate: func(t *Topology) { t.Provider = "applehv" },
			want:   nil,
		},
		{
			name:   "zero cpus",
			mutate: func(t *Topology) { t.Machines[0].CPUs = 0 },
			want:   []Finding{{"machines.infra.cpus", "must be positive, got 0"}},
		},
		{
			name:   "negative memory",
			mutate: func(t *Topology) { t.Machines[1].MemoryMiB = -1 },
			want:   []Finding{{"machines.services.memory-mib", "must be positive, got -1"}},
		},
		{
			name:   "zero disk",
			mutate: func(t *Topology) { t.Machines[2].DiskGiB = 0 },
			want:   []Finding{{"machines.agent.disk-gib", "must be positive, got 0"}},
		},
		{
			name:   "missing role",
			mutate: func(t *Topology) { t.Machines = t.Machines[:2] },
			want:   []Finding{{"machines", "machines.agent is missing"}},
		},
		{
			name: "unknown machine",
			mutate: func(t *Topology) {
				t.Machines = append(t.Machines, MachineSpec{Name: "extra", CPUs: 1, MemoryMiB: 1, DiskGiB: 1})
			},
			want: []Finding{{"machines.extra", "unknown machine; the roles are infra, services, agent"}},
		},
		{
			name: "duplicate role",
			mutate: func(t *Topology) {
				t.Machines = append(t.Machines, MachineSpec{Name: "agent", CPUs: 1, MemoryMiB: 1, DiskGiB: 1})
			},
			want: []Finding{{"machines", "machines.agent appears 2 times"}},
		},
		{
			name:   "workload under two machines",
			mutate: func(t *Topology) { t.Machines[2].Workloads = append(t.Machines[2].Workloads, "litellm") },
			want:   []Finding{{"machines.agent.workloads", `"litellm" is also listed under machines.services`}},
		},
		{
			name:   "workload listed twice",
			mutate: func(t *Topology) { t.Machines[2].Workloads = []Workload{"openclaw", "openclaw"} },
			want:   []Finding{{"machines.agent.workloads", `"openclaw" is listed twice`}},
		},
		{
			name:   "bad workload name",
			mutate: func(t *Topology) { t.Machines[2].Workloads = []Workload{"Open Claw"} },
			want:   []Finding{{"machines.agent.workloads", `"Open Claw" is not a valid workload name; use lowercase letters, digits and hyphens`}},
		},
		{
			name:   "empty workload name",
			mutate: func(t *Topology) { t.Machines[2].Workloads = []Workload{""} },
			want:   []Finding{{"machines.agent.workloads", `"" is not a valid workload name; use lowercase letters, digits and hyphens`}},
		},
		{
			name:   "leading hyphen",
			mutate: func(t *Topology) { t.Machines[2].Workloads = []Workload{"-bad"} },
			want:   []Finding{{"machines.agent.workloads", `"-bad" is not a valid workload name; use lowercase letters, digits and hyphens`}},
		},
		{
			name:   "missing files",
			mutate: func(*Topology) {},
			exists: func(top Topology) func(string) bool {
				all := present(top)
				return func(p string) bool {
					return all(p) && p != "workloads/openclaw/pod.yaml" && p != "workloads/openclaw/.containerignore"
				}
			},
			want: []Finding{{"machines.agent.workloads", "workloads/openclaw is missing pod.yaml, .containerignore"}},
		},
		{
			name: "volumes",
			mutate: func(t *Topology) {
				t.Machines[2].Volumes = []string{"/Users/me/ws:/mnt/ws", "relative:/mnt", "/a:b", "/nocolon"}
			},
			want: []Finding{
				{"machines.agent.volumes[1]", `"relative:/mnt" must be two absolute paths joined by a colon`},
				{"machines.agent.volumes[2]", `"/a:b" must be two absolute paths joined by a colon`},
				{"machines.agent.volumes[3]", `"/nocolon" must be two absolute paths joined by a colon`},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			top := validTopology()
			tt.mutate(&top)
			exists := present(validTopology())
			if tt.exists != nil {
				exists = tt.exists(top)
			}
			got := Validate(top, exists)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Validate() =\n%+v\nwant\n%+v", got, tt.want)
			}
		})
	}
}

func TestValidateReportsEverythingInFileOrder(t *testing.T) {
	top := validTopology()
	top.Provider = "qemu"
	top.Machines[0].CPUs = 0
	top.Machines[2].Workloads = []Workload{"litellm", "openclaw"}
	got := Validate(top, present(validTopology()))
	want := []Finding{
		{"provider", `must be one of libkrun, applehv, got "qemu"`},
		{"machines.infra.cpus", "must be positive, got 0"},
		{"machines.agent.workloads", `"litellm" is also listed under machines.services`},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Validate() =\n%+v\nwant\n%+v", got, want)
	}
}
