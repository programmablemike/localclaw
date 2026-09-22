package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
)

// Init writes the default scaffold into a directory the user owns and
// creates the keychain its topology names.
type Init struct {
	Defaults fs.FS // the default files, paths relative to the scaffold root
	Writer   FileWriter
	Scaffold DirOpener
	Topology TopologyLoader
	Keychain Keychain
}

// InitReport says what Init did with each default file. Paths are
// slash-separated and relative to Dir, in the order they were visited.
type InitReport struct {
	Dir      string
	Written  []string
	Skipped  []string
	Failed   []InitFailure
	Keychain KeychainOutcome
}

// InitFailure is one file that could not be written.
type InitFailure struct {
	Path string
	Err  error
}

// Ok reports whether nothing failed. A skipped file or an existing
// keychain is success.
func (r InitReport) Ok() bool {
	return len(r.Failed) == 0 && r.Keychain.State != KeychainFailed
}

// KeychainState is what Init did about the keychain. The strings are part
// of the CLI's interface.
type KeychainState string

const (
	KeychainCreated KeychainState = "created"
	KeychainSkipped KeychainState = "skipped"
	KeychainFailed  KeychainState = "failed"
)

// KeychainOutcome reports the keychain step. Path is empty when the
// topology could not be read, because the path is what it would have said.
type KeychainOutcome struct {
	Path  string
	State KeychainState
	Err   error
}

// Run writes every default file whose path does not yet exist under dir,
// skips the ones that do, and overwrites them instead when force is set. It
// continues past failures so the report is complete. The returned error is
// non-nil only when the context ends or the defaults cannot be read:
// everything a user can fix is in the report.
func (i *Init) Run(ctx context.Context, dir string, force bool) (InitReport, error) {
	r := InitReport{Dir: dir}
	err := fs.WalkDir(i.Defaults, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		data, err := fs.ReadFile(i.Defaults, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dir, filepath.FromSlash(p))
		switch err := i.Writer.WriteFile(target, data, force); {
		case err == nil:
			r.Written = append(r.Written, p)
		case errors.Is(err, fs.ErrExist):
			r.Skipped = append(r.Skipped, p)
		default:
			r.Failed = append(r.Failed, InitFailure{Path: p, Err: err})
		}
		return nil
	})
	if err != nil {
		return r, fmt.Errorf("init: %w", err)
	}
	r.Keychain = i.keychainStep(ctx, dir)
	if ctx.Err() != nil {
		return r, ctx.Err()
	}
	return r, nil
}

// keychainStep creates the keychain the topology names, if it does not
// already exist. force is deliberately not a parameter: overwriting a
// keychain destroys every secret in it, so an existing one is always
// skipped. A failure here is reported, not returned, so the file report
// still reaches the user. The topology is loaded after the files are
// written, so the keychain path comes from whichever lclaw.toml now
// exists, including one init itself just wrote.
func (i *Init) keychainStep(ctx context.Context, dir string) KeychainOutcome {
	fsys, err := i.Scaffold.OpenDir(dir)
	if err != nil {
		return KeychainOutcome{State: KeychainFailed, Err: fmt.Errorf("init: topology: %w", err)}
	}
	top, err := i.Topology.Load(fsys)
	if err != nil {
		return KeychainOutcome{State: KeychainFailed, Err: fmt.Errorf("init: topology: %w", err)}
	}
	out := KeychainOutcome{Path: top.KeychainPath}
	exists, err := i.Keychain.Exists(ctx, top.KeychainPath)
	switch {
	case err != nil:
		out.State, out.Err = KeychainFailed, fmt.Errorf("init: keychain: %w", err)
	case exists:
		out.State = KeychainSkipped
	default:
		if err := i.Keychain.Create(ctx, top.KeychainPath); err != nil {
			out.State, out.Err = KeychainFailed, fmt.Errorf("init: keychain: %w", err)
		} else {
			out.State = KeychainCreated
		}
	}
	return out
}
