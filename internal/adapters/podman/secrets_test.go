package podman

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/programmablemike/localclaw/internal/adapters/exec"
	"github.com/programmablemike/localclaw/internal/domain"
)

func TestKubeSecretGolden(t *testing.T) {
	got, err := kubeSecret("litellm-master-key", []byte("sk-..."))
	if err != nil {
		t.Fatal(err)
	}
	want := bytes.TrimSpace([]byte(fixture(t, "kube-secret.json")))
	if !bytes.Equal(got, want) {
		t.Fatalf("kubeSecret() =\n%s\nwant\n%s", got, want)
	}
}

func TestStoreSecretArgvAndBody(t *testing.T) {
	f := exec.NewFake()
	args := []string{"--connection", "lclaw-services", "secret", "create", "--replace", "--label", "app.kubernetes.io/part-of=localclaw", "litellm-master-key", "-"}
	f.Script("podman", args, exec.Response{Stdout: "0a1b2c3d\n"})
	if err := (&Client{Runner: f}).StoreSecret(context.Background(), domain.Services, "litellm-master-key", []byte("sk-...")); err != nil {
		t.Fatal(err)
	}
	if len(f.Calls) != 1 || !reflect.DeepEqual(f.Calls[0].Args, args) || !f.Calls[0].Sensitive {
		t.Fatalf("Calls = %+v", f.Calls)
	}
	if got, want := f.Calls[0].Stdin, strings.TrimSpace(fixture(t, "kube-secret.json")); got != want {
		t.Fatalf("stdin = %s, want %s", got, want)
	}
}

func TestStoreSecretError(t *testing.T) {
	f := exec.NewFake()
	f.Script("podman", []string{"--connection", "lclaw-agent", "secret", "create", "--replace", "--label", "app.kubernetes.io/part-of=localclaw", "k", "-"},
		exec.Response{Err: &exec.ExitError{Code: 125, Stderr: "Error: unable to connect"}})
	err := (&Client{Runner: f}).StoreSecret(context.Background(), domain.Agent, "k", []byte("v"))
	if err == nil || !strings.HasPrefix(err.Error(), "podman: store secret k: exit status 125") {
		t.Fatalf("err = %v", err)
	}
}

func TestMachineRunning(t *testing.T) {
	f := exec.NewFake()
	f.Script("podman", []string{"machine", "list", "--format", "json"}, exec.Response{Stdout: fixture(t, "machine-list.json")})
	c := &Client{Runner: f}
	if running, err := c.MachineRunning(context.Background(), domain.Infra); err != nil || !running {
		t.Fatalf("infra: %v, %v", running, err)
	}
	if running, err := c.MachineRunning(context.Background(), domain.Agent); err != nil || running {
		t.Fatalf("agent (not created): %v, %v", running, err)
	}
}

func TestSecretExists(t *testing.T) {
	args := []string{"--connection", "lclaw-services", "secret", "exists", "k"}
	for _, tt := range []struct {
		resp exec.Response
		want bool
		err  bool
	}{
		{exec.Response{}, true, false},
		{exec.Response{Err: &exec.ExitError{Code: 1}}, false, false},
		{exec.Response{Err: &exec.ExitError{Code: 125, Stderr: "cannot connect"}}, false, true},
	} {
		f := exec.NewFake()
		f.Script("podman", args, tt.resp)
		got, err := (&Client{Runner: f}).SecretExists(context.Background(), domain.Services, "k")
		if (err != nil) != tt.err || got != tt.want {
			t.Errorf("resp %+v: got %v, %v", tt.resp, got, err)
		}
	}
}

func TestRemoveSecret(t *testing.T) {
	f := exec.NewFake()
	f.Script("podman", []string{"--connection", "lclaw-services", "secret", "rm", "k"}, exec.Response{Stdout: "0a1b\n"})
	if err := (&Client{Runner: f}).RemoveSecret(context.Background(), domain.Services, "k"); err != nil {
		t.Fatal(err)
	}
}

func TestListSecretsByLabel(t *testing.T) {
	f := exec.NewFake()
	f.Script("podman", []string{"--connection", "lclaw-services", "secret", "ls", "--quiet"}, exec.Response{Stdout: fixture(t, "secret-ls-quiet.txt")})
	f.Script("podman", []string{"--connection", "lclaw-services", "secret", "inspect", "0a1b2c3d4e5f60718293a4b5c", "1b2c3d4e5f60718293a4b5c6d"}, exec.Response{Stdout: fixture(t, "secret-inspect.json")})
	got, err := (&Client{Runner: f}).ListSecrets(context.Background(), domain.Services)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"litellm-master-key"}) {
		t.Fatalf("ListSecrets() = %v, want only the labelled secret", got)
	}
}

func TestListSecretsEmpty(t *testing.T) {
	f := exec.NewFake()
	f.Script("podman", []string{"--connection", "lclaw-services", "secret", "ls", "--quiet"}, exec.Response{Stdout: ""})
	got, err := (&Client{Runner: f}).ListSecrets(context.Background(), domain.Services)
	if err != nil || len(got) != 0 {
		t.Fatalf("ListSecrets() = %v, %v", got, err)
	}
	if len(f.Calls) != 1 {
		t.Fatalf("inspect must not run with no ids: %+v", f.Calls)
	}
}

func TestListSecretsBadJSON(t *testing.T) {
	f := exec.NewFake()
	f.Script("podman", []string{"--connection", "lclaw-services", "secret", "ls", "--quiet"}, exec.Response{Stdout: "abc\n"})
	f.Script("podman", []string{"--connection", "lclaw-services", "secret", "inspect", "abc"}, exec.Response{Stdout: "{not json"})
	if _, err := (&Client{Runner: f}).ListSecrets(context.Background(), domain.Services); err == nil || !strings.HasPrefix(err.Error(), "podman: inspect secrets: decode: ") {
		t.Fatalf("err = %v", err)
	}
}

func TestRemoveVolume(t *testing.T) {
	exists := []string{"--connection", "lclaw-services", "volume", "exists", "k"}
	rm := []string{"--connection", "lclaw-services", "volume", "rm", "k"}

	f := exec.NewFake()
	f.Script("podman", exists, exec.Response{})
	f.Script("podman", rm, exec.Response{Stdout: "k\n"})
	if err := (&Client{Runner: f}).RemoveVolume(context.Background(), domain.Services, "k"); err != nil {
		t.Fatal(err)
	}
	if len(f.Calls) != 2 || !reflect.DeepEqual(f.Calls[1].Args, rm) {
		t.Fatalf("Calls = %+v", f.Calls)
	}

	f = exec.NewFake()
	f.Script("podman", exists, exec.Response{Err: &exec.ExitError{Code: 1}})
	if err := (&Client{Runner: f}).RemoveVolume(context.Background(), domain.Services, "k"); err != nil {
		t.Fatal(err)
	}
	if len(f.Calls) != 1 {
		t.Fatalf("rm must not run for an absent volume: %+v", f.Calls)
	}
}

func TestSecretStoreNotFound(t *testing.T) {
	f := exec.NewFake()
	f.Script("podman", []string{"--connection", "lclaw-services", "secret", "ls", "--quiet"}, exec.Response{Err: exec.ErrNotFound})
	if _, err := (&Client{Runner: f}).ListSecrets(context.Background(), domain.Services); !errors.Is(err, domain.ErrToolNotFound) {
		t.Fatalf("err = %v, want ErrToolNotFound", err)
	}
}
