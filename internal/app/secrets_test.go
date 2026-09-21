package app

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/programmablemike/localclaw/internal/domain"
)

func newSecrets(kc *fakeKeychain, tg *fakeTarget, topo domain.Topology) *Secrets {
	return &Secrets{
		Scaffold: &fakeScaffold{},
		Topology: &fakeLoader{topo: topo},
		Keychain: kc,
		Target:   tg,
		Minter:   &fakeMinter{err: ErrMinterUnavailable},
		Random:   &seqReader{},
	}
}

func TestSecretsListStatesAndEffectiveSource(t *testing.T) {
	kc := &fakeKeychain{exists: true, items: map[string]fakeItem{
		"anthropic-api-key":  {value: []byte("k"), source: domain.User},
		"litellm-master-key": {value: []byte("sk-x"), source: domain.User}, // overridden by hand
		"litellm-salt-key":   {value: []byte("s"), source: domain.Generated},
	}}
	got, err := newSecrets(kc, newFakeTarget(), servicesTopology()).List(context.Background(), "/d")
	if err != nil {
		t.Fatal(err)
	}
	type row struct {
		name   string
		source domain.Source
		set    bool
	}
	var rows []row
	for _, s := range got {
		rows = append(rows, row{s.Spec.Name, s.Source, s.Set})
	}
	want := []row{
		{"anthropic-api-key", domain.User, true},
		{"litellm-db-password", domain.Generated, false},
		{"litellm-master-key", domain.User, true},
		{"litellm-salt-key", domain.Generated, true},
		{"openclaw-gateway-token", domain.Generated, false},
		{"openclaw-litellm-key", domain.Minted, false},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("List() =\n%+v\nwant\n%+v", rows, want)
	}
}

func TestSecretsListKeychainMissing(t *testing.T) {
	kc := &fakeKeychain{exists: false}
	_, err := newSecrets(kc, newFakeTarget(), servicesTopology()).List(context.Background(), "/d")
	if !errors.Is(err, ErrKeychainMissing) {
		t.Fatalf("err = %v, want ErrKeychainMissing", err)
	}
	if !reflect.DeepEqual(kc.calls, []string{"exists " + kcPath}) {
		t.Fatalf("calls = %v, want only the existence check", kc.calls)
	}
}

func TestSecretsListReportsFindings(t *testing.T) {
	topo := domain.Topology{KeychainPath: kcPath, Machines: []domain.MachineSpec{{Name: "services", Secrets: []string{"Bad"}}}}
	_, err := newSecrets(&fakeKeychain{exists: true}, newFakeTarget(), topo).List(context.Background(), "/d")
	var fe *FindingsError
	if !errors.As(err, &fe) || len(fe.Findings) != 1 || fe.Findings[0].Where != "machines.services.secrets" {
		t.Fatalf("err = %v, want FindingsError naming the services secrets table", err)
	}
}

func TestSecretsListTopologyError(t *testing.T) {
	s := newSecrets(&fakeKeychain{exists: true}, newFakeTarget(), servicesTopology())
	s.Topology = &fakeLoader{err: errors.New("lclaw.toml: line 3: bad")}
	_, err := s.List(context.Background(), "/d")
	if err == nil || err.Error() != "secrets: lclaw.toml: line 3: bad" {
		t.Fatalf("err = %v", err)
	}
}

func TestSecretsSetStoresAndPushes(t *testing.T) {
	kc := &fakeKeychain{exists: true}
	tg := newFakeTarget()
	tg.running[domain.Services] = true
	o, err := newSecrets(kc, tg, servicesTopology()).Set(context.Background(), "/d", "anthropic-api-key", []byte("sk-ant"))
	if err != nil {
		t.Fatal(err)
	}
	if got := kc.items["anthropic-api-key"]; string(got.value) != "sk-ant" || got.source != domain.User {
		t.Fatalf("keychain item = %+v", got)
	}
	if !reflect.DeepEqual(kc.calls, []string{"exists " + kcPath, "put anthropic-api-key source=user replace=false"}) {
		t.Fatalf("keychain calls = %v", kc.calls)
	}
	if string(tg.stores[domain.Services]["anthropic-api-key"]) != "sk-ant" {
		t.Fatalf("store = %v", tg.stores)
	}
	want := Outcome{Name: "anthropic-api-key", Stores: []StoreOutcome{{Role: domain.Services, State: StoreStored}}}
	if !reflect.DeepEqual(o, want) {
		t.Fatalf("Outcome = %+v, want %+v", o, want)
	}
}

