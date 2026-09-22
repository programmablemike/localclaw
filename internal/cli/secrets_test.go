package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/programmablemike/localclaw/internal/app"
	"github.com/programmablemike/localclaw/internal/domain"
)

type fakeSecrets struct {
	list    []app.SecretStatus
	detail  app.SecretDetail
	outcome app.Outcome
	value   []byte
	err     error
	calls   []string
}

func (f *fakeSecrets) List(ctx context.Context, dir string) ([]app.SecretStatus, error) {
	f.calls = append(f.calls, "list "+dir)
	return f.list, f.err
}

func (f *fakeSecrets) Describe(ctx context.Context, dir, name string) (app.SecretDetail, error) {
	f.calls = append(f.calls, "describe "+name)
	return f.detail, f.err
}

func (f *fakeSecrets) Set(ctx context.Context, dir, name string, value []byte) (app.Outcome, error) {
	f.calls = append(f.calls, fmt.Sprintf("set %s %q", name, value))
	return f.outcome, f.err
}

func (f *fakeSecrets) Update(ctx context.Context, dir, name string, value []byte, generate bool) (app.Outcome, error) {
	f.calls = append(f.calls, fmt.Sprintf("update %s %q generate=%v", name, value, generate))
	return f.outcome, f.err
}

func (f *fakeSecrets) Delete(ctx context.Context, dir, name string) (app.Outcome, error) {
	f.calls = append(f.calls, "delete "+name)
	return f.outcome, f.err
}

func (f *fakeSecrets) Get(ctx context.Context, dir, name string) ([]byte, error) {
	f.calls = append(f.calls, "get "+name)
	return f.value, f.err
}

func spec(name string, src domain.Source, roles ...domain.Role) domain.SecretSpec {
	return domain.SecretSpec{Name: name, Source: src, Zones: roles}
}

var listing = []app.SecretStatus{
	{Spec: spec("anthropic-api-key", domain.User, domain.Services), Source: domain.User, Set: true},
	{Spec: spec("litellm-master-key", domain.Generated, domain.Services), Source: domain.User, Set: true},
	{Spec: spec("litellm-salt-key", domain.Generated, domain.Services), Source: domain.Generated},
	{Spec: spec("openclaw-litellm-key", domain.Minted, domain.Agent), Source: domain.Minted},
	{Spec: spec("shared-token", domain.User, domain.Services, domain.Agent), Source: domain.User},
}

var t0 = time.Date(2026, 9, 21, 7, 3, 45, 0, time.UTC)

var detail = app.SecretDetail{
	SecretStatus: app.SecretStatus{Spec: spec("shared-token", domain.User, domain.Services, domain.Agent), Source: domain.User, Set: true},
	Created:      t0,
	Modified:     t0.Add(time.Minute),
	Store:        app.StoreOutcome{State: app.StoreStored},
}

var detailUnset = app.SecretDetail{
	SecretStatus: app.SecretStatus{Spec: spec("litellm-master-key", domain.Generated, domain.Services), Source: domain.Generated},
	Store:        app.StoreOutcome{State: app.StoreAbsent},
}

func secretsDeps(f *fakeSecrets) Deps {
	return Deps{Doctor: &fakeDoctor{}, Init: &fakeInit{}, Secrets: f, DefaultDir: testDefaultDir, Stdin: strings.NewReader("")}
}

func TestSecretsListText(t *testing.T) {
	f := &fakeSecrets{list: listing}
	r := executeDeps(t, secretsDeps(f), "secrets", "list")
	if r.err != nil {
		t.Fatal(r.err)
	}
	golden(t, "secrets-list.txt", r.stdout.Bytes())
	if f.calls[0] != "list "+testDefaultDir {
		t.Fatalf("calls = %v", f.calls)
	}
}

func TestSecretsListJSON(t *testing.T) {
	r := executeDeps(t, secretsDeps(&fakeSecrets{list: listing}), "--output", "json", "secrets", "list")
	if r.err != nil {
		t.Fatal(r.err)
	}
	golden(t, "secrets-list.json", r.stdout.Bytes())
}

