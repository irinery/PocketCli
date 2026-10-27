package report_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"testing"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/report"
	"pocketcli/internal/investigation/web/mock"
)

// Opt-in integration gate for the real Chromium and Poppler renderer.
func TestRealPDFWithFixture(t *testing.T) {
	chromium := os.Getenv("POCKETCLI_CHROMIUM")
	if chromium == "" {
		t.Skip("defina POCKETCLI_CHROMIUM para validar o PDF real")
	}
	for _, test := range []struct {
		scenario string
		findings int64
	}{{"complete", 2}, {"partial", 2}, {"large_report", 237}} {
		t.Run(test.scenario, func(t *testing.T) {
			reader := mock.NewScenario(test.scenario)
			f, err := report.New(reader, t.TempDir(), chromium, true, nil, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			artifact, err := f.Finalize(context.Background(), contract.FinalizeRequest{SnapshotID: "snap-demo-001"})
			if err != nil {
				t.Fatal(err)
			}
			if artifact.Status != "completed" || artifact.FindingCount != test.findings || artifact.PageCount < 1 {
				t.Fatalf("artefato incompleto: %+v", artifact)
			}
			file, err := f.Open(artifact.ID)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			data, err := io.ReadAll(file)
			if err != nil || !bytes.HasPrefix(data, []byte("%PDF-")) {
				t.Fatalf("PDF inválido: tamanho=%d erro=%v", len(data), err)
			}
		})
	}
}
