package domain

import (
	"reflect"
	"testing"
)

func validTopology() Topology {
	return Topology{
		Schema:   2,
		Provider: "libkrun",
		Machine:  MachineSpec{CPUs: 4, MemoryMiB: 8192, DiskGiB: 60},
		Zones: []ZoneSpec{
			{Name: "infra", Workloads: []Workload{"wireguard", "kuma-cp", "gateway"}, Bridges: map[Workload][]string{"kuma-cp": {"services", "agent"}}},
			{Name: "services", Workloads: []Workload{"litellm-db", "litellm", "agentgateway"}, Bridges: map[Workload][]string{"agentgateway": {"agent"}}},
			{Name: "agent", Internal: true, Workloads: []Workload{"openclaw"}},
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
	if got := w.PodFile(); got != "workloads/openclaw/pod.yaml" {
		t.Errorf("PodFile() = %q", got)
	}
	if got := w.ImageTag(); got != "localhost/lclaw/openclaw:latest" {
		t.Errorf("ImageTag() = %q", got)
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

func TestTopologyZone(t *testing.T) {
	top := validTopology()
	z, ok := top.Zone(Agent)
	if !ok || !z.Internal || z.Name != "agent" {
		t.Fatalf("Zone(Agent) = %+v, %v", z, ok)
	}
	if _, ok := (Topology{}).Zone(Agent); ok {
		t.Fatal("an empty topology has no zones")
	}
}

func TestZoneNetworks(t *testing.T) {
	top := validTopology()
	infra, _ := top.Zone(Infra)
	if got, want := infra.Networks("kuma-cp"), []string{"lclaw-infra", "lclaw-services", "lclaw-agent"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Networks(kuma-cp) = %v, want %v", got, want)
	}
	if got, want := infra.Networks("wireguard"), []string{"lclaw-infra"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Networks(wireguard) = %v, want %v", got, want)
	}
	// Bridged zones come out in role order whatever the file said.
	infra.Bridges["kuma-cp"] = []string{"agent", "services"}
	if got, want := infra.Networks("kuma-cp"), []string{"lclaw-infra", "lclaw-services", "lclaw-agent"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Networks(kuma-cp) reordered = %v, want %v", got, want)
	}
}

func TestFindingString(t *testing.T) {
	f := Finding{Where: "machine.cpus", Message: "must be positive, got 0"}
	if got := f.String(); got != "machine.cpus: must be positive, got 0" {
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
		{"schema", "must be 2, got 0"},
		{"provider", `must be one of libkrun, applehv, got ""`},
		{"machine.cpus", "must be positive, got 0"},
		{"machine.memory-mib", "must be positive, got 0"},
		{"machine.disk-gib", "must be positive, got 0"},
		{"zones", "zones.infra is missing"},
		{"zones", "zones.services is missing"},
		{"zones", "zones.agent is missing"},
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
			name:   "schema 1 gets a migration hint",
			mutate: func(t *Topology) { t.Schema = 1 },
			want:   []Finding{{"schema", "schema 1 described three machines and is no longer supported; run `lclaw init --force` to write the schema 2 file"}},
		},
		{
			name:   "schema",
			mutate: func(t *Topology) { t.Schema = 3 },
			want:   []Finding{{"schema", "must be 2, got 3"}},
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
			mutate: func(t *Topology) { t.Machine.CPUs = 0 },
			want:   []Finding{{"machine.cpus", "must be positive, got 0"}},
		},
		{
			name:   "negative memory",
			mutate: func(t *Topology) { t.Machine.MemoryMiB = -1 },
			want:   []Finding{{"machine.memory-mib", "must be positive, got -1"}},
		},
		{
			name:   "zero disk",
			mutate: func(t *Topology) { t.Machine.DiskGiB = 0 },
			want:   []Finding{{"machine.disk-gib", "must be positive, got 0"}},
		},
		{
			name:   "missing role",
			mutate: func(t *Topology) { t.Zones = t.Zones[:2] },
			want:   []Finding{{"zones", "zones.agent is missing"}},
		},
		{
			name: "unknown zone",
			mutate: func(t *Topology) {
				t.Zones = append(t.Zones, ZoneSpec{Name: "extra"})
			},
			want: []Finding{{"zones.extra", "unknown zone; the zones are infra, services, agent"}},
		},
		{
			name: "duplicate role",
			mutate: func(t *Topology) {
				t.Zones = append(t.Zones, ZoneSpec{Name: "agent", Internal: true})
			},
			want: []Finding{{"zones", "zones.agent appears 2 times"}},
		},
		{
			name:   "workload under two zones",
			mutate: func(t *Topology) { t.Zones[2].Workloads = append(t.Zones[2].Workloads, "litellm") },
			want:   []Finding{{"zones.agent.workloads", `"litellm" is also listed under zones.services`}},
		},
		{
			name:   "workload listed twice",
			mutate: func(t *Topology) { t.Zones[2].Workloads = []Workload{"openclaw", "openclaw"} },
			want:   []Finding{{"zones.agent.workloads", `"openclaw" is listed twice`}},
		},
		{
			name:   "bad workload name",
			mutate: func(t *Topology) { t.Zones[2].Workloads = []Workload{"Open Claw"} },
			want:   []Finding{{"zones.agent.workloads", `"Open Claw" is not a valid workload name; use lowercase letters, digits and hyphens`}},
		},
		{
			name:   "empty workload name",
			mutate: func(t *Topology) { t.Zones[2].Workloads = []Workload{""} },
			want:   []Finding{{"zones.agent.workloads", `"" is not a valid workload name; use lowercase letters, digits and hyphens`}},
		},
		{
			name:   "leading hyphen",
			mutate: func(t *Topology) { t.Zones[2].Workloads = []Workload{"-bad"} },
			want:   []Finding{{"zones.agent.workloads", `"-bad" is not a valid workload name; use lowercase letters, digits and hyphens`}},
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
			want: []Finding{{"zones.agent.workloads", "workloads/openclaw is missing pod.yaml, .containerignore"}},
		},
		{
			name: "volumes",
			mutate: func(t *Topology) {
				t.Machine.Volumes = []string{"/Users/me/ws:/mnt/ws", "relative:/mnt", "/a:b", "/nocolon"}
			},
			want: []Finding{
				{"machine.volumes[1]", `"relative:/mnt" must be two absolute paths joined by a colon`},
				{"machine.volumes[2]", `"/a:b" must be two absolute paths joined by a colon`},
				{"machine.volumes[3]", `"/nocolon" must be two absolute paths joined by a colon`},
			},
		},
		{
			name:   "bridge names a workload of another zone",
			mutate: func(t *Topology) { t.Zones[0].Bridges["litellm"] = []string{"agent"} },
			want:   []Finding{{"zones.infra.bridges.litellm", `"litellm" is not a workload of this zone`}},
		},
		{
			name:   "bridge to an unknown zone",
			mutate: func(t *Topology) { t.Zones[1].Bridges["agentgateway"] = []string{"agent", "laptop"} },
			want:   []Finding{{"zones.services.bridges.agentgateway", `unknown zone "laptop"; the zones are infra, services, agent`}},
		},
		{
			name:   "bridge to its own zone",
			mutate: func(t *Topology) { t.Zones[1].Bridges["agentgateway"] = []string{"agent", "services"} },
			want:   []Finding{{"zones.services.bridges.agentgateway", "a workload already joins its own zone"}},
		},
		{
			name:   "bridge target listed twice",
			mutate: func(t *Topology) { t.Zones[1].Bridges["agentgateway"] = []string{"agent", "agent"} },
			want:   []Finding{{"zones.services.bridges.agentgateway", `"agent" is listed twice`}},
		},
		{
			name:   "internal zone bridges out",
			mutate: func(t *Topology) { t.Zones[2].Bridges = map[Workload][]string{"openclaw": {"services"}} },
			want:   []Finding{{"zones.agent.bridges.openclaw", "an internal zone bridges nothing out"}},
		},
		{
			name: "internal zone nobody bridges into",
			mutate: func(t *Topology) {
				delete(t.Zones[0].Bridges, "kuma-cp")
				delete(t.Zones[1].Bridges, "agentgateway")
			},
			want: []Finding{{"zones.agent.internal", "an internal zone must be the target of at least one bridge, or nothing can reach it"}},
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
	top.Machine.CPUs = 0
	top.Zones[2].Workloads = []Workload{"litellm", "openclaw"}
	got := Validate(top, present(validTopology()))
	want := []Finding{
		{"provider", `must be one of libkrun, applehv, got "qemu"`},
		{"machine.cpus", "must be positive, got 0"},
		{"zones.agent.workloads", `"litellm" is also listed under zones.services`},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Validate() =\n%+v\nwant\n%+v", got, want)
	}
}

func TestTopologyDeclaredSecrets(t *testing.T) {
	topo := Topology{Zones: []ZoneSpec{
		{Name: "services", Secrets: []string{"a", "b"}},
		{Name: "agent"},
	}}
	want := map[string][]string{"services": {"a", "b"}, "agent": nil}
	if got := topo.DeclaredSecrets(); !reflect.DeepEqual(got, want) {
		t.Fatalf("DeclaredSecrets() = %v, want %v", got, want)
	}
}

func TestParseRole(t *testing.T) {
	for _, r := range Roles() {
		got, ok := ParseRole(r.String())
		if !ok || got != r {
			t.Errorf("ParseRole(%q) = %v, %v", r.String(), got, ok)
		}
	}
	if _, ok := ParseRole("laptop"); ok {
		t.Error("ParseRole(laptop) should fail")
	}
}

func TestSelectRoles(t *testing.T) {
	if got, err := SelectRoles(nil); err != nil || !reflect.DeepEqual(got, Roles()) {
		t.Fatalf("SelectRoles(nil) = %v, %v", got, err)
	}
	if got, err := SelectRoles([]string{"agent", "infra"}); err != nil || !reflect.DeepEqual(got, []Role{Infra, Agent}) {
		t.Fatalf("SelectRoles(agent, infra) = %v, %v; want infra then agent", got, err)
	}
	if _, err := SelectRoles([]string{"laptop"}); err == nil || err.Error() != "unknown zone laptop; the zones are infra, services, agent" {
		t.Fatalf("SelectRoles(laptop) err = %v", err)
	}
	if _, err := SelectRoles([]string{"agent", "agent"}); err == nil || err.Error() != "zone agent is named twice" {
		t.Fatalf("SelectRoles(agent, agent) err = %v", err)
	}
}

func TestValidateReportsSecretFindings(t *testing.T) {
	top := validTopology()
	top.Zones[1].Secrets = []string{"Bad_Name"}
	findings := Validate(top, present(top))
	var got []string
	for _, f := range findings {
		got = append(got, f.String())
	}
	want := `zones.services.secrets: "Bad_Name" is not a valid secret name; use a lowercase DNS label of at most 63 characters`
	if len(got) != 1 || got[0] != want {
		t.Fatalf("findings = %v, want exactly %q", got, want)
	}
}

// An unknown zone is reported once, by the zone loop, even when it also
// declares secrets.
func TestValidateReportsAnUnknownZoneOnce(t *testing.T) {
	top := validTopology()
	top.Zones = append(top.Zones, ZoneSpec{Name: "laptop", Secrets: []string{"whatever"}})
	var unknown int
	for _, f := range Validate(top, present(top)) {
		if f.Where == "zones.laptop" {
			unknown++
		}
	}
	if unknown != 1 {
		t.Fatalf("zones.laptop reported %d times, want 1", unknown)
	}
}
