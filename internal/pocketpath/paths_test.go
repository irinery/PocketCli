package pocketpath

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureDataDirRepairsExistingPermissions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".local", "share", "pocketcli")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}

	got, err := EnsureDataDir()
	if err != nil {
		t.Fatalf("EnsureDataDir() error = %v", err)
	}
	if got != dir {
		t.Fatalf("EnsureDataDir() = %q, want %q", got, dir)
	}
	assertPrivatePath(t, dir, 0o700)
}

func TestAtomicWriteKeepsParentAndFilePrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime", "state.json")
	if err := AtomicWrite(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("AtomicWrite() error = %v", err)
	}
	assertPrivatePath(t, filepath.Dir(path), 0o700)
	assertPrivatePath(t, path, 0o600)
}

func assertPrivatePath(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(%s) error = %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("mode(%s) = %#o, want %#o", path, got, want)
	}
}
