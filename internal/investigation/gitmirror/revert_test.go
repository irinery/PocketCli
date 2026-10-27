package gitmirror

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeclaredRevertTargetRequiresExactGitTrailer(t *testing.T) {
	work := t.TempDir()
	runGit(t, work, "init", "-b", "main")
	runGit(t, work, "config", "user.name", "Fixture")
	runGit(t, work, "config", "user.email", "fixture@example.invalid")
	file := filepath.Join(work, "app.txt")
	if err := os.WriteFile(file, []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, work, "add", "app.txt")
	runGit(t, work, "commit", "-m", "initial")
	if err := os.WriteFile(file, []byte("two\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, work, "add", "app.txt")
	runGit(t, work, "commit", "-m", "change")
	target := runGit(t, work, "rev-parse", "HEAD")
	runGit(t, work, "revert", "--no-edit", target)
	revert := runGit(t, work, "rev-parse", "HEAD")
	m := &Mirror{Path: filepath.Join(work, ".git"), GitPath: "git", allowLocal: true}
	got, complete, err := m.DeclaredRevertTarget(context.Background(), revert)
	if err != nil || !complete || got != target {
		t.Fatalf("revert trailer: target=%q complete=%v err=%v", got, complete, err)
	}
	if err := os.WriteFile(file, []byte("three\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, work, "add", "app.txt")
	runGit(t, work, "commit", "-m", "fixes problem but does not revert")
	plain := runGit(t, work, "rev-parse", "HEAD")
	got, complete, err = m.DeclaredRevertTarget(context.Background(), plain)
	if err != nil || !complete || got != "" {
		t.Fatalf("free-form fix text accepted: target=%q complete=%v err=%v", got, complete, err)
	}
	if _, _, err := m.DeclaredRevertTarget(context.Background(), "--all"); err != ErrCorrupt {
		t.Fatalf("invalid OID accepted: %v", err)
	}
	runGit(t, work, "commit", "--allow-empty", "-m", "ambiguous", "-m", "This reverts commit "+target+".", "-m", "This reverts commit "+plain+".")
	ambiguous := runGit(t, work, "rev-parse", "HEAD")
	got, complete, err = m.DeclaredRevertTarget(context.Background(), ambiguous)
	if err != nil || !complete || got != "" {
		t.Fatalf("ambiguous trailer accepted: target=%q complete=%v err=%v", got, complete, err)
	}
	messageFile := filepath.Join(work, "long-message.txt")
	if err := os.WriteFile(messageFile, []byte("subject\n\n"+strings.Repeat("x", maxRevertMessageBytes)), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, work, "commit", "--allow-empty", "-F", messageFile)
	large := runGit(t, work, "rev-parse", "HEAD")
	got, complete, err = m.DeclaredRevertTarget(context.Background(), large)
	if err != nil || complete || got != "" {
		t.Fatalf("oversized message treated as inspected: target=%q complete=%v err=%v", got, complete, err)
	}
}
