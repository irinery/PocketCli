package report

import (
	"context"
	"strings"
	"testing"

	"pocketcli/internal/investigation/insightcontract"
)

func caseDetail(report insightcontract.ExecutiveReport, c insightcontract.DecisionCase) insightcontract.CaseDetail {
	return insightcontract.CaseDetail{SnapshotID: report.SnapshotID, AnalysisID: report.AnalysisID, Case: c, Evidence: []insightcontract.EvidenceView{}}
}

// The consolidated report keeps the whole record and adds the decisions as
// section 13 inside the same pages, listed in the table of contents.
func TestConsolidatedReportCarriesDecisionsInItsOwnContents(t *testing.T) {
	report, cases := executiveFixture(t)
	doc := readabilityDoc()
	plain, err := layoutReport(context.Background(), doc)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(plain.text(), "13. Decisões e análise executiva") {
		t.Fatal("o relatório comum não tem seção de decisões")
	}

	doc.Executive = &ExecutiveBundle{Report: report}
	for _, c := range cases {
		doc.Executive.Details = append(doc.Executive.Details, caseDetail(report, c))
	}
	doc.Executive.Report.TotalCases = int64(len(cases))
	l, err := layoutReport(context.Background(), doc)
	if err != nil {
		t.Fatal(err)
	}
	text := l.text()
	if len(l.pages) <= len(plain.pages) {
		t.Fatalf("as decisões deveriam acrescentar páginas: %d → %d", len(plain.pages), len(l.pages))
	}
	for _, want := range []string{"13. Decisões e análise executiva", "Casos para leitura", "Detalhe e evidências", cases[0].ID} {
		if !strings.Contains(text, want) {
			t.Fatalf("consolidado sem %q", want)
		}
	}
	if page := pageOfHeading(l, "13. Decisões e análise executiva"); page == 0 {
		t.Fatal("seção 13 sem página")
	}
	// The contents must point at the real page of section 13.
	view := newReportView(doc)
	found := false
	for _, sec := range view.plannedSections() {
		if sec.key == "decisions" {
			found = true
		}
	}
	if !found {
		t.Fatal("o índice não lista as decisões")
	}
}
