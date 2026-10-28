package presentation

import (
	"encoding/json"
	"os"
	"testing"

	"pocketcli/internal/investigation/insightcontract"
)

func TestBuildUsesFixtureWithoutInventingMetrics(t *testing.T) {
	raw, err := os.ReadFile("../insightcontract/testdata/fixture_go.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Config   insightcontract.AnalysisConfig   `json:"config"`
		Source   insightcontract.SourceBundle     `json:"source"`
		Facts    insightcontract.FactsResult      `json:"facts"`
		Analysis insightcontract.DecisionAnalysis `json:"analysis"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	report, err := Build(fixture.Source, fixture.Facts, fixture.Analysis, fixture.Config)
	if err != nil {
		t.Fatal(err)
	}
	if report.SnapshotID != fixture.Source.Snapshot.ID || report.AnalysisID != fixture.Analysis.ID {
		t.Fatalf("identity mismatch: %+v", report)
	}
	if report.TotalCases != int64(len(fixture.Analysis.Cases)) || len(report.Cases) != len(report.CaseIDs) {
		t.Fatalf("selection/count mismatch: %d/%d", len(report.Cases), report.TotalCases)
	}
	if report.PublicationStatus != "ready" || report.AnalysisStatus != fixture.Analysis.Status {
		t.Fatalf("statuses were conflated: %+v", report)
	}
	for i, c := range report.Cases {
		if report.CaseIDs[i] != c.ID {
			t.Fatalf("case id at %d does not match", i)
		}
	}
}

func TestBuildAppliesExecutiveSelectionLimitWithoutChangingTotal(t *testing.T) {
	raw, err := os.ReadFile("../insightcontract/testdata/fixture_go.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Config   insightcontract.AnalysisConfig   `json:"config"`
		Source   insightcontract.SourceBundle     `json:"source"`
		Facts    insightcontract.FactsResult      `json:"facts"`
		Analysis insightcontract.DecisionAnalysis `json:"analysis"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Analysis.Cases) < 2 {
		t.Fatal("fixture needs at least two cases")
	}
	original := append([]insightcontract.DecisionCase(nil), fixture.Analysis.Cases...)
	fixture.Config.Policy.MaximumExecutiveCases = 1
	report, err := Build(fixture.Source, fixture.Facts, fixture.Analysis, fixture.Config)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Cases) != 1 || report.TotalCases != int64(len(original)) || report.CaseIDs[0] != original[0].ID {
		t.Fatalf("selection/total/order mismatch: selected=%v total=%d original=%d", report.CaseIDs, report.TotalCases, len(original))
	}
	fixture.Config.Policy.MaximumExecutiveCases = -1
	if _, err := Build(fixture.Source, fixture.Facts, fixture.Analysis, fixture.Config); err == nil {
		t.Fatal("expected negative executive case limit rejection")
	}
}

func TestBuildRejectsMissingReference(t *testing.T) {
	raw, err := os.ReadFile("../insightcontract/testdata/fixture_go.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Config   insightcontract.AnalysisConfig   `json:"config"`
		Source   insightcontract.SourceBundle     `json:"source"`
		Facts    insightcontract.FactsResult      `json:"facts"`
		Analysis insightcontract.DecisionAnalysis `json:"analysis"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	fixture.Analysis.Cases[0].EntityIDs = append(fixture.Analysis.Cases[0].EntityIDs, "entity-does-not-exist")
	if _, err := Build(fixture.Source, fixture.Facts, fixture.Analysis, fixture.Config); err == nil {
		t.Fatal("expected missing reference error")
	}
}

func TestBuildRejectsAssessmentEvidenceAndObjectiveUnitMismatch(t *testing.T) {
	readFixture := func() struct {
		Config   insightcontract.AnalysisConfig   `json:"config"`
		Source   insightcontract.SourceBundle     `json:"source"`
		Facts    insightcontract.FactsResult      `json:"facts"`
		Analysis insightcontract.DecisionAnalysis `json:"analysis"`
	} {
		t.Helper()
		raw, err := os.ReadFile("../insightcontract/testdata/fixture_go.json")
		if err != nil {
			t.Fatal(err)
		}
		var fixture struct {
			Config   insightcontract.AnalysisConfig   `json:"config"`
			Source   insightcontract.SourceBundle     `json:"source"`
			Facts    insightcontract.FactsResult      `json:"facts"`
			Analysis insightcontract.DecisionAnalysis `json:"analysis"`
		}
		if err := json.Unmarshal(raw, &fixture); err != nil {
			t.Fatal(err)
		}
		return fixture
	}
	t.Run("assessment evidence must exist", func(t *testing.T) {
		f := readFixture()
		f.Analysis.Cases[0].Impact.EvidenceIDs = []string{"missing-evidence"}
		if _, err := Build(f.Source, f.Facts, f.Analysis, f.Config); err == nil {
			t.Fatal("expected invalid assessment evidence rejection")
		}
	})
	t.Run("objective and metric units must agree", func(t *testing.T) {
		f := readFixture()
		f.Config.Objectives[0].MetricID = f.Facts.Metrics[0].ID
		f.Config.Objectives[0].Unit = "hours"
		if _, err := Build(f.Source, f.Facts, f.Analysis, f.Config); err == nil {
			t.Fatal("expected objective/metric unit mismatch rejection")
		}
	})
}

func TestBuildRejectsCitationOutsideEvidenceExcerpt(t *testing.T) {
	raw, err := os.ReadFile("../insightcontract/testdata/fixture_go.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Config   insightcontract.AnalysisConfig   `json:"config"`
		Source   insightcontract.SourceBundle     `json:"source"`
		Facts    insightcontract.FactsResult      `json:"facts"`
		Analysis insightcontract.DecisionAnalysis `json:"analysis"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	mutated := false
	for i := range fixture.Analysis.Cases {
		if len(fixture.Analysis.Cases[i].Citations) > 0 {
			fixture.Analysis.Cases[i].Citations[0].Quote = "texto ausente da evidência"
			mutated = true
			break
		}
	}
	if !mutated {
		t.Fatal("fixture has no case citation")
	}
	if _, err := Build(fixture.Source, fixture.Facts, fixture.Analysis, fixture.Config); err == nil {
		t.Fatal("expected citation excerpt validation error")
	}
}

func TestMetricValueKeepsUnknownDistinctFromZero(t *testing.T) {
	zero := 0.0
	if got := MetricValue(insightcontract.Metric{State: "observed", Value: &zero, Unit: "days"}); got != "0 days" {
		t.Fatalf("observed zero = %q", got)
	}
	if got := MetricValue(insightcontract.Metric{State: "unknown", Unit: "days"}); got != "não determinado" {
		t.Fatalf("unknown = %q", got)
	}
}
