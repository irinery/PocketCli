package report

import (
	"context"
	"strings"
	"testing"

	"pocketcli/internal/investigation/contract"
)

func fase2Doc(realNames bool) model {
	snapshot := contract.Snapshot{ID: "snap-f2", InvestigationID: "inv", Status: contract.SnapshotProvisional, WindowEnd: "2026-11-03T00:00:00Z",
		ActivityStart: "2026-07-22T10:00:00Z", ActivityEnd: "2026-11-02T09:00:00Z",
		Resources: []contract.Resource{
			{ID: testTeamID, Source: "linear", Name: "ENG", CreatedAt: "2026-07-21T14:03:48Z"},
			{ID: "team-qa-0000-0000", Source: "linear", Name: "QA", CreatedAt: "2026-07-21T14:43:47Z", LastChangeAt: "2026-08-20T12:56:16Z"},
			{ID: testRepoID, Source: "github", Name: "org/legado", CreatedAt: "2023-03-01T00:00:00Z", LastChangeAt: "2023-04-01T00:00:00Z"},
		},
		Coverage: []contract.Coverage{
			{Source: "linear", ResourceID: testTeamID, Collection: "issues", Status: "complete", Observed: 971},
			{Source: "linear", ResourceID: testTeamID, Collection: "history", Status: "partial", Observed: 400, Reason: "fila de reconciliação Linear: 871 issues pendentes"},
			{Source: "linear", ResourceID: "team-qa-0000-0000", Collection: "issues", Status: "complete", Observed: 3},
			{Source: "github", ResourceID: testRepoID, Collection: "pull_requests", Status: "complete"},
		},
		Actors: []contract.Actor{{ID: "uuid-a", Source: "linear", Name: "Ana Souza", Pseudonym: "Pessoa A"}, {ID: "uuid-ab", Source: "linear", Name: "Bia", Pseudonym: "Pessoa AB"}},
	}
	metric := func(id, name, value string, dims map[string]string) contract.ReportRecord {
		return contract.ReportRecord{Kind: "metric", Metric: &contract.Metric{ID: id, Name: name, Value: contract.Cell{Kind: "integer", Value: value}, Unit: "items", Definition: "definição", Dimensions: dims}}
	}
	records := []contract.ReportRecord{
		{Kind: "component", Component: &contract.ComponentRecord{ID: "proj-abc123", Name: "Assinatura digital", Kind: "linear_project"}},
		{Kind: "link", Link: &contract.LinkRecord{ID: "link-1", SourceID: "linear-1", Relation: "belongs_to_project", TargetID: "proj-abc123", Status: "direct", EvidenceID: "ev"}},
		metric("m-a", "Conclusões registradas por Pessoa A", "3", nil),
		metric("m-ab", "Conclusões registradas por Pessoa AB", "1", nil),
		metric("m-c1", "c", "2", map[string]string{"series": "created", "month": "2026-07", "team": testTeamID}),
		metric("m-d1", "d", "1", map[string]string{"series": "completed", "month": "2026-07", "team": testTeamID}),
		metric("m-c2", "c", "5", map[string]string{"series": "created", "month": "2026-08", "team": testTeamID}),
		metric("m-cy", "cy", "7", map[string]string{"series": "cycle_scope", "team": testTeamID, "cycle": "3", "starts_at": "2026-07-27T00:00:00Z", "ends_at": "2026-08-10T00:00:00Z"}),
		metric("m-pj", "pj", "4", map[string]string{"series": "project_issues", "project": "proj-abc123", "name": "Assinatura digital"}),
	}
	stage := contract.Metric{ID: "m-st", Name: "Permanência mediana na etapa \"QA\" do team " + testTeamID, Value: contract.Cell{Kind: "null"}, Unit: "seconds", Definition: "x", Base: contract.NewMetricBase(100, 971)}
	records = append(records, contract.ReportRecord{Kind: "metric", Metric: &stage})
	return model{Snapshot: snapshot, CoverageStatus: coverageLabel(snapshot.Coverage), Records: records, RecordCount: len(records), GeneratedAt: "2026-11-03T00:00:00Z", RealNames: realNames}
}

func TestReportShowsActivityWindowProvisionalStatusAndBase(t *testing.T) {
	l, err := layoutReport(context.Background(), fase2Doc(false))
	if err != nil {
		t.Fatal(err)
	}
	text := l.text()
	for _, want := range []string{
		"2026-07-21T14:03:48Z — 2026-11-03T00:00:00Z (tempo de vida dos recursos)",
		"Janela de atividade: 2026-07-22T10:00:00Z — 2026-11-02T09:00:00Z",
		"provisório — fila de reconciliação pendente",
		"base: 100 de 971 issues do team (10%)",
		"baixa cobertura",
		"org/legado (sem registros; fora da janela)",
		"2026-08-20T12:56:16Z",
		"belongs_to_project → Assinatura digital",
		"Conclusões registradas por Pessoa A ",
		"pseudônimos estáveis",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("relatório sem %q", want)
		}
	}
	if strings.Contains(text, "Ana Souza") || strings.Contains(text, "2023-03-01T00:00:00Z —") {
		t.Fatal("nomes reais sem preferência ou janela puxada pelo repositório sem registros")
	}
}

func TestReportShowsRealNamesOnlyWhenProfileAsks(t *testing.T) {
	l, err := layoutReport(context.Background(), fase2Doc(true))
	if err != nil {
		t.Fatal(err)
	}
	text := l.text()
	if !strings.Contains(text, "Conclusões registradas por Ana Souza") || !strings.Contains(text, "Conclusões registradas por Bia") {
		t.Fatalf("nomes do perfil não aplicados:\n%s", text)
	}
	if strings.Contains(text, "por Pessoa A") || strings.Contains(text, "Ana SouzaB") {
		t.Fatal("pseudônimo restante ou substituição parcial de Pessoa AB")
	}
}

func TestEvolutionSectionComesBeforeRecords(t *testing.T) {
	doc := fase2Doc(false)
	l, err := layoutReport(context.Background(), doc)
	if err != nil {
		t.Fatal(err)
	}
	text := l.text()
	evolution, records := strings.Index(text, "\nEvolução\n"), strings.Index(text, "\n3. Registros da análise\n")
	if evolution < 0 || records < 0 || evolution > records {
		t.Fatalf("Evolução deveria vir antes dos registros: %d / %d", evolution, records)
	}
	section := text[evolution:records]
	for _, want := range []string{"Issues por mês e team", "2026-07", "2026-08", "ENG", "Ciclos do Linear", "2026-07-27", "Projetos do Linear", "Assinatura digital"} {
		if !strings.Contains(section, want) {
			t.Fatalf("seção Evolução sem %q:\n%s", want, section)
		}
	}
	var toc bool
	for _, s := range newReportView(doc).plannedSections() {
		toc = toc || s.key == "evolution"
	}
	if !toc {
		t.Fatal("Evolução fora do índice")
	}
}
