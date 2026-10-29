//go:build investigate_sqlite && cgo

package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/gitmirror"
	"pocketcli/internal/investigation/query"
	"pocketcli/internal/investigation/secrets"
	"pocketcli/internal/investigation/storage"
)

func storeFiles(t *testing.T, root string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && !strings.Contains(path, "/locks/") {
			rel, _ := filepath.Rel(root, path)
			out[rel] = true
		}
		return nil
	})
	return out
}

// Removing an investigation takes everything it collected, including state
// keyed only by hash, and leaves other investigations, shared evidence and
// the Linear schema alone.
func TestPurgeLeavesNothingOfTheInvestigation(t *testing.T) {
	store, _ := storage.Open(t.TempDir())
	ss := secrets.NewSessionStore()
	ss.Put(context.Background(), contract.CredentialInput{Source: "github", Secret: "test-secret", Persistence: "session"})
	c, err := NewCore(CoreConfig{Store: store, Secrets: ss, Clock: func() time.Time { return time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	seed := func(inv, plan, run, snapshot string, evidence ...string) contract.Plan {
		p := contract.Plan{ID: plan, InvestigationID: inv, Resources: []contract.Resource{{Source: "github", ID: "42", Name: "acme/api"}}, Input: contract.PlanInput{GitHubEnabled: true, Limits: contract.Limits{MaxTransferBytes: 1 << 20, MaxLocalBytes: 1 << 20}}}
		must := func(err error) {
			t.Helper()
			if err != nil {
				t.Fatal(err)
			}
		}
		must(store.Put("plans", plan, p))
		must(store.Put("planauth", plan, planAuth{GitHub: "identity-" + inv}))
		must(store.Put("runs", run, contract.Run{ID: run, PlanID: plan, SnapshotID: snapshot, State: "completed"}))
		must(store.Put("snapshots", snapshot, contract.Snapshot{ID: snapshot, InvestigationID: inv, PlanID: plan}))
		d := query.Dataset{Snapshot: contract.Snapshot{ID: snapshot, InvestigationID: inv}}
		for _, id := range evidence {
			d.Evidence = append(d.Evidence, contract.Evidence{ID: id})
			must(store.Put("objects", id, map[string]string{"raw": id}))
		}
		must(store.Put("datasets", snapshot, d))
		must(store.Put("journals", storage.Hash([]byte(plan+":42:github:pull_requests")), githubJournal{PlanID: plan, ResourceID: "42"}))
		must(os.MkdirAll(filepath.Join(store.Root, "reports"), 0o700))
		must(os.WriteFile(filepath.Join(store.Root, "reports", snapshot+"-r1.pdf"), []byte("%PDF"), 0o600))
		return p
	}
	a := seed("inv-a", "plan-aaaa1111", "run-aaaa1111", "snap-aaaa1111", "ev-only-a", "ev-shared")
	seed("inv-b", "plan-bbbb2222", "run-bbbb2222", "snap-bbbb2222", "ev-only-b", "ev-shared")
	otherBefore := storeFiles(t, store.Root)

	// Git state keyed only by hash, produced by the real collector.
	c.gitMirror = func(string) (gitObserver, error) {
		return &fakeGitObserver{result: gitmirror.Observation{Ref: "refs/heads/main", NewOID: strings.Repeat("a", 40), Relationship: "initial", GraphComplete: true, LocalBytes: 10}}, nil
	}
	run := contract.Run{ID: "run-aaaa1111", PlanID: "plan-aaaa1111", SnapshotID: "snap-aaaa1111", State: "completed"}
	if err := c.collectGitRef(context.Background(), a, a.Resources[0], &query.Dataset{}, &run); err != nil {
		t.Fatal(err)
	}
	mirror := filepath.Join(store.Root, "git", storage.Hash([]byte("inv-a:42:identity-inv-a")))
	if err := os.MkdirAll(mirror, 0o700); err != nil || os.WriteFile(filepath.Join(mirror, "HEAD"), []byte("ref"), 0o600) != nil {
		t.Fatal(err)
	}
	if err := store.Put("httpcache", "entry", map[string]string{"body": "raw api response"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Put("linearschema", "schema", map[string]string{"fields": "x"}); err != nil {
		t.Fatal(err)
	}
	withGit := storeFiles(t, store.Root)

	// A running collection blocks the removal.
	c.mu.Lock()
	c.runs["run-aaaa1111"] = func() {}
	c.mu.Unlock()
	if _, err := c.PurgeInvestigation(context.Background(), "inv-a"); err == nil || !strings.Contains(err.Error(), "ERR_CONFLICT") {
		t.Fatalf("coleta ativa deveria bloquear: %v", err)
	}
	c.mu.Lock()
	delete(c.runs, "run-aaaa1111")
	c.mu.Unlock()
	if len(storeFiles(t, store.Root)) != len(withGit) {
		t.Fatal("remoção bloqueada não pode apagar nada")
	}

	result, err := c.PurgeInvestigation(context.Background(), "inv-a")
	if err != nil {
		t.Fatal(err)
	}
	if result.Plans != 1 || result.Runs != 1 || result.Snapshots != 1 || len(result.Leftover) != 0 || !result.CachesCleared {
		t.Fatalf("resultado: %+v", result)
	}
	after := storeFiles(t, store.Root)
	for path := range after {
		if strings.Contains(path, "aaaa1111") || strings.Contains(path, "ev-only-a") || strings.HasPrefix(path, "git/") || strings.HasPrefix(path, "httpcache/") || strings.HasPrefix(path, "gitobservations/") || strings.HasPrefix(path, "gitrefs/") {
			t.Fatalf("sobrou dado da investigação: %s", path)
		}
	}
	for path := range otherBefore {
		if strings.Contains(path, "bbbb2222") || strings.Contains(path, "ev-only-b") || strings.Contains(path, "ev-shared") {
			if !after[path] {
				t.Fatalf("a outra investigação perdeu %s", path)
			}
		}
	}
	if !after["linearschema/schema.json"] {
		t.Fatal("o schema do Linear não é dado coletado e deve ficar")
	}
	if _, err := c.PurgeInvestigation(context.Background(), "inv-a"); err == nil || !strings.Contains(err.Error(), "ERR_NOT_FOUND") {
		t.Fatalf("segunda remoção: %v", err)
	}
	if _, err := c.GetSnapshot(context.Background(), "snap-aaaa1111"); err == nil {
		t.Fatal("o snapshot removido não pode mais ser lido")
	}
	if _, err := c.GetSnapshot(context.Background(), "snap-bbbb2222"); err != nil {
		t.Fatalf("snapshot da outra investigação: %v", err)
	}
}
