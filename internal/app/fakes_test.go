package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"testing/fstest"
	"time"

	"github.com/programmablemike/localclaw/internal/domain"
)

var t0 = time.Date(2026, 9, 21, 7, 3, 45, 0, time.UTC)

const kcPath = "/kc/lclaw.keychain-db"

// seqReader yields 0, 1, 2 ... forever, so generated values are literal
// strings in expectations.
type seqReader struct{ next byte }

func (r *seqReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = r.next
		r.next++
	}
	return len(p), nil
}

const masterKeyFromSeq = "sk-AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8"

type fakeItem struct {
	value  []byte
	source domain.Source
}

type fakeKeychain struct {
	exists    bool
	existsErr error
	items     map[string]fakeItem
	putErr    error
	createErr error
	calls     []string
}

func (k *fakeKeychain) Exists(ctx context.Context, path string) (bool, error) {
	k.calls = append(k.calls, "exists "+path)
	return k.exists, k.existsErr
}

func (k *fakeKeychain) Create(ctx context.Context, path string) error {
	k.calls = append(k.calls, "create "+path)
	if k.createErr != nil {
		return k.createErr
	}
	k.exists = true
	return nil
}

func (k *fakeKeychain) Get(ctx context.Context, path, name string) ([]byte, error) {
	k.calls = append(k.calls, "get "+name)
	it, ok := k.items[name]
	if !ok {
		return nil, fmt.Errorf("keychain: get %s: %w", name, ErrSecretNotFound)
	}
	return append([]byte(nil), it.value...), nil
}

func (k *fakeKeychain) Describe(ctx context.Context, path, name string) (domain.SecretItem, error) {
	k.calls = append(k.calls, "describe "+name)
	it, ok := k.items[name]
	if !ok {
		return domain.SecretItem{}, fmt.Errorf("keychain: describe %s: %w", name, ErrSecretNotFound)
	}
	return domain.SecretItem{Name: name, Source: it.source, Created: t0, Modified: t0.Add(time.Minute)}, nil
}

func (k *fakeKeychain) Put(ctx context.Context, path, name string, value []byte, source domain.Source, replace bool) error {
	k.calls = append(k.calls, fmt.Sprintf("put %s source=%s replace=%v", name, source, replace))
	if k.putErr != nil {
		return k.putErr
	}
	if _, ok := k.items[name]; ok && !replace {
		return fmt.Errorf("keychain: put %s: %w", name, ErrSecretExists)
	}
	if k.items == nil {
		k.items = map[string]fakeItem{}
	}
	k.items[name] = fakeItem{value: append([]byte(nil), value...), source: source}
	return nil
}

func (k *fakeKeychain) Delete(ctx context.Context, path, name string) error {
	k.calls = append(k.calls, "delete "+name)
	if _, ok := k.items[name]; !ok {
		return fmt.Errorf("keychain: delete %s: %w", name, ErrSecretNotFound)
	}
	delete(k.items, name)
	return nil
}

// fakeTarget is the machine's one secret store.
type fakeTarget struct {
	running    bool
	runningErr error
	stores     map[string][]byte
	volumes    map[string]bool
	storeErr   error
	listErr    error
	calls      []string
}

func newFakeTarget() *fakeTarget {
	return &fakeTarget{stores: map[string][]byte{}, volumes: map[string]bool{}}
}

func (t *fakeTarget) MachineRunning(ctx context.Context) (bool, error) {
	t.calls = append(t.calls, "running")
	if t.runningErr != nil {
		return false, t.runningErr
	}
	return t.running, nil
}

func (t *fakeTarget) StoreSecret(ctx context.Context, name string, value []byte) error {
	t.calls = append(t.calls, "store "+name)
	if t.storeErr != nil {
		return t.storeErr
	}
	t.stores[name] = append([]byte(nil), value...)
	return nil
}

func (t *fakeTarget) SecretExists(ctx context.Context, name string) (bool, error) {
	t.calls = append(t.calls, "exists "+name)
	_, ok := t.stores[name]
	return ok, nil
}

func (t *fakeTarget) RemoveSecret(ctx context.Context, name string) error {
	t.calls = append(t.calls, "remove "+name)
	if _, ok := t.stores[name]; !ok {
		return errors.New("podman: remove secret: no such secret")
	}
	delete(t.stores, name)
	return nil
}

