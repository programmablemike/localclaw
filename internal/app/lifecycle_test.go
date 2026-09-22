package app

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/programmablemike/localclaw/internal/domain"
)

// lifecycleFixture wires a Lifecycle whose every dependency succeeds: the
// keychain holds the provider key, the machine does not exist yet, and
// the minter answers.
type lifecycleFixture struct {
	lc       *Lifecycle
	kc       *fakeKeychain
	runtime  *fakeRuntime
	work     *fakeWorkloads
	target   *fakeTarget
	minter   *fakeMinter
	progress *fakeProgress
}

func newLifecycle(topo domain.Topology) *lifecycleFixture {
	f := &lifecycleFixture{
		kc:       &fakeKeychain{exists: true, items: map[string]fakeItem{"anthropic-api-key": {value: []byte("sk-ant"), source: domain.User}}},
		runtime:  &fakeRuntime{},
		work:     newFakeWorkloads(),
		target:   newFakeTarget(),
		minter:   &fakeMinter{value: []byte("sk-minted")},
		progress: &fakeProgress{},
	}
	f.lc = &Lifecycle{
		Runtime:   f.runtime,
		Workloads: f.work,
		Scaffold:  &fakeScaffold{fsys: fullScaffold()},
		Topology:  &fakeLoader{topo: topo},
		Keychain:  f.kc,
		Target:    f.target,
		Minter:    f.minter,
		Random:    &seqReader{},
		Progress:  f.progress,
	}
	return f
}

func statuses(r domain.Report) map[string]domain.Status {
	out := map[string]domain.Status{}
	for _, c := range r.Checks {
		out[c.Name] = c.Status
	}
	return out
}

func TestUpFirstRunCreatesEverything(t *testing.T) {
	f := newLifecycle(servicesTopology())
	r, err := f.lc.Up(context.Background(), dirArg, domain.Roles())
	if err != nil {
		t.Fatal(err)
	}
	if r.Worst() != domain.Pass {
		t.Fatalf("report has problems:\n%+v", r.Checks)
	}
	want := []string{
		"machine",
		"network/infra", "network/services", "network/agent",
		"infra/secrets", "infra/wireguard", "infra/kuma-cp", "infra/gateway",
		"services/secrets", "services/litellm-db", "services/litellm", "services/agentgateway", "services/ready",
		"agent/secrets", "agent/openclaw",
	}
	if got := names(r); !reflect.DeepEqual(got, want) {
		t.Fatalf("check names = %v, want %v", got, want)
	}
	if r.Checks[0].Summary != "created and started" {
		t.Fatalf("machine check = %+v", r.Checks[0])
	}
	if len(f.runtime.inits) != 1 {
		t.Fatalf("inits = %+v", f.runtime.inits)
	}
	init := f.runtime.inits[0]
	if init.Name != "lclaw" || init.Provider != "libkrun" || init.CPUs != 4 || init.MemoryMiB != 8192 || init.DiskGiB != 60 || init.Playbook != "machine/playbook.yaml" {
		t.Fatalf("init = %+v", init)
	}
	if !reflect.DeepEqual(f.runtime.calls, []string{"list", "init lclaw provider=libkrun", "start lclaw provider=libkrun"}) {
		t.Fatalf("runtime calls = %v", f.runtime.calls)
	}

	// Networks: the agent's is internal.
	wantNet := []string{"network exists lclaw-infra", "network create lclaw-infra internal=false", "network exists lclaw-services", "network create lclaw-services internal=false", "network exists lclaw-agent", "network create lclaw-agent internal=true"}
	if got := f.work.calls[:6]; !reflect.DeepEqual(got, wantNet) {
		t.Fatalf("network calls = %v, want %v", got, wantNet)
	}
	// Bridged pods join several networks; the agent pod gets --userns auto.
	for _, want := range []string{
		"play kuma-cp networks=[lclaw-infra lclaw-services lclaw-agent] userns=\"\"",
		"play agentgateway networks=[lclaw-services lclaw-agent] userns=\"\"",
		"play openclaw networks=[lclaw-agent] userns=\"auto\"",
		"build localhost/lclaw/litellm:latest " + dirArg + "/workloads/litellm",
	} {
		if !contains(f.work.calls, want) {
			t.Errorf("missing call %q in %v", want, f.work.calls)
		}
	}
	// Builds come before plays, zone by zone.
	if idx(f.work.calls, "play gateway networks=[lclaw-infra] userns=\"\"") > idx(f.work.calls, "build localhost/lclaw/litellm-db:latest "+dirArg+"/workloads/litellm-db") {
		t.Fatalf("services built before infra played: %v", f.work.calls)
	}

	// Secrets: generated values stored, the minted key requested after
	// services is ready, everything injected once.
	if f.minter.ready != 1 || !reflect.DeepEqual(f.minter.minted, []string{"openclaw-litellm-key"}) {
		t.Fatalf("minter: ready=%d minted=%v", f.minter.ready, f.minter.minted)
	}
	for _, name := range []string{"anthropic-api-key", "litellm-master-key", "litellm-salt-key", "litellm-db-password", "openclaw-gateway-token", "openclaw-litellm-key"} {
		if _, ok := f.target.stores[name]; !ok {
			t.Errorf("%s not injected; store = %v", name, f.target.stores)
		}
	}
	if string(f.kc.items["openclaw-litellm-key"].value) != "sk-minted" {
		t.Fatalf("minted key not stored in the keychain: %+v", f.kc.items)
	}
	if len(f.progress.steps) == 0 || !strings.HasPrefix(f.progress.steps[0], "secrets: ") {
		t.Fatalf("progress = %v", f.progress.steps)
	}
}

