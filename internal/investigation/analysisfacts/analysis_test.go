package analysisfacts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"pocketcli/internal/investigation/insightcontract"
)

type fixture struct {
	Config insightcontract.AnalysisConfig `json:"config"`
	Source insightcontract.SourceBundle   `json:"source"`
}

func loadFixture(t *testing.T) fixture {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "insightcontract", "testdata", "fixture_go.json"))
	if err != nil {
		t.Fatal(err)
	}
	var f fixture
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestAnalyzeFixtureKeepsCompletionCohortsAndContextualAgingSeparate(t *testing.T) {
	f := loadFixture(t)
	completedBefore := openEntity("ISS-BEFORE", "2026-09-01T00:00:00Z", false)
	completedBefore.State, completedBefore.Outcome, completedBefore.CompletedAt = "completed", "completed", stringPtr("2026-10-20T00:00:00Z")
	stateOnly := openEntity("ISS-STATE-ONLY", "2026-09-01T00:00:00Z", false)
	stateOnly.State, stateOnly.Outcome = "completed", "completed"
	transitionOnly := openEntity("ISS-TRANSITION", "2026-09-01T00:00:00Z", false)
	transitionOnly.State, transitionOnly.StateTemporalStatus = "unknown", "unknown"
	f.Source.Entities = append(f.Source.Entities, completedBefore, stateOnly, transitionOnly)
	transitionAt := "2026-10-24T00:00:00Z"
	f.Source.Events = []insightcontract.Event{{ID: "transition-only", EntityID: transitionOnly.ID, Type: "state_changed", EventAt: &transitionAt, OldValue: stringPtr("started"), NewValue: stringPtr("completed")}}
	got, err := Analyze(f.Source, f.Config)
	if err != nil {
		t.Fatal(err)
	}
	if cohortByDefinitionContains(got.Cohorts, "completed_at") == nil {
		t.Fatal("missing completed_at cohort")
	}
	completed := cohortByKey(got.Cohorts, "completed-at-report-window")
	current := cohortByKey(got.Cohorts, "completed-at-snapshot")
	transitions := cohortByKey(got.Cohorts, "completion-transitions-report-window")
	if !has(completed.EntityIDs, "ISS-DONE") || has(completed.EntityIDs, "ISS-BEFORE") || has(completed.EntityIDs, "ISS-STATE-ONLY") {
		t.Fatalf("completed_at window cohort incorrect: %#v", completed.EntityIDs)
	}
	if !has(current.EntityIDs, "ISS-DONE") || !has(current.EntityIDs, "ISS-BEFORE") || !has(current.EntityIDs, "ISS-STATE-ONLY") {
		t.Fatalf("snapshot completion cohort incorrect: %#v", current.EntityIDs)
	}
	if !has(transitions.EntityIDs, "ISS-TRANSITION") || has(transitions.EntityIDs, "ISS-DONE") {
		t.Fatalf("expected fixture issue in each semantically distinct cohort: %#v %#v %#v", completed.EntityIDs, current.EntityIDs, transitions.EntityIDs)
	}
	context := cohortByKey(got.Cohorts, "open-aging-context")
	if !has(context.EntityIDs, "ISS-OLD") || context.ReferenceAt != f.Source.Snapshot.AnalysisReferenceAt {
		t.Fatalf("old contextual stock missing or wrong anchor: %#v", context)
	}
	if context.Count != 1 {
		t.Fatalf("contextual stock count=%d, want 1", context.Count)
	}
}

func TestAnalyzeUsesSemiOpenWindowAndNoReferenceFallback(t *testing.T) {
	f := loadFixture(t)
	start, end := f.Config.RequestedScope.ReportWindow.Start, f.Config.RequestedScope.ReportWindow.End
	f.Source.Events = nil
	f.Source.Events = []insightcontract.Event{
		{ID: "at-start", EntityID: "ISS-PLAN", Type: "state_changed", EventAt: &start, NewValue: stringPtr("completed")},
		{ID: "at-end", EntityID: "ISS-OLD", Type: "state_changed", EventAt: &end, NewValue: stringPtr("completed")},
	}
	got, err := Analyze(f.Source, f.Config)
	if err != nil {
		t.Fatal(err)
	}
	if c := cohortByKey(got.Cohorts, "completion-transitions-report-window"); c.Count != 1 || !has(c.EntityIDs, "ISS-PLAN") || has(c.EntityIDs, "ISS-OLD") {
		t.Fatalf("semi-open boundaries not respected: %#v", c)
	}
	f.Source.Snapshot.AnalysisReferenceAt = "invalid"
	got, err = Analyze(f.Source, f.Config)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range got.Metrics {
		if m.Name == "Mediana de aging contextual" && (m.Value != nil || m.State != statusNotCalculable) {
			t.Fatalf("invalid anchor must not fall back: %#v", m)
		}
	}
}

