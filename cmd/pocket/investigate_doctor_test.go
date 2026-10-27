package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInvestigationDoctorReportsInvalidChromiumBeforeCollection(t *testing.T) {
	t.Setenv("POCKETCLI_CHROMIUM", filepath.Join(t.TempDir(), "missing-chromium"))
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
	if err != nil || runErr != nil || !strings.Contains(string(output), "PDF indisponível") || !strings.Contains(string(output), "executável Chromium configurado") {
		t.Fatalf("doctor não identificou dependência inválida: output=%q run=%v read=%v", output, runErr, err)
	}
}
