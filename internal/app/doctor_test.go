package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/programmablemike/localclaw/internal/domain"
)

type fakeEnvs struct {
	version domain.Version
	err     error
}

func (f *fakeEnvs) Version(ctx context.Context) (domain.Version, error) { return f.version, f.err }

// fakeScaffold and fakeLoader are declared once, in fakes_test.go, and
// shared by every test file in this package.

const dirArg = "/home/test/.config/lclaw"

var (
	podman584      = domain.Version{Major: 5, Minor: 8, Patch: 4, Raw: "5.8.4"}
	flox1131       = domain.Version{Major: 1, Minor: 13, Patch: 1, Raw: "1.13.1-g684cdfb"}
	runningMachine = []domain.Machine{{Name: "podman-machine-default"}, {Name: "lclaw", Running: true}}

	floxPass   = domain.Check{Name: "flox", Status: domain.Pass, Summary: "1.13.1 (minimum 1.0.0)"}
	podmanPass = domain.Check{Name: "podman", Status: domain.Pass, Summary: "5.8.4 (minimum 5.8.0)"}
)

// defaultTopology mirrors the embedded lclaw.toml, without the declared
// provider key.
func defaultTopology() domain.Topology {
	top := servicesTopology()
	top.Zones[1].Secrets = nil
	return top
}