func TestSecretsSetSkipsStoppedMachine(t *testing.T) {
	tg := newFakeTarget()
	o, err := newSecrets(&fakeKeychain{exists: true}, tg, servicesTopology()).Set(context.Background(), "/d", "anthropic-api-key", []byte("v"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(o.Stores, []StoreOutcome{{Role: domain.Services, State: StoreStopped}}) {
		t.Fatalf("Stores = %+v", o.Stores)
	}
	if !reflect.DeepEqual(tg.calls, []string{"running services"}) {
		t.Fatalf("target calls = %v, want no store call", tg.calls)
	}
}

func TestSecretsSetRefusesExisting(t *testing.T) {
	kc := &fakeKeychain{exists: true, items: map[string]fakeItem{"anthropic-api-key": {value: []byte("old"), source: domain.User}}}
	tg := newFakeTarget()
	_, err := newSecrets(kc, tg, servicesTopology()).Set(context.Background(), "/d", "anthropic-api-key", []byte("new"))
	if !errors.Is(err, ErrSecretExists) {
		t.Fatalf("err = %v, want ErrSecretExists", err)
	}
	if len(tg.calls) != 0 {
		t.Fatalf("target must not be touched: %v", tg.calls)
	}
	if string(kc.items["anthropic-api-key"].value) != "old" {
		t.Fatal("existing value was replaced")
	}
}

func TestSecretsSetUnknownName(t *testing.T) {
	kc := &fakeKeychain{exists: true}
	_, err := newSecrets(kc, newFakeTarget(), servicesTopology()).Set(context.Background(), "/d", "openai-api-key", []byte("v"))
	if !errors.Is(err, ErrUnknownSecret) {
		t.Fatalf("err = %v, want ErrUnknownSecret", err)
	}
	if len(kc.calls) != 0 {
		t.Fatalf("keychain must not be touched: %v", kc.calls)
	}
}

func TestSecretsSetInvalidValue(t *testing.T) {
	s := newSecrets(&fakeKeychain{exists: true}, newFakeTarget(), servicesTopology())
	for _, v := range [][]byte{nil, bytes.Repeat([]byte("x"), domain.MaxSecretLen+1)} {
		if _, err := s.Set(context.Background(), "/d", "anthropic-api-key", v); !errors.Is(err, domain.ErrInvalidValue) {
			t.Fatalf("%d bytes: err = %v, want ErrInvalidValue", len(v), err)
		}
	}
}

func TestSecretsUpdateReplacesAndPushes(t *testing.T) {
	kc := &fakeKeychain{exists: true, items: map[string]fakeItem{"anthropic-api-key": {value: []byte("old"), source: domain.User}}}
	tg := newFakeTarget()
	tg.running[domain.Services] = true
	o, err := newSecrets(kc, tg, servicesTopology()).Update(context.Background(), "/d", "anthropic-api-key", []byte("new"), false)
	if err != nil {
		t.Fatal(err)
	}
	if string(kc.items["anthropic-api-key"].value) != "new" {
		t.Fatalf("item = %+v", kc.items["anthropic-api-key"])
	}
	if !reflect.DeepEqual(kc.calls, []string{"exists " + kcPath, "describe anthropic-api-key", "put anthropic-api-key source=user replace=true"}) {
		t.Fatalf("keychain calls = %v", kc.calls)
	}
	if !reflect.DeepEqual(o.Stores, []StoreOutcome{{Role: domain.Services, State: StoreStored}}) {
		t.Fatalf("Stores = %+v", o.Stores)
	}
}

func TestSecretsUpdateNotSet(t *testing.T) {
	_, err := newSecrets(&fakeKeychain{exists: true}, newFakeTarget(), servicesTopology()).Update(context.Background(), "/d", "anthropic-api-key", []byte("v"), false)
	if !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("err = %v, want ErrSecretNotFound", err)
	}
}

func TestSecretsUpdateGenerateRestoresDefaultSource(t *testing.T) {
	kc := &fakeKeychain{exists: true, items: map[string]fakeItem{"litellm-master-key": {value: []byte("sk-byhand"), source: domain.User}}}
	tg := newFakeTarget()
	tg.running[domain.Services] = true
	o, err := newSecrets(kc, tg, servicesTopology()).Update(context.Background(), "/d", "litellm-master-key", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := kc.items["litellm-master-key"]; string(got.value) != masterKeyFromSeq || got.source != domain.Generated {
		t.Fatalf("item = %+v", got)
	}
	if string(tg.stores[domain.Services]["litellm-master-key"]) != masterKeyFromSeq {
		t.Fatalf("store = %q", tg.stores[domain.Services]["litellm-master-key"])
	}
	if o.Failed() {
		t.Fatalf("Outcome = %+v", o)
	}
}

func TestSecretsUpdateGenerateRefused(t *testing.T) {
	kc := &fakeKeychain{exists: true, items: map[string]fakeItem{
		"anthropic-api-key": {value: []byte("k"), source: domain.User},
		"litellm-salt-key":  {value: []byte("s"), source: domain.Generated},
	}}
	s := newSecrets(kc, newFakeTarget(), servicesTopology())
	for _, name := range []string{"anthropic-api-key", "litellm-salt-key"} {
		if _, err := s.Update(context.Background(), "/d", name, nil, true); !errors.Is(err, ErrNotGeneratable) {
			t.Fatalf("%s: err = %v, want ErrNotGeneratable", name, err)
		}
	}
	for _, c := range kc.calls {
		if c[:3] == "put" {
			t.Fatalf("keychain written despite refusal: %v", kc.calls)
		}
	}
}

func TestSecretsUpdateGenerateMinted(t *testing.T) {
	kc := &fakeKeychain{exists: true, items: map[string]fakeItem{"openclaw-litellm-key": {value: []byte("old"), source: domain.User}}}
	s := newSecrets(kc, newFakeTarget(), servicesTopology())
	minter := &fakeMinter{value: []byte("sk-minted")}
	s.Minter = minter
	if _, err := s.Update(context.Background(), "/d", "openclaw-litellm-key", nil, true); err != nil {
		t.Fatal(err)
	}
	if got := kc.items["openclaw-litellm-key"]; string(got.value) != "sk-minted" || got.source != domain.Minted {
		t.Fatalf("item = %+v", got)
	}
	if !reflect.DeepEqual(minter.minted, []string{"openclaw-litellm-key"}) {
		t.Fatalf("minted = %v", minter.minted)
	}
	s.Minter = &fakeMinter{err: ErrMinterUnavailable}
	if _, err := s.Update(context.Background(), "/d", "openclaw-litellm-key", nil, true); !errors.Is(err, ErrMinterUnavailable) {
		t.Fatalf("err = %v, want ErrMinterUnavailable", err)
	}
}

func TestSecretsUpdateOneStoreFails(t *testing.T) {
	kc := &fakeKeychain{exists: true, items: map[string]fakeItem{"shared-token": {value: []byte("old"), source: domain.User}}}
	tg := newFakeTarget()
	tg.running[domain.Services] = true
	tg.running[domain.Agent] = true
	tg.storeErr[domain.Agent] = errors.New("podman: store secret: exit status 125")
	o, err := newSecrets(kc, tg, sharedTopology()).Update(context.Background(), "/d", "shared-token", []byte("new"), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(o.Stores) != 2 || o.Stores[0].State != StoreStored || o.Stores[1].State != StoreFailed || o.Stores[1].Err == nil {
		t.Fatalf("Stores = %+v", o.Stores)
	}
	if !o.Failed() {
		t.Fatal("Failed() should be true")
	}
	if string(kc.items["shared-token"].value) != "new" {
		t.Fatal("keychain is the source of truth and must hold the new value")
	}
}

func TestSecretsDeleteRemovesEverywhere(t *testing.T) {
	kc := &fakeKeychain{exists: true, items: map[string]fakeItem{"shared-token": {value: []byte("v"), source: domain.User}}}
	tg := newFakeTarget()
	tg.running[domain.Services] = true
	tg.store(domain.Services)["shared-token"] = []byte("v")
	o, err := newSecrets(kc, tg, sharedTopology()).Delete(context.Background(), "/d", "shared-token")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := kc.items["shared-token"]; ok {
		t.Fatal("keychain item still present")
	}
	if _, ok := tg.stores[domain.Services]["shared-token"]; ok {
		t.Fatal("store still holds the secret")
	}
	want := Outcome{Name: "shared-token", Stores: []StoreOutcome{{Role: domain.Services, State: StoreRemoved}, {Role: domain.Agent, State: StoreStopped}}}
	if !reflect.DeepEqual(o, want) {
		t.Fatalf("Outcome = %+v, want %+v", o, want)
	}
}

func TestSecretsDeleteAbsentFromStore(t *testing.T) {
	kc := &fakeKeychain{exists: true, items: map[string]fakeItem{"anthropic-api-key": {value: []byte("v"), source: domain.User}}}
	tg := newFakeTarget()
	tg.running[domain.Services] = true
	o, err := newSecrets(kc, tg, servicesTopology()).Delete(context.Background(), "/d", "anthropic-api-key")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(o.Stores, []StoreOutcome{{Role: domain.Services, State: StoreAbsent}}) {
		t.Fatalf("Stores = %+v", o.Stores)
	}
}

func TestSecretsDeleteNotSet(t *testing.T) {
	_, err := newSecrets(&fakeKeychain{exists: true}, newFakeTarget(), servicesTopology()).Delete(context.Background(), "/d", "anthropic-api-key")
	if !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("err = %v, want ErrSecretNotFound", err)
	}
}

func TestSecretsDeleteNonRotatableWarns(t *testing.T) {
	kc := &fakeKeychain{exists: true, items: map[string]fakeItem{"litellm-salt-key": {value: []byte("s"), source: domain.Generated}}}
	o, err := newSecrets(kc, newFakeTarget(), servicesTopology()).Delete(context.Background(), "/d", "litellm-salt-key")
	if err != nil {
		t.Fatal(err)
	}
	if o.Warning == "" {
		t.Fatal("deleting the salt key must warn")
	}
}

func TestSecretsDescribe(t *testing.T) {
	kc := &fakeKeychain{exists: true, items: map[string]fakeItem{"shared-token": {value: []byte("v"), source: domain.User}}}
	tg := newFakeTarget()
	tg.running[domain.Services] = true
	tg.store(domain.Services)["shared-token"] = []byte("v")
	d, err := newSecrets(kc, tg, sharedTopology()).Describe(context.Background(), "/d", "shared-token")
	if err != nil {
		t.Fatal(err)
	}
	if !d.Set || d.Source != domain.User || d.Created != t0 || d.Modified != t0.Add(time.Minute) {
		t.Fatalf("detail = %+v", d)
	}
	want := []StoreOutcome{{Role: domain.Services, State: StoreStored}, {Role: domain.Agent, State: StoreStopped}}
	if !reflect.DeepEqual(d.Stores, want) {
		t.Fatalf("Stores = %+v, want %+v", d.Stores, want)
	}
}

func TestSecretsDescribeUnset(t *testing.T) {
	tg := newFakeTarget()
	tg.running[domain.Services] = true
	d, err := newSecrets(&fakeKeychain{exists: true}, tg, servicesTopology()).Describe(context.Background(), "/d", "litellm-master-key")
	if err != nil {
		t.Fatal(err)
	}
	if d.Set || d.Source != domain.Generated || !d.Created.IsZero() {
		t.Fatalf("detail = %+v", d)
	}
	if !reflect.DeepEqual(d.Stores, []StoreOutcome{{Role: domain.Services, State: StoreAbsent}}) {
		t.Fatalf("Stores = %+v", d.Stores)
	}
}

func TestSecretsGet(t *testing.T) {
	kc := &fakeKeychain{exists: true, items: map[string]fakeItem{"litellm-master-key": {value: []byte{0, 0xff, '\n'}, source: domain.Generated}}}
	s := newSecrets(kc, newFakeTarget(), servicesTopology())
	got, err := s.Get(context.Background(), "/d", "litellm-master-key")
	if err != nil || !bytes.Equal(got, []byte{0, 0xff, '\n'}) {
		t.Fatalf("Get() = %q, %v", got, err)
	}
	if _, err := s.Get(context.Background(), "/d", "litellm-salt-key"); !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("err = %v, want ErrSecretNotFound", err)
	}
}

func TestSecretsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := newSecrets(&fakeKeychain{exists: true}, newFakeTarget(), servicesTopology()).List(ctx, "/d")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}