func TestAnalyzeMarksWindowExtrapolationPartialAndEligibleUnknown(t *testing.T) {
	f := loadFixture(t)
	f.Config.RequestedScope.ReportWindow.End = "2026-11-01T00:00:00Z"
	got, err := Analyze(f.Source, f.Config)
	if err != nil {
		t.Fatal(err)
	}
	cohort := cohortByKey(got.Cohorts, "completed-at-report-window")
	if cohort == nil || cohort.CoverageStatus != coveragePartial || cohort.EligibleCount != nil {
		t.Fatalf("extrapolated report window must keep observed count partial and universe unknown: %#v", cohort)
	}
}

func TestAnalyzeGroupsOnlyExplicitTemporallyValidDependenciesAndReconcilesLegacy(t *testing.T) {
	f := loadFixture(t)
	valid := insightcontract.Link{ID: "L2", FromID: "ISS-PLAN", ToID: "ISS-OLD", Relation: "blocked_by", Status: "explicit", TemporalStatus: "observed_at_reference", EvidenceIDs: []string{"EV-LINK"}}
	f.Source.Links = append(f.Source.Links, valid, insightcontract.Link{ID: "candidate", FromID: "ISS-PLAN", ToID: "ISS-OLD", Relation: "blocked_by", Status: "candidate", TemporalStatus: "observed_at_reference"}, insightcontract.Link{ID: "unknown-time", FromID: "ISS-PLAN", ToID: "ISS-OLD", Relation: "blocked_by", Status: "explicit", TemporalStatus: "unknown"})
	f.Source.Links = append(f.Source.Links, insightcontract.Link{ID: "incomplete-interval", FromID: "ISS-PLAN", ToID: "ISS-OLD", Relation: "blocked_by", Status: "explicit", TemporalStatus: "known_interval", ValidFrom: stringPtr("2026-10-01T00:00:00Z")})
	f.Source.Links = append(f.Source.Links, insightcontract.Link{ID: "expired", FromID: "ISS-PLAN", ToID: "ISS-OLD", Relation: "blocked_by", Status: "explicit", TemporalStatus: "known_interval", ValidFrom: stringPtr("2026-10-01T00:00:00Z"), ValidTo: stringPtr("2026-10-20T00:00:00Z")})
	f.Source.LegacyFindings = append(f.Source.LegacyFindings, insightcontract.LegacyFinding{ID: "LEGACY-OTHER", RuleID: "R-BLOCK-001", SubjectID: "other"})
	got, err := Analyze(f.Source, f.Config)
	if err != nil {
		t.Fatal(err)
	}
	dep := factCaseByKindAndClassification(got.Cases, "dependency", "planned_dependency")
	if dep == nil || len(dep.LinkIDs) != 2 || !has(dep.LegacyFindingIDs, "FINDING-DEMO") || has(dep.LegacyFindingIDs, "LEGACY-OTHER") {
		t.Fatalf("bad grouped dependency/reconciliation: %#v", dep)
	}
	if metric := metricByName(got.Metrics, "Links blocked_by explícitos"); metric == nil || metric.Value == nil || *metric.Value != 2 {
		t.Fatalf("raw links must remain separate from distinct pairs: %#v", metric)
	}
	if metric := metricByName(got.Metrics, "Pares blocked_by distintos"); metric == nil || metric.Value == nil || *metric.Value != 1 {
		t.Fatalf("distinct pair count missing: %#v", metric)
	}
	if factCaseByKindAndClassification(got.Cases, "dependency", "unclassified") == nil || !caseByFinding(got.Cases, "LEGACY-OTHER") {
		t.Fatal("active dependency and unmatched legacy finding should remain explicit facts")
	}
	for _, id := range []string{"candidate", "unknown-time", "incomplete-interval", "expired"} {
		if has(dep.LinkIDs, id) {
			t.Fatalf("ineligible link included in grouped case: %#v", dep)
		}
	}
	for _, id := range []string{"candidate", "unknown-time", "incomplete-interval", "expired"} {
		if !caseByLink(got.Cases, id) {
			t.Fatalf("ineligible relation diagnostic missing for %s", id)
		}
	}
}

