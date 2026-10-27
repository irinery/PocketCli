package core

import (
	"context"
	"testing"

	"pocketcli/internal/investigation/contract"
)

// Nested shares map onto the run's 0..90 collection band and never go back.
func TestProgressRangesNestAndStayMonotonic(t *testing.T) {
	run := &contract.Run{}
	second := withProgressRange(context.Background(), 0.5, 1) // second of two resources: 45..90
	children := withProgressRange(second, 0.2, 0.9)           // 54..85.5
	reportProgress(children, run, 0)
	if run.Progress != 54 {
		t.Fatalf("início dos filhos = %d, esperado 54", run.Progress)
	}
	reportProgress(children, run, 1)
	if run.Progress != 85 {
		t.Fatalf("fim dos filhos = %d, esperado 85", run.Progress)
	}
	reportProgress(withProgressRange(context.Background(), 0, 0.5), run, 1) // first resource on resume: 45
	if run.Progress != 85 {
		t.Fatalf("progresso voltou para %d", run.Progress)
	}
	setProgress(run, 250)
	if run.Progress != progressCompleted {
		t.Fatalf("progresso acima de 100: %d", run.Progress)
	}
}
