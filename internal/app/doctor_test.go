package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"reflect"
	"testing"
	"testing/fstest"

	"github.com/programmablemike/localclaw/internal/domain"
)

type fakeRuntime struct {
	version  domain.Version
	verErr   error
	machines []domain.Machine
	listErr  error
	cancelOn string // "version" or "list": cancel the context when that call happens
	cancel   context.CancelFunc
}

func (f *fakeRuntime) Version(ctx context.Context) (domain.Version, error) {
	if f.cancelOn == "version" && f.cancel != nil {
		f.cancel()
	}
	return f.version, f.verErr
}

func (f *fakeRuntime) ListMachines(ctx context.Context) ([]domain.Machine, error) {
	if f.cancelOn == "list" && f.cancel != nil {
		f.cancel()
	}
	return f.machines, f.listErr
}

type fakeEnvs struct {
	version domain.Version
	err     error
}

func (f *fakeEnvs) Version(ctx context.Context) (domain.Version, error) { return f.version, f.err }

// fakeScaffold and fakeLoader are declared once, in fakes_test.go, and
// shared by every test file in this package.

const dirArg = "/home/test/.config/lclaw"

var (
	podman584 = domain.Version{Major: 5, Minor: 8, Patch: 4, Raw: "5.8.4"}
	flox1131  = domain.Version{Major: 1, Minor: 13, Patch: 1, Raw: "1.13.1-g684cdfb"}
	allThree  = []domain.Machine{{Name: "lclaw-infra", Running: true}, {Name: "lclaw-services", Running: true}, {Name: "lclaw-agent", Running: false}}

	floxPass   = domain.Check{Name: "flox", Status: domain.Pass, Summary: "1.13.1 (minimum 1.0.0)"}
	podmanPass = domain.Check{Name: "podman", Status: domain.Pass, Summary: "5.8.4 (minimum 5.8.0)"}
)

// defaultTopology mirrors the embedded lclaw.toml.
func defaultTopology() domain.Topology {
	return domain.Topology{
		Schema:   1,
		Provider: "libkrun",
		Machines: []domain.MachineSpec{
			{Name: "infra", CPUs: 1, MemoryMiB: 1024, DiskGiB: 10, Workloads: []domain.Workload{"wireguard", "kuma-cp", "gateway"}},
			{Name: "services", CPUs: 2, MemoryMiB: 4096, DiskGiB: 30, Workloads: []domain.Workload{"litellm-db", "litellm", "agentgateway"}},
			{Name: "agent", CPUs: 2, MemoryMiB: 4096, DiskGiB: 30, Workloads: []domain.Workload{"openclaw"}},
		},
	}
}

// fullScaffold is a file system holding every file the default topology needs.
func fullScaffold() fstest.MapFS {
	m := fstest.MapFS{domain.TopologyFile: {Data: []byte("schema = 1\n")}}
	for _, w := range defaultTopology().Workloads() {
		for _, f := range w.RequiredFiles() {
			m[f] = &fstest.MapFile{Data: []byte("x")}
		}
	}
	return m
}

// healthy returns a Doctor whose every dependency reports success.
func healthy() *Doctor {
	return &Doctor{
		Runtime:  &fakeRuntime{version: podman584, machines: allThree},
		Envs:     &fakeEnvs{version: flox1131},
		Scaffold: &fakeScaffold{fsys: fullScaffold()},
		Topology: &fakeLoader{topo: defaultTopology()},
	}
}

func names(r domain.Report) []string {
	out := make([]string, 0, len(r.Checks))
	for _, c := range r.Checks {
		out = append(out, c.Name)
	}
	return out
}

func TestDoctorAllPass(t *testing.T) {
	d := healthy()
	r, err := d.Run(context.Background(), dirArg)
	if err != nil {
		t.Fatal(err)
	}
	want := domain.Report{Checks: []domain.Check{
		floxPass,
		podmanPass,
		{Name: "scaffold", Status: domain.Pass, Summary: dirArg},
		{Name: "topology", Status: domain.Pass, Summary: "3 machines, 7 workloads"},
		{Name: "lclaw-infra", Status: domain.Pass, Summary: "running"},
		{Name: "lclaw-services", Status: domain.Pass, Summary: "running"},
		{Name: "lclaw-agent", Status: domain.Pass, Summary: "stopped"},
	}}
	if !reflect.DeepEqual(r, want) {
		t.Fatalf("Run() =\n%+v\nwant\n%+v", r, want)
	}
	if got := d.Scaffold.(*fakeScaffold).dirs; !reflect.DeepEqual(got, []string{dirArg}) {
		t.Errorf("OpenDir received %v, want [%q]", got, dirArg)
	}
}