// fullScaffold is a file system holding every file the default topology needs.
func fullScaffold() fstest.MapFS {
	m := fstest.MapFS{domain.TopologyFile: {Data: []byte("schema = 2\n")}}
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
		Runtime:   &fakeRuntime{version: podman584, machines: runningMachine},
		Workloads: allNetworks(),
		Envs:      &fakeEnvs{version: flox1131},
		Scaffold:  &fakeScaffold{fsys: fullScaffold()},
		Topology:  &fakeLoader{topo: defaultTopology()},
		Keychain:  &fakeKeychain{exists: true},
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
		{Name: "topology", Status: domain.Pass, Summary: "1 machine, 3 zones, 7 workloads"},
		keychainPass,
		{Name: "machine", Status: domain.Pass, Summary: "running"},
		{Name: "network/infra", Status: domain.Pass, Summary: "present"},
		{Name: "network/services", Status: domain.Pass, Summary: "present"},
		{Name: "network/agent", Status: domain.Pass, Summary: "present (internal)"},
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
	want := []string{"flox", "podman", "scaffold", "topology", "keychain", "machine"}
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
		{Name: "topology", Status: domain.Pass, Summary: "1 machine, 3 zones, 7 workloads"},
		keychainPass,
		{Name: "machine", Status: domain.Warn, Summary: "skipped because the podman check failed", Hint: "fix podman, then run `lclaw doctor` again"},
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
	if got, want := names(r), []string{"flox", "podman", "scaffold", "topology", "keychain", "machine"}; !reflect.DeepEqual(got, want) {
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
	want := domain.Check{Name: "machine", Status: domain.Fail, Summary: "podman: list machines: exit status 125: cannot connect"}
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
	if got, want := names(r), []string{"flox", "podman", "scaffold", "topology", "keychain", "machine"}; !reflect.DeepEqual(got, want) {
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
	top.Machine.CPUs = 0
	d.Topology = &fakeLoader{topo: top}
	scaffold := fullScaffold()
	delete(scaffold, "workloads/openclaw/pod.yaml")
	d.Scaffold = &fakeScaffold{fsys: scaffold}
	r, err := d.Run(context.Background(), dirArg)
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.Check{
		{Name: "topology", Status: domain.Fail, Summary: "machine.cpus: must be positive, got 0"},
		{Name: "topology", Status: domain.Fail, Summary: "zones.agent.workloads: workloads/openclaw is missing pod.yaml"},
	}
	if got := r.Checks[3:5]; !reflect.DeepEqual(got, want) {
		t.Fatalf("topology checks =\n%+v\nwant\n%+v", got, want)
	}
	if got, want := names(r), []string{"flox", "podman", "scaffold", "topology", "topology", "keychain", "machine", "network/infra", "network/services", "network/agent"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("check names = %v, want %v", got, want)
	}
}

func TestDoctorEverythingBrokenIsAllReported(t *testing.T) {
	d := &Doctor{
		Runtime:   &fakeRuntime{verErr: fmt.Errorf("podman: version: %w", ErrToolNotFound)},
		Workloads: newFakeWorkloads(),
		Envs:      &fakeEnvs{err: fmt.Errorf("flox: version: %w", ErrToolNotFound)},
		Scaffold:  &fakeScaffold{err: fmt.Errorf("osfs: open %s: %w", dirArg, fs.ErrNotExist)},
		Topology:  &fakeLoader{},
		Keychain:  &fakeKeychain{},
	}
	r, err := d.Run(context.Background(), dirArg)
	if err != nil {
		t.Fatal(err)
	}
	// scaffold and topology are Warn (skipped), keychain is Warn (skipped
	// because the topology check failed), and machines is Warn (skipped
	// because the podman check failed): four Warns alongside the two Fails
	// from flox and podman.
	if r.Count(domain.Fail) != 2 || r.Count(domain.Warn) != 4 {
		t.Fatalf("counts: fail=%d warn=%d; report %+v", r.Count(domain.Fail), r.Count(domain.Warn), r)
	}
}

func TestDoctorCancelledBetweenSteps(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d := healthy()
	d.Runtime = &fakeRuntime{version: podman584, machines: runningMachine, cancelOn: "version", cancel: cancel}
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
	d.Runtime = &fakeRuntime{version: podman584, machines: runningMachine, cancelOn: "list", cancel: cancel}
	r, err := d.Run(ctx, dirArg)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if got, want := names(r), []string{"flox", "podman", "scaffold", "topology", "keychain"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("check names = %v, want %v", got, want)
	}
}

// keychainPass is the check a present keychain produces, for the tests
// that only care about what comes after it.
var keychainPass = domain.Check{Name: "keychain", Status: domain.Pass, Summary: kcPath}

func newDoctorWithSecrets(kc *fakeKeychain, topo domain.Topology) *Doctor {
	d := healthy() // the existing helper wiring Runtime, Envs, Scaffold, Topology
	d.Topology = &fakeLoader{topo: topo}
	d.Keychain = kc
	return d
}

func TestDoctorReportsEveryDeclaredSecret(t *testing.T) {
	topo := servicesTopology()
	topo.Zones[1].Secrets = []string{"anthropic-api-key", "openai-api-key"}
	kc := &fakeKeychain{exists: true, items: map[string]fakeItem{
		"anthropic-api-key": {value: []byte("k"), source: domain.User},
	}}
	r, err := newDoctorWithSecrets(kc, topo).Run(context.Background(), "/d")
	if err != nil {
		t.Fatal(err)
	}
	got := checksNamed(r, "keychain", "anthropic-api-key", "openai-api-key")
	want := []domain.Check{
		keychainPass,
		{Name: "anthropic-api-key", Status: domain.Pass, Summary: "set"},
		{Name: "openai-api-key", Status: domain.Fail, Summary: "unset", Hint: "run `lclaw secrets set openai-api-key`"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("checks =\n%+v\nwant\n%+v", got, want)
	}
	for _, c := range kc.calls {
		if strings.HasPrefix(c, "get ") {
			t.Fatalf("doctor must not read values: %v", kc.calls)
		}
	}
}

func TestDoctorKeychainMissingSkipsSecretChecks(t *testing.T) {
	kc := &fakeKeychain{exists: false}
	r, err := newDoctorWithSecrets(kc, servicesTopology()).Run(context.Background(), "/d")
	if err != nil {
		t.Fatal(err)
	}
	got := checksNamed(r, "keychain", "secrets", "anthropic-api-key")
	want := []domain.Check{
		{Name: "keychain", Status: domain.Fail, Summary: "not found at " + kcPath, Hint: "run `lclaw init` to create it"},
		{Name: "secrets", Status: domain.Warn, Summary: "skipped because the keychain check failed", Hint: "run `lclaw init`, then `lclaw doctor` again"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("checks =\n%+v\nwant\n%+v", got, want)
	}
}

func TestDoctorKeychainSkippedWhenTheTopologyCannotBeRead(t *testing.T) {
	d := newDoctorWithSecrets(&fakeKeychain{exists: true}, servicesTopology())
	d.Topology = &fakeLoader{err: errors.New("lclaw.toml: line 3: expected key")}
	r, err := d.Run(context.Background(), "/d")
	if err != nil {
		t.Fatal(err)
	}
	got := checksNamed(r, "keychain")
	want := []domain.Check{{Name: "keychain", Status: domain.Warn, Summary: "skipped because the topology check failed", Hint: "fix lclaw.toml, then run `lclaw doctor` again"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("checks = %+v, want %+v", got, want)
	}
}

func TestDoctorNoDeclaredSecretsEmitsOnlyTheKeychainCheck(t *testing.T) {
	topo := servicesTopology()
	topo.Zones[1].Secrets = nil
	r, err := newDoctorWithSecrets(&fakeKeychain{exists: true}, topo).Run(context.Background(), "/d")
	if err != nil {
		t.Fatal(err)
	}
	if got := checksNamed(r, "keychain"); len(got) != 1 || got[0] != keychainPass {
		t.Fatalf("checks = %+v", got)
	}
	if got := checksNamed(r, "secrets"); len(got) != 0 {
		t.Fatalf("a secrets check appeared with nothing declared: %+v", got)
	}
}

// checksNamed returns the report's checks whose names are in want, in
// report order, so a test asserts on a slice of the report rather than on
// fragile indices.
func checksNamed(r domain.Report, want ...string) []domain.Check {
	keep := map[string]bool{}
	for _, n := range want {
		keep[n] = true
	}
	var out []domain.Check
	for _, c := range r.Checks {
		if keep[c.Name] {
			out = append(out, c)
		}
	}
	return out
}

func TestDoctorCheckOrder(t *testing.T) {
	r, err := newDoctorWithSecrets(&fakeKeychain{exists: true}, servicesTopology()).Run(context.Background(), "/d")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, c := range r.Checks {
		names = append(names, c.Name)
	}
	want := []string{"flox", "podman", "scaffold", "topology", "keychain", "anthropic-api-key", "machine", "network/infra", "network/services", "network/agent"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("check order = %v, want %v", names, want)
	}
}

// allNetworks is a WorkloadRuntime reporting every zone network present.
func allNetworks() *fakeWorkloads {
	w := newFakeWorkloads()
	for _, r := range domain.Roles() {
		w.networks[r.NetworkName()] = true
	}
	return w
}

func TestDoctorStoppedMachineSkipsNetworks(t *testing.T) {
	d := healthy()
	d.Runtime = &fakeRuntime{version: podman584, machines: []domain.Machine{{Name: "lclaw"}}}
	r, err := d.Run(context.Background(), dirArg)
	if err != nil {
		t.Fatal(err)
	}
	last := r.Checks[len(r.Checks)-1]
	if want := (domain.Check{Name: "machine", Status: domain.Pass, Summary: "stopped", Hint: "run `lclaw up`"}); last != want {
		t.Fatalf("last check = %+v, want %+v", last, want)
	}
}

func TestDoctorMissingNetworkIsAWarning(t *testing.T) {
	d := healthy()
	w := allNetworks()
	delete(w.networks, "lclaw-agent")
	d.Workloads = w
	r, err := d.Run(context.Background(), dirArg)
	if err != nil {
		t.Fatal(err)
	}
	last := r.Checks[len(r.Checks)-1]
	if want := (domain.Check{Name: "network/agent", Status: domain.Warn, Summary: "missing", Hint: "run `lclaw up agent`"}); last != want {
		t.Fatalf("last check = %+v, want %+v", last, want)
	}
}

func TestDoctorNetworkErrorIsAFailedCheck(t *testing.T) {
	d := healthy()
	w := allNetworks()
	w.netErr = errors.New("podman: network exists lclaw-infra: exit status 125: cannot connect")
	d.Workloads = w
	r, err := d.Run(context.Background(), dirArg)
	if err != nil {
		t.Fatal(err)
	}
	last := r.Checks[len(r.Checks)-1]
	if last.Name != "networks" || last.Status != domain.Fail {
		t.Fatalf("last check = %+v, want a failed networks check", last)
	}
}
