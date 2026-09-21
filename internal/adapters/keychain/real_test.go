package keychain

import (
	"bytes"
	"context"
	"errors"
	osexec "os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/programmablemike/localclaw/internal/adapters/exec"
	"github.com/programmablemike/localclaw/internal/app"
	"github.com/programmablemike/localclaw/internal/domain"
)

// TestRealKeychain exercises the adapter against a throwaway keychain in a
// temporary directory. The keychain is created with its password on stdin,
// the way a script would drive `lclaw init`, and deleted afterwards.
func TestRealKeychain(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS only")
	}
	if _, err := osexec.LookPath("security"); err != nil {
		t.Skip("security not on PATH")
	}
	ctx := context.Background()
	sys := &exec.System{}
	path := filepath.Join(t.TempDir(), "test.keychain-db")
	if _, err := sys.Run(ctx, exec.Command{Name: "security", Args: []string{"create-keychain", path}, Stdin: strings.NewReader("throwaway\nthrowaway\n")}); err != nil {
		t.Fatalf("create-keychain: %v", err)
	}
	t.Cleanup(func() { _, _ = sys.Run(ctx, exec.Command{Name: "security", Args: []string{"delete-keychain", path}}) })

	c := &Client{Runner: sys}
	big := bytes.Repeat([]byte{0x7f, 0x00, 0xff, 'x'}, domain.MaxSecretLen/4)
	values := map[string][]byte{
		"ascii":  []byte("hello world"),
		"binary": {0x00, 0xff, 0x0a, '"', '\\'},
		"big":    big,
	}
	for name, v := range values {
		if err := c.Put(ctx, path, name, v, domain.Generated, false); err != nil {
			t.Fatalf("put %s: %v", name, err)
		}
		got, err := c.Get(ctx, path, name)
		if err != nil || !bytes.Equal(got, v) {
			t.Fatalf("get %s = %q, %v; want %q", name, got, err, v)
		}
	}
	item, err := c.Describe(ctx, path, "ascii")
	if err != nil || item.Source != domain.Generated || item.Created.IsZero() || item.Modified.IsZero() {
		t.Fatalf("describe = %+v, %v", item, err)
	}
	if err := c.Put(ctx, path, "ascii", []byte("again"), domain.User, false); !errors.Is(err, app.ErrSecretExists) {
		t.Fatalf("second put: err = %v, want ErrSecretExists", err)
	}
	if err := c.Put(ctx, path, "ascii", []byte("again"), domain.User, true); err != nil {
		t.Fatalf("update: %v", err)
	}
	item, err = c.Describe(ctx, path, "ascii")
	if err != nil || item.Source != domain.User {
		t.Fatalf("describe after update = %+v, %v", item, err)
	}
	if got, _ := c.Get(ctx, path, "ascii"); string(got) != "again" {
		t.Fatalf("get after update = %q", got)
	}
	if err := c.Delete(ctx, path, "ascii"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := c.Get(ctx, path, "ascii"); !errors.Is(err, app.ErrSecretNotFound) {
		t.Fatalf("get after delete: err = %v, want ErrSecretNotFound", err)
	}
	if err := c.Delete(ctx, path, "ascii"); !errors.Is(err, app.ErrSecretNotFound) {
		t.Fatalf("second delete: err = %v, want ErrSecretNotFound", err)
	}
}
