package rules

import (
	"reflect"
	"strings"
	"testing"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/query"
)

func TestDetailedMetricsSeparateActorsEstimatesReworkAndOpenAging(t *testing.T) {
	zero, five := 0.0, 5.0
	d := query.Dataset{
		Snapshot: contract.Snapshot{WindowStart: "2026-01-01T00:00:00Z", WindowEnd: "2026-01-11T00:00:00Z", AnalysisReferenceAt: "2026-01-11T00:00:00Z"},
		Issues: []query.Issue{
			{ID: "issue-1", State: "done", TeamID: "team-a", CreatedAt: "2026-01-02T00:00:00Z", CompletedAt: "2026-01-05T00:00:00Z", EstimateCurrent: &zero, EvidenceID: "ev-1"},
			{ID: "issue-2", State: "open", TeamID: "team-a", CreatedAt: "2026-01-03T00:00:00Z", EvidenceID: "ev-2"},
			{ID: "issue-old", State: "done", TeamID: "team-b", CompletedAt: "2025-12-20T00:00:00Z", EstimateCurrent: &five, IsContext: true, EvidenceID: "ev-old"},
		},
		PullRequests: []query.PullRequest{
			{ID: "pr-1", AuthorID: "actor-a", MergedAt: "2026-01-07T00:00:00Z"},
			{ID: "pr-later", AuthorID: "actor-a", MergedAt: "2026-01-15T00:00:00Z"},
		},
		Events: []query.Event{
			{ID: "closed", EntityID: "issue-1", Type: "state_changed", OldValue: "started", NewValue: "done", ActorID: "actor-a", EventAt: "2026-01-05T00:00:00Z"},
			{ID: "review-1", EntityID: "pr-1", Type: "human_reviewed", ActorID: "actor-b", EventAt: "2026-01-06T00:00:00Z"},
			{ID: "review-2", EntityID: "pr-1", Type: "human_reviewed", ActorID: "actor-b", EventAt: "2026-01-07T00:00:00Z"},
		},
		Estimates: []contract.EstimateRecord{{ID: "estimate-1", IssueID: "issue-1", Before: &zero, After: &five, Timing: "after_start", EventAt: "2026-01-06T00:00:00Z"}},
		Rework:    []contract.ReworkRecord{{ID: "rework-1", SubjectID: "issue-1", Kind: "reopening", EvidenceStatus: "direct", EventAt: "2026-01-08T00:00:00Z", CauseStatus: "undetermined", EvidenceID: "ev-rework"}},
	}
	a, b := Evaluate(d), Evaluate(d)
	if !reflect.DeepEqual(a.Metrics, b.Metrics) {
		t.Fatal("metrics depend on machine time or map order")
	}
	byID := make(map[string]contract.Metric)
	for _, metric := range a.Metrics {
		if _, exists := byID[metric.ID]; exists {
			t.Fatalf("duplicate metric ID %s", metric.ID)
		}
		byID[metric.ID] = metric
		if strings.Contains(strings.ToLower(metric.Name), "ranking") || strings.Contains(strings.ToLower(metric.Name), "produtividade") {
			t.Fatalf("metric presents people as a ranking: %s", metric.Name)
		}
	}
	want := map[string]string{
		"metric-completed":                               "1",
		metricID("metric-closures-actor", "actor-a"):     "1",
		metricID("metric-merged-prs-author", "actor-a"):  "1",
		metricID("metric-reviewed-prs-actor", "actor-b"): "1",
		"metric-estimate-current-known":                  "1",
		"metric-estimate-current-zero":                   "1",
		"metric-estimate-current-missing":                "1",
		"metric-estimate-after-start-changes":            "1",
		metricID("metric-rework", "reopening"):           "1",
		"metric-rework-undetermined-cause":               "1",
		"metric-open-aging-median":                       "691200",
		"metric-open-aging-p85":                          "691200",
	}
	for id, value := range want {
		metric, ok := byID[id]
		if !ok || metric.Value.Value != value {
			t.Fatalf("metric %s=%+v want %s", id, metric, value)
		}
	}
	if byID["metric-estimate-current-zero"].EligibleSize == nil || *byID["metric-estimate-current-zero"].EligibleSize != 2 {
		t.Fatal("zero estimate lost eligible denominator")
	}
	if _, exists := byID[metricID("metric-estimate-known-team", "team-b")]; exists {
		t.Fatal("historical context counted as current team estimate")
	}
}