func TestSecretsDescribeText(t *testing.T) {
	r := executeDeps(t, secretsDeps(&fakeSecrets{detail: detail}), "secrets", "describe", "shared-token")
	if r.err != nil {
		t.Fatal(r.err)
	}
	golden(t, "secrets-describe.txt", r.stdout.Bytes())
}

func TestSecretsDescribeJSON(t *testing.T) {
	r := executeDeps(t, secretsDeps(&fakeSecrets{detail: detail}), "--output", "json", "secrets", "describe", "shared-token")
	if r.err != nil {
		t.Fatal(r.err)
	}
	golden(t, "secrets-describe.json", r.stdout.Bytes())
}

func TestSecretsDescribeUnsetText(t *testing.T) {
	r := executeDeps(t, secretsDeps(&fakeSecrets{detail: detailUnset}), "secrets", "describe", "litellm-master-key")
	if r.err != nil {
		t.Fatal(r.err)
	}
	golden(t, "secrets-describe-unset.txt", r.stdout.Bytes())
}

func TestSecretsSetFromPipeRemovesOneNewline(t *testing.T) {
	for in, want := range map[string]string{"sk-ant\n": "sk-ant", "sk-ant\n\n": "sk-ant\n", "sk-ant": "sk-ant", "sk-ant\r\n": "sk-ant"} {
		f := &fakeSecrets{outcome: app.Outcome{Name: "anthropic-api-key", Store: app.StoreOutcome{State: app.StoreStored}}}
		d := secretsDeps(f)
		d.Stdin = strings.NewReader(in)
		r := executeDeps(t, d, "secrets", "set", "anthropic-api-key")
		if r.err != nil {
			t.Fatalf("%q: %v", in, r.err)
		}
		if got, wantCall := f.calls[0], fmt.Sprintf("set anthropic-api-key %q", want); got != wantCall {
			t.Fatalf("%q: call = %q, want %q", in, got, wantCall)
		}
	}
}

func TestSecretsSetText(t *testing.T) {
	f := &fakeSecrets{outcome: app.Outcome{Name: "shared-token", Store: app.StoreOutcome{State: app.StoreStored}}}
	d := secretsDeps(f)
	d.Stdin = strings.NewReader("v\n")
	r := executeDeps(t, d, "secrets", "set", "shared-token")
	if r.err != nil {
		t.Fatal(r.err)
	}
	golden(t, "secrets-set.txt", r.stdout.Bytes())
}

func TestSecretsSetFromPrompt(t *testing.T) {
	f := &fakeSecrets{outcome: app.Outcome{Name: "anthropic-api-key"}}
	d := secretsDeps(f)
	d.Stdin = nil
	var prompted string
	d.Prompt = func(prompt string) ([]byte, error) { prompted = prompt; return []byte("typed"), nil }
	r := executeDeps(t, d, "secrets", "set", "anthropic-api-key")
	if r.err != nil {
		t.Fatal(r.err)
	}
	if prompted != "value for anthropic-api-key: " || f.calls[0] != `set anthropic-api-key "typed"` {
		t.Fatalf("prompt = %q, calls = %v", prompted, f.calls)
	}
}

// TestSecretsSetPromptWinsOverStdin pins the precedence readValue actually
// implements: the composition root only ever sets Deps.Prompt when stdin is
// a terminal, so when both are set (as they never are in production, but a
// test double can), the prompt wins and stdin is left untouched.
func TestSecretsSetPromptWinsOverStdin(t *testing.T) {
	f := &fakeSecrets{outcome: app.Outcome{Name: "anthropic-api-key"}}
	d := secretsDeps(f)
	const stdinContents = "from-stdin\n"
	stdin := strings.NewReader(stdinContents)
	d.Stdin = stdin
	var prompted string
	d.Prompt = func(prompt string) ([]byte, error) { prompted = prompt; return []byte("typed"), nil }
	r := executeDeps(t, d, "secrets", "set", "anthropic-api-key")
	if r.err != nil {
		t.Fatal(r.err)
	}
	if prompted != "value for anthropic-api-key: " || f.calls[0] != `set anthropic-api-key "typed"` {
		t.Fatalf("prompt = %q, calls = %v", prompted, f.calls)
	}
	if stdin.Len() != len(stdinContents) {
		t.Fatalf("stdin was consumed: Len() = %d, want %d", stdin.Len(), len(stdinContents))
	}
}

func TestSecretsSetFromFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(path, []byte("from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f := &fakeSecrets{outcome: app.Outcome{Name: "anthropic-api-key"}}
	r := executeDeps(t, secretsDeps(f), "secrets", "set", "anthropic-api-key", "--from-file", path)
	if r.err != nil {
		t.Fatal(r.err)
	}
	if f.calls[0] != `set anthropic-api-key "from-file"` {
		t.Fatalf("calls = %v", f.calls)
	}
}

func TestSecretsSetMissingName(t *testing.T) {
	r := executeDeps(t, secretsDeps(&fakeSecrets{}), "secrets", "set")
	if got := ExitCode(r.err); got != 2 || !strings.Contains(r.stderr.String(), "missing secret name") {
		t.Fatalf("exit %d, stderr %q", got, r.stderr.String())
	}
}

// TestSecretsSetNoValueIsUsageError checks that having no --from-file, no
// prompt and no stdin is a usage mistake (exit 2), like every other
// invocation error on this path, not a plain runtime error (exit 1).
func TestSecretsSetNoValueIsUsageError(t *testing.T) {
	d := secretsDeps(&fakeSecrets{})
	d.Stdin = nil
	r := executeDeps(t, d, "secrets", "set", "x")
	if got := ExitCode(r.err); got != 2 {
		t.Fatalf("exit code = %d (err %v), want 2", got, r.err)
	}
	if r.stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", r.stdout.String())
	}
}

func TestSecretsExitCodes(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"exists", fmt.Errorf("%w: x", app.ErrSecretExists), 1},
		{"unknown", fmt.Errorf("%w: x", app.ErrUnknownSecret), 2},
		{"too large", fmt.Errorf("%w: 1025 bytes", domain.ErrInvalidValue), 2},
		{"keychain missing", fmt.Errorf("%w at /k", app.ErrKeychainMissing), 1},
		{"not generatable", fmt.Errorf("%w: x", app.ErrNotGeneratable), 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := executeDeps(t, secretsDeps(&fakeSecrets{err: tt.err}), "secrets", "set", "x")
			if got := ExitCode(r.err); got != tt.want {
				t.Fatalf("exit code = %d (err %v), want %d", got, r.err, tt.want)
			}
			if Silent(r.err) {
				t.Fatal("the composition root must print this error")
			}
			if r.stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", r.stdout.String())
			}
		})
	}
}

func TestSecretsUpdateGenerate(t *testing.T) {
	f := &fakeSecrets{outcome: app.Outcome{Name: "litellm-master-key", Store: app.StoreOutcome{State: app.StoreStored}}}
	r := executeDeps(t, secretsDeps(f), "--output", "json", "secrets", "update", "litellm-master-key", "--generate")
	if r.err != nil {
		t.Fatal(r.err)
	}
	if f.calls[0] != `update litellm-master-key "" generate=true` {
		t.Fatalf("calls = %v", f.calls)
	}
	golden(t, "secrets-update.json", r.stdout.Bytes())
}

func TestSecretsUpdateTextMentionsNextUp(t *testing.T) {
	f := &fakeSecrets{outcome: app.Outcome{Name: "litellm-master-key", Store: app.StoreOutcome{State: app.StoreStored}}}
	d := secretsDeps(f)
	d.Stdin = strings.NewReader("v")
	r := executeDeps(t, d, "secrets", "update", "litellm-master-key")
	if r.err != nil {
		t.Fatal(r.err)
	}
	if !strings.Contains(r.stdout.String(), "next `lclaw up`") {
		t.Fatalf("stdout = %q", r.stdout.String())
	}
}

func TestSecretsUpdateGenerateWithFileIsUsageError(t *testing.T) {
	r := executeDeps(t, secretsDeps(&fakeSecrets{}), "secrets", "update", "x", "--generate", "--from-file", "/f")
	if got := ExitCode(r.err); got != 2 {
		t.Fatalf("exit code = %d (err %v), want 2", got, r.err)
	}
}

