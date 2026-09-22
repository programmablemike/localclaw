package cli

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/programmablemike/localclaw/internal/app"
	"github.com/programmablemike/localclaw/internal/domain"
)

type fakeLifecycle struct {
	report domain.Report
	err    error
	calls  []string
	roles  []domain.Role
}

func (f *fakeLifecycle) Up(ctx context.Context, dir string, roles []domain.Role) (domain.Report, error) {
	f.calls = append(f.calls, "up "+dir)
	f.roles = roles
	return f.report, f.err
}

func (f *fakeLifecycle) Down(ctx context.Context, dir string, roles []domain.Role, destroy bool) (domain.Report, error) {
	if destroy {
		f.calls = append(f.calls, "down --destroy "+dir)
	} else {
		f.calls = append(f.calls, "down "+dir)
	}
	f.roles = roles
	return f.report, f.err
}

func (f *fakeLifecycle) Status(ctx context.Context, dir string) (domain.Report, error) {
	f.calls = append(f.calls, "status "+dir)
	return f.report, f.err
}

func lifecycleDeps(f *fakeLifecycle) Deps {
	return Deps{Doctor: &fakeDoctor{}, Init: &fakeInit{}, Lifecycle: f, DefaultDir: testDefaultDir}
}

var upReport = domain.Report{Checks: []domain.Check{
	{Name: "machine", Status: domain.Pass, Summary: "created and started"},
	{Name: "network/infra", Status: domain.Pass, Summary: "created"},
	{Name: "network/services", Status: domain.Pass, Summary: "created"},
	{Name: "network/agent", Status: domain.Pass, Summary: "created (internal)"},
	{Name: "infra/secrets", Status: domain.Pass, Summary: "0 injected"},
	{Name: "infra/wireguard", Status: domain.Pass, Summary: "built and playing"},
	{Name: "infra/kuma-cp", Status: domain.Pass, Summary: "built and playing"},
	{Name: "infra/gateway", Status: domain.Pass, Summary: "built and playing"},
	{Name: "services/secrets", Status: domain.Pass, Summary: "4 injected"},
	{Name: "services/litellm-db", Status: domain.Pass, Summary: "built and playing"},
	{Name: "services/litellm", Status: domain.Fail, Summary: "podman: build localhost/lclaw/litellm:latest: exit status 1: STEP 1/1: FROM ghcr.io/berriai/litellm: no such image"},
	{Name: "agent", Status: domain.Warn, Summary: "skipped because services/litellm failed", Hint: "fix it, then run `lclaw up` again"},
}}

var statusReport = domain.Report{Checks: []domain.Check{
	{Name: "machine", Status: domain.Pass, Summary: "running"},
	{Name: "network/infra", Status: domain.Pass, Summary: "present"},
	{Name: "network/services", Status: domain.Pass, Summary: "present"},
	{Name: "network/agent", Status: domain.Pass, Summary: "present (internal)"},
	{Name: "infra/wireguard", Status: domain.Pass, Summary: "running"},
	{Name: "infra/kuma-cp", Status: domain.Pass, Summary: "running"},
	{Name: "infra/gateway", Status: domain.Pass, Summary: "running"},
	{Name: "services/litellm-db", Status: domain.Pass, Summary: "running"},
	{Name: "services/litellm", Status: domain.Fail, Summary: "degraded: container litellm-litellm exited", Hint: "podman --connection lclaw pod logs litellm"},
	{Name: "services/agentgateway", Status: domain.Pass, Summary: "running"},
	{Name: "agent/openclaw", Status: domain.Warn, Summary: "absent", Hint: "run `lclaw up agent`"},
}}

func TestUpText(t *testing.T) {
	f := &fakeLifecycle{report: upReport}
	r := executeDeps(t, lifecycleDeps(f), "up")
	if !errors.Is(r.err, ErrChecksFailed) {
		t.Fatalf("err = %v, want ErrChecksFailed for a failed check", r.err)
	}
	golden(t, "up-failed.txt", r.stdout.Bytes())
	if !reflect.DeepEqual(f.calls, []string{"up " + testDefaultDir}) || !reflect.DeepEqual(f.roles, domain.Roles()) {
		t.Fatalf("calls = %v roles = %v", f.calls, f.roles)
	}
}

func TestUpZonesAreSelectedInOrder(t *testing.T) {
	f := &fakeLifecycle{report: statusReport}
	r := executeDeps(t, lifecycleDeps(f), "up", "agent", "infra")
	if r.err == nil {
		t.Fatalf("a failed check in the report must return ErrChecksFailed")
	}
	if !reflect.DeepEqual(f.roles, []domain.Role{domain.Infra, domain.Agent}) {
		t.Fatalf("roles = %v, want infra then agent", f.roles)
	}
}