func TestUpSecondRunIsQuiet(t *testing.T) {
	f := newLifecycle(servicesTopology())
	if _, err := f.lc.Up(context.Background(), dirArg, domain.Roles()); err != nil {
		t.Fatal(err)
	}
	f.runtime.calls, f.work.calls, f.minter.minted = nil, nil, nil
	r, err := f.lc.Up(context.Background(), dirArg, domain.Roles())
	if err != nil {
		t.Fatal(err)
	}
	if r.Worst() != domain.Pass || r.Checks[0].Summary != "running" {
		t.Fatalf("second run:\n%+v", r.Checks)
	}
	if !reflect.DeepEqual(f.runtime.calls, []string{"list"}) {
		t.Fatalf("runtime calls = %v, want only list", f.runtime.calls)
	}
	for _, c := range f.work.calls {
		if strings.HasPrefix(c, "network create") {
			t.Fatalf("network created twice: %v", f.work.calls)
		}
	}
	if len(f.minter.minted) != 0 {
		t.Fatalf("minted again: %v", f.minter.minted)
	}
	if got := statuses(r)["network/infra"]; got != domain.Pass {
		t.Fatalf("network/infra = %v", got)
	}
}

func TestUpMissingUserSecretStopsBeforePodman(t *testing.T) {
	f := newLifecycle(servicesTopology())
	f.kc.items = nil
	r, err := f.lc.Up(context.Background(), dirArg, domain.Roles())
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.Check{{Name: "anthropic-api-key", Status: domain.Fail, Summary: "unset", Hint: "run `lclaw secrets set anthropic-api-key`"}}
	if !reflect.DeepEqual(r.Checks, want) {
		t.Fatalf("checks = %+v, want %+v", r.Checks, want)
	}
	if len(f.runtime.calls) != 0 || len(f.work.calls) != 0 {
		t.Fatalf("podman was touched: runtime %v, workloads %v", f.runtime.calls, f.work.calls)
	}
	for _, c := range f.kc.calls {
		if strings.HasPrefix(c, "get ") {
			t.Fatalf("a value was read: %v", f.kc.calls)
		}
	}
}

func TestUpKeychainMissingIsAnError(t *testing.T) {
	f := newLifecycle(servicesTopology())
	f.kc.exists = false
	_, err := f.lc.Up(context.Background(), dirArg, domain.Roles())
	if !errors.Is(err, ErrKeychainMissing) {
		t.Fatalf("err = %v, want ErrKeychainMissing", err)
	}
}

func TestUpInvalidTopologyIsFindings(t *testing.T) {
	topo := servicesTopology()
	topo.Machine.CPUs = 0
	f := newLifecycle(topo)
	_, err := f.lc.Up(context.Background(), dirArg, domain.Roles())
	var fe *FindingsError
	if !errors.As(err, &fe) || fe.Findings[0].Where != "machine.cpus" {
		t.Fatalf("err = %v, want findings naming machine.cpus", err)
	}
}