func TestContextIssueDoesNotProduceMissingLinkFinding(t *testing.T) {
	d := query.Dataset{
		Issues: []query.Issue{
			{ID: "historical", IsContext: true, EvidenceID: "ev-historical"},
			{ID: "active", EvidenceID: "ev-active"},
		},
		Coverage: []contract.Coverage{{Source: "analysis", Collection: "links", Status: "complete"}},
	}
	out := Evaluate(d)
	if len(out.Findings) != 1 || out.Findings[0].RuleID != "R-LINK-001" || out.Findings[0].SubjectID != "active" {
		t.Fatalf("context issue received missing-link finding: %+v", out.Findings)
	}
}

func TestUnclassifiedGitHubClosureCannotAnchorThirtyDayRework(t *testing.T) {
	d := query.Dataset{
		Snapshot: contract.Snapshot{WindowStart: "2026-01-01T00:00:00Z", WindowEnd: "2026-02-01T00:00:00Z", AnalysisReferenceAt: "2026-02-01T00:00:00Z"},
		Issues:   []query.Issue{{ID: "github-issue-8", Source: "github", State: "open", EvidenceID: "ev-issue"}},
		Events: []query.Event{
			{ID: "close", EntityID: "github-issue-8", Type: "state_changed", OldValue: "open", NewValue: "closed_unclassified", EventAt: "2026-01-03T00:00:00Z", EvidenceID: "ev-close"},
			{ID: "reopen", EntityID: "github-issue-8", Type: "reopened", EventAt: "2026-01-04T00:00:00Z", EvidenceID: "ev-reopen"},
		},
		Rework: []contract.ReworkRecord{{ID: "correction", SubjectID: "github-issue-8", Kind: "declared_correction", EvidenceStatus: "direct", EventAt: "2026-01-05T00:00:00Z", EvidenceID: "ev-pr"}},
	}
	out := Evaluate(d)
	values := make(map[string]string)
	for _, metric := range out.Metrics {
		values[metric.ID] = metric.Value.Value
	}
	if values["metric-rework-30d-observed"] != "0" || values["metric-rework-no-prior-completion"] != "1" || values["metric-declared-correction-30d-observed"] != "0" || values["metric-declared-correction-no-prior-completion"] != "1" {
		t.Fatalf("unclassified closure was treated as delivery: %+v", values)
	}
}

