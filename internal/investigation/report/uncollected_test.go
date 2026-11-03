package report

import (
	"context"
	"strings"
	"testing"

	"pocketcli/internal/investigation/contract"
)

func TestSummaryNamesForksAndUncollectedRepositories(t *testing.T) {
	total := int64(10)
	snapshot := contract.Snapshot{ID: "snap-refs", InvestigationID: "investigation-1",
		Resources: []contract.Resource{{Source: "github", ID: "1", Name: "acme/sdk", Fork: true, Parent: "vendor/sdk"}},
		Coverage: []contract.Coverage{
			{Source: "github", ResourceID: "acme/private", Collection: contract.ReferencedRepositoryCollection, Status: "unauthorized", Observed: 7, Reason: "PRs citados em anexos Linear"},
			{Source: "analysis", ResourceID: "investigation-1", Collection: contract.PullReferencesCollection, Status: "partial", Observed: 3, Expected: &total, Reason: "7 de 10 PRs"},
		}}
	doc := model{Snapshot: snapshot, CoverageStatus: coverageLabel(snapshot.Coverage)}
	layout, err := layoutReport(context.Background(), doc)
	if err != nil {
		t.Fatal(err)
	}
	text := layout.text()
	for _, want := range []string{
		"Cobertura Git insuficiente",
		"7 de 10 PRs citados no Linear (70%)",
		"acme/private (7 PRs, sem permissão)",
		"acme/sdk (fork de vendor/sdk)",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("PDF omitiu %q:\n%s", want, text)
		}
	}
}