func (t *fakeTarget) ListSecrets(ctx context.Context) ([]string, error) {
	t.calls = append(t.calls, "list")
	if t.listErr != nil {
		return nil, t.listErr
	}
	names := make([]string, 0, len(t.stores))
	for n := range t.stores {
		names = append(names, n)
	}
	sort.Strings(names)
	return names, nil
}

func (t *fakeTarget) RemoveVolume(ctx context.Context, name string) error {
	t.calls = append(t.calls, "rmvolume "+name)
	delete(t.volumes, name)
	return nil
}

// fakeRuntime is the machine side of Podman. Machine operations mutate
// the list so a test can drive create-then-start.
type fakeRuntime struct {
	version  domain.Version
	verErr   error
	machines []domain.Machine
	listErr  error
	initErr  error
	startErr error
	stopErr  error
	rmErr    error
	inits    []MachineInit
	calls    []string
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
	f.calls = append(f.calls, "list")
	if f.cancelOn == "list" && f.cancel != nil {
		f.cancel()
	}
	return append([]domain.Machine(nil), f.machines...), f.listErr
}

func (f *fakeRuntime) InitMachine(ctx context.Context, m MachineInit) error {
	f.calls = append(f.calls, "init "+m.Name+" provider="+m.Provider)
	f.inits = append(f.inits, m)
	if f.initErr != nil {
		return f.initErr
	}
	f.machines = append(f.machines, domain.Machine{Name: m.Name})
	return nil
}

func (f *fakeRuntime) set(name string, running bool) {
	for i := range f.machines {
		if f.machines[i].Name == name {
			f.machines[i].Running = running
		}
	}
}

func (f *fakeRuntime) StartMachine(ctx context.Context, provider, name string) error {
	f.calls = append(f.calls, "start "+name+" provider="+provider)
	if f.startErr != nil {
		return f.startErr
	}
	f.set(name, true)
	return nil
}

func (f *fakeRuntime) StopMachine(ctx context.Context, provider, name string) error {
	f.calls = append(f.calls, "stop "+name+" provider="+provider)
	if f.stopErr != nil {
		return f.stopErr
	}
	f.set(name, false)
	return nil
}

func (f *fakeRuntime) RemoveMachine(ctx context.Context, provider, name string) error {
	f.calls = append(f.calls, "rm "+name+" provider="+provider)
	if f.rmErr != nil {
		return f.rmErr
	}
	var kept []domain.Machine
	for _, m := range f.machines {
		if m.Name != name {
			kept = append(kept, m)
		}
	}
	f.machines = kept
	return nil
}

// fakeWorkloads is the inside of the machine: networks, images and pods.
type fakeWorkloads struct {
	networks   map[string]bool
	pods       map[string]domain.Pod
	buildErr   map[string]error // by tag
	playErr    map[string]error // by pod name
	downErr    map[string]error // by pod name
	netErr     error
	createErr  error
	listErr    error
	calls      []string
	cancelOn   string // a call prefix at which to cancel
	cancelFunc context.CancelFunc
}

func newFakeWorkloads() *fakeWorkloads {
	return &fakeWorkloads{
		networks: map[string]bool{},
		pods:     map[string]domain.Pod{},
		buildErr: map[string]error{},
		playErr:  map[string]error{},
		downErr:  map[string]error{},
	}
}

func (w *fakeWorkloads) record(call string) {
	w.calls = append(w.calls, call)
	if w.cancelOn != "" && w.cancelFunc != nil && strings.HasPrefix(call, w.cancelOn) {
		w.cancelFunc()
	}
}

func (w *fakeWorkloads) NetworkExists(ctx context.Context, name string) (bool, error) {
	w.record("network exists " + name)
	if w.netErr != nil {
		return false, w.netErr
	}
	return w.networks[name], nil
}

func (w *fakeWorkloads) CreateNetwork(ctx context.Context, name string, internal bool) error {
	w.record(fmt.Sprintf("network create %s internal=%v", name, internal))
	if w.createErr != nil {
		return w.createErr
	}
	w.networks[name] = true
	return nil
}

func (w *fakeWorkloads) RemoveNetwork(ctx context.Context, name string) error {
	w.record("network rm " + name)
	delete(w.networks, name)
	return nil
}

