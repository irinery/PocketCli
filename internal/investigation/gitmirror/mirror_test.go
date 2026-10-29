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

func TestDirectoryBytesToleratesVanishedGitTemporaryObject(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "tmp_pack")
	if err := os.WriteFile(path, []byte("temporary"), 0o600); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 {
		t.Fatalf("listagem do objeto temporário: %v, %d entradas", err, len(entries))
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if size, err := directoryEntryBytes(root, path, entries[0], nil); err != nil || size != 0 {
		t.Fatalf("arquivo renomeado durante a medição: bytes=%d err=%v", size, err)
	}
	if size, err := directoryEntryBytes(root, path, nil, os.ErrNotExist); err != nil || size != 0 {
		t.Fatalf("subdiretório removido durante a medição: bytes=%d err=%v", size, err)
	}
	if _, err := directoryEntryBytes(root, root, nil, os.ErrNotExist); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("raiz ausente deveria falhar: %v", err)
	}
	if _, err := directoryBytes(filepath.Join(root, "missing")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("diretório ausente deveria falhar: %v", err)
	}
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
	runGit(t, remote, "config", "uploadpack.allowFilter", "true")
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
	legacy, err := New(filepath.Join(root, "legacy-mirror.git"))
	if err != nil {
		t.Fatal(err)
	}
	legacy.allowLocal = true
	if err := legacy.init(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.command(context.Background(), remoteURL, nil, "fetch", "--quiet", "--filter=tree:0", remoteURL, "+"+first.Ref+":refs/pocketcli/observations/"+first.NewOID); err != nil || legacy.treesComplete(context.Background(), first.NewOID) {
		t.Fatalf("fixture commit-only inválida: %v", err)
	}
	used, err := legacy.EnsureTrees(context.Background(), remoteURL, nil, first.Ref, first.NewOID, 10*1024*1024)
	if err != nil || used <= 0 || !legacy.treesComplete(context.Background(), first.NewOID) {
		t.Fatalf("migração sem blobs: used=%d err=%v", used, err)
	}
	if used, err := legacy.EnsureTrees(context.Background(), remoteURL, nil, first.Ref, first.NewOID, 10*1024*1024); err != nil || used != 0 {
		t.Fatalf("migração repetida: used=%d err=%v", used, err)
	}
	rootFiles, allRootFiles, err := legacy.ChangedFiles(context.Background(), first.NewOID, 100)
	if err != nil || !allRootFiles || len(rootFiles) != 1 || rootFiles[0].Path != ".gitconfig" || rootFiles[0].Status != "A" {
		t.Fatalf("diff de commit raiz: files=%+v complete=%v err=%v", rootFiles, allRootFiles, err)
	}
	var treeFiles []TreeEntry
	completeTree, err := legacy.WalkTree(context.Background(), first.TreeOID, func(entry TreeEntry) (bool, error) {
		treeFiles = append(treeFiles, entry)
		return true, nil
	})
	if err != nil || !completeTree || len(treeFiles) != 1 || treeFiles[0].Path != ".gitconfig" {
		t.Fatalf("árvore local: files=%+v complete=%v err=%v", treeFiles, completeTree, err)
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
	changed, allChanged, err := m.ChangedFiles(context.Background(), third.NewOID, 100)
	if err != nil || !allChanged || len(changed) != 1 || changed[0].Path != "change.txt" || changed[0].Status != "A" {
		t.Fatalf("diff de avanço: files=%+v complete=%v err=%v", changed, allChanged, err)
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

func TestChangedFilesUsesParentTreeForDeletion(t *testing.T) {
	root := t.TempDir()
	work := filepath.Join(root, "work")
	remote := filepath.Join(root, "remote.git")
	runGit(t, root, "init", "-q", "-b", "main", work)
	runGit(t, work, "config", "user.name", "Fixture")
	runGit(t, work, "config", "user.email", "fixture@example.invalid")
	if err := os.WriteFile(filepath.Join(work, "go.mod"), []byte("module example.invalid/api\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "old.go"), []byte("package api\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, work, "add", ".")
	runGit(t, work, "commit", "-qm", "initial")
	first := runGit(t, work, "rev-parse", "HEAD")
	parentTree := runGit(t, work, "rev-parse", "HEAD^{tree}")
	if err := os.Remove(filepath.Join(work, "old.go")); err != nil {
		t.Fatal(err)
	}
	runGit(t, work, "add", "-A")
	runGit(t, work, "commit", "-qm", "delete")
	runGit(t, root, "init", "-q", "--bare", remote)
	runGit(t, remote, "config", "uploadpack.allowFilter", "true")
	runGit(t, work, "push", "-q", remote, "main")
	runGit(t, remote, "symbolic-ref", "HEAD", "refs/heads/main")
	m, err := New(filepath.Join(root, "mirror.git"))
	if err != nil {
		t.Fatal(err)
	}
	m.allowLocal = true
	observed, err := m.Observe(context.Background(), "file://"+remote, nil, RefState{}, 10*1024*1024)
	if err != nil || !m.treesComplete(context.Background(), observed.NewOID) {
		t.Fatalf("espelho com árvores indisponível: %+v %v", observed, err)
	}
	changes, complete, err := m.ChangedFiles(context.Background(), observed.NewOID, 100)
	if err != nil || !complete || len(changes) != 1 || changes[0].Path != "old.go" || changes[0].Status != "D" || changes[0].TreeOID != parentTree {
		t.Fatalf("exclusão mapeada contra árvore incorreta: %+v complete=%v err=%v", changes, complete, err)
	}
	if _, err := m.command(context.Background(), "", nil, "cat-file", "-e", first+"^{commit}"); err != nil {
		t.Fatal("commit pai não está no espelho")
	}
}

func TestMirrorReportsEmptyRemoteApartFromUnavailableOne(t *testing.T) {
	root := t.TempDir()
	empty := filepath.Join(root, "empty.git")
	runGit(t, root, "init", "--bare", empty)
	m, err := New(filepath.Join(root, "mirror.git"))
	if err != nil {
		t.Fatal(err)
	}
	m.allowLocal = true
	if _, err := m.Observe(context.Background(), "file://"+empty, nil, RefState{}, 10*1024*1024); !errors.Is(err, ErrEmptyRemote) {
		t.Fatalf("repositório vazio: err=%v", err)
	}
	if _, err := m.Observe(context.Background(), "file://"+filepath.Join(root, "missing.git"), nil, RefState{}, 10*1024*1024); !errors.Is(err, ErrUnavailable) || errors.Is(err, ErrEmptyRemote) {
		t.Fatalf("origem ausente deve continuar indisponível: err=%v", err)
	}
}
