package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
)

// Init writes the default scaffold into a directory the user owns.
type Init struct {
	Defaults fs.FS // the default files, paths relative to the scaffold root
	Writer   FileWriter
}

// InitReport says what Init did with each default file. Paths are
// slash-separated and relative to Dir, in the order they were visited.
type InitReport struct {
	Dir     string
	Written []string
	Skipped []string
	Failed  []InitFailure
}

// InitFailure is one file that could not be written.
type InitFailure struct {
	Path string
	Err  error
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
	return r, nil
}
