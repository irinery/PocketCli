package gitmirror

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsAncestorUsesOnlyPresentCompleteCommitGraph(t *testing.T) {
	work := t.TempDir()
	runGit(t, work, "init", "-q", "-b", "main")
	runGit(t, work, "config", "user.name", "Fixture")
	runGit(t, work, "config", "user.email", "fixture@example.invalid")
	if err := os.WriteFile(filepath.Join(work, "app.txt"), []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, work, "add", "app.txt")
	runGit(t, work, "commit", "-qm", "root")
	root := runGit(t, work, "rev-parse", "HEAD")
	runGit(t, work, "commit", "--allow-empty", "-qm", "fix")
	fix := runGit(t, work, "rev-parse", "HEAD")
	runGit(t, work, "branch", "fixpoint", fix)
	runGit(t, work, "commit", "--allow-empty", "-qm", "delivery")
	delivery := runGit(t, work, "rev-parse", "HEAD")
	m := &Mirror{Path: filepath.Join(work, ".git"), GitPath: "git", allowLocal: true}
	if yes, known, err := m.IsAncestor(context.Background(), fix, delivery); err != nil || !known || !yes {
		t.Fatalf("contained fix: yes=%t known=%t err=%v", yes, known, err)
	}
	if yes, known, err := m.IsAncestor(context.Background(), delivery, fix); err != nil || !known || yes {
		t.Fatalf("newer commit excluded from old revision: yes=%t known=%t err=%v", yes, known, err)
	}
	if yes, known, err := m.IsAncestor(context.Background(), root, root); err != nil || !known || !yes {
		t.Fatalf("identical commit: yes=%t known=%t err=%v", yes, known, err)
	}
	if yes, known, err := m.IsAncestor(context.Background(), fix, strings.Repeat("f", 40)); err != nil || known || yes {
		t.Fatalf("missing commit became negative proof: yes=%t known=%t err=%v", yes, known, err)
	}
	if _, _, err := m.IsAncestor(context.Background(), "--all", delivery); err != ErrCorrupt {
		t.Fatalf("invalid OID accepted: %v", err)
	}
	shallow := filepath.Join(t.TempDir(), "shallow.git")
	runGit(t, t.TempDir(), "clone", "-q", "--bare", "--depth=1", "file://"+work, shallow)
	runGit(t, shallow, "fetch", "-q", "--depth=1", "file://"+work, "refs/heads/fixpoint:refs/heads/fixpoint")
	shallowMirror := &Mirror{Path: shallow, GitPath: "git", allowLocal: true}
	if yes, known, err := shallowMirror.IsAncestor(context.Background(), fix, delivery); err != nil || known || yes {
		t.Fatalf("shallow graph treated as complete: yes=%t known=%t err=%v", yes, known, err)
	}
}
