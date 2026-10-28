package report

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"pocketcli/internal/investigation/insightcontract"
)

type executiveFixtureReader struct {
	report insightcontract.ExecutiveReport
	cases  []insightcontract.DecisionCase
	bad    bool
}

func (r *executiveFixtureReader) GetExecutiveReport(_ context.Context, snapshot, analysis string) (insightcontract.ExecutiveReport, error) {
	out := r.report
	out.SnapshotID = snapshot
	out.AnalysisID = analysis
	return out, nil
}
func (r *executiveFixtureReader) GetInsightCase(_ context.Context, snapshot, analysis, id string) (insightcontract.CaseDetail, error) {
	for _, c := range r.cases {
		if c.ID == id {
			return insightcontract.CaseDetail{SnapshotID: snapshot, AnalysisID: analysis, Case: c, Evidence: []insightcontract.EvidenceView{}}, nil
		}
	}
	return insightcontract.CaseDetail{}, os.ErrNotExist
}
func (r *executiveFixtureReader) ListInsightCases(_ context.Context, snapshot, analysis, cursor string, limit int) (insightcontract.CasePage, error) {
	if r.bad {
		return insightcontract.CasePage{SnapshotID: "other", AnalysisID: analysis, Total: int64(len(r.cases))}, nil
	}
	start := 0
	if cursor != "" {
		n, err := strconv.Atoi(strings.TrimPrefix(cursor, "cursor:"))
		if err == nil {
			start = n
		}
	}
	end := start + 1
	if end > len(r.cases) {
		end = len(r.cases)
	}
	page := insightcontract.CasePage{SnapshotID: snapshot, AnalysisID: analysis, Total: int64(len(r.cases)), Cases: append([]insightcontract.DecisionCase(nil), r.cases[start:end]...)}
	if end < len(r.cases) {
		page.NextCursor = fmt.Sprintf("cursor:%d", end)
	}
	return page, nil
}

func executiveFixture(t *testing.T) (insightcontract.ExecutiveReport, []insightcontract.DecisionCase) {
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
	sort.Slice(f.Analysis.Cases, func(i, j int) bool { return f.Analysis.Cases[i].ID < f.Analysis.Cases[j].ID })
	return f.Report, f.Analysis.Cases
}

func TestExecutiveExportUsesSelectionAndCompleteDetailWithIdempotency(t *testing.T) {
	report, cases := executiveFixture(t)
	reader := &executiveFixtureReader{report: report, cases: cases}
	dir := t.TempDir()
	exporter, err := NewExecutiveFinalizer(reader, dir)
	if err != nil {
		t.Fatal(err)
	}
	request := insightcontract.ExecutiveExportRequest{SnapshotID: report.SnapshotID, AnalysisID: report.AnalysisID, Mode: "executive", IdempotencyKey: "export-1"}
	short, err := exporter.ExportExecutive(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if short.SelectedCaseCount != int64(len(report.Cases)) || short.TotalCaseCount != report.TotalCases {
		t.Fatalf("executive counts: %+v", short)
	}
	if short.DownloadPath == "" || short.Mode != "executive" || short.ContentHash == "" || short.AnalysisHash == "" {
		t.Fatalf("incomplete artifact: %+v", short)
	}
	file, err := exporter.OpenExecutiveArtifact(short.ID)
	if err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 5)
	_, err = file.Read(b)
	_ = file.Close()
	if err != nil || string(b) != "%PDF-" {
		t.Fatalf("invalid PDF header %q: %v", b, err)
	}
	again, err := exporter.ExportExecutive(context.Background(), request)
	if err != nil || again.ID != short.ID {
		t.Fatalf("idempotent export: %+v %v", again, err)
	}

	request.Mode = "analysis_detail"
	request.IdempotencyKey = "export-2"
	detail, err := exporter.ExportExecutive(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if detail.SelectedCaseCount != detail.TotalCaseCount || detail.TotalCaseCount != int64(len(cases)) {
		t.Fatalf("detail counts: %+v", detail)
	}
	restarted, err := NewExecutiveFinalizer(reader, dir)
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := restarted.GetExecutiveArtifact(context.Background(), detail.ID)
	if err != nil || persisted.ContentHash != detail.ContentHash {
		t.Fatalf("persisted manifest: %+v %v", persisted, err)
	}
}

func TestExecutiveDetailFailsClosedOnCrossSnapshotPage(t *testing.T) {
	report, cases := executiveFixture(t)
	reader := &executiveFixtureReader{report: report, cases: cases, bad: true}
	dir := filepath.Join(t.TempDir(), "reports")
	exporter, err := NewExecutiveFinalizer(reader, dir)
	if err != nil {
		t.Fatal(err)
	}
	_, err = exporter.ExportExecutive(context.Background(), insightcontract.ExecutiveExportRequest{SnapshotID: report.SnapshotID, AnalysisID: report.AnalysisID, Mode: "analysis_detail", IdempotencyKey: "export-bad"})
	if err == nil {
		t.Fatal("expected cross-snapshot page rejection")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("failed export wrote artifacts: %v", entries)
	}
}
