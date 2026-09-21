// Package keychain wraps the macOS security command line tool. Nothing
// outside this package knows about -i, -X or -g. Every call is marked
// sensitive on the runner so no output is ever logged, and password lines
// are stripped from stderr before an error is wrapped.
package keychain

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/programmablemike/localclaw/internal/adapters/exec"
	"github.com/programmablemike/localclaw/internal/app"
	"github.com/programmablemike/localclaw/internal/domain"
)

const (
	tool    = "security"
	service = "lclaw"
	kind    = "LocalClaw secret"
	// lockTimeout is the idle time in seconds after which the keychain
	// locks itself. Changing it is a `security set-keychain-settings` call.
	lockTimeout = "900"
)

// security exits with the low byte of the Security framework status.
const (
	exitNotFound   = 44 // errSecItemNotFound
	exitDuplicate  = 45 // errSecDuplicateItem
	exitLocked     = 36 // errSecInteractionNotAllowed
	exitNoKeychain = 50 // errSecNoSuchKeychain
	exitAuthFailed = 51 // errSecAuthFailed
)

// Client satisfies app.Keychain.
type Client struct {
	Runner exec.Runner
}

// Exists reports whether the keychain file is present.
func (c *Client) Exists(ctx context.Context, path string) (bool, error) {
	_, err := os.Stat(path)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	default:
		return false, fmt.Errorf("keychain: stat %s: %w", path, err)
	}
}

// Create makes the keychain with the terminal attached, so security asks
// for the password itself, then sets it to lock on sleep and after
// lockTimeout idle seconds. The file is never added to the search list.
func (c *Client) Create(ctx context.Context, path string) error {
	if err := checkPath(path); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("keychain: create %s: %w", path, err)
	}
	if err := c.Runner.Interactive(ctx, exec.Command{Name: tool, Args: []string{"create-keychain", path}}); err != nil {
		return wrap("create-keychain", err)
	}
	_, err := c.Runner.Run(ctx, exec.Command{Name: tool, Args: []string{"set-keychain-settings", "-l", "-u", "-t", lockTimeout, path}, Sensitive: true})
	if err != nil {
		return wrap("set-keychain-settings", err)
	}
	return nil
}

// Get reads the value with find-generic-password -g, which prints it on
// stderr in one of two unambiguous forms.
func (c *Client) Get(ctx context.Context, path, name string) ([]byte, error) {
	if err := c.require(ctx, path); err != nil {
		return nil, err
	}
	res, err := c.run(ctx, path, nil, "find-generic-password", "-a", name, "-s", service, "-g", path)
	if err != nil {
		return nil, wrap("get "+name, err)
	}
	v, err := parsePassword(res.Stderr)
	if err != nil {
		return nil, fmt.Errorf("keychain: get %s: %w", name, err)
	}
	return v, nil
}

// Describe reads the attributes without the value.
func (c *Client) Describe(ctx context.Context, path, name string) (domain.SecretItem, error) {
	if err := c.require(ctx, path); err != nil {
		return domain.SecretItem{}, err
	}
	res, err := c.run(ctx, path, nil, "find-generic-password", "-a", name, "-s", service, path)
	if err != nil {
		return domain.SecretItem{}, wrap("describe "+name, err)
	}
	item, err := parseAttributes(res.Stdout)
	if err != nil {
		return domain.SecretItem{}, fmt.Errorf("keychain: describe %s: %w", name, err)
	}
	item.Name = name
	return item, nil
}

