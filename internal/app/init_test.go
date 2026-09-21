package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/programmablemike/localclaw/internal/domain"
)

type writeCall struct {
	path      string
	overwrite bool
}

// fakeWriter is an in-memory FileWriter. files holds what exists; failOn
// makes one path fail with the given error.
type fakeWriter struct {
	files  map[string][]byte
	failOn map[string]error
	calls  []writeCall
}

func newFakeWriter() *fakeWriter {
	return &fakeWriter{files: map[string][]byte{}, failOn: map[string]error{}}
}

func (f *fakeWriter) WriteFile(path string, data []byte, overwrite bool) error {
	f.calls = append(f.calls, writeCall{path: path, overwrite: overwrite})
	if err := f.failOn[path]; err != nil {
		return err
	}
	if _, exists := f.files[path]; exists && !overwrite {
		return fmt.Errorf("fake: write %s: %w", path, fs.ErrExist)
	}
	f.files[path] = append([]byte(nil), data...)
	return nil
}

var defaults = fstest.MapFS{
	"lclaw.toml":                          {Data: []byte("schema = 1\n")},
	"machines/infra/playbook.yaml":        {Data: []byte("- hosts: localhost\n")},
	"workloads/openclaw/.containerignore": {Data: []byte("pod.yaml\n")},
	"workloads/openclaw/Containerfile":    {Data: []byte("FROM x@sha256:0\n")},
	"workloads/openclaw/pod.yaml":         {Data: []byte("kind: Pod\n")},
}

// allPaths is every default in the order fs.WalkDir visits it.
var allPaths = []string{
	"lclaw.toml",
	"machines/infra/playbook.yaml",
	"workloads/openclaw/.containerignore",
	"workloads/openclaw/Containerfile",
	"workloads/openclaw/pod.yaml",
}

const dir = "/tmp/lclaw"

func target(rel string) string { return filepath.Join(dir, filepath.FromSlash(rel)) }

// newInitForTest wires Defaults and a fresh fake writer, plus safe fakes
// for the three keychain ports, so a nil Keychain never reaches Run. Tests
// that inspect the writer reach it through i.Writer.(*fakeWriter).
func newInitForTest() *Init {
	return &Init{
		Defaults: defaults,
		Writer:   newFakeWriter(),
		Scaffold: &fakeScaffold{},
		Topology: &fakeLoader{topo: servicesTopology()},
		Keychain: &fakeKeychain{},
	}
}

func TestInitWritesEverythingIntoAnEmptyDirectory(t *testing.T) {
	i := newInitForTest()
	w := i.Writer.(*fakeWriter)
	r, err := i.Run(context.Background(), dir, false)
	if err != nil {
		t.Fatal(err)
	}
	want := InitReport{Dir: dir, Written: allPaths, Keychain: KeychainOutcome{Path: kcPath, State: KeychainCreated}}
	if !reflect.DeepEqual(r, want) {
		t.Fatalf("report =\n%+v\nwant\n%+v", r, want)
	}
	for _, p := range allPaths {
		if got, want := string(w.files[target(p)]), string(defaults[p].Data); got != want {
			t.Errorf("%s: wrote %q, want %q", p, got, want)
		}
	}
	for _, c := range w.calls {
		if c.overwrite {
			t.Errorf("%s: overwrite was true without --force", c.path)
		}
	}
}

func TestInitSkipsEveryExistingFile(t *testing.T) {
	i := newInitForTest()
	w := i.Writer.(*fakeWriter)
	for _, p := range allPaths {
		w.files[target(p)] = []byte("user edit")
	}
	r, err := i.Run(context.Background(), dir, false)
	if err != nil {
		t.Fatal(err)
	}
	want := InitReport{Dir: dir, Skipped: allPaths, Keychain: KeychainOutcome{Path: kcPath, State: KeychainCreated}}
	if !reflect.DeepEqual(r, want) {
		t.Fatalf("report =\n%+v\nwant\n%+v", r, want)
	}
	for _, p := range allPaths {
		if got := string(w.files[target(p)]); got != "user edit" {
			t.Errorf("%s: content changed to %q", p, got)
		}
	}
}