func TestDoctorFloxMissingStillChecksEverything(t *testing.T) {
	d := healthy()
	d.Envs = &fakeEnvs{err: fmt.Errorf("flox: version: %w", ErrToolNotFound)}
	d.Runtime = &fakeRuntime{version: podman584, machines: nil}
	r, err := d.Run(context.Background(), dirArg)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"flox", "podman", "scaffold", "topology", "lclaw-infra", "lclaw-services", "lclaw-agent"}
	if got := names(r); !reflect.DeepEqual(got, want) {
		t.Fatalf("check names = %v, want %v", got, want)
	}
	if r.Checks[0].Status != domain.Fail || r.Checks[0].Hint != domain.FloxRequirement.InstallHint {
		t.Errorf("flox check = %+v", r.Checks[0])
	}
	if r.Worst() != domain.Fail {
		t.Errorf("Worst() = %v, want Fail", r.Worst())
	}
}

func TestDoctorPodmanMissingSkipsMachines(t *testing.T) {
	d := healthy()
	d.Runtime = &fakeRuntime{verErr: fmt.Errorf("podman: version: %w", ErrToolNotFound)}
	r, err := d.Run(context.Background(), dirArg)
	if err != nil {
		t.Fatal(err)
	}
	want := domain.Report{Checks: []domain.Check{
		floxPass,
		{Name: "podman", Status: domain.Fail, Summary: "not found", Hint: domain.PodmanRequirement.InstallHint},
		{Name: "scaffold", Status: domain.Pass, Summary: dirArg},
		{Name: "topology", Status: domain.Pass, Summary: "3 machines, 7 workloads"},
		{Name: "machines", Status: domain.Warn, Summary: "skipped because the podman check failed", Hint: "fix podman, then run `lclaw doctor` again"},
	}}
	if !reflect.DeepEqual(r, want) {
		t.Fatalf("Run() =\n%+v\nwant\n%+v", r, want)
	}
}

func TestDoctorPodmanTooOldSkipsMachines(t *testing.T) {
	d := healthy()
	d.Runtime = &fakeRuntime{version: domain.Version{Major: 5, Minor: 7, Patch: 2, Raw: "5.7.2"}}
	r, err := d.Run(context.Background(), dirArg)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := names(r), []string{"flox", "podman", "scaffold", "topology", "machines"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("check names = %v, want %v", got, want)
	}
	if r.Checks[1].Status != domain.Fail || r.Checks[1].Summary != "5.7.2 is older than the minimum 5.8.0" {
		t.Errorf("podman check = %+v", r.Checks[1])
	}
}

func TestDoctorMachineListErrorIsAFailedCheck(t *testing.T) {
	d := healthy()
	d.Runtime = &fakeRuntime{version: podman584, listErr: errors.New("podman: list machines: exit status 125: cannot connect")}
	r, err := d.Run(context.Background(), dirArg)
	if err != nil {
		t.Fatal(err)
	}
	last := r.Checks[len(r.Checks)-1]
	want := domain.Check{Name: "machines", Status: domain.Fail, Summary: "podman: list machines: exit status 125: cannot connect"}
	if last != want {
		t.Fatalf("last check = %+v, want %+v", last, want)
	}
}

func TestDoctorScaffoldMissingIsWarnAndTopologySkipped(t *testing.T) {
	d := healthy()
	d.Scaffold = &fakeScaffold{err: fmt.Errorf("osfs: open %s: %w", dirArg, fs.ErrNotExist)}
	r, err := d.Run(context.Background(), dirArg)
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.Check{
		{Name: "scaffold", Status: domain.Warn, Summary: dirArg + " not found", Hint: "run `lclaw init` to write the default files"},
		{Name: "topology", Status: domain.Warn, Summary: "skipped because the scaffold check failed", Hint: "run `lclaw init`, then `lclaw doctor` again"},
	}
	if got := r.Checks[2:4]; !reflect.DeepEqual(got, want) {
		t.Fatalf("scaffold checks =\n%+v\nwant\n%+v", got, want)
	}
	if r.Worst() != domain.Warn {
		t.Errorf("Worst() = %v, want Warn: a missing scaffold is not a failure", r.Worst())
	}
	if got, want := names(r), []string{"flox", "podman", "scaffold", "topology", "lclaw-infra", "lclaw-services", "lclaw-agent"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("check names = %v, want %v", got, want)
	}
}