func (w *fakeWorkloads) Build(ctx context.Context, tag, contextDir string) error {
	w.record("build " + tag + " " + contextDir)
	return w.buildErr[tag]
}

func (w *fakeWorkloads) Play(ctx context.Context, file string, networks []string, userns string) error {
	name := podName(file)
	w.record(fmt.Sprintf("play %s networks=%v userns=%q", name, networks, userns))
	if err := w.playErr[name]; err != nil {
		return err
	}
	w.pods[name] = domain.Pod{Name: name, Containers: []domain.Container{{Name: name + "-" + name, State: "running"}}}
	return nil
}

func (w *fakeWorkloads) Down(ctx context.Context, file string) error {
	name := podName(file)
	w.record("down " + name)
	if err := w.downErr[name]; err != nil {
		return err
	}
	delete(w.pods, name)
	return nil
}

func (w *fakeWorkloads) ListPods(ctx context.Context) ([]domain.Pod, error) {
	w.record("pod ps")
	if w.listErr != nil {
		return nil, w.listErr
	}
	names := make([]string, 0, len(w.pods))
	for n := range w.pods {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]domain.Pod, 0, len(names))
	for _, n := range names {
		out = append(out, w.pods[n])
	}
	return out, nil
}

// podName is the workload name from ".../workloads/<name>/pod.yaml".
func podName(file string) string {
	parts := strings.Split(strings.Trim(file, "/"), "/")
	if len(parts) < 2 {
		return file
	}
	return parts[len(parts)-2]
}

// fakeScaffold is a DirOpener over one in-memory directory. OpenDir returns
// fsys, or err when set, and records every requested dir in dirs; the
// doctor tests assert on dirs directly.
type fakeScaffold struct {
	fsys fs.FS
	err  error
	dirs []string
}

func (s *fakeScaffold) OpenDir(dir string) (fs.FS, error) {
	s.dirs = append(s.dirs, dir)
	if s.err != nil {
		return nil, s.err
	}
	if s.fsys == nil {
		return fstest.MapFS{}, nil
	}
	return s.fsys, nil
}

// fakeLoader returns a fixed topology, whatever the file system holds.
type fakeLoader struct {
	topo domain.Topology
	err  error
	n    int
}

func (l *fakeLoader) Load(fs.FS) (domain.Topology, error) {
	l.n++
	return l.topo, l.err
}

type fakeMinter struct {
	value    []byte
	err      error
	readyErr error
	minted   []string
	revoked  []string
	ready    int
}

func (m *fakeMinter) Ready(ctx context.Context) error {
	m.ready++
	return m.readyErr
}

func (m *fakeMinter) Mint(ctx context.Context, name string) ([]byte, error) {
	m.minted = append(m.minted, name)
	return m.value, m.err
}

func (m *fakeMinter) Revoke(ctx context.Context, name string, value []byte) error {
	m.revoked = append(m.revoked, name)
	return nil
}

// fakeProgress records every step.
type fakeProgress struct{ steps []string }

func (p *fakeProgress) Step(name, detail string) { p.steps = append(p.steps, name+": "+detail) }

// servicesTopology declares one provider key in the services zone. It is
// the embedded default, so validation passes and doctor's keychain and
// secret checks run rather than reporting findings.
func servicesTopology() domain.Topology {
	return domain.Topology{
		Schema:       2,
		Provider:     "libkrun",
		KeychainPath: kcPath,
		Machine:      domain.MachineSpec{CPUs: 4, MemoryMiB: 8192, DiskGiB: 60},
		Zones: []domain.ZoneSpec{
			{Name: "infra", Workloads: []domain.Workload{"kuma-cp", "gateway"}, Bridges: map[domain.Workload][]string{"kuma-cp": {"services", "agent"}}},
			{Name: "services", Workloads: []domain.Workload{"litellm-db", "litellm", "agentgateway"}, Secrets: []string{"anthropic-api-key"}, Bridges: map[domain.Workload][]string{"agentgateway": {"agent"}}},
			{Name: "agent", Internal: true, Workloads: []domain.Workload{"openclaw"}},
		},
	}
}

// sharedTopology declares one key in both services and agent.
func sharedTopology() domain.Topology {
	top := servicesTopology()
	top.Zones[1].Secrets = []string{"shared-token"}
	top.Zones[2].Secrets = []string{"shared-token"}
	return top
}
