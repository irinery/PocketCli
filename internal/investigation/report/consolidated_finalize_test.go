package report_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/insightcontract"
	"pocketcli/internal/investigation/report"
	"pocketcli/internal/investigation/web/mock"
)

type fixedSource struct {
	bundle *report.ExecutiveBundle
	err    error
}

func (s fixedSource) LatestAnalysisID(context.Context, string) (string, bool, error) {
	if s.bundle == nil {
		return "", false, s.err
	}
	return s.bundle.Report.AnalysisID, true, s.err
}
func (s fixedSource) Bundle(context.Context, string) (report.ExecutiveBundle, bool, error) {
	if s.bundle == nil {
		return report.ExecutiveBundle{}, false, s.err
	}
	return *s.bundle, true, s.err
}

func fixtureBundle(t *testing.T) *report.ExecutiveBundle {
	t.Helper()
	raw, err := os.ReadFile("../insightcontract/testdata/fixture_go.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Report   insightcontract.ExecutiveReport  `json:"report"`
		Analysis insightcontract.DecisionAnalysis `json:"analysis"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	bundle := &report.ExecutiveBundle{Report: f.Report}
	for _, c := range f.Analysis.Cases {
		bundle.Details = append(bundle.Details, insightcontract.CaseDetail{SnapshotID: f.Report.SnapshotID, AnalysisID: f.Report.AnalysisID, Case: c, Evidence: []insightcontract.EvidenceView{}})
	}
	bundle.Report.TotalCases = int64(len(bundle.Details))
	return bundle
}

// A consolidated PDF is the complete record in one file. It carries the
// decisions when an analysis exists and says so when it does not, instead of
// silently looking complete.
func TestConsolidatedFinalizeIncludesDecisionsOrWarns(t *testing.T) {
	finalize := func(source report.ExecutiveSource, consolidated bool) (contract.ReportArtifact, error) {
		f, err := report.New(mock.NewScenario("complete"), t.TempDir(), true, nil, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if source != nil {
			f.SetExecutiveSource(source)
		}
		return f.Finalize(context.Background(), contract.FinalizeRequest{SnapshotID: "snap-demo-001", Consolidated: consolidated})
	}
	common, err := finalize(nil, false)
	if err != nil || common.Consolidated || len(common.Warnings) != 0 {
		t.Fatalf("PDF comum: %+v err=%v", common, err)
	}
	none, err := finalize(nil, true)
	if err != nil || !none.Consolidated || len(none.Warnings) != 1 || !strings.Contains(none.Warnings[0], "sem decisões") {
		t.Fatalf("sem fonte executiva: %+v err=%v", none, err)
	}
	empty, err := finalize(fixedSource{}, true)
	if err != nil || len(empty.Warnings) != 1 || !strings.Contains(empty.Warnings[0], "ainda não tem análise") {
		t.Fatalf("sem análise: %+v err=%v", empty, err)
	}
	full, err := finalize(fixedSource{bundle: fixtureBundle(t)}, true)
	if err != nil || !full.Consolidated || len(full.Warnings) != 0 || full.PageCount <= common.PageCount {
		t.Fatalf("consolidado com decisões: %+v (comum %d páginas) err=%v", full, common.PageCount, err)
	}
	if _, err := finalize(fixedSource{err: os.ErrPermission, bundle: fixtureBundle(t)}, true); err == nil || !strings.Contains(err.Error(), "ERR_PDF_FAILED") {
		t.Fatalf("decisões ilegíveis não podem ser omitidas em silêncio: %v", err)
	}
}
