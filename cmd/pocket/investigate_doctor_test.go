package main

import (
	"io"
	"os"
	"strings"
	"testing"
)

// The PDF is rendered in-process: doctor reports it available with no browser
// or Poppler on PATH.
func TestInvestigationDoctorReportsInProcessPDFWithoutExternalTools(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	withArgs(t, []string{"pocket", "doctor"})
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stdout
	os.Stdout = write
	defer func() { os.Stdout = original }()
	runErr := newInvestigateCommand().Execute()
	if err := write.Close(); err != nil {
		t.Fatal(err)
	}
	output, err := io.ReadAll(read)
	read.Close()
	if err != nil || runErr != nil || !strings.Contains(string(output), "PDF disponível (renderizador interno)") {
		t.Fatalf("doctor não reportou o PDF interno: output=%q run=%v read=%v", output, runErr, err)
	}
}
