package report

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"pocketcli/internal/investigation/contract"
)

const (
	testTeamID = "4a8c180e-519b-4698-933b-0147f9fb4094"
	testRepoID = "617622053"
)

func readabilityDoc() model {
	snapshot := contract.Snapshot{ID: "snap-read", InvestigationID: "investigation-1", Status: "published",
		Resources: []contract.Resource{{ID: testTeamID, Source: "linear", Name: "ENG"}, {ID: testRepoID, Source: "github", Name: "Somos-UNI/fuzzy-happiness"}},
		Coverage: []contract.Coverage{
			{Source: "linear", ResourceID: testTeamID, Collection: "issues", Status: "partial", Observed: 345, Reason: "lista Linear paginada sem snapshot estável"},
			{Source: "linear", ResourceID: testTeamID, Collection: "history", Status: "partial", Observed: 366, Reason: "fila de reconciliação do team " + testTeamID},
			{Source: "github", ResourceID: testRepoID, Collection: "pull_requests", Status: "complete"},
			{Source: "git", ResourceID: testRepoID, Collection: "code_history", Status: "partial", Reason: "grafo Git indisponível"},
			{Source: "github", ResourceID: "all", Collection: "actions", Status: "not_selected", Reason: "módulo CI não selecionado"},
			{Source: "analysis", ResourceID: "investigation-1", Collection: "links", Status: "partial", Observed: 376, Reason: "vínculos formais não demonstrados"},
		}}
	eligible := int64(4)
	var records []contract.ReportRecord
	finding := func(id, rule, title, severity string, evidence ...string) {
		records = append(records, contract.ReportRecord{Kind: "finding", Finding: &contract.Finding{ID: id, RuleID: rule, RuleVersion: "1.16.0", Title: title, Severity: severity, SubjectID: "linear-x", EvidenceIDs: evidence, EvidenceStatus: "direct", DeterministicText: "texto determinístico"}})
	}
	finding("f-1", "R-COVER-001", "Lacuna de cobertura", "info", "ev-2")
	finding("f-2", "R-BLOCK-001", "Dependência pendente observada", "medium", "ev-0", "ev-1", "ev-rel")
	finding("f-3", "R-BLOCK-001", "Dependência pendente observada", "medium", "ev-0", "ev-1", "ev-rel")
	finding("f-4", "R-BLOCK-001", "Dependência pendente observada", "medium", "ev-0")
	records = append(records, contract.ReportRecord{Kind: "metric", Metric: &contract.Metric{ID: "m-1", Name: "Tempo de ciclo mediano no team " + testTeamID, Definition: "Issues concluídas do team " + testTeamID, Value: contract.Cell{Kind: "integer", Value: "78412"}, Unit: "seconds", SampleSize: 110, EligibleSize: &eligible}})
	records = append(records, contract.ReportRecord{Kind: "coverage", Coverage: &contract.Coverage{Source: "linear", ResourceID: testTeamID, Collection: "issues", Status: "partial", Observed: 345}})
	records = append(records, contract.ReportRecord{Kind: "stage", Stage: &contract.StageRecord{ID: "st-1", SubjectID: "linear-x", Stage: "linear:QA", DurationValid: true}})
	for i := 0; i < 400; i++ { // enough evidence to span several pages
		records = append(records, contract.ReportRecord{Kind: "evidence", Evidence: &contract.Evidence{ID: fmt.Sprintf("ev-%d", i), Source: "linear", Locator: fmt.Sprintf("ENG-%d", i+1), ObservedAt: "2026-10-27T00:00:00Z", Preview: "Metadados coletados", SHA256: strings.Repeat("a", 64)}})
	}
	// A relation evidence: its locator is not a plain item identifier.
	records = append(records, contract.ReportRecord{Kind: "evidence", Evidence: &contract.Evidence{ID: "ev-rel", Source: "linear", Locator: "relation:7035f526-a4ad-41ab-b094-3db5607878a9", ObservedAt: "2026-10-27T00:00:00Z", Preview: "Metadados coletados", SHA256: strings.Repeat("b", 64)}})
	return model{Snapshot: snapshot, CoverageStatus: coverageLabel(snapshot.Coverage), Records: records, FindingCount: 4, RecordCount: len(records), GeneratedAt: "2026-10-28T04:21:32Z"}
}

func pageOfHeading(l *pdfLayout, title string) int {
	for i, p := range l.pages {
		for _, line := range p.lines {
			if line == title {
				return i + 1
			}
		}
	}
	return 0
}