func TestSecretsDeleteWarnsOnStderr(t *testing.T) {
	f := &fakeSecrets{outcome: app.Outcome{Name: "litellm-salt-key", Warning: "litellm-salt-key may not be rotated", Store: app.StoreOutcome{State: app.StoreRemoved}}}
	r := executeDeps(t, secretsDeps(f), "secrets", "delete", "litellm-salt-key")
	if r.err != nil {
		t.Fatal(r.err)
	}
	if r.stderr.String() != "lclaw: warning: litellm-salt-key may not be rotated\n" {
		t.Fatalf("stderr = %q", r.stderr.String())
	}
	if !strings.Contains(r.stdout.String(), "deleted litellm-salt-key\n  store  removed\n") {
		t.Fatalf("stdout = %q", r.stdout.String())
	}
}

func TestSecretsOutcomeFailureExits1(t *testing.T) {
	f := &fakeSecrets{outcome: app.Outcome{Name: "x", Store: app.StoreOutcome{State: app.StoreFailed, Err: errors.New("podman: store secret x: exit status 125")}}}
	r := executeDeps(t, secretsDeps(f), "secrets", "delete", "x")
	if !errors.Is(r.err, ErrChecksFailed) {
		t.Fatalf("err = %v, want ErrChecksFailed", r.err)
	}
	if !strings.Contains(r.stdout.String(), "store  failed: podman: store secret x: exit status 125") {
		t.Fatalf("stdout = %q", r.stdout.String())
	}
}

func TestSecretsGetWritesRawValue(t *testing.T) {
	r := executeDeps(t, secretsDeps(&fakeSecrets{value: []byte("sk-\x00\xff")}), "secrets", "get", "litellm-master-key")
	if r.err != nil {
		t.Fatal(r.err)
	}
	if r.stdout.String() != "sk-\x00\xff" || r.stderr.Len() != 0 {
		t.Fatalf("stdout = %q, stderr = %q", r.stdout.String(), r.stderr.String())
	}
}

func TestSecretsFindingsRenderAsReport(t *testing.T) {
	tests := []struct {
		name    string
		finding domain.Finding
		want    string
	}{
		{
			name:    "plain message",
			finding: domain.Finding{Where: "zones.services.secrets", Message: `"Bad" is not a valid secret name; use a lowercase DNS label of at most 63 characters`},
			want:    "FAIL  zones.services.secrets  \"Bad\" is not a valid secret name; use a lowercase DNS label of at most 63 characters\n\n0 passed, 0 warnings, 1 failed\n",
		},
		{
			// A secret name that itself contains "; " must not fool the
			// renderer into splitting mid-name: the whole message stays on
			// one line, unsplit.
			name:    "semicolon inside quoted name",
			finding: domain.Finding{Where: "zones.services.secrets", Message: `"a; b" is not a valid secret name; use a lowercase DNS label of at most 63 characters`},
			want:    "FAIL  zones.services.secrets  \"a; b\" is not a valid secret name; use a lowercase DNS label of at most 63 characters\n\n0 passed, 0 warnings, 1 failed\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := &app.FindingsError{Findings: []domain.Finding{tt.finding}}
			r := executeDeps(t, secretsDeps(&fakeSecrets{err: err}), "secrets", "list")
			if !errors.Is(r.err, ErrChecksFailed) {
				t.Fatalf("err = %v, want ErrChecksFailed", r.err)
			}
			if r.stdout.String() != tt.want {
				t.Fatalf("stdout = %q, want %q", r.stdout.String(), tt.want)
			}
		})
	}
}

func TestSecretsGroupHelpAndUnknown(t *testing.T) {
	r := executeDeps(t, secretsDeps(&fakeSecrets{}), "secrets")
	if r.err != nil || !strings.Contains(r.stdout.String(), "list") || !strings.Contains(r.stdout.String(), "describe") {
		t.Fatalf("err = %v, stdout = %q", r.err, r.stdout.String())
	}
	r = executeDeps(t, secretsDeps(&fakeSecrets{}), "secrets", "bogus")
	if got := ExitCode(r.err); got != 2 || !strings.Contains(r.stderr.String(), `unknown command "bogus"`) {
		t.Fatalf("exit %d, stderr %q", got, r.stderr.String())
	}
}