func TestDoctorScaffoldUnreadableIsFail(t *testing.T) {
	d := healthy()
	d.Scaffold = &fakeScaffold{err: errors.New("osfs: open /x: not a directory")}
	r, err := d.Run(context.Background(), dirArg)
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.Check{
		{Name: "scaffold", Status: domain.Fail, Summary: "osfs: open /x: not a directory"},
		{Name: "topology", Status: domain.Warn, Summary: "skipped because the scaffold check failed", Hint: "fix the scaffold directory, then run `lclaw doctor` again"},
	}
	if got := r.Checks[2:4]; !reflect.DeepEqual(got, want) {
		t.Fatalf("scaffold checks =\n%+v\nwant\n%+v", got, want)
	}
}

func TestDoctorTopologyMissingFileIsFailWithInitHint(t *testing.T) {
	d := healthy()
	d.Topology = &fakeLoader{err: fmt.Errorf("toml: read lclaw.toml: %w", fs.ErrNotExist)}
	r, err := d.Run(context.Background(), dirArg)
	if err != nil {
		t.Fatal(err)
	}
	want := domain.Check{Name: "topology", Status: domain.Fail, Summary: "toml: read lclaw.toml: file does not exist", Hint: "run `lclaw init` to write the default lclaw.toml"}
	if got := r.Checks[3]; got != want {
		t.Fatalf("topology check = %+v, want %+v", got, want)
	}
}

func TestDoctorTopologyDecodeErrorIsFail(t *testing.T) {
	d := healthy()
	d.Topology = &fakeLoader{err: errors.New("lclaw.toml: unknown keys: colour")}
	r, err := d.Run(context.Background(), dirArg)
	if err != nil {
		t.Fatal(err)
	}
	want := domain.Check{Name: "topology", Status: domain.Fail, Summary: "lclaw.toml: unknown keys: colour"}
	if got := r.Checks[3]; got != want {
		t.Fatalf("topology check = %+v, want %+v", got, want)
	}
}

func TestDoctorTopologyFindingsAreEachAFailedCheck(t *testing.T) {
	d := healthy()
	top := defaultTopology()
	top.Machines[0].CPUs = 0
	d.Topology = &fakeLoader{topo: top}
	scaffold := fullScaffold()
	delete(scaffold, "workloads/openclaw/pod.yaml")
	d.Scaffold = &fakeScaffold{fsys: scaffold}
	r, err := d.Run(context.Background(), dirArg)
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.Check{
		{Name: "topology", Status: domain.Fail, Summary: "machines.infra.cpus: must be positive, got 0"},
		{Name: "topology", Status: domain.Fail, Summary: "machines.agent.workloads: workloads/openclaw is missing pod.yaml"},
	}
	if got := r.Checks[3:5]; !reflect.DeepEqual(got, want) {
		t.Fatalf("topology checks =\n%+v\nwant\n%+v", got, want)
	}
	if got, want := names(r), []string{"flox", "podman", "scaffold", "topology", "topology", "lclaw-infra", "lclaw-services", "lclaw-agent"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("check names = %v, want %v", got, want)
	}
}

func TestDoctorEverythingBrokenIsAllReported(t *testing.T) {
	d := &Doctor{
		Runtime:  &fakeRuntime{verErr: fmt.Errorf("podman: version: %w", ErrToolNotFound)},
		Envs:     &fakeEnvs{err: fmt.Errorf("flox: version: %w", ErrToolNotFound)},
		Scaffold: &fakeScaffold{err: fmt.Errorf("osfs: open %s: %w", dirArg, fs.ErrNotExist)},
		Topology: &fakeLoader{},
	}
	r, err := d.Run(context.Background(), dirArg)
	if err != nil {
		t.Fatal(err)
	}
	if r.Count(domain.Fail) != 2 || r.Count(domain.Warn) != 3 {
		t.Fatalf("counts: fail=%d warn=%d; report %+v", r.Count(domain.Fail), r.Count(domain.Warn), r)
	}
}

func TestDoctorCancelledBetweenSteps(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d := healthy()
	d.Runtime = &fakeRuntime{version: podman584, machines: allThree, cancelOn: "version", cancel: cancel}
	r, err := d.Run(ctx, dirArg)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	// The podman check ran; nothing after it may be appended.
	if got, want := names(r), []string{"flox", "podman"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("check names = %v, want %v", got, want)
	}
}

func TestDoctorCancelledDuringMachineList(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d := healthy()
	d.Runtime = &fakeRuntime{version: podman584, machines: allThree, cancelOn: "list", cancel: cancel}
	r, err := d.Run(ctx, dirArg)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if got, want := names(r), []string{"flox", "podman", "scaffold", "topology"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("check names = %v, want %v", got, want)
	}
}
