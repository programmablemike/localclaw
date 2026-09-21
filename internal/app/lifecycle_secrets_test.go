package app

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/programmablemike/localclaw/internal/domain"
)

func servicesCatalogue(t *testing.T, topo domain.Topology) domain.Catalogue {
	t.Helper()
	cat, findings := domain.NewCatalogue(domain.BuiltinSecrets(), topo.DeclaredSecrets())
	if len(findings) != 0 {
		t.Fatalf("findings: %+v", findings)
	}
	return cat
}

func resolvedNames(rs []ResolvedSecret) []string {
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.Name)
	}
	return out
}

func TestResolveEverythingPresent(t *testing.T) {
	kc := &fakeKeychain{exists: true, items: map[string]fakeItem{
		"anthropic-api-key":   {value: []byte("a"), source: domain.User},
		"litellm-db-password": {value: []byte("d"), source: domain.Generated},
		"litellm-master-key":  {value: []byte("m"), source: domain.Generated},
		"litellm-salt-key":    {value: []byte("s"), source: domain.Generated},
	}}
	r := &ResolveSecrets{Keychain: kc, Minter: &fakeMinter{err: ErrMinterUnavailable}, Random: &seqReader{}}
	got, findings, err := r.Run(context.Background(), kcPath, servicesCatalogue(t, servicesTopology()), domain.Services)
	if err != nil || len(findings) != 0 {
		t.Fatalf("err=%v findings=%+v", err, findings)
	}
	want := []ResolvedSecret{{"anthropic-api-key", []byte("a")}, {"litellm-db-password", []byte("d")}, {"litellm-master-key", []byte("m")}, {"litellm-salt-key", []byte("s")}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("resolved = %+v, want %+v", got, want)
	}
	for _, c := range kc.calls {
		if c[:3] == "put" {
			t.Fatalf("nothing should be written: %v", kc.calls)
		}
	}
}

func TestResolveKeychainMissing(t *testing.T) {
	kc := &fakeKeychain{exists: false}
	r := &ResolveSecrets{Keychain: kc, Minter: &fakeMinter{err: ErrMinterUnavailable}, Random: &seqReader{}}
	got, findings, err := r.Run(context.Background(), kcPath, servicesCatalogue(t, servicesTopology()), domain.Services)
	if !errors.Is(err, ErrKeychainMissing) {
		t.Fatalf("err = %v, want ErrKeychainMissing", err)
	}
	if got != nil || findings != nil {
		t.Fatalf("got = %+v, findings = %+v, want both nil", got, findings)
	}
	if !reflect.DeepEqual(kc.calls, []string{"exists " + kcPath}) {
		t.Fatalf("calls = %v, want only the existence check", kc.calls)
	}
}

