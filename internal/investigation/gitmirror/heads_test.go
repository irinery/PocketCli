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

func TestObserveHeadTracksAdvanceDivergenceAndMovedRef(t *testing.T) {
	root := t.TempDir()
	work := filepath.Join(root, "work")
	remote := filepath.Join(root, "remote.git")
	runGit(t, root, "init", "-q", "-b", "main", work)
	runGit(t, work, "config", "user.name", "Fixture")
	runGit(t, work, "config", "user.email", "fixture@example.invalid")
	if err := os.WriteFile(filepath.Join(work, "go.mod"), []byte("module example.invalid/app\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, work, "add", ".")
	runGit(t, work, "commit", "-qm", "root")
	rootOID := runGit(t, work, "rev-parse", "HEAD")
	runGit(t, root, "init", "-q", "--bare", remote)
	runGit(t, remote, "config", "uploadpack.allowFilter", "true")
	runGit(t, work, "remote", "add", "origin", remote)
	runGit(t, work, "push", "-q", "origin", "main")
	runGit(t, remote, "symbolic-ref", "HEAD", "refs/heads/main")
	runGit(t, work, "checkout", "-qb", "feature")
	if err := os.WriteFile(filepath.Join(work, "feature.txt"), []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, work, "add", "feature.txt")
	runGit(t, work, "commit", "-qm", "feature")
	firstOID := runGit(t, work, "rev-parse", "HEAD")
	runGit(t, work, "push", "-q", "origin", "feature")
	m, err := New(filepath.Join(root, "mirror.git"))
	if err != nil {
		t.Fatal(err)
	}
	m.allowLocal = true
	remoteURL := "file://" + remote
	if _, err := m.Observe(context.Background(), remoteURL, nil, RefState{}, 16<<20); err != nil {
		t.Fatal(err)
	}
	ref := "refs/heads/feature"
	first, err := m.ObserveHead(context.Background(), remoteURL, nil, HeadRef{Name: ref, OID: firstOID}, RefState{}, 16<<20)
	if err != nil || first.Relationship != "initial" || !first.GraphComplete || !oidPattern.MatchString(first.TreeOID) {
		t.Fatalf("branch inicial: %+v %v", first, err)
	}
	if err := os.WriteFile(filepath.Join(work, "feature.txt"), []byte("two"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, work, "commit", "-qam", "advance")
	secondOID := runGit(t, work, "rev-parse", "HEAD")
	runGit(t, work, "push", "-q", "origin", "feature")
	second, err := m.ObserveHead(context.Background(), remoteURL, nil, HeadRef{Name: ref, OID: secondOID}, RefState{Ref: ref, OID: firstOID}, 16<<20)
	if err != nil || second.Relationship != "advanced" || second.OldOID != firstOID {
		t.Fatalf("avanço da branch: %+v %v", second, err)
	}
	runGit(t, work, "reset", "--hard", rootOID)
	if err := os.WriteFile(filepath.Join(work, "feature.txt"), []byte("rewritten"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, work, "add", "feature.txt")
	runGit(t, work, "commit", "-qm", "rewrite")
	thirdOID := runGit(t, work, "rev-parse", "HEAD")
	runGit(t, work, "push", "-q", "--force", "origin", "feature")
	third, err := m.ObserveHead(context.Background(), remoteURL, nil, HeadRef{Name: ref, OID: thirdOID}, RefState{Ref: ref, OID: secondOID}, 16<<20)
	if err != nil || third.Relationship != "ref_diverged" {
		t.Fatalf("divergência da branch: %+v %v", third, err)
	}
	if _, err := m.command(context.Background(), "", nil, "cat-file", "-e", secondOID+"^{commit}"); err != nil {
		t.Fatal("commit anterior foi perdido")
	}
	if err := os.WriteFile(filepath.Join(work, "feature.txt"), []byte("moved once"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, work, "commit", "-qam", "move once")
	listedOID := runGit(t, work, "rev-parse", "HEAD")
	runGit(t, work, "push", "-q", "origin", "feature")
	if err := os.WriteFile(filepath.Join(work, "feature.txt"), []byte("moved twice"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, work, "commit", "-qam", "move twice")
	runGit(t, work, "push", "-q", "origin", "feature")
	if _, err := m.ObserveHead(context.Background(), remoteURL, nil, HeadRef{Name: ref, OID: listedOID}, RefState{Ref: ref, OID: thirdOID}, 16<<20); !errors.Is(err, ErrMoved) {
		t.Fatalf("ref mudou depois da listagem: %v", err)
	}
}
