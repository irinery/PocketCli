package insights

import (
	"context"
	"strings"
	"testing"

	"pocketcli/internal/investigation/insightcontract"
	"pocketcli/internal/investigation/storage"
)

func timelineText(value string) *string { return &value }

func intentFixture() insightcontract.SourceBundle {
	return insightcontract.SourceBundle{
		Snapshot: insightcontract.SnapshotRef{ID: "synthetic-snapshot"},
		Entities: []insightcontract.Entity{{ID: "item", Source: "linear", ResourceID: "team", Identifier: "TEST-1", OwnerID: timelineText("owner")}},
		Actors:   []insightcontract.TimelineActor{{ID: "executor", Label: "Pessoa A"}, {ID: "owner", Label: "Pessoa B"}},
		Events:   []insightcontract.Event{{ID: "change", EntityID: "item", Type: "state_changed", EventAt: timelineText("2026-01-02T00:00:00Z"), ActorID: timelineText("executor"), OldValue: timelineText("started"), NewValue: timelineText("backlog"), EvidenceIDs: []string{"proof"}}},
		Evidence: []insightcontract.EvidenceView{{ID: "proof", IntegrityStatus: "verified", Excerpt: "Aguardar a dependência antes de retomar o trabalho."}},
		Coverage: []insightcontract.SourceCoverage{{Key: insightcontract.CoverageKey{Source: "linear", ResourceID: "team", Collection: "history"}, Status: "partial"}},
	}
}

func TestDecisionTimelineInfersOperationalIntentWithoutAttributingDecisionMaker(t *testing.T) {
	source := intentFixture()
	items := decisionTimeline(source, nil)
	if len(items) != 1 {
		t.Fatalf("items=%d", len(items))
	}
	item := items[0]
	if item.ActorLabel != "Pessoa A" || item.DecisionMakerID != nil || item.RationaleBasis != "hypothesis" || item.Confidence != "low" || len(item.Alternatives) == 0 {
		t.Fatalf("unfounded attribution: %+v", item)
	}
	if !strings.Contains(strings.Join(item.Limitations, " "), "Histórico parcial") || item.OldValue == nil || *item.NewValue != "backlog" {
		t.Fatalf("lost context: %+v", item)
	}
	source.Events[0].ActorID = nil
	if got := decisionTimeline(source, nil)[0]; got.ActorID != nil || got.ActorLabel == "Pessoa B" {
		t.Fatal("assignee substituted for executor")
	}
}

func TestDecisionTimelineDoesNotInferWithoutVerifiedChange(t *testing.T) {
	for _, mode := range []string{"missing-evidence", "mismatch", "unchanged", "missing-before"} {
		t.Run(mode, func(t *testing.T) {
			source := intentFixture()
			switch mode {
			case "missing-evidence":
				source.Evidence = nil
			case "mismatch":
				source.Evidence[0].IntegrityStatus = "mismatch"
			case "unchanged":
				source.Events[0].OldValue = source.Events[0].NewValue
			case "missing-before":
				source.Events[0].OldValue = nil
			}
			item := decisionTimeline(source, nil)[0]
			if item.RationaleBasis != "unknown" || item.Rationale != "" || item.Confidence != "undetermined" {
				t.Fatalf("invented intention: %+v", item)
			}
		})
	}
}

func TestDecisionTimelineDeclaresOnlyCompleteVerifiedRationale(t *testing.T) {
	source := intentFixture()
	reason := source.Evidence[0].Excerpt
	decision := insightcontract.DecisionRecord{ID: "decision", Method: "source", Rationale: reason, Citations: []insightcontract.Citation{{EvidenceID: "proof", Quote: reason}}, DecisionMakerID: timelineText("owner"), RecordedByID: timelineText("executor")}
	item := decisionTimelineItem(source, decision, []string{"proof"}, timelineEvidenceIndex(source))
	if item.RationaleBasis != "source" || item.ActorID != nil || item.DecisionMakerLabel != "Pessoa B" || item.RecordedByLabel != "Pessoa A" {
		t.Fatalf("roles or reason lost: %+v", item)
	}
	// A source method flag cannot turn a paraphrase or an omitted negation into a declaration.
	source.Evidence[0].Excerpt = "Não " + reason
	item = decisionTimelineItem(source, decision, []string{"proof"}, timelineEvidenceIndex(source))
	if item.RationaleBasis != "hypothesis" {
		t.Fatal("partial quotation promoted to declared rationale")
	}
	decision.Method = "model"
	source.Evidence[0].Excerpt = reason
	if got := decisionTimelineItem(source, decision, []string{"proof"}, timelineEvidenceIndex(source)); got.RationaleBasis != "hypothesis" {
		t.Fatal("model rationale presented as a statement")
	}
}

func TestDecisionTimelinePagesAreReadOnlyAndScoped(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	source := intentFixture()
	unknown := source.Events[0]
	unknown.ID, unknown.EventAt = "unknown-time", nil
	source.Events = append(source.Events, unknown)
	a := analysisArtifacts{ID: "analysis", Snapshot: "snapshot", Source: source}
	if err := store.Put("insightanalyses", a.ID, a); err != nil {
		t.Fatal(err)
	}
	service := &Service{deps: Dependencies{Store: store}} // No provider or collector is available.
	page, err := service.ListDecisionTimeline(context.Background(), a.Snapshot, a.ID, "", 1)
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != "change" || page.NextCursor == "" {
		t.Fatalf("first page: %+v err=%v", page, err)
	}
	last, err := service.ListDecisionTimeline(context.Background(), a.Snapshot, a.ID, page.NextCursor, 1)
	if err != nil || last.Items[0].At != nil || last.NextCursor != "" {
		t.Fatalf("unknown date ordered: %+v err=%v", last, err)
	}
	for _, cursor := range []string{encodeCursor("snapshot:analysis", 1), encodeCursor("other:analysis:"+timelineVersion, 1)} {
		if _, err := service.ListDecisionTimeline(context.Background(), a.Snapshot, a.ID, cursor, 1); err == nil {
			t.Fatal("accepted cursor from another list")
		}
	}
	if _, err := service.ListDecisionTimeline(context.Background(), a.Snapshot, a.ID, "", 201); err == nil {
		t.Fatal("accepted oversized page")
	}
}
