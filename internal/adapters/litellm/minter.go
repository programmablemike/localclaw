// Package litellm mints and revokes the agent's LiteLLM virtual key. It
// runs a short Python script inside the LiteLLM container through
// `podman exec`, so the master key never leaves the machine: the script
// reads it from the container's own environment, where the Pod file put
// it, and talks to LiteLLM on the container's loopback. The package
// satisfies app.KeyMinter and knows the endpoints; nothing else does.
package litellm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/programmablemike/localclaw/internal/adapters/exec"
	"github.com/programmablemike/localclaw/internal/app"
	"github.com/programmablemike/localclaw/internal/domain"
)

// Container is the LiteLLM container's name on the machine: kube play
// names containers <pod>-<container>, and the default Pod file uses
// "litellm" for both.
const Container = "litellm-litellm"

// Minter satisfies app.KeyMinter.
type Minter struct {
	Runner exec.Runner
	// Timeout bounds Ready; zero means two minutes, which covers LiteLLM's
	// first-boot schema migration on a laptop.
	Timeout time.Duration
	// Interval is the readiness poll interval; zero means two seconds.
	Interval time.Duration
	// Sleep is replaced in tests.
	Sleep func(context.Context, time.Duration) error
}

// Scripts run with `python3 -` inside the container. They print exactly
// one line, the value or "ok", and exit non-zero with a message otherwise.
// Only urllib is used, which every CPython ships.
const (
	readyScript = `import sys, urllib.request
try:
    with urllib.request.urlopen("http://127.0.0.1:4000/health/readiness", timeout=5) as r:
        sys.exit(0 if r.status == 200 else 1)
except Exception as e:
    print(e, file=sys.stderr)
    sys.exit(1)
`
	mintScript = `import json, os, sys, urllib.request
key = os.environ.get("LITELLM_MASTER_KEY")
if not key:
    print("LITELLM_MASTER_KEY is not set in the container", file=sys.stderr)
    sys.exit(1)
body = json.dumps({"key_alias": sys.argv[1], "metadata": {"created_by": "lclaw"}}).encode()
req = urllib.request.Request("http://127.0.0.1:4000/key/generate", data=body, method="POST",
    headers={"Authorization": "Bearer " + key, "Content-Type": "application/json"})
with urllib.request.urlopen(req, timeout=30) as r:
    print(json.load(r)["key"])
`
	revokeScript = `import json, os, sys, urllib.request
key = os.environ.get("LITELLM_MASTER_KEY")
if not key:
    print("LITELLM_MASTER_KEY is not set in the container", file=sys.stderr)
    sys.exit(1)
value = sys.stdin.readline().rstrip("\n")
body = json.dumps({"keys": [value]}).encode()
req = urllib.request.Request("http://127.0.0.1:4000/key/delete", data=body, method="POST",
    headers={"Authorization": "Bearer " + key, "Content-Type": "application/json"})
with urllib.request.urlopen(req, timeout=30) as r:
    r.read()
print("ok")
`
)

// exec runs a script inside the container with stdin as its input. Every
// call is sensitive: the mint script prints the key and the revoke script
// reads it.
func (m *Minter) exec(ctx context.Context, script string, stdin string, args ...string) (string, error) {
	cmd := exec.Command{
		Name:      "podman",
		Args:      append([]string{"--connection", domain.MachineName, "exec", "-i", Container, "python3", "-c", script}, args...),
		Stdin:     strings.NewReader(stdin),
		Sensitive: true,
	}
	res, err := m.Runner.Run(ctx, cmd)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(res.Stdout)), nil
}

// Ready implements app.KeyMinter: it polls the readiness endpoint until it
// answers 200, the timeout passes, or the context ends. A missing
// container is reported as the services zone not being up.
func (m *Minter) Ready(ctx context.Context) error {
	timeout, interval := m.Timeout, m.Interval
	if timeout == 0 {
		timeout = 2 * time.Minute
	}
	if interval == 0 {
		interval = 2 * time.Second
	}
	sleep := m.Sleep
	if sleep == nil {
		sleep = sleepCtx
	}
	deadline := time.Now().Add(timeout)
	var last error
	for {
		_, err := m.exec(ctx, readyScript, "")
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		last = err
		if time.Now().After(deadline) {
			return fmt.Errorf("litellm: not ready after %s: %w", timeout, redact(last))
		}
		if err := sleep(ctx, interval); err != nil {
			return err
		}
	}
}

// Mint implements app.KeyMinter. The alias is the secret's name, so the
// key is recognisable in LiteLLM's dashboard.
func (m *Minter) Mint(ctx context.Context, name string) ([]byte, error) {
	out, err := m.exec(ctx, mintScript, "", name)
	if err != nil {
		return nil, fmt.Errorf("litellm: mint %s: %w", name, unavailable(err))
	}
	if out == "" {
		return nil, fmt.Errorf("litellm: mint %s: empty key", name)
	}
	return []byte(out), nil
}

// Revoke implements app.KeyMinter. The key travels on stdin, never in an
// argument.
func (m *Minter) Revoke(ctx context.Context, name string, value []byte) error {
	if _, err := m.exec(ctx, revokeScript, string(value)+"\n"); err != nil {
		return fmt.Errorf("litellm: revoke %s: %w", name, exec.Redact(unavailable(err), string(value)))
	}
	return nil
}

// unavailable maps "no such container" from podman exec, which means the
// services zone is not up, onto app.ErrMinterUnavailable.
func unavailable(err error) error {
	var ee *exec.ExitError
	if errors.As(err, &ee) && (strings.Contains(ee.Stderr, "no such container") || strings.Contains(ee.Stderr, "is not running")) {
		return app.ErrMinterUnavailable
	}
	if errors.Is(err, exec.ErrNotFound) {
		return fmt.Errorf("%w", domain.ErrToolNotFound)
	}
	return err
}

// redact keeps only the last line of a readiness failure, which is the
// exception text, and drops nothing sensitive because the script prints
// none.
func redact(err error) error {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		lines := strings.Split(strings.TrimSpace(ee.Stderr), "\n")
		return errors.New(lines[len(lines)-1])
	}
	return err
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

var _ = bytes.MinRead