func TestUpAgentAloneNeedsServices(t *testing.T) {
	f := newLifecycle(servicesTopology())
	f.minter = &fakeMinter{err: ErrMinterUnavailable}
	f.lc.Minter = f.minter
	r, err := f.lc.Up(context.Background(), dirArg, []domain.Role{domain.Agent})
	if err != nil {
		t.Fatal(err)
	}
	st := statuses(r)
	if st["machine"] != domain.Pass || st["network/agent"] != domain.Pass || st["openclaw-litellm-key"] != domain.Fail {
		t.Fatalf("checks = %+v", r.Checks)
	}
	if _, played := st["agent/openclaw"]; played {
		t.Fatalf("the agent pod must not play without its key: %+v", r.Checks)
	}
	if _, ok := st["network/infra"]; ok {
		t.Fatalf("only the agent zone was selected: %+v", r.Checks)
	}
}

func TestUpBuildFailureStopsTheZoneAndSkipsTheRest(t *testing.T) {
	f := newLifecycle(servicesTopology())
	f.work.buildErr["localhost/lclaw/litellm:latest"] = errors.New("podman: build localhost/lclaw/litellm:latest: exit status 1: step 1/1 failed")
	r, err := f.lc.Up(context.Background(), dirArg, domain.Roles())
	if err != nil {
		t.Fatal(err)
	}
	st := statuses(r)
	if st["services/litellm-db"] != domain.Pass || st["services/litellm"] != domain.Fail {
		t.Fatalf("checks = %+v", r.Checks)
	}
	if _, ok := st["services/agentgateway"]; ok {
		t.Fatalf("the workload after the failure ran: %+v", r.Checks)
	}
	last := r.Checks[len(r.Checks)-1]
	if last.Name != "agent" || last.Status != domain.Warn || !strings.Contains(last.Summary, "skipped because services/litellm failed") {
		t.Fatalf("last check = %+v", last)
	}
	if r.Worst() != domain.Fail {
		t.Fatal("a failed build must fail the report")
	}
	if len(f.minter.minted) != 0 {
		t.Fatal("nothing should be minted after a failure")
	}
}

func TestUpMachineStartFailure(t *testing.T) {
	f := newLifecycle(servicesTopology())
	f.runtime.startErr = errors.New("podman: start machine lclaw: exit status 125: boom")
	r, err := f.lc.Up(context.Background(), dirArg, domain.Roles())
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Checks) != 1 || r.Checks[0].Name != "machine" || r.Checks[0].Status != domain.Fail {
		t.Fatalf("checks = %+v", r.Checks)
	}
	if len(f.work.calls) != 0 {
		t.Fatalf("workloads touched after a failed start: %v", f.work.calls)
	}
}

func TestUpStartsAStoppedMachine(t *testing.T) {
	f := newLifecycle(servicesTopology())
	f.runtime.machines = []domain.Machine{{Name: "lclaw"}}
	r, err := f.lc.Up(context.Background(), dirArg, []domain.Role{domain.Infra})
	if err != nil {
		t.Fatal(err)
	}
	if r.Checks[0].Summary != "started" || len(f.runtime.inits) != 0 {
		t.Fatalf("machine check = %+v, inits = %v", r.Checks[0], f.runtime.inits)
	}
}

func TestUpReadinessTimeoutFailsServices(t *testing.T) {
	f := newLifecycle(servicesTopology())
	f.minter.readyErr = errors.New("litellm: not ready after 2m0s: connection refused")
	r, err := f.lc.Up(context.Background(), dirArg, domain.Roles())
	if err != nil {
		t.Fatal(err)
	}
	st := statuses(r)
	if st["services/ready"] != domain.Fail || st["agent"] != domain.Warn {
		t.Fatalf("checks = %+v", r.Checks)
	}
}

func TestUpCancelledDuringBuild(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f := newLifecycle(servicesTopology())
	f.work.cancelOn, f.work.cancelFunc = "build localhost/lclaw/kuma-cp:latest", cancel
	_, err := f.lc.Up(ctx, dirArg, domain.Roles())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	for _, c := range f.work.calls {
		if strings.HasPrefix(c, "play gateway") {
			t.Fatalf("work continued after cancellation: %v", f.work.calls)
		}
	}
}