func TestUpUnknownZoneIsUsageError(t *testing.T) {
	f := &fakeLifecycle{}
	r := executeDeps(t, lifecycleDeps(f), "up", "laptop")
	if got := ExitCode(r.err); got != 2 {
		t.Fatalf("exit code = %d (err %v), want 2", got, r.err)
	}
	if !strings.Contains(r.stderr.String(), "unknown zone laptop; the zones are infra, services, agent") || len(f.calls) != 0 {
		t.Fatalf("stderr = %q, calls = %v", r.stderr.String(), f.calls)
	}
}

func TestUpJSON(t *testing.T) {
	r := executeDeps(t, lifecycleDeps(&fakeLifecycle{report: upReport}), "--output", "json", "up")
	if !errors.Is(r.err, ErrChecksFailed) {
		t.Fatal(r.err)
	}
	if !bytes.Contains(r.stdout.Bytes(), []byte(`"status": "fail"`)) || !bytes.Contains(r.stdout.Bytes(), []byte(`"name": "services/litellm"`)) {
		t.Fatalf("stdout = %s", r.stdout.String())
	}
	if r.stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty: a fake lifecycle reports no progress", r.stderr.String())
	}
}

func TestUpFindingsRenderAsReport(t *testing.T) {
	f := &fakeLifecycle{err: &app.FindingsError{Findings: []domain.Finding{{Where: "machine.cpus", Message: "must be positive, got 0"}}}}
	r := executeDeps(t, lifecycleDeps(f), "up")
	if !errors.Is(r.err, ErrChecksFailed) {
		t.Fatalf("err = %v", r.err)
	}
	if want := "FAIL  machine.cpus  must be positive, got 0\n\n0 passed, 0 warnings, 1 failed\n"; r.stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", r.stdout.String(), want)
	}
}

func TestUpOtherErrorsExit1(t *testing.T) {
	f := &fakeLifecycle{err: errors.New("keychain not found at /kc; run `lclaw init` to create it")}
	r := executeDeps(t, lifecycleDeps(f), "up")
	if got := ExitCode(r.err); got != 1 || Silent(r.err) {
		t.Fatalf("exit code = %d, silent = %v (err %v)", got, Silent(r.err), r.err)
	}
}

func TestDownDestroy(t *testing.T) {
	f := &fakeLifecycle{report: domain.Report{Checks: []domain.Check{{Name: "destroy", Status: domain.Pass, Summary: "lclaw removed with its images and volumes"}}}}
	r := executeDeps(t, lifecycleDeps(f), "down", "--destroy")
	if r.err != nil {
		t.Fatal(r.err)
	}
	if !reflect.DeepEqual(f.calls, []string{"down --destroy " + testDefaultDir}) {
		t.Fatalf("calls = %v", f.calls)
	}
}

func TestDownDestroyWithAZoneIsUsageError(t *testing.T) {
	f := &fakeLifecycle{}
	r := executeDeps(t, lifecycleDeps(f), "down", "--destroy", "agent")
	if got := ExitCode(r.err); got != 2 || len(f.calls) != 0 {
		t.Fatalf("exit code = %d, calls = %v", got, f.calls)
	}
}

func TestDownZone(t *testing.T) {
	f := &fakeLifecycle{report: domain.Report{Checks: []domain.Check{{Name: "agent/openclaw", Status: domain.Pass, Summary: "stopped"}}}}
	r := executeDeps(t, lifecycleDeps(f), "down", "agent")
	if r.err != nil {
		t.Fatal(r.err)
	}
	if !reflect.DeepEqual(f.roles, []domain.Role{domain.Agent}) {
		t.Fatalf("roles = %v", f.roles)
	}
}

func TestStatusText(t *testing.T) {
	f := &fakeLifecycle{report: statusReport}
	r := executeDeps(t, lifecycleDeps(f), "status")
	if !errors.Is(r.err, ErrChecksFailed) {
		t.Fatalf("err = %v, want ErrChecksFailed for a degraded pod", r.err)
	}
	golden(t, "status-mixed.txt", r.stdout.Bytes())
	if got := ExitCode(r.err); got != 1 {
		t.Fatalf("exit code = %d, want 1", got)
	}
}

func TestStatusArgumentIsUsageError(t *testing.T) {
	r := executeDeps(t, lifecycleDeps(&fakeLifecycle{}), "status", "agent")
	if got := ExitCode(r.err); got != 2 {
		t.Fatalf("exit code = %d, want 2", got)
	}
}

func TestProgressWritesOneLinePerStep(t *testing.T) {
	var buf bytes.Buffer
	Progress(&buf).Step("machine", "starting lclaw")
	if got, want := buf.String(), "==> machine: starting lclaw\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
