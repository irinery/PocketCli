package gitmirror

import (
	"context"
	"os"
	"os/exec"
	"strings"
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
	heads, completeHeads, branchBytes, err := m.ListHeads(ctx, remote, []byte(secret), 10000, 1<<20)
	if err != nil || !completeHeads || len(heads) == 0 || branchBytes <= 0 {
		t.Fatalf("listagem de branches falhou: heads=%d complete=%v bytes=%d err=%v", len(heads), completeHeads, branchBytes, err)
	}
	foundDefault := false
	for _, head := range heads {
		if head.Name == observation.Ref && head.OID == observation.NewOID {
			foundDefault = true
		}
	}
	if !foundDefault {
		t.Fatal("head padrão ausente da listagem de branches")
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
	t.Logf("fetch, diff e árvore locais concluídos: %d commits, %d entradas de árvore, %d branches, %d bytes locais adicionais e %d bytes na listagem", commits, treeEntries, len(heads), observation.LocalBytes, branchBytes)
}

// Opt-in read-only smoke for an inventoried branch, bounded by time and bytes.
func TestRealBranchGraphSmoke(t *testing.T) {
	repo := os.Getenv("POCKETCLI_INVESTIGATE_REAL_GIT_REPO")
	if repo == "" {
		t.Skip("configure POCKETCLI_INVESTIGATE_REAL_GIT_REPO para smoke real")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	secret := os.Getenv("POCKETCLI_INVESTIGATE_GITHUB_TOKEN")
	if secret == "" {
		data, err := exec.CommandContext(ctx, "gh", "auth", "token").Output()
		if err != nil {
			t.Fatal("credencial GitHub local indisponível")
		}
		secret = strings.TrimSpace(string(data))
	}
	if secret == "" {
		t.Fatal("credencial GitHub local vazia")
	}
	m, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	remote := "https://github.com/" + repo + ".git"
	heads, complete, listingBytes, err := m.ListHeads(ctx, remote, []byte(secret), 10000, 1<<20)
	if err != nil || !complete || len(heads) == 0 {
		t.Fatalf("inventário de heads: count=%d complete=%v err=%v", len(heads), complete, err)
	}
	const budget int64 = 128 << 20
	if listingBytes >= budget {
		t.Fatal("listagem esgotou o orçamento Git")
	}
	head := heads[0]
	observed, err := m.ObserveHead(ctx, remote, []byte(secret), head, RefState{}, budget-listingBytes)
	if err != nil || observed.NewOID != head.OID || observed.Ref != head.Name || !observed.GraphComplete || !m.treesComplete(ctx, head.OID) {
		t.Fatalf("grafo da branch: ref=%s relationship=%s bytes=%d err=%v", head.Name, observed.Relationship, observed.LocalBytes, err)
	}
	commits := 0
	_, err = m.WalkCommits(ctx, observed.NewOID, func(commit Commit) (bool, error) {
		commits++
		if commits == 1 {
			if _, _, err := m.ChangedFiles(ctx, commit.OID, 3000); err != nil {
				return false, err
			}
		}
		return commits < 5, nil
	})
	if err != nil || commits == 0 {
		t.Fatalf("commits da branch: count=%d err=%v", commits, err)
	}
	t.Logf("branch %s observada: %d commits amostrados, %d bytes de lista, %d bytes adicionais de grafo", head.Name, commits, listingBytes, observed.LocalBytes)
}