// upAll brings everything up and clears the recorded calls.
func upAll(t *testing.T, f *lifecycleFixture) {
	t.Helper()
	if r, err := f.lc.Up(context.Background(), dirArg, domain.Roles()); err != nil || r.Worst() != domain.Pass {
		t.Fatalf("up: %v %+v", err, r.Checks)
	}
	f.runtime.calls, f.work.calls, f.target.calls = nil, nil, nil
}

func TestDownAllStopsEverything(t *testing.T) {
	f := newLifecycle(servicesTopology())
	upAll(t, f)
	r, err := f.lc.Down(context.Background(), dirArg, domain.Roles(), false)
	if err != nil {
		t.Fatal(err)
	}
	if r.Worst() != domain.Pass {
		t.Fatalf("report has problems:\n%+v", r.Checks)
	}
	want := []string{
		"agent/openclaw", "network/agent",
		"services/agentgateway", "services/litellm", "services/litellm-db", "network/services",
		"infra/gateway", "infra/kuma-cp", "infra/wireguard", "network/infra",
		"secrets", "machine",
	}
	if got := names(r); !reflect.DeepEqual(got, want) {
		t.Fatalf("check names = %v, want %v", got, want)
	}
	if len(f.target.stores) != 0 {
		t.Fatalf("secrets left in the store: %v", f.target.stores)
	}
	if !reflect.DeepEqual(f.runtime.calls, []string{"list", "stop lclaw provider=libkrun"}) {
		t.Fatalf("runtime calls = %v", f.runtime.calls)
	}
	if len(f.work.networks) != 0 || len(f.work.pods) != 0 {
		t.Fatalf("networks %v pods %v remain", f.work.networks, f.work.pods)
	}
	// Order within a zone is reversed.
	if idx(f.work.calls, "down agentgateway") > idx(f.work.calls, "down litellm-db") {
		t.Fatalf("workloads not torn down in reverse: %v", f.work.calls)
	}
}

func TestDownOneZoneKeepsTheMachineAndTheStore(t *testing.T) {
	f := newLifecycle(servicesTopology())
	upAll(t, f)
	r, err := f.lc.Down(context.Background(), dirArg, []domain.Role{domain.Agent}, false)
	if err != nil {
		t.Fatal(err)
	}
	st := statuses(r)
	if st["agent/openclaw"] != domain.Pass || st["network/agent"] != domain.Pass || st["secrets"] != domain.Warn || st["machine"] != domain.Warn {
		t.Fatalf("checks = %+v", r.Checks)
	}
	if len(f.target.stores) == 0 {
		t.Fatal("the store was purged with pods still running")
	}
	if contains(f.runtime.calls, "stop lclaw provider=libkrun") {
		t.Fatalf("the machine was stopped: %v", f.runtime.calls)
	}
	if _, ok := f.work.pods["litellm"]; !ok {
		t.Fatal("a services pod was torn down")
	}
}

func TestDownContinuesPastAFailure(t *testing.T) {
	f := newLifecycle(servicesTopology())
	upAll(t, f)
	f.work.downErr["litellm"] = errors.New("podman: kube down: exit status 125: stuck")
	r, err := f.lc.Down(context.Background(), dirArg, domain.Roles(), false)
	if err != nil {
		t.Fatal(err)
	}
	st := statuses(r)
	if st["services/litellm"] != domain.Fail || st["services/litellm-db"] != domain.Pass || st["infra/wireguard"] != domain.Pass {
		t.Fatalf("checks = %+v", r.Checks)
	}
	if st["network/services"] != domain.Warn {
		t.Fatalf("the services network must be kept while a pod remains: %+v", r.Checks)
	}
	if st["machine"] != domain.Warn || contains(f.runtime.calls, "stop lclaw provider=libkrun") {
		t.Fatalf("the machine must stay up with a pod remaining: %+v %v", r.Checks, f.runtime.calls)
	}
}

