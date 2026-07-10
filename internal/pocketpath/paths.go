package pocketpath

import (
	"os"
	"path/filepath"
	"strings"
)

const privateDirMode = 0o700

func HomeDir() (string, error) {
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		return home, nil
	}
	return os.UserHomeDir()
}

func CodeDir() (string, error) {
	home, err := HomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".pocketcli"), nil
}

func ConfigDir() (string, error) {
	home, err := HomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "pocketcli"), nil
}

func DataDir() (string, error) {
	home, err := HomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "pocketcli"), nil
}

func CacheDir() (string, error) {
	home, err := HomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cache", "pocketcli"), nil
}

func EnsureDataDir() (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	if err := EnsurePrivateDir(dir); err != nil {
		return "", err
	}
	return dir, nil
}

func EnsureConfigDir() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	if err := EnsurePrivateDir(dir); err != nil {
		return "", err
	}
	return dir, nil
}

// EnsurePrivateDir creates a user-owned runtime directory and repairs modes
// left permissive by older PocketCli releases.
func EnsurePrivateDir(dir string) error {
	if err := os.MkdirAll(dir, privateDirMode); err != nil {
		return err
	}
	return os.Chmod(dir, privateDirMode)
}

func AtomicWrite(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := EnsurePrivateDir(dir); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		_ = os.Remove(tmpName)
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	return os.Chmod(path, perm)
}