func TestAnalyzeDoesNotInventCommitmentOrOwner(t *testing.T) {
	f := loadFixture(t)
	got, err := Analyze(f.Source, f.Config)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range got.Cases {
		if c.Classification == "commitment_at_risk" {
			t.Fatalf("invented commitment: %#v", c)
		}
	}
	due := "2026-10-20T00:00:00Z"
	owner := "owner-1"
	for i := range f.Source.Entities {
		if f.Source.Entities[i].ID == "ISS-PLAN" {
			f.Source.Entities[i].DueAt = &due
			f.Source.Entities[i].OwnerID = &owner
		}
	}
	f.Config.Objectives = []insightcontract.Objective{{ID: "OBJ-1", EntityIDs: []string{"ISS-PLAN"}, Deadline: &due, OwnerID: &owner}}
	got, err = Analyze(f.Source, f.Config)
	if err != nil {
		t.Fatal(err)
	}
	if factCaseByKindAndClassification(got.Cases, "dependency", "commitment_at_risk") == nil {
		t.Fatal("explicit expired deadline with applicable dependency was not surfaced")
	}
	f.Config.Objectives[0].OwnerID = nil
	for i := range f.Source.Entities {
		if f.Source.Entities[i].ID == "ISS-PLAN" {
			f.Source.Entities[i].OwnerID = nil
		}
	}
	got, err = Analyze(f.Source, f.Config)
	if err != nil {
		t.Fatal(err)
	}
	caseAtRisk := factCaseByKindAndClassification(got.Cases, "dependency", "commitment_at_risk")
	if caseAtRisk == nil || !has(caseAtRisk.OpenQuestions, "owner explícito ausente; responsabilidade desconhecida") {
		t.Fatalf("missing owner must remain unknown: %#v", caseAtRisk)
	}
}

func TestAnalyzeReworkMaturityRequiresContinuousCoverage(t *testing.T) {
	f := loadFixture(t)
	ref := f.Source.Snapshot.AnalysisReferenceAt
	start, end := "2026-09-28T00:00:00Z", ref
	reopenAt, reversalAt, correctionAt, linkEnd := "2026-10-01T00:00:00Z", "2026-10-02T00:00:00Z", "2026-10-03T00:00:00Z", "2026-10-29T00:00:00Z"
	f.Source.Events = append(f.Source.Events, insightcontract.Event{ID: "EVENT-REOPEN", EntityID: "ISS-DONE", Type: "issue_reopened", EventAt: &reopenAt, EvidenceIDs: []string{"EV-HISTORY"}})
	f.Source.Links = append(f.Source.Links,
		insightcontract.Link{ID: "LINK-REVERT", FromID: "COMMIT-2", ToID: "COMMIT-1", Relation: "declares_revert_of", Status: "explicit", TemporalStatus: "known_interval", ValidFrom: &reversalAt, ValidTo: &linkEnd},
		insightcontract.Link{ID: "LINK-FIX", FromID: "COMMIT-3", ToID: "ISS-DONE", Relation: "declares_fix_of", Status: "explicit", TemporalStatus: "known_interval", ValidFrom: &correctionAt, ValidTo: &linkEnd},
	)
	f.Source.Coverage = []insightcontract.SourceCoverage{
		{Key: insightcontract.CoverageKey{Source: "linear", ResourceID: "TEAM-DEMO", Collection: "history"}, Status: "complete", HistoryCoveredIntervals: []insightcontract.Window{{Start: start, End: "2026-10-10T00:00:00Z"}}},
		{Key: insightcontract.CoverageKey{Source: "linear", ResourceID: "TEAM-DEMO", Collection: "history"}, Status: "complete", HistoryCoveredIntervals: []insightcontract.Window{{Start: "2026-10-10T00:00:00Z", End: end}}},
		{Key: insightcontract.CoverageKey{Source: "linear", ResourceID: "TEAM-DEMO", Collection: "links"}, Status: "complete", HistoryCoveredIntervals: []insightcontract.Window{{Start: start, End: end}}},
	}
	got, err := Analyze(f.Source, f.Config)
	if err != nil {
		t.Fatal(err)
	}
	m := metricByName(got.Metrics, "Ocorrências de retrabalho observadas em 30 dias")
	if m.State != statusObserved || m.Value == nil {
		t.Fatalf("expected complete continuous coverage to make event count observable: %#v", m)
	}
	for _, name := range []string{"Reaberturas observadas em 30 dias", "Reversões declaradas em 30 dias", "Correções declaradas em 30 dias"} {
		if metricByName(got.Metrics, name) == nil {
			t.Fatalf("missing separate rework category %q", name)
		}
	}
	if m.Value == nil || *m.Value != 3 {
		t.Fatalf("three observed rework records should remain categorized separately: %#v", m)
	}
	if !caseByLink(got.Cases, "LINK-REVERT") || !caseByLink(got.Cases, "LINK-FIX") {
		t.Fatal("rework fact cases must retain source link references")
	}
	f.Source.Coverage[0].HistoryCoveredIntervals = []insightcontract.Window{{Start: start, End: "2026-10-09T00:00:00Z"}}
	f.Source.Coverage[1].HistoryCoveredIntervals = []insightcontract.Window{{Start: "2026-10-10T00:00:00Z", End: end}}
	got, err = Analyze(f.Source, f.Config)
	if err != nil {
		t.Fatal(err)
	}
	m = metricByName(got.Metrics, "Ocorrências de retrabalho observadas em 30 dias")
	if m.State != statusImmature || m.Value == nil || *m.Value != 3 {
		t.Fatalf("gap envelope must remain immature despite observed events: %#v", m)
	}
}

