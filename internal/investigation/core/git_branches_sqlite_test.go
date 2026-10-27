//go:build investigate_sqlite && cgo

package core

import (
	"context"
	"strings"
	"testing"
	"time"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/gitmirror"
	"pocketcli/internal/investigation/query"
	"pocketcli/internal/investigation/secrets"
	"pocketcli/internal/investigation/storage"
)

func TestBranchInventoryOnlyMarksDeletionAfterCompleteList(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ss := secrets.NewSessionStore()
	if _, err := ss.Put(context.Background(), contract.CredentialInput{Source: "github", Secret: "test-secret", Persistence: "session"}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC)
	c, err := NewCore(CoreConfig{Store: store, Secrets: ss, Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	mainOID, featureOID := strings.Repeat("a", 40), strings.Repeat("b", 40)
	observer := &fakeGitObserver{heads: []gitmirror.HeadRef{{Name: "refs/heads/main", OID: mainOID}, {Name: "refs/heads/feature", OID: featureOID}}}
	c.gitMirror = func(string) (gitObserver, error) { return observer, nil }
	r := contract.Resource{Source: "github", ID: "42", Name: "acme/api"}
	plan := contract.Plan{ID: "branches-one", InvestigationID: "branches-inv", Input: contract.PlanInput{Limits: contract.Limits{MaxTransferBytes: 1 << 20}}}
	for _, id := range []string{"branches-one", "branches-partial", "branches-complete"} {
		if err := store.Put("planauth", id, planAuth{GitHub: "identity"}); err != nil {
			t.Fatal(err)
		}
	}
	first := query.Dataset{}
	if err := c.collectGitBranches(context.Background(), plan, r, &first, &contract.Run{ID: "branch-run-one"}); err != nil {
		t.Fatal(err)
	}
	if len(first.Events) != 2 || first.Coverage[0].Status != "complete" {
		t.Fatalf("primeira lista: %+v", first)
	}
	now = now.Add(24 * time.Hour)
	observer.heads = []gitmirror.HeadRef{{Name: "refs/heads/main", OID: mainOID}}
	observer.headsIncomplete = true
	plan.ID = "branches-partial"
	partial := query.Dataset{}
	if err := c.collectGitBranches(context.Background(), plan, r, &partial, &contract.Run{ID: "branch-run-partial"}); err != nil {
		t.Fatal(err)
	}
	if len(partial.Events) != 0 || partial.Coverage[0].Status != "partial" {
		t.Fatalf("lista parcial inferiu exclusão: %+v", partial)
	}
	observer.headsIncomplete = false
	plan.ID = "branches-complete"
	complete := query.Dataset{}
	if err := c.collectGitBranches(context.Background(), plan, r, &complete, &contract.Run{ID: "branch-run-complete"}); err != nil {
		t.Fatal(err)
	}
	if len(complete.Events) != 1 || complete.Events[0].Type != "branch_ref_deleted_observed" || complete.Events[0].OldValue != featureOID || complete.Coverage[0].Status != "complete" {
		t.Fatalf("remoção não observada: %+v", complete)
	}
	if len(first.Events) != 2 {
		t.Fatal("observação anterior foi alterada")
	}
	beforeReplay := observer.headListCalls
	replayed := query.Dataset{}
	if err := c.collectGitBranches(context.Background(), plan, r, &replayed, &contract.Run{ID: "branch-run-replay"}); err != nil {
		t.Fatal(err)
	}
	if observer.headListCalls != beforeReplay || len(replayed.Events) != 1 || replayed.Events[0].ID != complete.Events[0].ID {
		t.Fatalf("retomada não reproduziu journal: events=%+v calls=%d", replayed.Events, observer.headListCalls)
	}
}