func TestExplicitIssueOutcomesSeparateCancelledDuplicatesAndDeliveries(t *testing.T) {
	d := query.Dataset{
		Snapshot: contract.Snapshot{WindowStart: "2026-01-01T00:00:00Z", WindowEnd: "2026-02-01T00:00:00Z"},
		Issues: []query.Issue{
			{ID: "delivered", Source: "linear", State: "completed", Outcome: "completed", TeamID: "team-a", CompletedAt: "2026-01-08T00:00:00Z"},
			{ID: "cancelled", Source: "linear", State: "canceled", Outcome: "cancelled", TeamID: "team-a", CompletedAt: "2026-01-09T00:00:00Z", EvidenceID: "ev-cancelled"},
			{ID: "duplicate", Source: "github", State: "closed", Outcome: "duplicate", TeamID: "team-a", CompletedAt: "2026-01-10T00:00:00Z", EvidenceID: "ev-duplicate"},
			{ID: "not-planned", Source: "github", State: "closed", Outcome: "not_planned", TeamID: "team-a", CompletedAt: "2026-01-11T00:00:00Z", EvidenceID: "ev-not-planned"},
			{ID: "unknown", Source: "github", State: "closed", Outcome: "unknown", TeamID: "team-a", CompletedAt: "2026-01-12T00:00:00Z"},
		},
	}
	out := Evaluate(d)
	values := make(map[string]contract.Metric)
	for _, metric := range out.Metrics {
		values[metric.ID] = metric
	}
	want := map[string]string{
		"metric-completed":                 "1",
		"metric-issue-outcome-completed":   "1",
		"metric-issue-outcome-cancelled":   "1",
		"metric-issue-outcome-duplicate":   "1",
		"metric-issue-outcome-not-planned": "1",
		"metric-issue-outcome-unknown":     "1",
	}
	for id, value := range want {
		if values[id].Value.Value != value || values[id].EligibleSize == nil && id != "metric-completed" {
			t.Fatalf("outcome metric %s=%+v, want %s", id, values[id], value)
		}
	}
	teamCompleted := values[metricID("metric-completed-team", "team-a")]
	if teamCompleted.Value.Value != "1" {
		t.Fatalf("cancelled or duplicate issue entered team deliveries: %+v", teamCompleted)
	}
	var classifiedFindings int
	for _, finding := range out.Findings {
		if finding.RuleID == "R-OUTCOME-001" {
			classifiedFindings++
			if finding.EvidenceStatus != "direct" || len(finding.EvidenceIDs) != 1 {
				t.Fatalf("classification lost source evidence: %+v", finding)
			}
		}
	}
	if classifiedFindings != 3 {
		t.Fatalf("non-delivery outcomes missing from report: %+v", out.Findings)
	}
}

func TestPeriodMetricsExcludeOldCompletionAndReviewAfterMetadataEdit(t *testing.T) {
	wait := int64(3600)
	d := query.Dataset{
		Snapshot:     contract.Snapshot{WindowStart: "2026-01-01T00:00:00Z", WindowEnd: "2026-01-11T00:00:00Z"},
		Issues:       []query.Issue{{ID: "old-issue", State: "done", CompletedAt: "2025-12-20T00:00:00Z", UpdatedAt: "2026-01-10T00:00:00Z"}},
		PullRequests: []query.PullRequest{{ID: "old-pr", ReadyAt: "2025-12-19T00:00:00Z", FirstHumanReviewAt: "2025-12-20T00:00:00Z", ReviewWaitSeconds: &wait, UpdatedAt: "2026-01-10T00:00:00Z"}},
	}
	metrics := metrics(d)
	if len(metrics) == 0 || metrics[0].ID != "metric-completed" || metrics[0].Value.Value != "0" {
		t.Fatalf("historical completion counted in current window: %+v", metrics)
	}
	for _, metric := range metrics {
		if metric.ID == "metric-review-median" || metric.ID == "metric-review-p85" {
			t.Fatalf("historical review counted after metadata edit: %+v", metric)
		}
	}
}

func TestEstimateBeforeStartNeedsCompleteHistoryWithinWindow(t *testing.T) {
	zero, three, five := 0.0, 3.0, 5.0
	base := query.Dataset{
		Snapshot: contract.Snapshot{WindowStart: "2026-01-01T00:00:00Z", WindowEnd: "2026-02-01T00:00:00Z"},
		Coverage: []contract.Coverage{{Source: "linear", ResourceID: "team-a", Collection: "history", Status: "complete"}},
		Issues: []query.Issue{
			{ID: "linear-1", Source: "linear", TeamID: "team-a", StartedAt: "2026-01-05T00:00:00Z"},
			{ID: "linear-2", Source: "linear", TeamID: "team-a", StartedAt: "2026-01-05T00:00:00Z"},
			{ID: "linear-old", Source: "linear", TeamID: "team-a", StartedAt: "2025-12-05T00:00:00Z"},
		},
		Estimates: []contract.EstimateRecord{
			{ID: "a", IssueID: "linear-1", EventAt: "2026-01-03T00:00:00Z", After: &zero},
			{ID: "b", IssueID: "linear-1", EventAt: "2026-01-06T00:00:00Z", Before: &zero, After: &five},
			{ID: "c", IssueID: "linear-2", EventAt: "2026-01-06T00:00:00Z", Before: &three, After: &five},
			{ID: "d", IssueID: "linear-old", EventAt: "2026-01-06T00:00:00Z", Before: &three, After: &five},
		},
	}
	out := Evaluate(base)
	if out.Issues[0].EstimateBeforeStart == nil || *out.Issues[0].EstimateBeforeStart != 0 || out.Issues[1].EstimateBeforeStart == nil || *out.Issues[1].EstimateBeforeStart != 3 || out.Issues[2].EstimateBeforeStart != nil {
		t.Fatalf("incorrect before-start estimates: %+v", out.Issues)
	}
	for _, metric := range out.Metrics {
		if metric.ID == "metric-estimate-before-start-known" && metric.Value.Value != "2" {
			t.Fatalf("before-start known count: %+v", metric)
		}
	}
	base.Coverage = append(base.Coverage, contract.Coverage{Source: "linear", ResourceID: "team-a", Collection: "history", Status: "partial"})
	out = Evaluate(base)
	if out.Issues[0].EstimateBeforeStart != nil || out.Issues[1].EstimateBeforeStart != nil {
		t.Fatalf("partial history reconstructed pre-start estimate: %+v", out.Issues)
	}
}