func TestAnalyzeQuantileSampleThresholdDoesNotInferExpectedUniverse(t *testing.T) {
	f := loadFixture(t)
	f.Config.Policy.MinimumQuantileSample = 10
	for i := 0; i < 8; i++ {
		f.Source.Entities = append(f.Source.Entities, openEntity("n"+itoa(i), "2026-10-20T00:00:00Z", false))
	}
	got, err := Analyze(f.Source, f.Config)
	if err != nil {
		t.Fatal(err)
	}
	median := metricByName(got.Metrics, "Mediana de aging não contextual")
	if median.Value != nil || median.State != statusNotCalculable || median.SampleSize != 9 {
		t.Fatalf("n=9 must not publish quantile: %#v", median)
	}
	encoded, err := json.Marshal(median)
	if err != nil {
		t.Fatal(err)
	}
	if !stringsContains(string(encoded), `"value":null`) || !stringsContains(string(encoded), `"eligible_size":null`) {
		t.Fatalf("unknown numeric outputs must serialize as null: %s", encoded)
	}
	f.Source.Entities = append(f.Source.Entities, openEntity("n9", "2026-10-20T00:00:00Z", false))
	got, err = Analyze(f.Source, f.Config)
	if err != nil {
		t.Fatal(err)
	}
	median = metricByName(got.Metrics, "Mediana de aging não contextual")
	if median.Value == nil || median.SampleSize != 10 || median.EligibleSize != nil {
		t.Fatalf("n=10 quantile or denominators missing: %#v", median)
	}
}

func TestAnalyzeReportsUnknownCoverageWithoutInventingCompleteness(t *testing.T) {
	f := loadFixture(t)
	f.Source.Coverage[0].Expected = nil
	f.Source.Coverage[0].Status = "partial"
	got, err := Analyze(f.Source, f.Config)
	if err != nil {
		t.Fatal(err)
	}
	cohort := cohortByKey(got.Cohorts, "completed-at-report-window")
	if cohort.CoverageStatus != coveragePartial || cohort.EligibleCount != nil {
		t.Fatalf("partial coverage/observed count lost: %#v", cohort)
	}
	for _, m := range got.Metrics {
		if m.Denominator != nil && *m.Denominator < 0 {
			t.Fatal("invented denominator")
		}
	}
}

