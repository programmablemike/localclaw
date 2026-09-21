package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sort"
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

type fakeTarget struct {
	running    map[domain.Role]bool
	runningErr map[domain.Role]error
	stores     map[domain.Role]map[string][]byte
	volumes    map[domain.Role]map[string]bool
	storeErr   map[domain.Role]error
	listErr    error
	calls      []string
}

func newFakeTarget() *fakeTarget {
	return &fakeTarget{
		running:    map[domain.Role]bool{},
		runningErr: map[domain.Role]error{},
		stores:     map[domain.Role]map[string][]byte{},
		volumes:    map[domain.Role]map[string]bool{},
		storeErr:   map[domain.Role]error{},
	}
}

func (t *fakeTarget) store(role domain.Role) map[string][]byte {
	if t.stores[role] == nil {
		t.stores[role] = map[string][]byte{}
	}
	return t.stores[role]
}

func (t *fakeTarget) MachineRunning(ctx context.Context, role domain.Role) (bool, error) {
	t.calls = append(t.calls, "running "+role.String())
	if err := t.runningErr[role]; err != nil {
		return false, err
	}
	return t.running[role], nil
}

func (t *fakeTarget) StoreSecret(ctx context.Context, role domain.Role, name string, value []byte) error {
	t.calls = append(t.calls, "store "+role.String()+" "+name)
	if err := t.storeErr[role]; err != nil {
		return err
	}
	t.store(role)[name] = append([]byte(nil), value...)
	return nil
}

func (t *fakeTarget) SecretExists(ctx context.Context, role domain.Role, name string) (bool, error) {
	t.calls = append(t.calls, "exists "+role.String()+" "+name)
	_, ok := t.store(role)[name]
	return ok, nil
}

func (t *fakeTarget) RemoveSecret(ctx context.Context, role domain.Role, name string) error {
	t.calls = append(t.calls, "remove "+role.String()+" "+name)
	if _, ok := t.store(role)[name]; !ok {
		return errors.New("podman: remove secret: no such secret")
	}
	delete(t.store(role), name)
	return nil
}

func (t *fakeTarget) ListSecrets(ctx context.Context, role domain.Role) ([]string, error) {
	t.calls = append(t.calls, "list "+role.String())
	if t.listErr != nil {
		return nil, t.listErr
	}
	names := make([]string, 0, len(t.store(role)))
	for n := range t.store(role) {
		names = append(names, n)
	}
	sort.Strings(names)
	return names, nil
}

func (t *fakeTarget) RemoveVolume(ctx context.Context, role domain.Role, name string) error {
	t.calls = append(t.calls, "rmvolume "+role.String()+" "+name)
	delete(t.volumes[role], name)
	return nil
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
	value  []byte
	err    error
	minted []string
}

func (m *fakeMinter) Mint(ctx context.Context, name string) ([]byte, error) {
	m.minted = append(m.minted, name)
	return m.value, m.err
}

func (m *fakeMinter) Revoke(ctx context.Context, name string) error { return nil }

// servicesTopology declares one provider key on the services machine. Every
// machine carries the minimum CPUs, memory and disk domain.Validate
// requires, so a topology built from it passes validation cleanly and
// doctor's keychain and secret checks run rather than reporting findings.
func servicesTopology() domain.Topology {
	return domain.Topology{
		Schema:       1,
		Provider:     "libkrun",
		KeychainPath: kcPath,
		Machines: []domain.MachineSpec{
			{Name: "infra", CPUs: 1, MemoryMiB: 1024, DiskGiB: 10},
			{Name: "services", CPUs: 1, MemoryMiB: 1024, DiskGiB: 10, Secrets: []string{"anthropic-api-key"}},
			{Name: "agent", CPUs: 1, MemoryMiB: 1024, DiskGiB: 10},
		},
	}
}

// sharedTopology declares one key on both services and agent.
func sharedTopology() domain.Topology {
	return domain.Topology{
		Schema:       1,
		Provider:     "libkrun",
		KeychainPath: kcPath,
		Machines: []domain.MachineSpec{
			{Name: "services", Secrets: []string{"shared-token"}},
			{Name: "agent", Secrets: []string{"shared-token"}},
		},
	}
}
