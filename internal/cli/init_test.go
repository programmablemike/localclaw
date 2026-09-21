package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/programmablemike/localclaw/internal/app"
)

type fakeInit struct {
	report app.InitReport
	err    error
	dir    string
	force  bool
	calls  int
}

func (f *fakeInit) Run(ctx context.Context, dir string, force bool) (app.InitReport, error) {
	f.calls++
	f.dir, f.force = dir, force
	return f.report, f.err
}

// executeInit runs the tree with ini as the init use case.
func executeInit(t *testing.T, ini *fakeInit, args ...string) *run {
	t.Helper()
	return executeDeps(t, Deps{Doctor: &fakeDoctor{}, Init: ini, DefaultDir: testDefaultDir}, args...)
}

var initWritten = app.InitReport{
	Dir: testDefaultDir,
	Written: []string{
		"lclaw.toml",
		"machines/agent/playbook.yaml",
		"workloads/openclaw/.containerignore",
		"workloads/openclaw/Containerfile",
		"workloads/openclaw/pod.yaml",
	},
	Keychain: app.KeychainOutcome{Path: "/home/tester/Library/Keychains/lclaw.keychain-db", State: app.KeychainCreated},
}

var initMixed = app.InitReport{
	Dir:     testDefaultDir,
	Written: []string{"lclaw.toml", "machines/agent/playbook.yaml"},
	Skipped: []string{"workloads/openclaw/pod.yaml"},
	Failed: []app.InitFailure{{
		Path: "workloads/openclaw/Containerfile",
		Err:  errors.New("osfs: write /home/test/.config/lclaw/workloads/openclaw/Containerfile: permission denied"),
	}},
	Keychain: app.KeychainOutcome{Path: "/home/tester/Library/Keychains/lclaw.keychain-db", State: app.KeychainFailed, Err: errors.New("keychain: create-keychain: exit status 1")},
}

func TestInitText(t *testing.T) {
	unsetenv(t, "LCLAW_DIR")
	ini := &fakeInit{report: initWritten}
	r := executeInit(t, ini, "init")
	if r.err != nil {
		t.Fatal(r.err)
	}
	golden(t, "init-written.txt", r.stdout.Bytes())
	if r.stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", r.stderr.String())
	}
	if ini.dir != testDefaultDir || ini.force {
		t.Fatalf("Run(dir=%q, force=%v), want default dir and no force", ini.dir, ini.force)
	}
}

func TestInitMixedTextExit1(t *testing.T) {
	ini := &fakeInit{report: initMixed}
	r := executeInit(t, ini, "init")
	if !errors.Is(r.err, ErrInitFailed) {
		t.Fatalf("err = %v, want ErrInitFailed", r.err)
	}
	if got := ExitCode(r.err); got != 1 {
		t.Fatalf("exit code = %d, want 1", got)
	}
	golden(t, "init-mixed.txt", r.stdout.Bytes())
}

func TestInitMixedJSON(t *testing.T) {
	ini := &fakeInit{report: initMixed}
	r := executeInit(t, ini, "--output", "json", "init")
	if !errors.Is(r.err, ErrInitFailed) {
		t.Fatalf("err = %v, want ErrInitFailed", r.err)
	}
	golden(t, "init-mixed.json", r.stdout.Bytes())
}

func TestInitKeychainFailureExits1(t *testing.T) {
	rep := initWritten
	rep.Keychain = app.KeychainOutcome{Path: "/k/lclaw.keychain-db", State: app.KeychainFailed, Err: errors.New("keychain: create-keychain: exit status 1")}
	r := executeInit(t, &fakeInit{report: rep}, "init")
	if !errors.Is(r.err, ErrInitFailed) || ExitCode(r.err) != 1 {
		t.Fatalf("err = %v, exit %d", r.err, ExitCode(r.err))
	}
	if !strings.Contains(r.stdout.String(), "failed   /k/lclaw.keychain-db: keychain: create-keychain: exit status 1") {
		t.Fatalf("stdout = %q", r.stdout.String())
	}
}

func TestInitAllSkippedIsSuccess(t *testing.T) {
	ini := &fakeInit{report: app.InitReport{Dir: "/x", Skipped: []string{"lclaw.toml"}}}
	r := executeInit(t, ini, "--output", "json", "--dir", "/x", "init")
	if r.err != nil {
		t.Fatalf("err = %v, want nil: nothing to write is success", r.err)
	}
	out := r.stdout.String()
	for _, want := range []string{`"dir": "/x"`, `"written": []`, `"skipped": [`, `"failed": []`} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout lacks %s:\n%s", want, out)
		}
	}
}

func TestInitForceAndDir(t *testing.T) {
	ini := &fakeInit{report: app.InitReport{Dir: "/y"}}
	if r := executeInit(t, ini, "init", "--force", "--dir", "/y"); r.err != nil {
		t.Fatal(r.err)
	}
	if ini.dir != "/y" || !ini.force {
		t.Fatalf("Run(dir=%q, force=%v), want /y and force", ini.dir, ini.force)
	}
}

func TestInitNoDirIsUsageError(t *testing.T) {
	unsetenv(t, "LCLAW_DIR")
	ini := &fakeInit{}
	r := executeDeps(t, Deps{Doctor: &fakeDoctor{}, Init: ini, DefaultDir: ""}, "init")
	if got := ExitCode(r.err); got != 2 {
		t.Fatalf("exit code = %d (err %v), want 2", got, r.err)
	}
	if ini.calls != 0 {
		t.Fatal("init ran without a directory")
	}
}

func TestInitInterrupted(t *testing.T) {
	ini := &fakeInit{err: fmt.Errorf("init: %w", context.Canceled)}
	r := executeInit(t, ini, "init")
	if got := ExitCode(r.err); got != 130 {
		t.Fatalf("exit code = %d (err %v), want 130", got, r.err)
	}
	if r.stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", r.stdout.String())
	}
}

func TestRenderInitTextEmpty(t *testing.T) {
	var buf bytes.Buffer
	renderInitText(&buf, app.InitReport{Dir: "/x"})
	if got, want := buf.String(), "\n/x: 0 written, 0 skipped, 0 failed\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRenderInitJSONEmpty(t *testing.T) {
	var buf bytes.Buffer
	if err := renderInitJSON(&buf, app.InitReport{Dir: "/x"}); err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"dir\": \"/x\",\n  \"written\": [],\n  \"skipped\": [],\n  \"failed\": [],\n  \"keychain\": {\n    \"state\": \"\"\n  }\n}\n"
	if got := buf.String(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
