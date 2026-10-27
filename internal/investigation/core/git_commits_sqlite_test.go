//go:build investigate_sqlite && cgo

package core

import (
	"context"
	"fmt"
	"testing"
	"time"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/gitmirror"
	"pocketcli/internal/investigation/query"
	"pocketcli/internal/investigation/rules"
	"pocketcli/internal/investigation/storage"
)

func TestGitCommitWalkContinuesFromPublishedEvents(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	observer := &fakeGitObserver{result: gitmirror.Observation{Ref: "refs/heads/main", GraphComplete: true}}
	for i := 103; i >= 1; i-- {
		date := "2026-01-10T00:00:00Z"
		if i == 1 {
			date = "2025-01-10T00:00:00Z" // Reachable context outside the selected period.
		}
		observer.commits = append(observer.commits, gitmirror.Commit{OID: fmt.Sprintf("%040x", i), CommittedAt: date})
	}
	observer.result.NewOID = observer.commits[0].OID
	c, err := NewCore(CoreConfig{Store: store, Clock: func() time.Time { return time.Date(2026, 1, 20, 0, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	c.gitMirror = func(string) (gitObserver, error) { return observer, nil }
	r := contract.Resource{Source: "github", ID: "42", Name: "acme/api"}
	plan := contract.Plan{ID: "commit-plan", InvestigationID: "commit-investigation", WindowStart: "2026-01-01T00:00:00Z", WindowEnd: "2026-02-01T00:00:00Z"}
	journalID := storage.Hash([]byte(plan.ID + ":" + r.ID + ":git_ref"))
	if err := store.Put("gitobservations", journalID, gitRunObservation{Observation: observer.result, ObservedAt: "2026-01-20T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Put("gitbranchobservations", storage.Hash([]byte(plan.ID+":"+r.ID+":git_branches")), gitBranchObservation{Complete: true, ObservedAt: "2026-01-20T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Put("planauth", plan.ID, planAuth{GitHub: "identity"}); err != nil {
		t.Fatal(err)
	}
	first := query.Dataset{}
	firstRun := contract.Run{ID: "commit-run-1"}
	if err := c.collectGitCommits(context.Background(), plan, r, &first, &firstRun); err != nil {
		t.Fatal(err)
	}
	if len(first.Events) != commitsPerRound || first.Coverage[0].Status != "partial" || firstRun.Requests != 0 {
		t.Fatalf("primeira rodada: events=%d coverage=%+v requests=%d", len(first.Events), first.Coverage, firstRun.Requests)
	}
	second := query.Dataset{Events: append([]query.Event(nil), first.Events...), Evidence: append([]contract.Evidence(nil), first.Evidence...)}
	secondRun := contract.Run{ID: "commit-run-2"}
	if err := c.collectGitCommits(context.Background(), plan, r, &second, &secondRun); err != nil {
		t.Fatal(err)
	}
	if len(second.Events) != 102 || len(second.Evidence) != 102 || second.Coverage[0].Status != "complete" || second.Coverage[0].Observed != 102 || secondRun.Requests != 0 || secondRun.Items != 2 {
		t.Fatalf("segunda rodada: events=%d evidence=%d coverage=%+v run=%+v", len(second.Events), len(second.Evidence), second.Coverage, secondRun)
	}
	if second.Events[101].NewValue != fmt.Sprintf("%040x", 2) {
		t.Fatalf("commit fora da janela foi incluído: %+v", second.Events[101])
	}
}

func TestGitCommitWalkProjectsDeclaredRevertAndKeepsOversizedMessagePartial(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	targetOID := fmt.Sprintf("%040x", 1)
	revertOID := fmt.Sprintf("%040x", 2)
	largeOID := fmt.Sprintf("%040x", 3)
	observer := &fakeGitObserver{
		result: gitmirror.Observation{Ref: "refs/heads/main", NewOID: largeOID, GraphComplete: true},
		commits: []gitmirror.Commit{
			{OID: largeOID, CommittedAt: "2026-01-12T00:00:00Z"},
			{OID: revertOID, CommittedAt: "2026-01-11T00:00:00Z"},
			{OID: targetOID, CommittedAt: "2026-01-10T00:00:00Z"},
		},
		revertTargets:     map[string]string{revertOID: targetOID},
		revertUninspected: map[string]bool{largeOID: true},
	}
	c, err := NewCore(CoreConfig{Store: store, Clock: func() time.Time { return time.Date(2026, 1, 20, 0, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	c.gitMirror = func(string) (gitObserver, error) { return observer, nil }
	r := contract.Resource{Source: "github", ID: "42", Name: "acme/api"}
	plan := contract.Plan{ID: "revert-plan", InvestigationID: "revert-investigation", WindowStart: "2026-01-01T00:00:00Z", WindowEnd: "2026-02-01T00:00:00Z"}
	if err := store.Put("gitobservations", storage.Hash([]byte(plan.ID+":"+r.ID+":git_ref")), gitRunObservation{Observation: observer.result, ObservedAt: "2026-01-20T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Put("gitbranchobservations", storage.Hash([]byte(plan.ID+":"+r.ID+":git_branches")), gitBranchObservation{Complete: true, ObservedAt: "2026-01-20T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Put("planauth", plan.ID, planAuth{GitHub: "identity"}); err != nil {
		t.Fatal(err)
	}
	d := query.Dataset{}
	if err := c.collectGitCommits(context.Background(), plan, r, &d, &contract.Run{ID: "revert-run"}); err != nil {
		t.Fatal(err)
	}
	var declared, uninspected int
	for _, event := range d.Events {
		if event.Type == "commit_revert_declared" && event.OldValue == targetOID && event.NewValue == revertOID && event.EvidenceID != "" {
			declared++
		}
		if event.Type == "commit_revert_message_uninspected" && event.NewValue == largeOID {
			uninspected++
		}
	}
	if declared != 1 || uninspected != 1 || len(d.Events) != 5 || d.Coverage[0].Status != "partial" {
		t.Fatalf("revert projection or coverage missing: events=%+v coverage=%+v", d.Events, d.Coverage)
	}
	analyzed := rules.Evaluate(d)
	if len(analyzed.Rework) != 1 || analyzed.Rework[0].Kind != "declared_reversal" || len(analyzed.Links) != 1 || analyzed.Links[0].Relation != "declares_revert_of" {
		t.Fatalf("collector event did not reach deterministic graph: rework=%+v links=%+v", analyzed.Rework, analyzed.Links)
	}
	refresh := query.Dataset{Events: append([]query.Event(nil), d.Events...), Evidence: append([]contract.Evidence(nil), d.Evidence...)}
	if err := c.collectGitCommits(context.Background(), plan, r, &refresh, &contract.Run{ID: "revert-refresh"}); err != nil {
		t.Fatal(err)
	}
	if refresh.Coverage[0].Status != "partial" {
		t.Fatalf("uninspected message was forgotten on refresh: %+v", refresh.Coverage)
	}
}
