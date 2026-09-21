package keychain

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/programmablemike/localclaw/internal/adapters/exec"
	"github.com/programmablemike/localclaw/internal/app"
	"github.com/programmablemike/localclaw/internal/domain"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// existingKeychain returns the path of an empty file standing in for a
// keychain, so the adapter's existence guard passes.
func existingKeychain(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "lclaw.keychain-db")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestExists(t *testing.T) {
	c := &Client{Runner: exec.NewFake()}
	path := existingKeychain(t)
	if ok, err := c.Exists(context.Background(), path); err != nil || !ok {
		t.Fatalf("Exists(existing) = %v, %v", ok, err)
	}
	if ok, err := c.Exists(context.Background(), filepath.Join(t.TempDir(), "nope.keychain-db")); err != nil || ok {
		t.Fatalf("Exists(missing) = %v, %v", ok, err)
	}
}

func TestPutSendsHexCommandOnStdin(t *testing.T) {
	path := existingKeychain(t)
	f := exec.NewFake()
	f.Script("security", []string{"-i"}, exec.Response{})
	c := &Client{Runner: f}
	if err := c.Put(context.Background(), path, "litellm-master-key", []byte("sk-abc"), domain.Generated, false); err != nil {
		t.Fatal(err)
	}
	want := []exec.Call{{
		Name:      "security",
		Args:      []string{"-i"},
		Stdin:     `add-generic-password -a litellm-master-key -s lclaw -D "LocalClaw secret" -j generated -X 736b2d616263 "` + path + `"` + "\n",
		Sensitive: true,
	}}
	if !reflect.DeepEqual(f.Calls, want) {
		t.Fatalf("Calls =\n%+v\nwant\n%+v", f.Calls, want)
	}
}

func TestPutReplaceAddsUpdateFlag(t *testing.T) {
	path := existingKeychain(t)
	f := exec.NewFake()
	f.Script("security", []string{"-i"}, exec.Response{})
	c := &Client{Runner: f}
	if err := c.Put(context.Background(), path, "k", []byte{0, 0xff}, domain.User, true); err != nil {
		t.Fatal(err)
	}
	want := `add-generic-password -a k -s lclaw -D "LocalClaw secret" -j user -U -X 00ff "` + path + `"` + "\n"
	if f.Calls[0].Stdin != want {
		t.Fatalf("stdin = %q, want %q", f.Calls[0].Stdin, want)
	}
}

func TestPutDuplicateIsSecretExists(t *testing.T) {
	path := existingKeychain(t)
	f := exec.NewFake()
	f.Script("security", []string{"-i"}, exec.Response{Err: &exec.ExitError{Code: 45, Stderr: "security: SecKeychainItemCreateFromContent: The specified item already exists in the keychain.\nadd-generic-password: returned -25299"}})
	err := (&Client{Runner: f}).Put(context.Background(), path, "k", []byte("v"), domain.User, false)
	if !errors.Is(err, app.ErrSecretExists) {
		t.Fatalf("err = %v, want ErrSecretExists", err)
	}
}

// TestPutRedactsHexValueOnFailure covers the case where security -i echoes
// the offending stdin line back on stderr: the hex-encoded value (and the
// raw value it decodes to) must never reach the wrapped error, while an
// unrelated diagnostic line and the exit status survive.
func TestPutRedactsHexValueOnFailure(t *testing.T) {
	path := existingKeychain(t)
	f := exec.NewFake()
	stdinLine := `add-generic-password -a k -s lclaw -D "LocalClaw secret" -j user -X 736b2d616263 "` + path + `"` + "\n"
	f.Script("security", []string{"-i"}, exec.Response{Err: &exec.ExitError{
		Code:   1,
		Stderr: stdinLine + "security: some diagnostic message\n",
	}})
	err := (&Client{Runner: f}).Put(context.Background(), path, "k", []byte("sk-abc"), domain.User, false)
	if err == nil {
		t.Fatal("want an error")
	}
	msg := err.Error()
	if strings.Contains(msg, "736b2d616263") {
		t.Fatalf("error leaks the hex value: %s", msg)
	}
	if strings.Contains(msg, "sk-abc") {
		t.Fatalf("error leaks the raw value: %s", msg)
	}
	if !strings.Contains(msg, "some diagnostic message") {
		t.Fatalf("error dropped the diagnostic line: %s", msg)
	}
	if !strings.Contains(msg, "exit status 1") {
		t.Fatalf("error dropped the exit status: %s", msg)
	}
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.Code != 1 {
		t.Fatalf("errors.As = %v, %v, want an *exec.ExitError{Code: 1}", ee, err)
	}
}

