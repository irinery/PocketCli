package report

import (
	"context"
	"strings"
	"testing"

	"pocketcli/internal/investigation/contract"
)

func TestPartialCoverageAppearsOnCoverAndKeepsEveryReason(t *testing.T) {
	snapshot := contract.Snapshot{ID: "snap-partial", InvestigationID: "investigation-1", Coverage: []contract.Coverage{
		{Source: "github", ResourceID: "repo-1", Collection: "reviews", Status: "partial", Reason: "Fila de revisões ainda pendente"},
		{Source: "linear", ResourceID: "team-1", Collection: "issues", Status: "unauthorized", Reason: "Leitura negada para o time"},
	}}
	doc := model{Snapshot: snapshot, CoverageStatus: coverageLabel(snapshot.Coverage)}
	layout, err := layoutReport(context.Background(), doc)
	if err != nil {
		t.Fatal(err)
	}
	text := layout.text()
	for _, want := range []string{
		"Cobertura: parcial",
		"2. Cobertura — parcial",
		"Fila de revisões ainda pendente",
		"Leitura negada para o time",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("PDF omitiu %q", want)
		}
	}
	if got := coverageLabel([]contract.Coverage{{Status: "complete"}, {Status: "not_selected"}}); got != "completa" {
		t.Fatalf("cobertura completa=%q", got)
	}
	if got := coverageLabel(nil); got != "não determinada" {
		t.Fatalf("cobertura vazia=%q", got)
	}
}

func TestNullMetricInReportShowsUncalculatedValueAndSample(t *testing.T) {
	eligible := int64(4)
	metric := contract.Metric{ID: "cycle-small", Name: "Tempo de ciclo mediano", Value: contract.Cell{Kind: "null"}, Unit: "seconds", SampleSize: 3, EligibleSize: &eligible, Limitations: []string{"comparação insuficiente"}}
	doc := model{Records: []contract.ReportRecord{{Kind: "metric", Metric: &metric}}}
	layout, err := layoutReport(context.Background(), doc)
	if err != nil {
		t.Fatal(err)
	}
	text := layout.text()
	if !strings.Contains(text, "Tempo de ciclo mediano = não calculado · amostra 3 / 4") || !strings.Contains(text, "comparação insuficiente") {
		t.Fatalf("amostra pequena não ficou explícita no PDF: %s", text)
	}
}
