//go:build darwin || linux || freebsd || openbsd || netbsd

package queries

import (
	"os"
	"path/filepath"
	"syscall"
)

func lockJournal(dir string) (func(), error) {
	f, err := os.OpenFile(filepath.Join(dir, "queries.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}
