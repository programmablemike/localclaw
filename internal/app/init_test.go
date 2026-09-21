package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"reflect"
	"testing"
	"testing/fstest"
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

func TestInitWritesEverythingIntoAnEmptyDirectory(t *testing.T) {
	w := newFakeWriter()
	r, err := (&Init{Defaults: defaults, Writer: w}).Run(context.Background(), dir, false)
	if err != nil {
		t.Fatal(err)
	}
	want := InitReport{Dir: dir, Written: allPaths}
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
	w := newFakeWriter()
	for _, p := range allPaths {
		w.files[target(p)] = []byte("user edit")
	}
	r, err := (&Init{Defaults: defaults, Writer: w}).Run(context.Background(), dir, false)
	if err != nil {
		t.Fatal(err)
	}
	want := InitReport{Dir: dir, Skipped: allPaths}
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
	w := newFakeWriter()
	w.files[target("lclaw.toml")] = []byte("user edit")
	w.files[target("workloads/openclaw/pod.yaml")] = []byte("user edit")
	r, err := (&Init{Defaults: defaults, Writer: w}).Run(context.Background(), dir, false)
	if err != nil {
		t.Fatal(err)
	}
	want := InitReport{
		Dir:     dir,
		Written: []string{"machines/infra/playbook.yaml", "workloads/openclaw/.containerignore", "workloads/openclaw/Containerfile"},
		Skipped: []string{"lclaw.toml", "workloads/openclaw/pod.yaml"},
	}
	if !reflect.DeepEqual(r, want) {
		t.Fatalf("report =\n%+v\nwant\n%+v", r, want)
	}
}

func TestInitForceOverwrites(t *testing.T) {
	w := newFakeWriter()
	for _, p := range allPaths {
		w.files[target(p)] = []byte("user edit")
	}
	r, err := (&Init{Defaults: defaults, Writer: w}).Run(context.Background(), dir, true)
	if err != nil {
		t.Fatal(err)
	}
	want := InitReport{Dir: dir, Written: allPaths}
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
	w := newFakeWriter()
	boom := errors.New("fake: write: permission denied")
	w.failOn[target("workloads/openclaw/Containerfile")] = boom
	r, err := (&Init{Defaults: defaults, Writer: w}).Run(context.Background(), dir, false)
	if err != nil {
		t.Fatal(err)
	}
	want := InitReport{
		Dir:     dir,
		Written: []string{"lclaw.toml", "machines/infra/playbook.yaml", "workloads/openclaw/.containerignore", "workloads/openclaw/pod.yaml"},
		Failed:  []InitFailure{{Path: "workloads/openclaw/Containerfile", Err: boom}},
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
	w := newFakeWriter()
	r, err := (&Init{Defaults: defaults, Writer: w}).Run(ctx, dir, false)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if len(w.calls) != 0 || len(r.Written) != 0 {
		t.Fatalf("wrote %d files after cancellation", len(w.calls))
	}
}
