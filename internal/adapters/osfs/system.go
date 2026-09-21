// Package osfs is the file-system adapter: the only place lclaw reads or
// writes files on the host. System satisfies app.FileWriter and
// app.DirOpener.
package osfs

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// System reads and writes the real file system.
type System struct{}

// OpenDir implements app.DirOpener with os.DirFS, after confirming dir is a
// directory because os.DirFS itself never fails.
func (System) OpenDir(dir string) (fs.FS, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("osfs: open %s: %w", dir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("osfs: open %s: not a directory", dir)
	}
	return os.DirFS(dir), nil
}

// WriteFile implements app.FileWriter. It creates parent directories, writes
// to a temporary name in the same directory and renames it into place, so an
// interrupted run leaves no half-written file. Without overwrite an existing
// path is left alone and reported as fs.ErrExist. The existence check and
// the rename are two steps, so a file created by another process in between
// is replaced; for a scaffold written by one user that window does not
// matter.
func (System) WriteFile(path string, data []byte, overwrite bool) error {
	if !overwrite {
		_, err := os.Lstat(path)
		if err == nil {
			return fmt.Errorf("osfs: write %s: %w", path, fs.ErrExist)
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("osfs: write %s: %w", path, err)
		}
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("osfs: write %s: %w", path, err)
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("osfs: write %s: %w", path, err)
	}
	name := tmp.Name()
	if err := fill(tmp, data); err != nil {
		os.Remove(name)
		return fmt.Errorf("osfs: write %s: %w", path, err)
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return fmt.Errorf("osfs: write %s: %w", path, err)
	}
	return nil
}

// fill writes data, sets the usual file mode (CreateTemp uses 0600) and
// closes f, returning the first error.
func fill(f *os.File, data []byte) error {
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Chmod(0o644); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
