package rules

import (
	"reflect"
	"strings"
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

func TestDeclaredGitRevertRequiresObservedTargetAndDoesNotClaimPatchEquivalence(t *testing.T) {
	targetOID := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	revertOID := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	d := query.Dataset{
		Events: []query.Event{
			{ID: "commit-target", EntityID: "repo-1", Type: "commit_reachable_observed", NewValue: targetOID, EvidenceID: "ev-target"},
			{ID: "commit-revert", EntityID: "repo-1", Type: "commit_reachable_observed", NewValue: revertOID, EvidenceID: "ev-revert"},
			{ID: "declared", EntityID: "repo-1", Type: "commit_revert_declared", OldValue: targetOID, NewValue: revertOID, EventAt: "2026-01-10T00:00:00Z", EvidenceID: "ev-declaration"},
			{ID: "fix-text", EntityID: "repo-1", Type: "commit_reachable_observed", NewValue: "cccccccccccccccccccccccccccccccccccccccc", EvidenceID: "ev-fix"},
		},
	}
	out := Evaluate(d)
	if len(out.Rework) != 1 || out.Rework[0].Kind != "declared_reversal" || out.Rework[0].SubjectID != "commit-target" || out.Rework[0].CauseStatus != "undetermined" || out.Rework[0].EvidenceID != "ev-declaration" {
		t.Fatalf("declaration was lost or unrelated commit became rework: %+v", out.Rework)
	}
	if len(out.Links) != 1 || out.Links[0].Relation != "declares_revert_of" || out.Links[0].SourceID != "commit-revert" || out.Links[0].TargetID != "commit-target" {
		t.Fatalf("declared revert relation missing: %+v", out.Links)
	}
	metricFound := false
	for _, metric := range out.Metrics {
		if metric.ID == metricID("metric-rework", "declared_reversal") {
			metricFound = true
			if !strings.Contains(strings.Join(metric.Limitations, " "), "não foi demonstrada") {
				t.Fatalf("revert metric claims patch equivalence: %+v", metric)
			}
		}
	}
	if !metricFound {
		t.Fatal("declared reversal metric missing")
	}
	again := Evaluate(out)
	if len(again.Rework) != 1 || len(again.Links) != 1 {
		t.Fatalf("rules duplicated reversal on reevaluation: rework=%+v links=%+v", again.Rework, again.Links)
	}
	d.Events = d.Events[1:]
	missing := Evaluate(d)
	if len(missing.Rework) != 0 || len(missing.Links) != 0 {
		t.Fatalf("reversal confirmed without observed target commit: rework=%+v links=%+v", missing.Rework, missing.Links)
	}
	if len(missing.Coverage) != 1 || missing.Coverage[0].Collection != "revert_links" || missing.Coverage[0].Status != "partial" {
		t.Fatalf("unresolved revert did not mark analysis partial: %+v", missing.Coverage)
	}
}
