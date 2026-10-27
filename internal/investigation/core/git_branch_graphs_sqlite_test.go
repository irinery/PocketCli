//go:build investigate_sqlite && cgo

package core

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/gitmirror"
	"pocketcli/internal/investigation/query"
	"pocketcli/internal/investigation/secrets"
	"pocketcli/internal/investigation/storage"
)

func TestBranchGraphQueueAndJournalReplay(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ss := secrets.NewSessionStore()
	if _, err := ss.Put(context.Background(), contract.CredentialInput{Source: "github", Secret: "test-secret", Persistence: "session"}); err != nil {
		t.Fatal(err)
	}
	c, err := NewCore(CoreConfig{Store: store, Secrets: ss, Clock: func() time.Time { return time.Date(2026, 1, 20, 0, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	mainOID := strings.Repeat("a", 40)
	refs := []gitmirror.HeadRef{{Name: "refs/heads/main", OID: mainOID}}
	for i := 1; i <= branchGraphsPerRound+1; i++ {
		refs = append(refs, gitmirror.HeadRef{Name: fmt.Sprintf("refs/heads/feature-%d", i), OID: fmt.Sprintf("%040x", i)})
	}
	observer := &fakeGitObserver{result: gitmirror.Observation{Ref: "refs/heads/main", NewOID: mainOID, TreeOID: strings.Repeat("b", 40), GraphComplete: true, LocalBytes: 100}, heads: refs}
	c.gitMirror = func(string) (gitObserver, error) { return observer, nil }
	r := contract.Resource{Source: "github", ID: "42", Name: "acme/api"}
	plan := contract.Plan{ID: "branch-graphs-one", InvestigationID: "branch-graphs-inv", Input: contract.PlanInput{Limits: contract.Limits{MaxTransferBytes: 16 << 20, MaxLocalBytes: 16 << 20}}}
	for _, id := range []string{"branch-graphs-one", "branch-graphs-two", "branch-graphs-three", "branch-graphs-four", "branch-graphs-five"} {
		if err := store.Put("planauth", id, planAuth{GitHub: "identity"}); err != nil {
			t.Fatal(err)
		}
		if err := store.Put("gitobservations", storage.Hash([]byte(id+":"+r.ID+":git_ref")), gitRunObservation{Observation: observer.result, ObservedAt: "2026-01-20T00:00:00Z"}); err != nil {
			t.Fatal(err)
		}
	}
	first := query.Dataset{}
	firstRun := contract.Run{ID: "branch-graphs-run-one"}
	if err := c.collectGitBranches(context.Background(), plan, r, &first, &firstRun); err != nil {
		t.Fatal(err)
	}
	if err := c.collectGitBranchGraphs(context.Background(), plan, r, &first, &firstRun); err != nil {
		t.Fatal(err)
	}
	if observer.branchFetches != branchGraphsPerRound || first.Coverage[len(first.Coverage)-1].Status != "partial" {
		t.Fatalf("fila inicial: fetches=%d coverage=%+v", observer.branchFetches, first.Coverage)
	}
	firstStats, err := c.RunStats(context.Background(), firstRun.ID)
	if err != nil || firstStats.GitBranchBytesAdded != 100*branchGraphsPerRound || firstStats.GitRefBytesAdded != 0 {
		t.Fatalf("bytes de branches da primeira rodada: %+v err=%v", firstStats, err)
	}
	firstHeads, firstComplete, err := c.observedGitHeads(plan, r, planAuth{GitHub: "identity"})
	if err != nil || firstComplete || len(firstHeads) != branchGraphsPerRound+1 {
		t.Fatalf("branch ainda pendente entrou no grafo: heads=%+v complete=%v err=%v", firstHeads, firstComplete, err)
	}
	replayed := query.Dataset{}
	if err := c.collectGitBranchGraphs(context.Background(), plan, r, &replayed, &contract.Run{ID: "branch-graphs-replay"}); err != nil {
		t.Fatal(err)
	}
	if observer.branchFetches != branchGraphsPerRound || len(replayed.Events) != branchGraphsPerRound || replayed.Events[0].ID == "" {
		t.Fatalf("journal não reproduziu observações: fetches=%d events=%+v", observer.branchFetches, replayed.Events)
	}
	plan.ID = "branch-graphs-two"
	second := query.Dataset{}
	if err := c.collectGitBranches(context.Background(), plan, r, &second, &contract.Run{ID: "branch-list-two"}); err != nil {
		t.Fatal(err)
	}
	if err := c.collectGitBranchGraphs(context.Background(), plan, r, &second, &contract.Run{ID: "branch-graphs-run-two"}); err != nil {
		t.Fatal(err)
	}
	if observer.branchFetches != branchGraphsPerRound+1 || second.Coverage[len(second.Coverage)-1].Status != "complete" {
		t.Fatalf("fila não avançou no refresh: fetches=%d coverage=%+v", observer.branchFetches, second.Coverage)
	}
	secondStats, err := c.RunStats(context.Background(), "branch-graphs-run-two")
	if err != nil || secondStats.GitBranchBytesAdded != 100 || secondStats.GitRefBytesAdded != 0 {
		t.Fatalf("bytes de branch nova no refresh: %+v err=%v", secondStats, err)
	}
	secondHeads, secondComplete, err := c.observedGitHeads(plan, r, planAuth{GitHub: "identity"})
	if err != nil || !secondComplete || len(secondHeads) != branchGraphsPerRound+2 {
		t.Fatalf("grafo completo não inclui todas as heads: heads=%+v complete=%v err=%v", secondHeads, secondComplete, err)
	}
	for i := 1; i < len(refs); i++ {
		refs[i].OID = fmt.Sprintf("%040x", i+10)
	}
	observer.heads = refs
	observer.branchErrors = map[string]error{"refs/heads/feature-1": gitmirror.ErrMoved}
	for _, id := range []string{"branch-graphs-three", "branch-graphs-four"} {
		plan.ID = id
		d := query.Dataset{}
		if err := c.collectGitBranches(context.Background(), plan, r, &d, &contract.Run{ID: id + "-list"}); err != nil {
			t.Fatal(err)
		}
		if err := c.collectGitBranchGraphs(context.Background(), plan, r, &d, &contract.Run{ID: id + "-graph"}); err != nil {
			t.Fatal(err)
		}
		if d.Coverage[len(d.Coverage)-1].Status != "partial" {
			t.Fatalf("ref movida não deixou cobertura parcial: %+v", d.Coverage)
		}
	}
	plan.ID = "branch-graphs-four"
	progressed, complete, err := c.observedGitHeads(plan, r, planAuth{GitHub: "identity"})
	if err != nil || complete || len(progressed) != len(refs)-1 {
		t.Fatalf("ref falha bloqueou as demais: heads=%+v complete=%v err=%v", progressed, complete, err)
	}
	delete(observer.branchErrors, "refs/heads/feature-1")
	plan.ID = "branch-graphs-five"
	last := query.Dataset{}
	if err := c.collectGitBranches(context.Background(), plan, r, &last, &contract.Run{ID: "branch-list-five"}); err != nil {
		t.Fatal(err)
	}
	if err := c.collectGitBranchGraphs(context.Background(), plan, r, &last, &contract.Run{ID: "branch-graph-five"}); err != nil {
		t.Fatal(err)
	}
	if last.Coverage[len(last.Coverage)-1].Status != "complete" {
		t.Fatalf("fila não recuperou ref movida: %+v", last.Coverage)
	}
}

func TestBranchOnlyCommitAndFileReachSnapshot(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ss := secrets.NewSessionStore()
	if _, err := ss.Put(context.Background(), contract.CredentialInput{Source: "github", Secret: "test-secret", Persistence: "session"}); err != nil {
		t.Fatal(err)
	}
	c, err := NewCore(CoreConfig{Store: store, Secrets: ss, Clock: func() time.Time { return time.Date(2026, 1, 20, 0, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	mainOID, featureOID := strings.Repeat("a", 40), strings.Repeat("b", 40)
	treeOID, blobOID := strings.Repeat("c", 40), strings.Repeat("d", 40)
	observer := &fakeGitObserver{
		result: gitmirror.Observation{Ref: "refs/heads/main", NewOID: mainOID, TreeOID: treeOID, GraphComplete: true},
		heads:  []gitmirror.HeadRef{{Name: "refs/heads/main", OID: mainOID}, {Name: "refs/heads/feature", OID: featureOID}},
		commitsByHead: map[string][]gitmirror.Commit{
			mainOID:    {{OID: mainOID, CommittedAt: "2025-01-10T00:00:00Z"}},
			featureOID: {{OID: featureOID, CommittedAt: "2026-01-10T00:00:00Z"}, {OID: mainOID, CommittedAt: "2025-01-10T00:00:00Z"}},
		},
		changes: map[string][]gitmirror.ChangedFile{featureOID: {{Path: "services/api/handler.go", Status: "M", TreeOID: treeOID}}},
		trees:   map[string][]gitmirror.TreeEntry{treeOID: {{Path: "services/api/go.mod", Mode: "100644", Type: "blob", OID: blobOID}}},
	}
	c.gitMirror = func(string) (gitObserver, error) { return observer, nil }
	r := contract.Resource{Source: "github", ID: "42", Name: "acme/api"}
	plan := contract.Plan{ID: "branch-commit-plan", InvestigationID: "branch-commit-inv", WindowStart: "2026-01-01T00:00:00Z", WindowEnd: "2026-02-01T00:00:00Z", Input: contract.PlanInput{Limits: contract.Limits{MaxTransferBytes: 16 << 20, MaxLocalBytes: 16 << 20}}}
	if err := store.Put("planauth", plan.ID, planAuth{GitHub: "identity"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Put("gitobservations", storage.Hash([]byte(plan.ID+":"+r.ID+":git_ref")), gitRunObservation{Observation: observer.result, ObservedAt: "2026-01-20T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	d := query.Dataset{}
	run := contract.Run{ID: "branch-commit-run"}
	for _, step := range []func(context.Context, contract.Plan, contract.Resource, *query.Dataset, *contract.Run) error{
		c.collectGitBranches, c.collectGitBranchGraphs, c.collectGitCommits, c.collectGitCommitFiles,
	} {
		if err := step(context.Background(), plan, r, &d, &run); err != nil {
			t.Fatal(err)
		}
	}
	commitEvent := false
	for _, event := range d.Events {
		if event.Type == "commit_reachable_observed" && event.NewValue == featureOID {
			commitEvent = true
		}
	}
	fileLink, componentLink := false, false
	for _, link := range d.Links {
		if link.Relation == "modifies_file" {
			fileLink = true
		}
		if link.Relation == "maps_to_component" {
			componentLink = true
		}
	}
	if !commitEvent || !fileLink || !componentLink || d.Coverage[len(d.Coverage)-1].Status != "complete" {
		t.Fatalf("commit da branch não chegou ao snapshot: events=%+v links=%+v coverage=%+v", d.Events, d.Links, d.Coverage)
	}
}