func TestResolveGeneratesMissing(t *testing.T) {
	kc := &fakeKeychain{exists: true, items: map[string]fakeItem{
		"anthropic-api-key":   {value: []byte("a"), source: domain.User},
		"litellm-db-password": {value: []byte("d"), source: domain.Generated},
		"litellm-salt-key":    {value: []byte("s"), source: domain.Generated},
	}}
	r := &ResolveSecrets{Keychain: kc, Minter: &fakeMinter{err: ErrMinterUnavailable}, Random: &seqReader{}}
	got, findings, err := r.Run(context.Background(), kcPath, servicesCatalogue(t, servicesTopology()), domain.Services)
	if err != nil || len(findings) != 0 {
		t.Fatalf("err=%v findings=%+v", err, findings)
	}
	if item := kc.items["litellm-master-key"]; string(item.value) != masterKeyFromSeq || item.source != domain.Generated {
		t.Fatalf("generated item = %+v", item)
	}
	if got[2].Name != "litellm-master-key" || string(got[2].Value) != masterKeyFromSeq {
		t.Fatalf("resolved[2] = %+v", got[2])
	}
	if !contains(kc.calls, "put litellm-master-key source=generated replace=false") {
		t.Fatalf("calls = %v", kc.calls)
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func TestResolveMissingUserSecretIsAFinding(t *testing.T) {
	kc := &fakeKeychain{exists: true, items: map[string]fakeItem{
		"litellm-db-password": {value: []byte("d"), source: domain.Generated},
		"litellm-master-key":  {value: []byte("m"), source: domain.Generated},
		"litellm-salt-key":    {value: []byte("s"), source: domain.Generated},
	}}
	minter := &fakeMinter{err: ErrMinterUnavailable}
	r := &ResolveSecrets{Keychain: kc, Minter: minter, Random: &seqReader{}}
	got, findings, err := r.Run(context.Background(), kcPath, servicesCatalogue(t, servicesTopology()), domain.Services)
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.Check{{Name: "anthropic-api-key", Status: domain.Fail, Summary: "unset", Hint: "run `lclaw secrets set anthropic-api-key`"}}
	if !reflect.DeepEqual(findings, want) {
		t.Fatalf("findings = %+v, want %+v", findings, want)
	}
	if got := resolvedNames(got); !reflect.DeepEqual(got, []string{"litellm-db-password", "litellm-master-key", "litellm-salt-key"}) {
		t.Fatalf("resolved = %v", got)
	}
	if len(minter.minted) != 0 {
		t.Fatal("minter must not be called for a user secret")
	}
}

func TestResolveReportsEveryMissingSecret(t *testing.T) {
	topo := domain.Topology{KeychainPath: kcPath, Machines: []domain.MachineSpec{{Name: "services", Secrets: []string{"anthropic-api-key", "openai-api-key"}}}}
	kc := &fakeKeychain{exists: true}
	r := &ResolveSecrets{Keychain: kc, Minter: &fakeMinter{err: ErrMinterUnavailable}, Random: &seqReader{}}
	_, findings, err := r.Run(context.Background(), kcPath, servicesCatalogue(t, topo), domain.Services)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 2 || findings[0].Name != "anthropic-api-key" || findings[1].Name != "openai-api-key" {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestResolveMintsMissingMintedSecret(t *testing.T) {
	kc := &fakeKeychain{exists: true, items: map[string]fakeItem{"openclaw-gateway-token": {value: []byte("g"), source: domain.Generated}}}
	minter := &fakeMinter{value: []byte("sk-minted")}
	r := &ResolveSecrets{Keychain: kc, Minter: minter, Random: &seqReader{}}
	got, findings, err := r.Run(context.Background(), kcPath, servicesCatalogue(t, servicesTopology()), domain.Agent)
	if err != nil || len(findings) != 0 {
		t.Fatalf("err=%v findings=%+v", err, findings)
	}
	if !reflect.DeepEqual(minter.minted, []string{"openclaw-litellm-key"}) {
		t.Fatalf("minted = %v", minter.minted)
	}
	if item := kc.items["openclaw-litellm-key"]; string(item.value) != "sk-minted" || item.source != domain.Minted {
		t.Fatalf("minted item = %+v", item)
	}
	if got := resolvedNames(got); !reflect.DeepEqual(got, []string{"openclaw-gateway-token", "openclaw-litellm-key"}) {
		t.Fatalf("resolved = %v", got)
	}
}

func TestResolveMinterUnavailableIsAFinding(t *testing.T) {
	kc := &fakeKeychain{exists: true, items: map[string]fakeItem{"openclaw-gateway-token": {value: []byte("g"), source: domain.Generated}}}
	r := &ResolveSecrets{Keychain: kc, Minter: UnavailableMinter{}, Random: &seqReader{}}
	_, findings, err := r.Run(context.Background(), kcPath, servicesCatalogue(t, servicesTopology()), domain.Agent)
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.Check{{Name: "openclaw-litellm-key", Status: domain.Fail, Summary: "not minted: " + ErrMinterUnavailable.Error(), Hint: "bring the services machine up first"}}
	if !reflect.DeepEqual(findings, want) {
		t.Fatalf("findings = %+v, want %+v", findings, want)
	}
}

func TestResolveKeychainErrorStops(t *testing.T) {
	kc := &fakeKeychain{exists: true}
	r := &ResolveSecrets{Keychain: &erroringKeychain{fakeKeychain: kc, err: errors.New("keychain: get x: exit status 36")}, Minter: UnavailableMinter{}, Random: &seqReader{}}
	_, _, err := r.Run(context.Background(), kcPath, servicesCatalogue(t, servicesTopology()), domain.Services)
	if err == nil || err.Error() != "resolve secrets: anthropic-api-key: keychain: get x: exit status 36" {
		t.Fatalf("err = %v", err)
	}
}

// erroringKeychain fails every Get with err.
type erroringKeychain struct {
	*fakeKeychain
	err error
}

func (k *erroringKeychain) Get(ctx context.Context, path, name string) ([]byte, error) {
	return nil, k.err
}

func TestResolveCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := &ResolveSecrets{Keychain: &fakeKeychain{exists: true}, Minter: UnavailableMinter{}, Random: &seqReader{}}
	if _, _, err := r.Run(ctx, kcPath, servicesCatalogue(t, servicesTopology()), domain.Services); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestInjectStoresEverySecret(t *testing.T) {
	tg := newFakeTarget()
	secrets := []ResolvedSecret{{"a", []byte("1")}, {"b", []byte("2")}}
	if err := (&InjectSecrets{Target: tg}).Run(context.Background(), domain.Services, secrets); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tg.calls, []string{"store services a", "store services b"}) {
		t.Fatalf("calls = %v", tg.calls)
	}
	if string(tg.stores[domain.Services]["b"]) != "2" {
		t.Fatalf("store = %v", tg.stores)
	}
}

func TestInjectStoreFailure(t *testing.T) {
	tg := newFakeTarget()
	tg.storeErr[domain.Services] = errors.New("podman: store secret: exit status 125")
	err := (&InjectSecrets{Target: tg}).Run(context.Background(), domain.Services, []ResolvedSecret{{"a", []byte("1")}})
	if err == nil || err.Error() != "inject secrets: a: podman: store secret: exit status 125" {
		t.Fatalf("err = %v", err)
	}
}

func TestInjectCancelledBetweenSecrets(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	tg := &cancellingTarget{fakeTarget: newFakeTarget(), cancel: cancel}
	err := (&InjectSecrets{Target: tg}).Run(ctx, domain.Services, []ResolvedSecret{{"a", []byte("1")}, {"b", []byte("2")}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if !reflect.DeepEqual(tg.calls, []string{"store services a"}) {
		t.Fatalf("calls = %v, want only the first store", tg.calls)
	}
}

// cancellingTarget cancels the context after its first StoreSecret.
type cancellingTarget struct {
	*fakeTarget
	cancel context.CancelFunc
}

func (t *cancellingTarget) StoreSecret(ctx context.Context, role domain.Role, name string, value []byte) error {
	err := t.fakeTarget.StoreSecret(ctx, role, name, value)
	t.cancel()
	return err
}

func TestPurgeRemovesSecretsAndVolumes(t *testing.T) {
	tg := newFakeTarget()
	tg.store(domain.Services)["litellm-master-key"] = []byte("m")
	tg.store(domain.Services)["anthropic-api-key"] = []byte("a")
	tg.volumes[domain.Services] = map[string]bool{"anthropic-api-key": true, "litellm-db-data": true}
	names, err := (&PurgeSecrets{Target: tg}).Run(context.Background(), domain.Services)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(names, []string{"anthropic-api-key", "litellm-master-key"}) {
		t.Fatalf("names = %v", names)
	}
	if len(tg.stores[domain.Services]) != 0 {
		t.Fatalf("store = %v", tg.stores[domain.Services])
	}
	if !reflect.DeepEqual(tg.volumes[domain.Services], map[string]bool{"litellm-db-data": true}) {
		t.Fatalf("volumes = %v, want only the unrelated one", tg.volumes[domain.Services])
	}
	want := []string{"list services", "remove services anthropic-api-key", "rmvolume services anthropic-api-key", "remove services litellm-master-key", "rmvolume services litellm-master-key"}
	if !reflect.DeepEqual(tg.calls, want) {
		t.Fatalf("calls = %v, want %v", tg.calls, want)
	}
}

func TestPurgeListError(t *testing.T) {
	tg := newFakeTarget()
	tg.listErr = errors.New("podman: list secrets: exit status 125")
	if _, err := (&PurgeSecrets{Target: tg}).Run(context.Background(), domain.Services); err == nil || err.Error() != "purge secrets: podman: list secrets: exit status 125" {
		t.Fatalf("err = %v", err)
	}
}
