package litellm

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/programmablemike/localclaw/internal/adapters/exec"
	"github.com/programmablemike/localclaw/internal/app"
	"github.com/programmablemike/localclaw/internal/domain"
)

func execArgs(script string, extra ...string) []string {
	return append([]string{"--connection", "lclaw", "exec", "-i", "litellm-litellm", "python3", "-c", script}, extra...)
}

func noSleep(context.Context, time.Duration) error { return nil }

func TestMintRunsInsideTheContainer(t *testing.T) {
	f := exec.NewFake()
	f.Script("podman", execArgs(mintScript, "openclaw-litellm-key"), exec.Response{Stdout: "sk-abc123\n"})
	got, err := (&Minter{Runner: f}).Mint(context.Background(), "openclaw-litellm-key")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "sk-abc123" {
		t.Fatalf("key = %q", got)
	}
	call := f.Calls[0]
	if !call.Sensitive {
		t.Fatal("the mint call must be sensitive: its output is the key")
	}
	if strings.Contains(strings.Join(call.Args, " "), "LITELLM_MASTER_KEY=") {
		t.Fatal("the master key must never be an argument")
	}
	if !strings.Contains(mintScript, `os.environ.get("LITELLM_MASTER_KEY")`) || !strings.Contains(mintScript, "/key/generate") {
		t.Fatal("the script must read the master key from the container's environment and call /key/generate")
	}
}

func TestMintEmptyKeyIsAnError(t *testing.T) {
	f := exec.NewFake()
	f.Script("podman", execArgs(mintScript, "k"), exec.Response{Stdout: "\n"})
	if _, err := (&Minter{Runner: f}).Mint(context.Background(), "k"); err == nil || !strings.Contains(err.Error(), "empty key") {
		t.Fatalf("err = %v", err)
	}
}

func TestMintWithoutTheContainerIsUnavailable(t *testing.T) {
	f := exec.NewFake()
	f.Script("podman", execArgs(mintScript, "k"), exec.Response{Err: &exec.ExitError{Code: 125, Stderr: "Error: no such container litellm-litellm"}})
	_, err := (&Minter{Runner: f}).Mint(context.Background(), "k")
	if !errors.Is(err, app.ErrMinterUnavailable) {
		t.Fatalf("err = %v, want ErrMinterUnavailable", err)
	}
}

func TestMintWithoutPodman(t *testing.T) {
	f := exec.NewFake()
	f.Script("podman", execArgs(mintScript, "k"), exec.Response{Err: exec.ErrNotFound})
	if _, err := (&Minter{Runner: f}).Mint(context.Background(), "k"); !errors.Is(err, domain.ErrToolNotFound) {
		t.Fatalf("err = %v, want ErrToolNotFound", err)
	}
}

func TestRevokeSendsTheKeyOnStdin(t *testing.T) {
	f := exec.NewFake()
	f.Script("podman", execArgs(revokeScript), exec.Response{Stdout: "ok\n"})
	if err := (&Minter{Runner: f}).Revoke(context.Background(), "k", []byte("sk-abc123")); err != nil {
		t.Fatal(err)
	}
	if f.Calls[0].Stdin != "sk-abc123\n" || !f.Calls[0].Sensitive {
		t.Fatalf("call = %+v", f.Calls[0])
	}
}

func TestRevokeErrorRedactsTheKey(t *testing.T) {
	f := exec.NewFake()
	f.Script("podman", execArgs(revokeScript), exec.Response{Err: &exec.ExitError{Code: 1, Stderr: "Traceback: bad key sk-abc123 rejected"}})
	err := (&Minter{Runner: f}).Revoke(context.Background(), "k", []byte("sk-abc123"))
	if err == nil || strings.Contains(err.Error(), "sk-abc123") {
		t.Fatalf("err = %v, must not carry the key", err)
	}
}

func TestReadyPollsUntilItAnswers(t *testing.T) {
	f := exec.NewFake()
	f.Script("podman", execArgs(readyScript),
		exec.Response{Err: &exec.ExitError{Code: 1, Stderr: "<urlopen error [Errno 111] Connection refused>"}},
		exec.Response{Err: &exec.ExitError{Code: 1, Stderr: "HTTP Error 503: Service Unavailable"}},
		exec.Response{},
	)
	m := &Minter{Runner: f, Timeout: time.Minute, Sleep: noSleep}
	if err := m.Ready(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(f.Calls) != 3 {
		t.Fatalf("polled %d times, want 3", len(f.Calls))
	}
}

func TestReadyGivesUp(t *testing.T) {
	f := exec.NewFake()
	f.Script("podman", execArgs(readyScript), exec.Response{Err: &exec.ExitError{Code: 1, Stderr: "boom\n<urlopen error [Errno 111] Connection refused>"}})
	m := &Minter{Runner: f, Timeout: time.Nanosecond, Sleep: noSleep}
	err := m.Ready(context.Background())
	if err == nil || !strings.HasPrefix(err.Error(), "litellm: not ready after 1ns: <urlopen error") {
		t.Fatalf("err = %v", err)
	}
}

func TestReadyHonoursCancellation(t *testing.T) {
	f := exec.NewFake()
	f.Script("podman", execArgs(readyScript), exec.Response{Err: &exec.ExitError{Code: 1, Stderr: "refused"}})
	ctx, cancel := context.WithCancel(context.Background())
	m := &Minter{Runner: f, Timeout: time.Hour, Sleep: func(context.Context, time.Duration) error { cancel(); return nil }}
	if err := m.Ready(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}
