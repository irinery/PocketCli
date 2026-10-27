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

func TestBlockFindingRequiresBothActiveStatesAndDirectEvidence(t *testing.T) {
	base := query.Dataset{
		Issues: []query.Issue{
			{ID: "dependent", State: "unstarted", Outcome: "active", EvidenceID: "ev-dependent"},
			{ID: "blocker", State: "started", Outcome: "active", EvidenceID: "ev-blocker"},
		},
		Links: []contract.LinkRecord{{ID: "link-block", SourceID: "dependent", Relation: "blocked_by", TargetID: "blocker", Status: "direct", EvidenceID: "ev-link"}},
	}
	blockFindings := func(d query.Dataset) []contract.Finding {
		t.Helper()
		var found []contract.Finding
		for _, finding := range Evaluate(d).Findings {
			if finding.RuleID == "R-BLOCK-001" {
				found = append(found, finding)
			}
		}
		return found
	}
	findings := blockFindings(base)
	if len(findings) != 1 || !reflect.DeepEqual(findings[0].EvidenceIDs, []string{"ev-blocker", "ev-dependent", "ev-link"}) {
		t.Fatalf("known active dependency lacks direct evidence: %+v", findings)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*query.Dataset)
	}{
		{"blocker unknown", func(d *query.Dataset) { d.Issues[1].State, d.Issues[1].Outcome = "", "unclassified" }},
		{"dependent unknown", func(d *query.Dataset) { d.Issues[0].State, d.Issues[0].Outcome = "", "unclassified" }},
		{"blocker finished", func(d *query.Dataset) { d.Issues[1].State, d.Issues[1].Outcome = "completed", "completed" }},
		{"contradictory blocker state", func(d *query.Dataset) { d.Issues[1].State = "closed" }},
		{"dependent finished", func(d *query.Dataset) { d.Issues[0].State, d.Issues[0].Outcome = "completed", "completed" }},
		{"blocker cancelled", func(d *query.Dataset) { d.Issues[1].State, d.Issues[1].Outcome = "canceled", "cancelled" }},
		{"missing state evidence", func(d *query.Dataset) { d.Issues[1].EvidenceID = "" }},
		{"missing relation evidence", func(d *query.Dataset) { d.Links[0].EvidenceID = "" }},
		{"inferred relation", func(d *query.Dataset) { d.Links[0].Status = "inferred" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := base
			d.Issues = append([]query.Issue(nil), base.Issues...)
			d.Links = append([]contract.LinkRecord(nil), base.Links...)
			tc.mutate(&d)
			if got := blockFindings(d); len(got) != 0 {
				t.Fatalf("unsupported blocker became pending: %+v", got)
			}
		})
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

func TestDeclaredFixAfterObservedReopeningIsSeparatedFromGenericReference(t *testing.T) {
	d := query.Dataset{
		Snapshot: contract.Snapshot{WindowStart: "2026-01-01T00:00:00Z", WindowEnd: "2026-02-01T00:00:00Z"},
		Issues:   []query.Issue{{ID: "issue-8", EvidenceID: "ev-issue"}},
		PullRequests: []query.PullRequest{
			{ID: "pr-fix", MergedAt: "2026-01-20T00:00:00Z", EvidenceID: "ev-pr-fix"},
			{ID: "pr-generic", MergedAt: "2026-01-21T00:00:00Z", EvidenceID: "ev-pr-generic"},
			{ID: "pr-before", MergedAt: "2026-01-05T00:00:00Z", EvidenceID: "ev-pr-before"},
			{ID: "pr-same", MergedAt: "2026-01-10T00:00:00Z", EvidenceID: "ev-pr-same"},
		},
		Events: []query.Event{{ID: "reopened", EntityID: "issue-8", Type: "reopened", EventAt: "2026-01-10T00:00:00Z", EvidenceID: "ev-reopen"}},
		Links: []contract.LinkRecord{
			{ID: "fix", SourceID: "pr-fix", Relation: "declares_fix_of", TargetID: "issue-8", Status: "direct", EvidenceID: "ev-pr-fix"},
			{ID: "generic", SourceID: "issue-8", Relation: "references_pr", TargetID: "pr-generic", Status: "direct", EvidenceID: "ev-pr-generic"},
			{ID: "before", SourceID: "pr-before", Relation: "declares_fix_of", TargetID: "issue-8", Status: "direct", EvidenceID: "ev-pr-before"},
			{ID: "same", SourceID: "pr-same", Relation: "declares_fix_of", TargetID: "issue-8", Status: "direct", EvidenceID: "ev-pr-same"},
		},
	}
	out := Evaluate(d)
	fixes := 0
	for _, record := range out.Rework {
		if record.Kind == "declared_correction" {
			fixes++
			if record.SubjectID != "issue-8" || record.EventAt != "2026-01-20T00:00:00Z" || record.EvidenceID != "ev-pr-fix" || record.CauseStatus != "undetermined" {
				t.Fatalf("incorrect correction record: %+v", record)
			}
		}
	}
	if fixes != 1 {
		t.Fatalf("generic or earlier PR became correction: %+v", out.Rework)
	}
	metricFound, findingFound := false, false
	for _, metric := range out.Metrics {
		if metric.ID == metricID("metric-rework", "declared_correction") {
			metricFound = metric.Value.Value == "1" && strings.Contains(strings.Join(metric.Limitations, " "), "não demonstra efeito")
		}
	}
	for _, finding := range out.Findings {
		if finding.RuleID == "R-CORRECTION-001" {
			findingFound = len(finding.EvidenceIDs) == 2 && finding.EvidenceIDs[0] == "ev-pr-fix" && finding.EvidenceIDs[1] == "ev-reopen"
		}
	}
	if !metricFound || !findingFound {
		t.Fatalf("correction declaration missing metric or two-source finding: metrics=%+v findings=%+v", out.Metrics, out.Findings)
	}
	withoutReopen := d
	withoutReopen.Events = nil
	for _, record := range Evaluate(withoutReopen).Rework {
		if record.Kind == "declared_correction" {
			t.Fatalf("fix declaration became rework without observed reopening: %+v", record)
		}
	}
}
