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
	"time"

	"pocketcli/internal/investigation/insightcontract"
)

type executiveFixtureReader struct {
	report insightcontract.ExecutiveReport
	cases  []insightcontract.DecisionCase
	bad    bool
	page   func(snapshot, analysis, cursor string, limit int) insightcontract.CasePage
	detail func(snapshot, analysis, id string) insightcontract.CaseDetail
}

func (r *executiveFixtureReader) GetExecutiveReport(_ context.Context, snapshot, analysis string) (insightcontract.ExecutiveReport, error) {
	out := r.report
	out.SnapshotID = snapshot
	out.AnalysisID = analysis
	return out, nil
}
func (r *executiveFixtureReader) GetInsightCase(_ context.Context, snapshot, analysis, id string) (insightcontract.CaseDetail, error) {
	if r.detail != nil {
		return r.detail(snapshot, analysis, id), nil
	}
	for _, c := range r.cases {
		if c.ID == id {
			return insightcontract.CaseDetail{SnapshotID: snapshot, AnalysisID: analysis, Case: c, Evidence: []insightcontract.EvidenceView{}}, nil
		}
	}
	return insightcontract.CaseDetail{}, os.ErrNotExist
}
func (r *executiveFixtureReader) ListInsightCases(_ context.Context, snapshot, analysis, cursor string, limit int) (insightcontract.CasePage, error) {
	if r.page != nil {
		return r.page(snapshot, analysis, cursor, limit), nil
	}
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
	request.IdempotencyKey = "a-distinct-client-key"
	again, err = exporter.ExportExecutive(context.Background(), request)
	if err != nil || again.ID != short.ID {
		t.Fatalf("semantic export identity changed with request key: %+v %v", again, err)
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

func TestExecutiveDetailFailsClosedOnRepeatedCursorAndChangingTotal(t *testing.T) {
	for _, tc := range []struct {
		name string
		page func(snapshot, analysis, cursor string, limit int) insightcontract.CasePage
	}{
		{
			name: "repeated cursor",
			page: func(snapshot, analysis, cursor string, _ int) insightcontract.CasePage {
				return insightcontract.CasePage{SnapshotID: snapshot, AnalysisID: analysis, Total: 2, Cases: []insightcontract.DecisionCase{{ID: "case-1"}}, NextCursor: "same"}
			},
		},
		{
			name: "total changes",
			page: func(snapshot, analysis, cursor string, _ int) insightcontract.CasePage {
				total := int64(2)
				if cursor != "" {
					total = 3
				}
				cases := []insightcontract.DecisionCase{{ID: "case-1"}}
				next := "next"
				if cursor != "" {
					cases = []insightcontract.DecisionCase{{ID: "case-2"}}
					next = ""
				}
				return insightcontract.CasePage{SnapshotID: snapshot, AnalysisID: analysis, Total: total, Cases: cases, NextCursor: next}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			report, cases := executiveFixture(t)
			reader := &executiveFixtureReader{report: report, cases: cases, page: tc.page}
			exporter, err := NewExecutiveFinalizer(reader, filepath.Join(t.TempDir(), "reports"))
			if err != nil {
				t.Fatal(err)
			}
			_, err = exporter.ExportExecutive(context.Background(), insightcontract.ExecutiveExportRequest{SnapshotID: report.SnapshotID, AnalysisID: report.AnalysisID, Mode: "analysis_detail", IdempotencyKey: "bad-page"})
			if err == nil {
				t.Fatal("expected fail-closed export")
			}
		})
	}
}

func TestExecutiveDetailFailsClosedWhenCaseDetailIdentityDiffers(t *testing.T) {
	report, cases := executiveFixture(t)
	reader := &executiveFixtureReader{report: report, cases: cases, detail: func(snapshot, analysis, id string) insightcontract.CaseDetail {
		return insightcontract.CaseDetail{SnapshotID: snapshot, AnalysisID: "other-analysis", Case: insightcontract.DecisionCase{ID: id}}
	}}
	exporter, err := NewExecutiveFinalizer(reader, filepath.Join(t.TempDir(), "reports"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = exporter.ExportExecutive(context.Background(), insightcontract.ExecutiveExportRequest{SnapshotID: report.SnapshotID, AnalysisID: report.AnalysisID, Mode: "analysis_detail", IdempotencyKey: "bad-detail"})
	if err == nil {
		t.Fatal("expected case detail identity rejection")
	}
}

func TestSyntheticCaseC15KeepsExecutiveSelectionSeparateFromFullExport(t *testing.T) {
	fixtureBytes, err := os.ReadFile("../insightcontract/testdata/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			ID       string   `json:"id"`
			Expected []string `json:"expected"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(fixtureBytes, &fixture); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range fixture.Cases {
		if c.ID == "C15" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("synthetic export case C15 is missing")
	}

	cases := make([]insightcontract.DecisionCase, 12)
	for i := range cases {
		cases[i].ID = fmt.Sprintf("case-%02d", i+1)
		cases[i].Title = cases[i].ID
	}
	report := insightcontract.ExecutiveReport{
		Version: insightcontract.Version, SnapshotID: "snap-synthetic", AnalysisID: "analysis-synthetic",
		PublicationStatus: "ready", AnalysisStatus: "complete", SufficiencyStatus: "partial",
		Headline: "12 casos", Summary: "fixture sintética C15", TotalCases: 12,
		Cases: append([]insightcontract.DecisionCase(nil), cases[:5]...), CaseIDs: []string{"case-01", "case-02", "case-03", "case-04", "case-05"},
	}
	reader := &executiveFixtureReader{report: report, cases: cases}
	exporter, err := NewExecutiveFinalizer(reader, filepath.Join(t.TempDir(), "reports"))
	if err != nil {
		t.Fatal(err)
	}
	short, err := exporter.ExportExecutive(context.Background(), insightcontract.ExecutiveExportRequest{SnapshotID: report.SnapshotID, AnalysisID: report.AnalysisID, Mode: "executive", IdempotencyKey: "c15-short"})
	if err != nil {
		t.Fatal(err)
	}
	if short.SelectedCaseCount != 5 || short.TotalCaseCount != 12 {
		t.Fatalf("C15 executive selection counts: %+v", short)
	}
	full, err := exporter.ExportExecutive(context.Background(), insightcontract.ExecutiveExportRequest{SnapshotID: report.SnapshotID, AnalysisID: report.AnalysisID, Mode: "analysis_detail", IdempotencyKey: "c15-full"})
	if err != nil {
		t.Fatal(err)
	}
	if full.SelectedCaseCount != 12 || full.TotalCaseCount != 12 {
		t.Fatalf("C15 detail counts: %+v", full)
	}
}

func TestTimelineLeavesAmbiguousDatesUnordered(t *testing.T) {
	known := newTimelineEntry("2026-10-28T12:00:00Z", "decision-1", "Decisão", "texto", "escopo", nil)
	ambiguous := newTimelineEntry("28/10/2026", "observation-1", "Observação", "texto", "escopo", nil)
	unknown := newTimelineEntry("", "claim-1", "Afirmação", "texto", "escopo", nil)
	if !known.ordered || ambiguous.ordered || unknown.ordered {
		t.Fatalf("unexpected timeline ordering: known=%t ambiguous=%t unknown=%t", known.ordered, ambiguous.ordered, unknown.ordered)
	}
}

func TestCaseDetailLayoutKeepsIDsTimelineScopeAndEvidenceHashes(t *testing.T) {
	caseDetail := insightcontract.CaseDetail{
		Case: insightcontract.DecisionCase{
			ID: "case-C15", FactCaseIDs: []string{"fact-1"}, ClaimIDs: []string{"claim-1", "claim-2"},
			DecisionIDs: []string{"decision-1"}, RelationIDs: []string{"relation-1"}, ObjectiveIDs: []string{"objective-1"},
		},
		Facts: []insightcontract.FactCase{{ID: "fact-1", Kind: "delivery", Classification: "observed", Title: "Entrega"}},
		Claims: []insightcontract.Claim{
			{ID: "claim-1", Kind: "result", Status: "supported", Text: "Resultado observado", EventAt: strptr("28/10/2026"), Scope: insightcontract.ContextScope{Description: "ambiente de teste"}},
			{ID: "claim-2", Kind: "outcome", Status: "disputed", Text: "Resultado contraditório", EventAt: strptr("28/10/2026"), Scope: insightcontract.ContextScope{Description: "ambiente de teste"}},
		},
		ClaimRelations: []insightcontract.ClaimRelation{{ID: "claim-relation-1", FromClaimID: "claim-1", ToClaimID: "claim-2", Kind: "contradicts", Status: "candidate", Reason: "Fontes simultâneas divergentes"}},
		Decisions:      []insightcontract.DecisionRecord{{ID: "decision-1", Question: "Como seguir?", Choice: "Aguardar", Status: "superseded", DecidedAt: strptr("2026-10-28T12:00:00Z"), Scope: insightcontract.ContextScope{Description: "time A"}}},
		Relations:      []insightcontract.DecisionRelation{{ID: "relation-1", FromDecisionID: "decision-1", ToDecisionID: "decision-0", Kind: "supersedes", Status: "valid"}},
		Evidence:       []insightcontract.EvidenceView{{ID: "evidence-1", Source: "tracker", Locator: "issue/1", ObservedAt: "2026-10-28T13:00:00Z", EventAt: strptr("2026-10-28T12:00:00Z"), ContentHash: "content-hash", ExcerptHash: "excerpt-hash", ContentStatus: "available", IntegrityStatus: "verified", SanitizationVersion: "sanitize-v1"}},
	}
	l := newPDFLayout()
	renderCaseDetail(l, caseDetail)
	text := l.text()
	for _, want := range []string{
		"case-C15", "fact-1", "claim-1", "claim-2", "Resultado observado", "Resultado contraditório", "Fontes simultâneas divergentes", "decision-1", "relation-1", "objective-1",
		"data/ordem desconhecida (28/10/2026)", "2026-10-28T12:00:00Z", "ambiente de teste", "time A",
		"content-hash", "excerpt-hash", "sanitize-v1", "observada 2026-10-28T13:00:00Z",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("case detail PDF layout omits %q", want)
		}
	}
}

func TestExecutivePDFLayoutKeepsStatusesSelectionAndInsufficiencyWarning(t *testing.T) {
	report, _ := executiveFixture(t)
	report.Cases = report.Cases[:1]
	report.CaseIDs = report.CaseIDs[:1]
	layout, err := layoutExecutive(context.Background(), report, nil, "executive", time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	text := layout.text()
	for _, want := range []string{
		"ID da análise: " + report.AnalysisID,
		"Estado da análise: parcial",
		"Publicação: pronta",
		"Suficiência: insuficiente",
		"Apresentação: " + executiveRendererVersion,
		"Escopo solicitado", "Histórico:", "2022-10-28T00:00:00Z", "Janela do relatório:",
		"2026-10-21T00:00:00Z", "TEAM-DEMO", "Equipe sintética",
		"Aviso: a análise não tem suficiência plena.",
		"Esta seleção é parcial.",
		"de 3 casos selecionados.",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("executive PDF layout omits %q", want)
		}
	}
}

func TestExecutiveAndDetailPDFUseModeSpecificHeadline(t *testing.T) {
	report, _ := executiveFixture(t)
	report.Headline = "1 caso(s) para leitura executiva"
	report.TotalCases = 1
	executive, err := layoutExecutive(context.Background(), report, nil, "executive", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	detail, err := layoutExecutive(context.Background(), report, nil, "analysis_detail", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(executive.text(), report.Headline) {
		t.Fatal("executive PDF should preserve the report headline")
	}
	if !strings.Contains(detail.text(), "Análise detalhada · 1 caso") || strings.Contains(detail.text(), "caso(s) para leitura executiva") {
		t.Fatal("detail PDF must identify its complete analysis mode")
	}
}

func TestCaseAssessmentsKeepStateDistinctFromLevel(t *testing.T) {
	l := newPDFLayout()
	renderDecisionCase(l, insightcontract.DecisionCase{
		ID: "case-assessment", Title: "Assessment states",
		Impact:     insightcontract.Assessment{State: "not_applicable", Level: "unknown", Rationale: "Não se aplica."},
		Complexity: insightcontract.Assessment{State: "indeterminate", Level: "unknown", Rationale: "Falta evidência."},
		Urgency:    insightcontract.Assessment{State: "assessed", Level: "low", Rationale: "Sem prazo próximo."},
		Confidence: insightcontract.Assessment{State: "indeterminate", Level: "unknown", Rationale: "Baixa base."},
	})
	text := l.text()
	for _, want := range []string{
		"Impacto: não aplicável · desconhecido", "Complexidade: indeterminada · desconhecido",
		"Urgência: avaliado · baixo", "Confiança: indeterminada · desconhecido",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("assessment state/level distinction omitted %q", want)
		}
	}
}

func strptr(value string) *string { return &value }