func TestInitMixed(t *testing.T) {
	i := newInitForTest()
	w := i.Writer.(*fakeWriter)
	w.files[target("lclaw.toml")] = []byte("user edit")
	w.files[target("workloads/openclaw/pod.yaml")] = []byte("user edit")
	r, err := i.Run(context.Background(), dir, false)
	if err != nil {
		t.Fatal(err)
	}
	want := InitReport{
		Dir:      dir,
		Written:  []string{"machines/infra/playbook.yaml", "workloads/openclaw/.containerignore", "workloads/openclaw/Containerfile"},
		Skipped:  []string{"lclaw.toml", "workloads/openclaw/pod.yaml"},
		Keychain: KeychainOutcome{Path: kcPath, State: KeychainCreated},
	}
	if !reflect.DeepEqual(r, want) {
		t.Fatalf("report =\n%+v\nwant\n%+v", r, want)
	}
}

func TestInitForceOverwrites(t *testing.T) {
	i := newInitForTest()
	w := i.Writer.(*fakeWriter)
	for _, p := range allPaths {
		w.files[target(p)] = []byte("user edit")
	}
	r, err := i.Run(context.Background(), dir, true)
	if err != nil {
		t.Fatal(err)
	}
	want := InitReport{Dir: dir, Written: allPaths, Keychain: KeychainOutcome{Path: kcPath, State: KeychainCreated}}
	if !reflect.DeepEqual(r, want) {
		t.Fatalf("report =\n%+v\nwant\n%+v", r, want)
	}
	for _, p := range allPaths {
		if got, want := string(w.files[target(p)]), string(defaults[p].Data); got != want {
			t.Errorf("%s: content %q, want the default %q", p, got, want)
		}
	}
	for _, c := range w.calls {
		if !c.overwrite {
			t.Errorf("%s: overwrite was false with --force", c.path)
		}
	}
}

func TestInitContinuesPastAFailure(t *testing.T) {
	i := newInitForTest()
	w := i.Writer.(*fakeWriter)
	boom := errors.New("fake: write: permission denied")
	w.failOn[target("workloads/openclaw/Containerfile")] = boom
	r, err := i.Run(context.Background(), dir, false)
	if err != nil {
		t.Fatal(err)
	}
	want := InitReport{
		Dir:      dir,
		Written:  []string{"lclaw.toml", "machines/infra/playbook.yaml", "workloads/openclaw/.containerignore", "workloads/openclaw/pod.yaml"},
		Failed:   []InitFailure{{Path: "workloads/openclaw/Containerfile", Err: boom}},
		Keychain: KeychainOutcome{Path: kcPath, State: KeychainCreated},
	}
	if !reflect.DeepEqual(r, want) {
		t.Fatalf("report =\n%+v\nwant\n%+v", r, want)
	}
	if len(w.calls) != len(allPaths) {
		t.Fatalf("%d writes attempted, want %d: the run must continue past a failure", len(w.calls), len(allPaths))
	}
}

func TestInitStopsWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	i := newInitForTest()
	w := i.Writer.(*fakeWriter)
	r, err := i.Run(ctx, dir, false)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if len(w.calls) != 0 || len(r.Written) != 0 {
		t.Fatalf("wrote %d files after cancellation", len(w.calls))
	}
}

// newInitWithKeychain returns an Init wired by newInitForTest, which sets
// all five ports (Defaults, Writer, Scaffold, Topology and Keychain), then
// swaps in kc for the keychain port and a loader that returns topo.
func newInitWithKeychain(kc *fakeKeychain, topo domain.Topology) *Init {
	i := newInitForTest()
	i.Topology = &fakeLoader{topo: topo}
	i.Keychain = kc
	return i
}

func TestInitCreatesTheKeychain(t *testing.T) {
	kc := &fakeKeychain{}
	rep, err := newInitWithKeychain(kc, servicesTopology()).Run(context.Background(), "/d", false)
	if err != nil {
		t.Fatal(err)
	}
	want := KeychainOutcome{Path: kcPath, State: KeychainCreated}
	if rep.Keychain != want {
		t.Fatalf("Keychain = %+v, want %+v", rep.Keychain, want)
	}
	if !reflect.DeepEqual(kc.calls, []string{"exists " + kcPath, "create " + kcPath}) {
		t.Fatalf("keychain calls = %v", kc.calls)
	}
}

func TestInitSkipsAnExistingKeychainEvenWithForce(t *testing.T) {
	for _, force := range []bool{false, true} {
		kc := &fakeKeychain{exists: true}
		rep, err := newInitWithKeychain(kc, servicesTopology()).Run(context.Background(), "/d", force)
		if err != nil {
			t.Fatal(err)
		}
		if rep.Keychain.State != KeychainSkipped {
			t.Fatalf("force=%v: Keychain = %+v, want skipped", force, rep.Keychain)
		}
		if len(kc.calls) != 1 {
			t.Fatalf("force=%v: calls = %v, want only the existence check", force, kc.calls)
		}
	}
}