func TestReopenedIssueStaysInOpenAging(t *testing.T) {
	d := query.Dataset{
		Snapshot: contract.Snapshot{AnalysisReferenceAt: "2026-01-11T00:00:00Z"},
		Issues:   []query.Issue{{ID: "reopened", State: "reopened", CreatedAt: "2026-01-01T00:00:00Z", CompletedAt: "2026-01-05T00:00:00Z"}},
	}
	metrics := openAgingMetrics(d)
	if len(metrics) != 2 || metrics[0].Value.Value != "864000" {
		t.Fatalf("reopened issue omitted from open aging: %+v", metrics)
	}
}

func TestObservedAssigneeAndClosureActorRemainSeparate(t *testing.T) {
	d := query.Dataset{
		Snapshot: contract.Snapshot{WindowStart: "2026-01-01T00:00:00Z", WindowEnd: "2026-02-01T00:00:00Z"},
		Issues: []query.Issue{
			{ID: "assigned", AssigneeID: "assignee-1", State: "done", CompletedAt: "2026-01-10T00:00:00Z"},
			{ID: "unassigned", State: "done", CompletedAt: "2026-01-11T00:00:00Z"},
			{ID: "old", AssigneeID: "assignee-1", State: "done", CompletedAt: "2025-12-10T00:00:00Z"},
		},
		Events: []query.Event{{ID: "closure", EntityID: "assigned", Type: "state_changed", OldValue: "started", NewValue: "done", ActorID: "recorder-1", EventAt: "2026-01-10T00:00:00Z"}},
	}
	got := map[string]contract.Metric{}
	for _, metric := range actorMetrics(d) {
		got[metric.ID] = metric
	}
	for id, value := range map[string]string{
		metricID("metric-closures-actor", "recorder-1"):     "1",
		metricID("metric-completed-assignee", "assignee-1"): "1",
		"metric-completed-assignee-unknown":                 "1",
	} {
		if metric, ok := got[id]; !ok || metric.Value.Value != value {
			t.Fatalf("metric %s=%+v want %s", id, metric, value)
		}
	}
	if got[metricID("metric-completed-assignee", "assignee-1")].EligibleSize == nil || *got[metricID("metric-completed-assignee", "assignee-1")].EligibleSize != 2 {
		t.Fatal("assignee metric lost completed-issue denominator")
	}
}

