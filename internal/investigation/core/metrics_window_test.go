package core

import (
	"context"
	"testing"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/query"
	"pocketcli/internal/investigation/storage"
)

func TestCollectDatasetAppliesPlanWindowBeforeEvaluatingMetrics(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCore(CoreConfig{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	previous := contract.Snapshot{ID: "snapshot-previous", PlanID: "plan-previous", InvestigationID: "investigation-window", PublishedAt: "2026-01-10T00:00:00Z"}
	prior := query.Dataset{Issues: []query.Issue{{ID: "issue-old", State: "done", CreatedAt: "2025-12-01T00:00:00Z", CompletedAt: "2025-12-20T00:00:00Z", UpdatedAt: "2026-01-09T00:00:00Z"}}}
	for _, item := range []struct {
		kind, id string
		value    any
	}{
		{"snapshots", previous.ID, previous},
		{"datasets", previous.ID, prior},
		{"planauth", previous.PlanID, planAuth{}},
		{"planauth", "plan-current", planAuth{}},
	} {
		if err := store.Put(item.kind, item.id, item.value); err != nil {
			t.Fatal(err)
		}
	}
	plan := contract.Plan{ID: "plan-current", InvestigationID: previous.InvestigationID, WindowStart: "2026-01-01T00:00:00Z", WindowEnd: "2026-01-11T00:00:00Z"}
	d, err := c.collectDataset(context.Background(), plan, &contract.Run{ID: "run-window"})
	if err != nil {
		t.Fatal(err)
	}
	if d.Snapshot.WindowStart != plan.WindowStart || d.Snapshot.AnalysisReferenceAt != plan.WindowEnd || len(d.Issues) != 1 {
		t.Fatalf("plan window or reused issue missing: snapshot=%+v issues=%+v", d.Snapshot, d.Issues)
	}
	for _, metric := range d.Metrics {
		if metric.ID == "metric-completed" {
			if metric.Value.Value != "0" {
				t.Fatalf("old completion counted as new delivery: %+v", metric)
			}
			return
		}
	}
	t.Fatal("completed metric missing")
}