func TestInitKeychainFailureIsReportedNotReturned(t *testing.T) {
	kc := &fakeKeychain{createErr: errors.New("keychain: create-keychain: exit status 1")}
	rep, err := newInitWithKeychain(kc, servicesTopology()).Run(context.Background(), "/d", false)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Keychain.State != KeychainFailed || rep.Keychain.Err == nil {
		t.Fatalf("Keychain = %+v", rep.Keychain)
	}
	if rep.Keychain.Path != kcPath {
		t.Fatalf("Path = %q, want %q to survive a create failure", rep.Keychain.Path, kcPath)
	}
	if rep.Ok() {
		t.Fatal("Ok() must be false when the keychain step failed")
	}
}

// TestInitKeychainStepReportsATopologyFailure covers the scaffold failing
// to open: the error must be wrapped and prefixed as a topology problem,
// not left bare where the CLI would blame the keychain for it, and the
// keychain port must never be touched.
func TestInitKeychainStepReportsATopologyFailure(t *testing.T) {
	boom := errors.New("open dir: permission denied")
	kc := &fakeKeychain{}
	i := newInitWithKeychain(kc, servicesTopology())
	i.Scaffold = &fakeScaffold{err: boom}
	rep, err := i.Run(context.Background(), "/d", false)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Keychain.State != KeychainFailed {
		t.Fatalf("State = %v, want KeychainFailed", rep.Keychain.State)
	}
	if rep.Keychain.Path != "" {
		t.Fatalf("Path = %q, want empty: the topology could not be read", rep.Keychain.Path)
	}
	if !errors.Is(rep.Keychain.Err, boom) {
		t.Fatalf("Err = %v, want it to wrap %v", rep.Keychain.Err, boom)
	}
	if !strings.HasPrefix(rep.Keychain.Err.Error(), "init: topology: ") {
		t.Fatalf("Err = %q, want prefix %q", rep.Keychain.Err.Error(), "init: topology: ")
	}
	if len(kc.calls) != 0 {
		t.Fatalf("keychain calls = %v, want none: a topology failure must never touch the keychain", kc.calls)
	}
}

// TestInitKeychainStepReportsAnExistsError covers the keychain port itself
// failing: the error must be wrapped and prefixed as a keychain problem,
// the path must still be reported (it came from the topology, which did
// load), and Create must never be attempted.
func TestInitKeychainStepReportsAnExistsError(t *testing.T) {
	boom := errors.New("keychain: stat: permission denied")
	kc := &fakeKeychain{existsErr: boom}
	rep, err := newInitWithKeychain(kc, servicesTopology()).Run(context.Background(), "/d", false)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Keychain.State != KeychainFailed {
		t.Fatalf("State = %v, want KeychainFailed", rep.Keychain.State)
	}
	if rep.Keychain.Path != kcPath {
		t.Fatalf("Path = %q, want %q", rep.Keychain.Path, kcPath)
	}
	if !errors.Is(rep.Keychain.Err, boom) {
		t.Fatalf("Err = %v, want it to wrap %v", rep.Keychain.Err, boom)
	}
	if !strings.HasPrefix(rep.Keychain.Err.Error(), "init: keychain: ") {
		t.Fatalf("Err = %q, want prefix %q", rep.Keychain.Err.Error(), "init: keychain: ")
	}
	for _, c := range kc.calls {
		if strings.HasPrefix(c, "create") {
			t.Fatalf("keychain calls = %v, want no create call after an exists error", kc.calls)
		}
	}
}

func TestInitTopologyFailureLeavesThePathUnknown(t *testing.T) {
	i := newInitWithKeychain(&fakeKeychain{}, servicesTopology())
	i.Topology = &fakeLoader{err: errors.New("lclaw.toml: line 3: expected key")}
	rep, err := i.Run(context.Background(), "/d", false)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Keychain.State != KeychainFailed || rep.Keychain.Path != "" || rep.Keychain.Err == nil {
		t.Fatalf("Keychain = %+v", rep.Keychain)
	}
}

// Writing the files still succeeds when the keychain step fails, and the
// other way round, so one report shows both.
func TestInitReportsFilesAndKeychainTogether(t *testing.T) {
	kc := &fakeKeychain{}
	rep, err := newInitWithKeychain(kc, servicesTopology()).Run(context.Background(), "/d", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Written) == 0 || rep.Keychain.State != KeychainCreated {
		t.Fatalf("report = %+v", rep)
	}
}
