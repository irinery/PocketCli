package rules

import (
	"reflect"
	"testing"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/query"
)

func TestDeterministicRulesAndUnknownCause(t *testing.T) {
	before, after := 3.0, 5.0
	wait := int64(172800)
	d := query.Dataset{
		Issues:       []query.Issue{{ID: "iss-1", Identifier: "UNI-1", State: "done", CompletedAt: "2026-01-04T00:00:00Z", EvidenceID: "ev-1"}, {ID: "iss-3", Identifier: "UNI-3", State: "open", EvidenceID: "ev-3"}},
		PullRequests: []query.PullRequest{{ID: "pr-10", ReadyAt: "2026-01-02T00:00:00Z", FirstHumanReviewAt: "2026-01-04T00:00:00Z", ReviewWaitSeconds: &wait, EvidenceID: "ev-4", MergeSHA: "new-sha"}},
		Coverage:     []contract.Coverage{{Source: "linear", ResourceID: "team", Collection: "links", Status: "complete"}},
		Links:        []contract.LinkRecord{{ID: "link-1", SourceID: "iss-1", TargetID: "pr-10", Relation: "references_pr", Status: "direct", EvidenceID: "ev-1"}, {ID: "link-2", SourceID: "pr-10", TargetID: "cmp-api", Relation: "modifies_component", Status: "direct", EvidenceID: "ev-4"}},
		Events:       []query.Event{{ID: "evt-1", EntityID: "iss-1", Type: "reopened", EventAt: "2026-01-03T00:00:00Z", EvidenceID: "ev-7"}, {ID: "evt-ready", EntityID: "pr-10", Type: "ready_for_review", EventAt: "2026-01-02T00:00:00Z", EvidenceID: "ev-ready"}, {ID: "evt-review", EntityID: "pr-10", Type: "human_reviewed", EventAt: "2026-01-04T00:00:00Z", EvidenceID: "ev-review"}},
		Estimates:    []contract.EstimateRecord{{ID: "est-1", IssueID: "iss-1", Before: &before, After: &after, Timing: "after_start", EvidenceID: "ev-6"}},
		Deliveries:   []contract.DeliveryRecord{{ID: "art-1", ComponentID: "cmp-api", RevisionSHA: "old-sha", ProvenanceStatus: "declared", EvidenceID: "ev-8"}},
	}
	a := Evaluate(d)
	b := Evaluate(d)
	if !reflect.DeepEqual(a.Findings, b.Findings) || !reflect.DeepEqual(a.Metrics, b.Metrics) {
		t.Fatal("rules are not reproducible")
	}
	want := map[string]bool{"R-LINK-001": false, "R-FLOW-001": false, "R-REOPEN-001": false, "R-EST-001": false, "R-DELIVERY-001": false}
	for _, f := range a.Findings {
		if _, ok := want[f.RuleID]; ok {
			want[f.RuleID] = true
		}
		if f.RuleID == "R-DELIVERY-001" && len(f.EvidenceIDs) != 2 {
			t.Fatal("delivery evidence incomplete")
		}
		if f.RuleID == "R-FLOW-001" && (len(f.EvidenceIDs) != 2 || f.EvidenceIDs[0] != "ev-ready" || f.EvidenceIDs[1] != "ev-review") {
			t.Fatalf("review wait evidence incomplete: %+v", f.EvidenceIDs)
		}
	}
	for id, found := range want {
		if !found {
			t.Fatalf("rule %s missing", id)
		}
	}
	if len(a.Rework) != 1 || a.Rework[0].CauseStatus != "undetermined" {
		t.Fatal("reopening invented a cause")
	}
	if a.Metrics[0].Value.Value != "1" {
		t.Fatal("completed metric incorrect")
	}
}