func TestDownWhenNothingIsUp(t *testing.T) {
	f := newLifecycle(servicesTopology())
	r, err := f.lc.Down(context.Background(), dirArg, domain.Roles(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Checks) != 1 || r.Checks[0].Status != domain.Warn || r.Checks[0].Summary != "not created" {
		t.Fatalf("checks = %+v", r.Checks)
	}
	f.runtime.machines = []domain.Machine{{Name: "lclaw"}}
	r, err = f.lc.Down(context.Background(), dirArg, domain.Roles(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Checks) != 1 || r.Checks[0].Summary != "stopped" {
		t.Fatalf("checks = %+v", r.Checks)
	}
}

func TestDownDestroyRemovesTheMachineAndForgetsTheKey(t *testing.T) {
	f := newLifecycle(servicesTopology())
	upAll(t, f)
	r, err := f.lc.Down(context.Background(), dirArg, domain.Roles(), true)
	if err != nil {
		t.Fatal(err)
	}
	if r.Worst() != domain.Pass {
		t.Fatalf("report has problems:\n%+v", r.Checks)
	}
	if !contains(f.runtime.calls, "rm lclaw provider=libkrun") || len(f.runtime.machines) != 0 {
		t.Fatalf("runtime calls = %v, machines = %v", f.runtime.calls, f.runtime.machines)
	}
	if _, ok := f.kc.items["openclaw-litellm-key"]; ok {
		t.Fatal("the minted key must be forgotten")
	}
	if _, ok := f.kc.items["litellm-master-key"]; !ok {
		t.Fatal("generated secrets must survive destroy")
	}
	st := statuses(r)
	if st["destroy"] != domain.Pass || st["secrets/openclaw-litellm-key"] != domain.Pass {
		t.Fatalf("checks = %+v", r.Checks)
	}
}

func TestDownDestroyOfAStoppedMachine(t *testing.T) {
	f := newLifecycle(servicesTopology())
	f.runtime.machines = []domain.Machine{{Name: "lclaw"}}
	r, err := f.lc.Down(context.Background(), dirArg, domain.Roles(), true)
	if err != nil {
		t.Fatal(err)
	}
	if statuses(r)["destroy"] != domain.Pass || len(f.runtime.machines) != 0 {
		t.Fatalf("checks = %+v, machines = %v", r.Checks, f.runtime.machines)
	}
}

func TestDownDestroyRefusesAZone(t *testing.T) {
	f := newLifecycle(servicesTopology())
	_, err := f.lc.Down(context.Background(), dirArg, []domain.Role{domain.Agent}, true)
	if err == nil || !strings.Contains(err.Error(), "--destroy") {
		t.Fatalf("err = %v", err)
	}
}

func TestStatusReportsEverything(t *testing.T) {
	f := newLifecycle(servicesTopology())
	upAll(t, f)
	f.work.pods["litellm"] = domain.Pod{Name: "litellm", Containers: []domain.Container{{Name: "litellm-litellm", State: "exited"}}}
	delete(f.work.pods, "openclaw")
	r, err := f.lc.Status(context.Background(), dirArg)
	if err != nil {
		t.Fatal(err)
	}
	st := statuses(r)
	if st["machine"] != domain.Pass || st["network/agent"] != domain.Pass || st["infra/kuma-cp"] != domain.Pass {
		t.Fatalf("checks = %+v", r.Checks)
	}
	if st["services/litellm"] != domain.Fail || st["agent/openclaw"] != domain.Warn {
		t.Fatalf("checks = %+v", r.Checks)
	}
	if r.Worst() != domain.Fail {
		t.Fatal("a degraded pod must fail the report")
	}
	if len(f.runtime.calls) != 1 || len(f.target.calls) != 0 {
		t.Fatalf("status must only read: runtime %v target %v", f.runtime.calls, f.target.calls)
	}
}

func TestStatusStoppedMachine(t *testing.T) {
	f := newLifecycle(servicesTopology())
	f.runtime.machines = []domain.Machine{{Name: "lclaw"}}
	r, err := f.lc.Status(context.Background(), dirArg)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Checks) != 1 || r.Checks[0].Summary != "stopped" || r.Worst() != domain.Pass {
		t.Fatalf("checks = %+v", r.Checks)
	}
}

func idx(calls []string, want string) int {
	for i, c := range calls {
		if c == want {
			return i
		}
	}
	return -1
}
