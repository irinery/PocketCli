//go:build !(darwin || linux || freebsd || openbsd || netbsd)

package queries

import "errors"

func lockJournal(string) (func(), error) {
	return nil, errors.New("query journal locking unavailable on this platform")
}
