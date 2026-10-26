package gitmirror

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestMirrorDetectsAdvanceAndDivergenceWithoutRunningRepositoryCode(t *testing.T) {
	root := t.TempDir()
	work := filepath.Join(root, "work")
	remote := filepath.Join(root, "remote.git")
	if err := os.Mkdir(work, 0o700); err != nil {
		t.Fatal(err)
	}
	runGit(t, work, "init", "-b", "main")
	runGit(t, work, "config", "user.name", "Fixture")
	runGit(t, work, "config", "user.email", "fixture@example.invalid")
	if err := os.WriteFile(filepath.Join(work, ".gitconfig"), []byte("[alias]\n  malicious = !touch "+filepath.Join(root, "executed")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, work, "add", ".gitconfig")
	runGit(t, work, "commit", "-m", "initial")
	initial := runGit(t, work, "rev-parse", "HEAD")
	runGit(t, root, "init", "--bare", remote)
	runGit(t, work, "remote", "add", "origin", remote)
	runGit(t, work, "push", "origin", "main")
	runGit(t, remote, "symbolic-ref", "HEAD", "refs/heads/main")
	m, err := New(filepath.Join(root, "mirror.git"))
	if err != nil {
		t.Fatal(err)
	}
	m.allowLocal = true
	remoteURL := "file://" + remote
	if _, err := m.Observe(context.Background(), remoteURL, nil, RefState{}, 1); !errors.Is(err, ErrBudget) {
		t.Fatalf("tiny Git budget err=%v", err)
	}
	first, err := m.Observe(context.Background(), remoteURL, nil, RefState{}, 10*1024*1024)
	if err != nil || first.NewOID != initial || !oidPattern.MatchString(first.TreeOID) || first.Relationship != "initial" || !first.GraphComplete {
		t.Fatalf("initial=%+v err=%v", first, err)
	}
	second, err := m.Observe(context.Background(), remoteURL, nil, RefState{Ref: first.Ref, OID: first.NewOID}, 10*1024*1024)
	if err != nil || second.Relationship != "unchanged" || second.LocalBytes != 0 {
		t.Fatalf("unchanged=%+v err=%v", second, err)
	}
	if err := os.WriteFile(filepath.Join(work, "change.txt"), []byte("advance"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, work, "add", "change.txt")
	runGit(t, work, "commit", "-m", "advance")
	runGit(t, work, "push", "origin", "main")
	third, err := m.Observe(context.Background(), remoteURL, nil, RefState{Ref: first.Ref, OID: first.NewOID}, 10*1024*1024)
	if err != nil || third.Relationship != "advanced" || third.NewOID == initial {
		t.Fatalf("advance=%+v err=%v", third, err)
	}
	var commits []Commit
	complete, err := m.WalkCommits(context.Background(), third.NewOID, func(commit Commit) (bool, error) {
		commits = append(commits, commit)
		return true, nil
	})
	if err != nil || !complete || len(commits) != 2 || commits[0].OID != third.NewOID || commits[1].OID != initial || commits[0].CommittedAt == "" {
		t.Fatalf("grafo de commits=%+v complete=%v err=%v", commits, complete, err)
	}
	commits = nil
	complete, err = m.WalkCommits(context.Background(), third.NewOID, func(commit Commit) (bool, error) {
		commits = append(commits, commit)
		return false, nil
	})
	if err != nil || complete || len(commits) != 1 || commits[0].OID != third.NewOID {
		t.Fatalf("interrupção do walk=%+v complete=%v err=%v", commits, complete, err)
	}
	if _, err := m.WalkCommits(context.Background(), "--all", func(Commit) (bool, error) { return true, nil }); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("OID inválido aceito: %v", err)
	}
	runGit(t, work, "reset", "--hard", initial)
	if err := os.WriteFile(filepath.Join(work, "change.txt"), []byte("rewritten"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, work, "add", "change.txt")
	runGit(t, work, "commit", "-m", "rewrite")
	runGit(t, work, "push", "--force", "origin", "main")
	fourth, err := m.Observe(context.Background(), remoteURL, nil, RefState{Ref: third.Ref, OID: third.NewOID}, 10*1024*1024)
	if err != nil || fourth.Relationship != "ref_diverged" || !fourth.GraphComplete {
		t.Fatalf("divergence=%+v err=%v", fourth, err)
	}
	if _, err := os.Stat(filepath.Join(root, "executed")); !os.IsNotExist(err) {
		t.Fatalf("repository code executed: %v", err)
	}
	if _, err := m.command(context.Background(), "", nil, "cat-file", "-e", third.NewOID+"^{commit}"); err != nil {
		t.Fatal("old commit was lost after divergence")
	}
	if err := os.WriteFile(filepath.Join(m.Path, "shallow"), []byte(fourth.NewOID+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, work, "reset", "--hard", initial)
	if err := os.WriteFile(filepath.Join(work, "change.txt"), []byte("another rewrite"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, work, "add", "change.txt")
	runGit(t, work, "commit", "-m", "rewrite again")
	runGit(t, work, "push", "--force", "origin", "main")
	fifth, err := m.Observe(context.Background(), remoteURL, nil, RefState{Ref: fourth.Ref, OID: fourth.NewOID}, 10*1024*1024)
	if err != nil || fifth.Relationship != "unknown" || fifth.GraphComplete {
		t.Fatalf("shallow ancestry=%+v err=%v", fifth, err)
	}
}

func TestMirrorRejectsUnapprovedRemote(t *testing.T) {
	m, err := New(filepath.Join(t.TempDir(), "mirror.git"))
	if err != nil {
		t.Fatal(err)
	}
	for _, remote := range []string{"file:///tmp/repo.git", "https://evil.invalid/a/b.git", "https://github.com/a/b.git?token=x", "https://github.com/a/../../private.git"} {
		if _, err := m.Observe(context.Background(), remote, nil, RefState{}, 1000); err == nil {
			t.Fatalf("accepted remote %q", remote)
		}
	}
}
