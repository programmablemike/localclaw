package osfs

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteFileCreatesParentsAndContent(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "workloads", "openclaw", "Containerfile")
	if err := (System{}).WriteFile(path, []byte("FROM x\n"), false); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "FROM x\n" {
		t.Errorf("content = %q", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Errorf("mode = %o, want 644", info.Mode().Perm())
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "Containerfile" {
		t.Errorf("directory holds %v, want only Containerfile (no temporary file left behind)", entries)
	}
}

func TestWriteFileRefusesToOverwrite(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "lclaw.toml")
	if err := os.WriteFile(path, []byte("user edit"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := (System{}).WriteFile(path, []byte("default"), false)
	if !errors.Is(err, fs.ErrExist) {
		t.Fatalf("err = %v, want fs.ErrExist", err)
	}
	if !strings.HasPrefix(err.Error(), "osfs: write "+path+": ") {
		t.Errorf("err = %q, want the osfs prefix and the path", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "user edit" {
		t.Errorf("content = %q, want unchanged", got)
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 1 {
		t.Errorf("directory holds %v, want only lclaw.toml", entries)
	}
}

func TestWriteFileOverwritesWhenAsked(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "lclaw.toml")
	if err := os.WriteFile(path, []byte("user edit"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := (System{}).WriteFile(path, []byte("default"), true); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "default" {
		t.Errorf("content = %q, want the new content", got)
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 1 {
		t.Errorf("directory holds %v, want only lclaw.toml", entries)
	}
}

func TestWriteFileRefusesADirectoryTarget(t *testing.T) {
	for _, overwrite := range []bool{false, true} {
		root := t.TempDir()
		path := filepath.Join(root, "lclaw.toml")
		if err := os.Mkdir(path, 0o755); err != nil {
			t.Fatal(err)
		}
		err := (System{}).WriteFile(path, []byte("default"), overwrite)
		if err == nil {
			t.Fatalf("overwrite=%v: expected an error when the target is a directory", overwrite)
		}
		if !strings.HasSuffix(err.Error(), "is a directory") {
			t.Errorf("overwrite=%v: err = %q, want it to end with \"is a directory\"", overwrite, err)
		}
		if errors.Is(err, fs.ErrExist) {
			t.Errorf("overwrite=%v: err = %v must not look like an existing file", overwrite, err)
		}
		info, statErr := os.Stat(path)
		if statErr != nil {
			t.Fatalf("overwrite=%v: %v", overwrite, statErr)
		}
		if !info.IsDir() {
			t.Errorf("overwrite=%v: path is no longer a directory", overwrite)
		}
		entries, err := os.ReadDir(root)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 || entries[0].Name() != "lclaw.toml" {
			t.Errorf("overwrite=%v: directory holds %v, want only lclaw.toml (no temporary file left behind)", overwrite, entries)
		}
	}
}

func TestWriteFileReportsOtherErrors(t *testing.T) {
	root := t.TempDir()
	blocker := filepath.Join(root, "workloads")
	if err := os.WriteFile(blocker, []byte("a file where a directory must go"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := (System{}).WriteFile(filepath.Join(blocker, "openclaw", "pod.yaml"), []byte("x"), false)
	if err == nil {
		t.Fatal("expected an error when the parent is a file")
	}
	if errors.Is(err, fs.ErrExist) {
		t.Fatalf("err = %v must not look like an existing file", err)
	}
}

func TestOpenDir(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "lclaw.toml"), []byte("schema = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fsys, err := (System{}).OpenDir(root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := fs.ReadFile(fsys, "lclaw.toml")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "schema = 1\n" {
		t.Errorf("content = %q", got)
	}
}

func TestOpenDirMissing(t *testing.T) {
	_, err := (System{}).OpenDir(filepath.Join(t.TempDir(), "absent"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want fs.ErrNotExist", err)
	}
	if !strings.HasPrefix(err.Error(), "osfs: open ") {
		t.Errorf("err = %q, want the osfs prefix", err)
	}
}

func TestOpenDirOnAFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := (System{}).OpenDir(path)
	if err == nil || errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want a not-a-directory error", err)
	}
	if !strings.HasSuffix(err.Error(), "not a directory") {
		t.Errorf("err = %q", err)
	}
}
