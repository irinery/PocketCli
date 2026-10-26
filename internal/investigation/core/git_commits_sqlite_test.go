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
