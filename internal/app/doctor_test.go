package app

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

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

var (
	podman584 = domain.Version{Major: 5, Minor: 8, Patch: 4, Raw: "5.8.4"}
	flox1131  = domain.Version{Major: 1, Minor: 13, Patch: 1, Raw: "1.13.1-g684cdfb"}
	allThree  = []domain.Machine{{Name: "lclaw-infra", Running: true}, {Name: "lclaw-services", Running: true}, {Name: "lclaw-agent", Running: false}}
)

func names(r domain.Report) []string {
	out := make([]string, 0, len(r.Checks))
	for _, c := range r.Checks {
		out = append(out, c.Name)
	}
	return out
}

func TestDoctorAllPass(t *testing.T) {
	d := &Doctor{
		Runtime: &fakeRuntime{version: podman584, machines: allThree},
		Envs:    &fakeEnvs{version: flox1131},
	}
	r, err := d.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := domain.Report{Checks: []domain.Check{
		{Name: "flox", Status: domain.Pass, Summary: "1.13.1 (minimum 1.0.0)"},
		{Name: "podman", Status: domain.Pass, Summary: "5.8.4 (minimum 5.0.0)"},
		{Name: "lclaw-infra", Status: domain.Pass, Summary: "running"},
		{Name: "lclaw-services", Status: domain.Pass, Summary: "running"},
		{Name: "lclaw-agent", Status: domain.Pass, Summary: "stopped"},
	}}
	if !reflect.DeepEqual(r, want) {
		t.Fatalf("Run() =\n%+v\nwant\n%+v", r, want)
	}
}

func TestDoctorFloxMissingStillChecksEverything(t *testing.T) {
	d := &Doctor{
		Runtime: &fakeRuntime{version: podman584, machines: nil},
		Envs:    &fakeEnvs{err: fmt.Errorf("flox: version: %w", ErrToolNotFound)},
	}
	r, err := d.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := names(r), []string{"flox", "podman", "lclaw-infra", "lclaw-services", "lclaw-agent"}; !reflect.DeepEqual(got, want) {
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
	rt := &fakeRuntime{verErr: fmt.Errorf("podman: version: %w", ErrToolNotFound)}
	d := &Doctor{Runtime: rt, Envs: &fakeEnvs{version: flox1131}}
	r, err := d.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := domain.Report{Checks: []domain.Check{
		{Name: "flox", Status: domain.Pass, Summary: "1.13.1 (minimum 1.0.0)"},
		{Name: "podman", Status: domain.Fail, Summary: "not found", Hint: domain.PodmanRequirement.InstallHint},
		{Name: "machines", Status: domain.Warn, Summary: "skipped because the podman check failed", Hint: "fix podman, then run `lclaw doctor` again"},
	}}
	if !reflect.DeepEqual(r, want) {
		t.Fatalf("Run() =\n%+v\nwant\n%+v", r, want)
	}
}

func TestDoctorPodmanTooOldSkipsMachines(t *testing.T) {
	rt := &fakeRuntime{version: domain.Version{Major: 4, Minor: 9, Patch: 3, Raw: "4.9.3"}}
	d := &Doctor{Runtime: rt, Envs: &fakeEnvs{version: flox1131}}
	r, err := d.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := names(r), []string{"flox", "podman", "machines"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("check names = %v, want %v", got, want)
	}
	if r.Checks[1].Status != domain.Fail {
		t.Errorf("podman check = %+v, want Fail", r.Checks[1])
	}
}

func TestDoctorMachineListErrorIsAFailedCheck(t *testing.T) {
	rt := &fakeRuntime{version: podman584, listErr: errors.New("podman: list machines: exit status 125: cannot connect")}
	d := &Doctor{Runtime: rt, Envs: &fakeEnvs{version: flox1131}}
	r, err := d.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	last := r.Checks[len(r.Checks)-1]
	want := domain.Check{Name: "machines", Status: domain.Fail, Summary: "podman: list machines: exit status 125: cannot connect"}
	if last != want {
		t.Fatalf("last check = %+v, want %+v", last, want)
	}
}

func TestDoctorEverythingBrokenIsAllReported(t *testing.T) {
	rt := &fakeRuntime{verErr: fmt.Errorf("podman: version: %w", ErrToolNotFound)}
	d := &Doctor{Runtime: rt, Envs: &fakeEnvs{err: fmt.Errorf("flox: version: %w", ErrToolNotFound)}}
	r, err := d.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.Count(domain.Fail) != 2 || r.Count(domain.Warn) != 1 {
		t.Fatalf("counts: fail=%d warn=%d; report %+v", r.Count(domain.Fail), r.Count(domain.Warn), r)
	}
}

func TestDoctorCancelledBetweenSteps(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rt := &fakeRuntime{version: podman584, machines: allThree, cancelOn: "version", cancel: cancel}
	d := &Doctor{Runtime: rt, Envs: &fakeEnvs{version: flox1131}}
	r, err := d.Run(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	// The podman check ran; the machine list must not have been appended.
	if got, want := names(r), []string{"flox", "podman"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("check names = %v, want %v", got, want)
	}
}

func TestDoctorCancelledDuringMachineList(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rt := &fakeRuntime{version: podman584, machines: allThree, cancelOn: "list", cancel: cancel}
	d := &Doctor{Runtime: rt, Envs: &fakeEnvs{version: flox1131}}
	r, err := d.Run(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	// The list returned, but the context ended first: no machine checks are appended.
	if got, want := names(r), []string{"flox", "podman"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("check names = %v, want %v", got, want)
	}
}
