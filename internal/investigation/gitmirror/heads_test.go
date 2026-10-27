package gitmirror

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListHeadsTracksBranchDeletionWithoutFetching(t *testing.T) {
	root := t.TempDir()
	work := filepath.Join(root, "work")
	remote := filepath.Join(root, "remote.git")
	runGit(t, root, "init", "-q", "-b", "main", work)
	runGit(t, work, "config", "user.name", "Fixture")
	runGit(t, work, "config", "user.email", "fixture@example.invalid")
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, work, "add", ".")
	runGit(t, work, "commit", "-qm", "initial")
	runGit(t, root, "init", "--bare", remote)
	runGit(t, work, "remote", "add", "origin", remote)
	runGit(t, work, "push", "origin", "main")
	runGit(t, work, "branch", "feature")
	runGit(t, work, "push", "origin", "feature")
	m, err := New(filepath.Join(root, "mirror.git"))
	if err != nil {
		t.Fatal(err)
	}
	m.allowLocal = true
	remoteURL := "file://" + remote
	refs, complete, used, err := m.ListHeads(context.Background(), remoteURL, nil, 100, 1<<20)
	if err != nil || !complete || len(refs) != 2 || used <= 0 || refs[0].Name != "refs/heads/feature" || refs[1].Name != "refs/heads/main" {
		t.Fatalf("heads iniciais=%+v complete=%v used=%d err=%v", refs, complete, used, err)
	}
	if _, _, _, err := m.ListHeads(context.Background(), remoteURL, nil, 100, 1); !errors.Is(err, ErrBudget) {
		t.Fatalf("orçamento pequeno não interrompeu listagem: %v", err)
	}
	if refs, complete, _, err := m.ListHeads(context.Background(), remoteURL, nil, 1, 1<<20); err != nil || complete || len(refs) != 1 {
		t.Fatalf("limite de refs não marcou parcial: %+v %v %v", refs, complete, err)
	}
	runGit(t, work, "push", "origin", ":feature")
	refs, complete, _, err = m.ListHeads(context.Background(), remoteURL, nil, 100, 1<<20)
	if err != nil || !complete || len(refs) != 1 || refs[0].Name != "refs/heads/main" {
		t.Fatalf("remoção de branch=%+v complete=%v err=%v", refs, complete, err)
	}
}

func TestParseHeadRefsRejectsInvalidRecordsWithoutProvingDeletion(t *testing.T) {
	oid := strings.Repeat("a", 40)
	refs, complete := parseHeadRefs([]byte(oid+"\trefs/heads/main\n"+oid+"\trefs/heads/../bad\n"), 10)
	if complete || len(refs) != 1 || refs[0].Name != "refs/heads/main" {
		t.Fatalf("registro inválido deve deixar cobertura parcial: %+v %v", refs, complete)
	}
}
