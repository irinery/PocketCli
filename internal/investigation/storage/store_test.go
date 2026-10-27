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

func TestSharedLockBlocksWriterAndAllowsReaders(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.LockShared("provider-github")
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.LockShared("provider-github")
	if err != nil {
		first()
		t.Fatal(err)
	}
	if release, err := s.Lock("provider-github"); err == nil {
		release()
		t.Fatal("writer admitted while readers are active")
	}
	first()
	second()
	writer, err := s.Lock("provider-github")
	if err != nil {
		t.Fatal(err)
	}
	if release, err := s.LockShared("provider-github"); err == nil {
		release()
		t.Fatal("reader admitted while writer is active")
	}
	writer()
}