func TestCycleTimeMetricsUseOnlyValidCompletedIssuesInTeam(t *testing.T) {
	a, b, c, invalid := int64(100), int64(300), int64(500), int64(900)
	d := query.Dataset{
		Snapshot: contract.Snapshot{WindowStart: "2026-01-01T00:00:00Z", WindowEnd: "2026-02-01T00:00:00Z"},
		Issues: []query.Issue{
			{ID: "a", TeamID: "team-a", CompletedAt: "2026-01-04T00:00:00Z", DurationValid: true, CycleSeconds: &a},
			{ID: "b", TeamID: "team-a", CompletedAt: "2026-01-05T00:00:00Z", DurationValid: true, CycleSeconds: &b},
			{ID: "c", TeamID: "team-a", CompletedAt: "2026-01-06T00:00:00Z", DurationValid: true, CycleSeconds: &c},
			{ID: "no-start", TeamID: "team-a", CompletedAt: "2026-01-06T00:00:00Z", CycleSeconds: &invalid},
			{ID: "old", TeamID: "team-a", CompletedAt: "2025-12-06T00:00:00Z", DurationValid: true, CycleSeconds: &invalid},
			{ID: "open", TeamID: "team-a", DurationValid: true, CycleSeconds: &invalid},
		},
	}
	metrics := cycleTimeMetrics(d)
	if len(metrics) != 2 || metrics[0].Value.Value != "300" || metrics[1].Value.Value != "500" {
		t.Fatalf("cycle median/p85 include wrong issue: %+v", metrics)
	}
	for _, metric := range metrics {
		if metric.SampleSize != 3 || metric.EligibleSize == nil || *metric.EligibleSize != 4 || !strings.Contains(strings.Join(metric.Limitations, " "), "Amostra insuficiente") {
			t.Fatalf("cycle cohort or sample disclosure missing: %+v", metric)
		}
	}
}

func TestReworkThirtyDayObservationWindowDoesNotInferAbsence(t *testing.T) {
	d := query.Dataset{
		Snapshot: contract.Snapshot{WindowStart: "2026-01-01T00:00:00Z", WindowEnd: "2026-03-10T00:00:00Z", AnalysisReferenceAt: "2026-03-10T00:00:00Z"},
		Issues: []query.Issue{
			{ID: "within"}, {ID: "after-window"}, {ID: "recent"}, {ID: "unknown"},
		},
		Events: []query.Event{
			{ID: "done-1", EntityID: "within", Type: "state_changed", OldValue: "started", NewValue: "done", EventAt: "2026-01-02T00:00:00Z"},
			{ID: "done-2", EntityID: "after-window", Type: "state_changed", OldValue: "started", NewValue: "done", EventAt: "2026-01-03T00:00:00Z"},
			{ID: "done-3", EntityID: "recent", Type: "state_changed", OldValue: "started", NewValue: "done", EventAt: "2026-03-01T00:00:00Z"},
		},
		Rework: []contract.ReworkRecord{
			{ID: "r-1", SubjectID: "within", Kind: "reopening", EvidenceStatus: "direct", EventAt: "2026-01-20T00:00:00Z"},
			{ID: "r-2", SubjectID: "after-window", Kind: "reopening", EvidenceStatus: "direct", EventAt: "2026-02-10T00:00:00Z"},
			{ID: "r-3", SubjectID: "unknown", Kind: "reopening", EvidenceStatus: "direct", EventAt: "2026-02-10T00:00:00Z"},
		},
	}
	got := make(map[string]contract.Metric)
	for _, metric := range reworkWindowMetrics(d) {
		got[metric.ID] = metric
	}
	for id, value := range map[string]string{
		"metric-rework-30d-observed":           "1",
		"metric-rework-after-30d-observed":     "1",
		"metric-rework-30d-elapsed-windows":    "2",
		"metric-rework-30d-incomplete-windows": "1",
		"metric-rework-no-prior-completion":    "1",
	} {
		if metric, ok := got[id]; !ok || metric.Value.Value != value {
			t.Fatalf("metric %s=%+v want %s", id, metric, value)
		}
		if strings.Contains(strings.ToLower(got[id].Name), "taxa") {
			t.Fatalf("rate claimed from incomplete coverage: %+v", got[id])
		}
	}
}

