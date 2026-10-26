package storage

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"syscall"

	"pocketcli/internal/pocketpath"
)

type Store struct{ Root string }

var safeID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func Open(root string) (*Store, error) {
	if root == "" {
		data, err := pocketpath.DataDir()
		if err != nil {
			return nil, err
		}
		root = filepath.Join(data, "investigations")
	}
	if err := pocketpath.EnsurePrivateDir(root); err != nil {
		return nil, err
	}
	return &Store{Root: root}, nil
}

func ID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
func Hash(data []byte) string { h := sha256.Sum256(data); return hex.EncodeToString(h[:]) }

func (s *Store) Path(kind, id string) (string, error) {
	if !safeID.MatchString(kind) || !safeID.MatchString(id) {
		return "", errors.New("invalid storage identifier")
	}
	return filepath.Join(s.Root, kind, id+".json"), nil
}
func (s *Store) Put(kind, id string, value interface{}) error {
	p, err := s.Path(kind, id)
	if err != nil {
		return err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	dir := filepath.Dir(p)
	if err := pocketpath.EnsurePrivateDir(dir); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".write-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.Write(append(data, '\n')); err != nil {
		f.Close()
		return err
	}
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, p); err != nil {
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
func (s *Store) Get(kind, id string, value interface{}) error {
	p, err := s.Path(kind, id)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, value)
}
func (s *Store) List(kind string) ([]string, error) {
	if !safeID.MatchString(kind) {
		return nil, errors.New("invalid storage kind")
	}
	entries, err := os.ReadDir(filepath.Join(s.Root, kind))
	if os.IsNotExist(err) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if filepath.Ext(name) == ".json" {
			id := name[:len(name)-5]
			if safeID.MatchString(id) {
				out = append(out, id)
			}
		}
	}
	return out, nil
}

func (s *Store) Lock(id string) (func(), error) {
	path, err := s.Path("locks", id)
	if err != nil {
		return nil, err
	}
	if err := pocketpath.EnsurePrivateDir(filepath.Dir(path)); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, err
	}
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }, nil
}