// Put writes the item through interactive mode: one add-generic-password
// line on stdin with the value hexadecimal-encoded after -X, the keychain
// path last, and -U when replacing. The value never enters an argument
// list.
func (c *Client) Put(ctx context.Context, path, name string, value []byte, source domain.Source, replace bool) error {
	if err := c.require(ctx, path); err != nil {
		return err
	}
	if err := domain.ValidateName(name); err != nil {
		return fmt.Errorf("keychain: put: %w", err)
	}
	if err := checkPath(path); err != nil {
		return err
	}
	var line strings.Builder
	line.WriteString("add-generic-password -a " + name + " -s " + service + ` -D "` + kind + `" -j ` + source.String())
	if replace {
		line.WriteString(" -U")
	}
	line.WriteString(" -X " + hex.EncodeToString(value) + ` "` + path + `"` + "\n")
	if _, err := c.run(ctx, path, []byte(line.String()), "-i"); err != nil {
		return wrap("put "+name, err)
	}
	return nil
}

// Delete removes the item.
func (c *Client) Delete(ctx context.Context, path, name string) error {
	if err := c.require(ctx, path); err != nil {
		return err
	}
	if _, err := c.run(ctx, path, nil, "delete-generic-password", "-a", name, "-s", service, path); err != nil {
		return wrap("delete "+name, err)
	}
	return nil
}

// require refuses to touch a keychain whose file is absent. With a missing
// keychain path, security add-generic-password silently writes to the
// default keychain and the find commands report "not found"; both mislead.
func (c *Client) require(ctx context.Context, path string) error {
	exists, err := c.Exists(ctx, path)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("keychain: %w at %s", app.ErrKeychainMissing, path)
	}
	return nil
}

// checkPath rejects paths the interactive-mode parser cannot carry inside
// a quoted argument.
func checkPath(path string) error {
	if strings.ContainsAny(path, "\"\\\n\r") {
		return fmt.Errorf("keychain: path %q must not contain quotes, backslashes or newlines", path)
	}
	return nil
}

// run executes one security command, marked sensitive. When the keychain
// is locked in a session that cannot show the unlock dialog, security exits
// 36; lclaw then unlocks with the terminal attached and retries once. stdin
// is rebuilt per attempt so the retry sends the same bytes.
func (c *Client) run(ctx context.Context, path string, stdin []byte, args ...string) (exec.Result, error) {
	attempt := func() (exec.Result, error) {
		cmd := exec.Command{Name: tool, Args: args, Sensitive: true}
		if stdin != nil {
			cmd.Stdin = bytes.NewReader(stdin)
		}
		return c.Runner.Run(ctx, cmd)
	}
	res, err := attempt()
	if exitCode(err) != exitLocked {
		return res, err
	}
	if uerr := c.Runner.Interactive(ctx, exec.Command{Name: tool, Args: []string{"unlock-keychain", path}}); uerr != nil {
		return res, uerr
	}
	return attempt()
}

func exitCode(err error) int {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.Code
	}
	return -1
}

// wrap prefixes err with the adapter and operation, mapping a missing
// executable to domain.ErrToolNotFound and the known exit codes to the
// app sentinels. Anything else keeps its exit status and a stderr tail with
// every password line removed.
func wrap(op string, err error) error {
	var ee *exec.ExitError
	switch {
	case errors.Is(err, exec.ErrNotFound):
		return fmt.Errorf("keychain: %s: %w", op, domain.ErrToolNotFound)
	case errors.As(err, &ee):
		switch ee.Code {
		case exitNotFound:
			return fmt.Errorf("keychain: %s: %w", op, app.ErrSecretNotFound)
		case exitDuplicate:
			return fmt.Errorf("keychain: %s: %w", op, app.ErrSecretExists)
		case exitLocked:
			return fmt.Errorf("keychain: %s: %w", op, app.ErrKeychainLocked)
		case exitNoKeychain:
			return fmt.Errorf("keychain: %s: %w", op, app.ErrKeychainMissing)
		case exitAuthFailed:
			return fmt.Errorf("keychain: %s: %w", op, app.ErrWrongPassword)
		}
		return fmt.Errorf("keychain: %s: %w", op, &exec.ExitError{Code: ee.Code, Stderr: stripPassword(ee.Stderr)})
	}
	return fmt.Errorf("keychain: %s: %w", op, err)
}
