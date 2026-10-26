package gitmirror

import (
	"context"
	"os"
	"testing"
	"time"
)

// Opt-in read-only smoke. Never run this in CI with a personal token.
func TestRealGitHistorySmoke(t *testing.T) {
	repo := os.Getenv("POCKETCLI_INVESTIGATE_REAL_GIT_REPO")
	if repo == "" {
		t.Skip("configure POCKETCLI_INVESTIGATE_REAL_GIT_REPO para smoke real")
	}
	secret := os.Getenv("POCKETCLI_INVESTIGATE_GITHUB_TOKEN")
	if secret == "" {
		t.Fatal("POCKETCLI_INVESTIGATE_GITHUB_TOKEN ausente")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	m, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	remote := "https://github.com/" + repo + ".git"
	observation, err := m.Observe(ctx, remote, []byte(secret), RefState{}, 128<<20)
	if err != nil || observation.NewOID == "" || !m.treesComplete(ctx, observation.NewOID) {
		t.Fatalf("fetch Git filtrado falhou: relationship=%s bytes=%d err=%v", observation.Relationship, observation.LocalBytes, err)
	}
	commits := 0
	_, err = m.WalkCommits(ctx, observation.NewOID, func(commit Commit) (bool, error) {
		commits++
		if commits == 1 {
			if _, _, err := m.ChangedFiles(ctx, commit.OID, 3000); err != nil {
				return false, err
			}
		}
		return commits < 5, nil
	})
	if err != nil || commits == 0 {
		t.Fatalf("histórico Git local falhou: commits=%d err=%v", commits, err)
	}
	treeEntries := 0
	_, err = m.WalkTree(ctx, observation.TreeOID, func(TreeEntry) (bool, error) {
		treeEntries++
		return treeEntries < 5, nil
	})
	if err != nil {
		t.Fatalf("árvore Git local falhou: %v", err)
	}
	t.Logf("fetch, diff e árvore locais concluídos: %d commits e %d entradas de árvore amostrados, %d bytes locais adicionais", commits, treeEntries, observation.LocalBytes)
}
