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

// A mirror is a blob:none partial clone with lazy fetching disabled. Reading a
// commit message must not need any blob: `git show -s` still runs rename
// detection and asks the promisor remote for the blobs it lacks, so a commit
// that touches a file failed here while the commit graph itself walked fine.
func TestDeclaredRevertTargetReadsMessagesInBlobLessMirror(t *testing.T) {
	origin := t.TempDir()
	runGit(t, origin, "init", "-b", "main")
	runGit(t, origin, "config", "user.name", "Fixture")
	runGit(t, origin, "config", "user.email", "fixture@example.invalid")
	runGit(t, origin, "config", "uploadpack.allowFilter", "true")
	runGit(t, origin, "config", "uploadpack.allowAnySHA1InWant", "true")
	file := filepath.Join(origin, "README.md")
	for i, text := range []string{"one\n", "two\n", "three\n"} {
		if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		runGit(t, origin, "add", "README.md")
		runGit(t, origin, "commit", "-m", "change "+string(rune('a'+i)))
	}
	// Moving a file while editing it is what makes git compare blob contents
	// (inexact rename detection), so those blobs are needed to diff the commit.
	plain := runGit(t, origin, "rev-parse", "HEAD")
	runGit(t, origin, "mv", "README.md", "profile.md")
	if err := os.WriteFile(filepath.Join(origin, "profile.md"), []byte("three\nplus a new line\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, origin, "add", "profile.md")
	runGit(t, origin, "commit", "-m", "move and edit")
	target := runGit(t, origin, "rev-parse", "HEAD")
	runGit(t, origin, "revert", "--no-edit", target)
	revert := runGit(t, origin, "rev-parse", "HEAD")

	mirror := filepath.Join(t.TempDir(), "mirror.git")
	runGit(t, filepath.Dir(mirror), "clone", "--bare", "--no-local", "--filter=blob:none", "file://"+origin, mirror)
	if blobs := runGit(t, mirror, "rev-list", "--objects", "--missing=print", "--all"); !strings.Contains(blobs, "?") {
		t.Skip("this git did not create a blob-less clone; nothing to guard")
	}
	m := &Mirror{Path: mirror, GitPath: "git", allowLocal: true}
	for name, test := range map[string]struct{ oid, want string }{"revert trailer": {revert, target}, "plain change": {plain, ""}} {
		got, complete, err := m.DeclaredRevertTarget(context.Background(), test.oid)
		if err != nil || !complete || got != test.want {
			t.Fatalf("%s in a blob-less mirror: target=%q complete=%v err=%v", name, got, complete, err)
		}
	}
	seen := 0
	if complete, err := m.WalkCommits(context.Background(), revert, func(c Commit) (bool, error) {
		seen++
		_, _, err := m.DeclaredRevertTarget(context.Background(), c.OID)
		return true, err
	}); err != nil || !complete || seen != 5 {
		t.Fatalf("walk with message reads: complete=%v seen=%d err=%v", complete, seen, err)
	}
}