func TestAnalyzeConfigIdentityIncludesSnapshotSchemaAndCatalogVersions(t *testing.T) {
	f := loadFixture(t)
	first, err := Analyze(f.Source, f.Config)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Analyze(f.Source, f.Config)
	if err != nil {
		t.Fatal(err)
	}
	if first.ConfigHash != second.ConfigHash {
		t.Fatal("same inputs/config must produce a stable ConfigHash")
	}
	f.Source.Snapshot.QueryCatalogVersion = "catalog-next"
	third, err := Analyze(f.Source, f.Config)
	if err != nil {
		t.Fatal(err)
	}
	if first.ConfigHash == third.ConfigHash {
		t.Fatal("QueryCatalogVersion must participate in analysis identity")
	}
}

func TestAnalyzeKeepsIncompleteCoverageOutOfMatureReworkMetric(t *testing.T) {
	f := loadFixture(t)
	f.Source.Coverage = append(f.Source.Coverage, insightcontract.SourceCoverage{Key: insightcontract.CoverageKey{Source: "linear", ResourceID: "TEAM-DEMO", Collection: "links"}, Status: "partial", HistoryCoveredIntervals: []insightcontract.Window{{Start: "2026-09-28T00:00:00Z", End: f.Source.Snapshot.AnalysisReferenceAt}}})
	got, err := Analyze(f.Source, f.Config)
	if err != nil {
		t.Fatal(err)
	}
	m := metricByName(got.Metrics, "Ocorrências de retrabalho observadas em 30 dias")
	if m.State != statusImmature || m.EligibleSize != nil || m.Numerator == nil || *m.Numerator != 0 {
		t.Fatalf("zero must stay distinct from immature coverage: %#v", m)
	}
}

func cohortByKey(cohorts []insightcontract.Cohort, key string) *insightcontract.Cohort {
	needle := map[string]string{"completed-at-report-window": "completed_at em", "completed-at-snapshot": "estado concluído", "completion-transitions-report-window": "transição observada", "open-aging-context": "grupo contextual", "open-aging-non-context": "grupo não contextual"}[key]
	for i := range cohorts {
		if needle != "" && stringsContains(cohorts[i].Definition, needle) {
			return &cohorts[i]
		}
	}
	return nil
}
func cohortByDefinitionContains(cohorts []insightcontract.Cohort, s string) *insightcontract.Cohort {
	for i := range cohorts {
		if stringsContains(cohorts[i].Definition, s) {
			return &cohorts[i]
		}
	}
	return nil
}
func factCaseByKindAndClassification(cases []insightcontract.FactCase, kind, class string) *insightcontract.FactCase {
	for i := range cases {
		if cases[i].Kind == kind && cases[i].Classification == class {
			return &cases[i]
		}
	}
	return nil
}
func metricByName(metrics []insightcontract.Metric, name string) *insightcontract.Metric {
	for i := range metrics {
		if metrics[i].Name == name {
			return &metrics[i]
		}
	}
	return nil
}
func caseByFinding(cases []insightcontract.FactCase, findingID string) bool {
	for _, c := range cases {
		if has(c.LegacyFindingIDs, findingID) {
			return true
		}
	}
	return false
}
func caseByLink(cases []insightcontract.FactCase, linkID string) bool {
	for _, c := range cases {
		if has(c.LinkIDs, linkID) {
			return true
		}
	}
	return false
}
func has(values []string, target string) bool {
	for _, v := range values {
		if v == target {
			return true
		}
	}
	return false
}
func stringPtr(s string) *string { return &s }
func openEntity(id, created string, context bool) insightcontract.Entity {
	return insightcontract.Entity{ID: id, Kind: "issue", Source: "linear", ResourceID: "TEAM-DEMO", State: "started", Outcome: "active", StateTemporalStatus: "known_at_reference", CreatedAt: &created, IsContext: context}
}
func stringsContains(s, part string) bool { return len(s) >= len(part) && containsSubstring(s, part) }
func containsSubstring(s, part string) bool {
	for i := 0; i+len(part) <= len(s); i++ {
		if s[i:i+len(part)] == part {
			return true
		}
	}
	return false
}
func itoa(i int) string {
	const digits = "0123456789"
	if i < 10 {
		return string(digits[i])
	}
	return "10"
}