func TestContentsPageNumbersMatchTheRealHeadings(t *testing.T) {
	doc := readabilityDoc()
	l, err := layoutReport(context.Background(), doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.pages) < 5 {
		t.Fatalf("o relatório de teste deveria ter várias páginas: %d", len(l.pages))
	}
	view := newReportView(doc)
	sections := view.plannedSections()
	if len(sections) < 8 {
		t.Fatalf("índice curto: %+v", sections)
	}
	for _, sec := range sections {
		var toc int
		for _, line := range l.pages[0].lines {
			if strings.HasPrefix(line, sec.title) && strings.Contains(line, " … ") {
				toc, _ = strconv.Atoi(line[strings.LastIndex(line, " … ")+len(" … "):])
			}
		}
		actual := pageOfHeading(l, sec.title)
		if actual == 0 || toc != actual {
			t.Fatalf("%q: índice aponta página %d, título está na página %d", sec.title, toc, actual)
		}
	}
	if last := pageOfHeading(l, "3.5 Evidências (apêndice) (401)"); last < 3 {
		t.Fatalf("apêndice de evidências deveria estar no fim: página %d", last)
	}
}

func TestSectionsPutFindingsFirstAndEvidenceLast(t *testing.T) {
	doc := readabilityDoc()
	l, err := layoutReport(context.Background(), doc)
	if err != nil {
		t.Fatal(err)
	}
	order := []string{"3.1 Achados (4)", "3.2 Métricas (1)", "3.3 Cobertura por coleção (1)", "3.4 Etapas (1)", "3.5 Evidências (apêndice) (401)", "10. Comentários do operador"}
	previous := 0
	for _, title := range order {
		page := pageOfHeading(l, title)
		if page == 0 || page < previous {
			t.Fatalf("%q fora de ordem (página %d, anterior %d)", title, page, previous)
		}
		previous = page
	}
	if findings, evidence := pageOfHeading(l, "3.1 Achados (4)"), pageOfHeading(l, "3.5 Evidências (apêndice) (401)"); evidence <= findings {
		t.Fatalf("achados na página %d e evidências na %d: deveriam estar separados", findings, evidence)
	}
}

func TestResourcesAreNamedAndAttentionTableSkipsCompleteCollections(t *testing.T) {
	doc := readabilityDoc()
	l, err := layoutReport(context.Background(), doc)
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, p := range l.pages {
		lines = append(lines, p.lines...)
	}
	between := func(from, to string) string {
		start, end := -1, len(lines)
		for i, line := range lines {
			if line == from && start < 0 {
				start = i
			}
			if line == to && start >= 0 && i > start {
				end = i
				break
			}
		}
		return strings.Join(lines[start+1:end], "\n")
	}
	coverage := between("2. Cobertura — parcial", "3. Registros da análise")
	for _, want := range []string{"6 coleções: 1 completas · 4 parciais · 1 não selecionadas", "ENG", "lista Linear paginada", "fila de reconciliação do team ENG", "Somos-UNI/fuzzy-happiness", "investigação"} {
		if !strings.Contains(coverage, want) {
			t.Fatalf("seção 2 sem %q:\n%s", want, coverage)
		}
	}
	if strings.Contains(coverage, "pull_requests") || strings.Contains(coverage, "actions") {
		t.Fatalf("seção 2 lista coleções completas ou não selecionadas:\n%s", coverage)
	}
	if strings.Contains(coverage, testTeamID) {
		t.Fatalf("seção 2 mostra o UUID do team:\n%s", coverage)
	}
	summary := between("1. Resumo determinístico", "2. Cobertura — parcial")
	for _, want := range []string{"Achados por regra", "medium | R-BLOCK-001 | Dependência pendente observada | 3", "info | R-COVER-001 | Lacuna de cobertura | 1", "Recursos analisados", "linear | ENG | " + testTeamID, "Registros por tipo"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("resumo sem %q:\n%s", want, summary)
		}
	}
	if strings.Index(summary, "R-BLOCK-001") > strings.Index(summary, "R-COVER-001") {
		t.Fatalf("achados por regra fora da ordem de severidade:\n%s", summary)
	}
	metrics := between("3.2 Métricas (1)", "3.3 Cobertura por coleção (1)")
	if !strings.Contains(metrics, "no team ENG = 78412 seconds") || !strings.Contains(metrics, "do team ENG") || strings.Contains(metrics, testTeamID) {
		t.Fatalf("métrica com UUID em vez da chave do team:\n%s", metrics)
	}
	findings := between("3.1 Achados (4)", "3.2 Métricas (1)")
	for _, want := range []string{"Itens: ENG-1, ENG-2", "ev-1 (ENG-2)", "ev-rel (relation:7035f526"} {
		if !strings.Contains(findings, want) {
			t.Fatalf("achados sem %q:\n%s", want, findings)
		}
	}
}

func TestLayoutIsDeterministic(t *testing.T) {
	first, err := layoutReport(context.Background(), readabilityDoc())
	if err != nil {
		t.Fatal(err)
	}
	second, err := layoutReport(context.Background(), readabilityDoc())
	if err != nil {
		t.Fatal(err)
	}
	if first.text() != second.text() || len(first.pages) != len(second.pages) {
		t.Fatal("o mesmo modelo gerou layouts diferentes")
	}
}