func TestPutRefusesMissingKeychain(t *testing.T) {
	// security add-generic-password with a missing keychain path writes to
	// the default keychain and exits 0. The adapter must never get there.
	f := exec.NewFake()
	err := (&Client{Runner: f}).Put(context.Background(), filepath.Join(t.TempDir(), "missing.keychain-db"), "k", []byte("v"), domain.User, false)
	if !errors.Is(err, app.ErrKeychainMissing) {
		t.Fatalf("err = %v, want ErrKeychainMissing", err)
	}
	if len(f.Calls) != 0 {
		t.Fatalf("security must not be run: %+v", f.Calls)
	}
}

func TestPutRejectsBadNameAndPath(t *testing.T) {
	path := existingKeychain(t)
	f := exec.NewFake()
	c := &Client{Runner: f}
	if err := c.Put(context.Background(), path, "Bad Name", []byte("v"), domain.User, false); !errors.Is(err, domain.ErrInvalidName) {
		t.Fatalf("bad name: err = %v", err)
	}
	quoted := filepath.Join(t.TempDir(), `we"ird.keychain-db`)
	if err := os.WriteFile(quoted, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := c.Put(context.Background(), quoted, "k", []byte("v"), domain.User, false); err == nil || !strings.Contains(err.Error(), "must not contain") {
		t.Fatalf("quoted path: err = %v", err)
	}
	if len(f.Calls) != 0 {
		t.Fatalf("security must not be run: %+v", f.Calls)
	}
}

func TestPutRefusesAnInvalidValue(t *testing.T) {
	tests := []struct {
		name  string
		value []byte
	}{
		{"empty", []byte{}},
		{"too long", bytes.Repeat([]byte("a"), domain.MaxSecretLen+1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := existingKeychain(t)
			f := exec.NewFake()
			c := &Client{Runner: f}
			if err := c.Put(context.Background(), path, "k", tt.value, domain.User, false); !errors.Is(err, domain.ErrInvalidValue) {
				t.Fatalf("err = %v, want ErrInvalidValue", err)
			}
			if len(f.Calls) != 0 {
				t.Fatalf("security must not be run: %+v", f.Calls)
			}
		})
	}
}

func TestGetParsesBothOutputForms(t *testing.T) {
	tests := []struct {
		name   string
		stderr string
		want   []byte
	}{
		{"ascii", fixture(t, "find-g-ascii.stderr"), []byte("hello")},
		{"hex", fixture(t, "find-g-hex.stderr"), []byte{0x00, 0xff, 0x0a}},
		{"hex with rendering", fixture(t, "find-g-hex-rendered.stderr"), []byte(`he said "hi" \`)},
		{"embedded quote", "password: \"a\"b\"\n", []byte(`a"b`)},
		{"leading and trailing spaces", "password: \" a \"\n", []byte(" a ")},
		{"empty", "password: \n", []byte{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := existingKeychain(t)
			f := exec.NewFake()
			f.Script("security", []string{"find-generic-password", "-a", "demo", "-s", "lclaw", "-g", path}, exec.Response{Stdout: fixture(t, "find-attrs.stdout"), Stderr: tt.stderr})
			got, err := (&Client{Runner: f}).Get(context.Background(), path, "demo")
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, tt.want) {
				t.Fatalf("Get() = %q, want %q", got, tt.want)
			}
			if !f.Calls[0].Sensitive {
				t.Fatal("find-generic-password must be sensitive")
			}
		})
	}
}

func TestGetNoPasswordLine(t *testing.T) {
	path := existingKeychain(t)
	f := exec.NewFake()
	f.Script("security", []string{"find-generic-password", "-a", "demo", "-s", "lclaw", "-g", path}, exec.Response{Stdout: fixture(t, "find-attrs.stdout")})
	if _, err := (&Client{Runner: f}).Get(context.Background(), path, "demo"); err == nil {
		t.Fatal("want error when no password line is present")
	}
}

func TestExitCodesMapToSentinels(t *testing.T) {
	tests := []struct {
		code int
		want error
	}{
		{44, app.ErrSecretNotFound},
		{45, app.ErrSecretExists},
		{36, app.ErrKeychainLocked},
		{50, app.ErrKeychainMissing},
		{51, app.ErrWrongPassword},
	}
	for _, tt := range tests {
		path := existingKeychain(t)
		f := exec.NewFake()
		args := []string{"find-generic-password", "-a", "demo", "-s", "lclaw", "-g", path}
		f.Script("security", args, exec.Response{Err: &exec.ExitError{Code: tt.code, Stderr: "security: some message"}})
		// 36 triggers an unlock attempt; script it to succeed and the retry to fail again.
		f.Script("security", []string{"unlock-keychain", path}, exec.Response{})
		_, err := (&Client{Runner: f}).Get(context.Background(), path, "demo")
		if !errors.Is(err, tt.want) {
			t.Errorf("exit %d: err = %v, want %v", tt.code, err, tt.want)
		}
	}
}

func TestUnknownExitStripsPasswordLines(t *testing.T) {
	path := existingKeychain(t)
	f := exec.NewFake()
	f.Script("security", []string{"find-generic-password", "-a", "demo", "-s", "lclaw", "-g", path},
		exec.Response{Err: &exec.ExitError{Code: 1, Stderr: "password: \"hunter2\"\nsecurity: something else went wrong"}})
	_, err := (&Client{Runner: f}).Get(context.Background(), path, "demo")
	if err == nil || strings.Contains(err.Error(), "hunter2") {
		t.Fatalf("err = %v, must not carry the value", err)
	}
	if !strings.Contains(err.Error(), "keychain: get demo: exit status 1: security: something else went wrong") {
		t.Fatalf("err = %v", err)
	}
}

func TestLockedKeychainIsUnlockedAndRetriedOnce(t *testing.T) {
	path := existingKeychain(t)
	f := exec.NewFake()
	args := []string{"find-generic-password", "-a", "demo", "-s", "lclaw", "-g", path}
	f.Script("security", args,
		exec.Response{Err: &exec.ExitError{Code: 36, Stderr: "User interaction is not allowed."}},
		exec.Response{Stdout: fixture(t, "find-attrs.stdout"), Stderr: fixture(t, "find-g-ascii.stderr")})
	f.Script("security", []string{"unlock-keychain", path}, exec.Response{})
	got, err := (&Client{Runner: f}).Get(context.Background(), path, "demo")
	if err != nil || string(got) != "hello" {
		t.Fatalf("Get() = %q, %v", got, err)
	}
	if len(f.Calls) != 3 || !f.Calls[1].Interactive || f.Calls[1].Args[0] != "unlock-keychain" {
		t.Fatalf("calls = %+v, want find, interactive unlock, find", f.Calls)
	}
}

func TestUnlockFailureIsReported(t *testing.T) {
	path := existingKeychain(t)
	f := exec.NewFake()
	args := []string{"find-generic-password", "-a", "demo", "-s", "lclaw", "-g", path}
	f.Script("security", args, exec.Response{Err: &exec.ExitError{Code: 36}})
	f.Script("security", []string{"unlock-keychain", path}, exec.Response{Err: &exec.ExitError{Code: 51}})
	_, err := (&Client{Runner: f}).Get(context.Background(), path, "demo")
	if !errors.Is(err, app.ErrWrongPassword) {
		t.Fatalf("err = %v, want ErrWrongPassword", err)
	}
}

func TestDescribeParsesAttributes(t *testing.T) {
	path := existingKeychain(t)
	f := exec.NewFake()
	f.Script("security", []string{"find-generic-password", "-a", "demo", "-s", "lclaw", path}, exec.Response{Stdout: fixture(t, "find-attrs.stdout")})
	got, err := (&Client{Runner: f}).Describe(context.Background(), path, "demo")
	if err != nil {
		t.Fatal(err)
	}
	want := domain.SecretItem{
		Name:     "demo",
		Source:   domain.User,
		Created:  time.Date(2026, 9, 21, 7, 3, 45, 0, time.UTC),
		Modified: time.Date(2026, 9, 21, 7, 12, 39, 0, time.UTC),
	}
	if !got.Created.Equal(want.Created) || !got.Modified.Equal(want.Modified) || got.Name != want.Name || got.Source != want.Source {
		t.Fatalf("Describe() = %+v, want %+v", got, want)
	}
}

func TestDescribeNullCommentIsUser(t *testing.T) {
	path := existingKeychain(t)
	f := exec.NewFake()
	f.Script("security", []string{"find-generic-password", "-a", "demo", "-s", "lclaw", path}, exec.Response{Stdout: fixture(t, "find-attrs-null.stdout")})
	got, err := (&Client{Runner: f}).Describe(context.Background(), path, "demo")
	if err != nil || got.Source != domain.User {
		t.Fatalf("Describe() = %+v, %v", got, err)
	}
}

func TestDescribeNotFound(t *testing.T) {
	path := existingKeychain(t)
	f := exec.NewFake()
	f.Script("security", []string{"find-generic-password", "-a", "demo", "-s", "lclaw", path}, exec.Response{Err: &exec.ExitError{Code: 44}})
	if _, err := (&Client{Runner: f}).Describe(context.Background(), path, "demo"); !errors.Is(err, app.ErrSecretNotFound) {
		t.Fatalf("err = %v, want ErrSecretNotFound", err)
	}
}

func TestDelete(t *testing.T) {
	path := existingKeychain(t)
	f := exec.NewFake()
	f.Script("security", []string{"delete-generic-password", "-a", "demo", "-s", "lclaw", path}, exec.Response{Stdout: "password has been deleted.\n"})
	if err := (&Client{Runner: f}).Delete(context.Background(), path, "demo"); err != nil {
		t.Fatal(err)
	}
	f.Script("security", []string{"delete-generic-password", "-a", "demo", "-s", "lclaw", path}, exec.Response{Err: &exec.ExitError{Code: 44}})
	if err := (&Client{Runner: f}).Delete(context.Background(), path, "demo"); !errors.Is(err, app.ErrSecretNotFound) {
		t.Fatalf("err = %v, want ErrSecretNotFound", err)
	}
}

func TestCreateRunsInteractivelyThenSetsLockSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new", "lclaw.keychain-db")
	f := exec.NewFake()
	f.Script("security", []string{"create-keychain", path}, exec.Response{})
	f.Script("security", []string{"set-keychain-settings", "-l", "-u", "-t", "900", path}, exec.Response{})
	if err := (&Client{Runner: f}).Create(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	want := []exec.Call{
		{Name: "security", Args: []string{"create-keychain", path}, Interactive: true},
		{Name: "security", Args: []string{"set-keychain-settings", "-l", "-u", "-t", "900", path}, Sensitive: true},
	}
	if !reflect.DeepEqual(f.Calls, want) {
		t.Fatalf("Calls =\n%+v\nwant\n%+v", f.Calls, want)
	}
	if _, err := os.Stat(filepath.Dir(path)); err != nil {
		t.Fatalf("parent directory not created: %v", err)
	}
}

func TestToolNotFound(t *testing.T) {
	path := existingKeychain(t)
	f := exec.NewFake()
	f.Script("security", []string{"find-generic-password", "-a", "demo", "-s", "lclaw", "-g", path}, exec.Response{Err: exec.ErrNotFound})
	if _, err := (&Client{Runner: f}).Get(context.Background(), path, "demo"); !errors.Is(err, domain.ErrToolNotFound) {
		t.Fatalf("err = %v, want ErrToolNotFound", err)
	}
}
