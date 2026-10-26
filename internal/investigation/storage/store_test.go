package storage

import "testing"

func TestExclusiveLock(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	release, err := s.Lock("investigation-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Lock("investigation-1"); err == nil {
		t.Fatal("second writer admitted")
	}
	release()
	release, err = s.Lock("investigation-1")
	if err != nil {
		t.Fatal(err)
	}
	release()
}