func TestReworkWindowUsesGeneratedReopening(t *testing.T) {
	d := query.Dataset{
		Snapshot: contract.Snapshot{WindowStart: "2026-01-01T00:00:00Z", WindowEnd: "2026-02-20T00:00:00Z", AnalysisReferenceAt: "2026-02-20T00:00:00Z"},
		Issues:   []query.Issue{{ID: "issue-1", EvidenceID: "ev-issue"}},
		Events: []query.Event{
			{ID: "done", EntityID: "issue-1", Type: "state_changed", OldValue: "started", NewValue: "done", EventAt: "2026-01-03T00:00:00Z", EvidenceID: "ev-done"},
			{ID: "reopened", EntityID: "issue-1", Type: "state_changed", OldValue: "done", NewValue: "reopened", EventAt: "2026-01-10T00:00:00Z", EvidenceID: "ev-reopen"},
		},
	}
	out := Evaluate(d)
	for _, metric := range out.Metrics {
		if metric.ID == "metric-rework-30d-observed" {
			if metric.Value.Value != "1" {
				t.Fatalf("generated reopening not counted: %+v", metric)
			}
			return
		}
	}
	t.Fatal("30-day rework metric missing")
}

func TestReworkWindowDoesNotOrderSameTimestampEvents(t *testing.T) {
	d := query.Dataset{
		Snapshot: contract.Snapshot{WindowStart: "2026-01-01T00:00:00Z", WindowEnd: "2026-02-20T00:00:00Z"},
		Issues:   []query.Issue{{ID: "issue-1"}},
		Events:   []query.Event{{ID: "done", EntityID: "issue-1", Type: "state_changed", OldValue: "started", NewValue: "done", EventAt: "2026-01-03T00:00:00Z"}},
		Rework:   []contract.ReworkRecord{{ID: "reopened", SubjectID: "issue-1", Kind: "reopening", EvidenceStatus: "direct", EventAt: "2026-01-03T00:00:00Z"}},
	}
	metrics := reworkWindowMetrics(d)
	for _, metric := range metrics {
		if metric.ID == "metric-rework-no-prior-completion" && metric.Value.Value == "1" {
			return
		}
	}
	t.Fatalf("same-timestamp events were ordered without evidence: %+v", metrics)
}

func TestDeclaredCorrectionThirtyDayWindowKeepsUnknownAnchor(t *testing.T) {
	d := query.Dataset{
		Snapshot: contract.Snapshot{WindowStart: "2026-01-01T00:00:00Z", WindowEnd: "2026-03-10T00:00:00Z", AnalysisReferenceAt: "2026-03-10T00:00:00Z"},
		Issues:   []query.Issue{{ID: "within"}, {ID: "later"}, {ID: "unknown"}},
		Events: []query.Event{
			{ID: "done-1", EntityID: "within", Type: "state_changed", OldValue: "open", NewValue: "closed", EventAt: "2026-01-02T00:00:00Z"},
			{ID: "done-2", EntityID: "later", Type: "state_changed", OldValue: "open", NewValue: "closed", EventAt: "2026-01-03T00:00:00Z"},
		},
		Rework: []contract.ReworkRecord{
			{ID: "fix-1", SubjectID: "within", Kind: "declared_correction", EvidenceStatus: "direct", EventAt: "2026-01-20T00:00:00Z"},
			{ID: "fix-2", SubjectID: "later", Kind: "declared_correction", EvidenceStatus: "direct", EventAt: "2026-02-10T00:00:00Z"},
			{ID: "fix-3", SubjectID: "unknown", Kind: "declared_correction", EvidenceStatus: "direct", EventAt: "2026-02-10T00:00:00Z"},
		},
	}
	got := map[string]contract.Metric{}
	for _, metric := range reworkWindowMetrics(d) {
		got[metric.ID] = metric
	}
	for id, value := range map[string]string{
		"metric-declared-correction-30d-observed":        "1",
		"metric-declared-correction-after-30d-observed":  "1",
		"metric-declared-correction-no-prior-completion": "1",
	} {
		metric, ok := got[id]
		if !ok || metric.Value.Value != value || metric.EligibleSize == nil || *metric.EligibleSize != 3 {
			t.Fatalf("correction metric %s=%+v want %s of 3", id, metric, value)
		}
	}
}
