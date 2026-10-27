package report

import (
	"bytes"
	"html/template"
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
	tmpl, err := template.New("report").Funcs(template.FuncMap{"recordID": recordID, "intPtr": formatIntPtr, "floatPtr": formatFloatPtr}).Parse(reportTemplate)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := tmpl.Execute(&output, doc); err != nil {
		t.Fatal(err)
	}
	html := output.String()
	for _, want := range []string{
		"<b>Cobertura:</b> parcial",
		"<h2>2. Cobertura — parcial</h2>",
		"Fila de revisões ainda pendente",
		"Leitura negada para o time",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("PDF HTML omitiu %q", want)
		}
	}
	if got := coverageLabel([]contract.Coverage{{Status: "complete"}, {Status: "not_selected"}}); got != "completa" {
		t.Fatalf("cobertura completa=%q", got)
	}
	if got := coverageLabel(nil); got != "não determinada" {
		t.Fatalf("cobertura vazia=%q", got)
	}
}
