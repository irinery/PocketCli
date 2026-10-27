package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/secrets"
	"pocketcli/internal/pocketpath"
)

// RFC 02 L03-10 / RFC 01 S02 passo 6: o PDF indisponível aparece no
// resumo do plano, antes da confirmação, sem bloquear nem instalar nada.
func TestPlanSummaryShowsMissingPDFEngineBeforeCollection(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	plan := contract.Plan{Hash: "hash-1", WindowEnd: "2026-02-01T00:00:00Z", Resources: []contract.Resource{}, Warnings: []string{}}
	var unavailable bytes.Buffer
	if err := printInvestigationPlanSummary(&unavailable, plan, "Plano", errors.New("ERR_PDF_DEPENDENCY: diretório de relatórios indisponível (permission denied)")); err != nil {
		t.Fatal(err)
	}
	text := unavailable.String()
	for _, want := range []string{"PDF automático: indisponível", "diretório de relatórios", "report_failed", "nada é instalado automaticamente"} {
		if !strings.Contains(text, want) {
			t.Fatalf("resumo sem %q: %q", want, text)
		}
	}
	var available bytes.Buffer
	if err := printInvestigationPlanSummary(&available, plan, "Plano", nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(available.String(), "PDF automático: disponível (renderizador interno)") || strings.Contains(available.String(), "indisponível") {
		t.Fatalf("resumo com PDF disponível incorreto: %q", available.String())
	}
}

func TestPDFWarningJSONIsSingleStructuredStderrRecord(t *testing.T) {
	var none bytes.Buffer
	if err := writeInvestigationPDFWarningJSON(&none, nil); err != nil || none.Len() != 0 {
		t.Fatalf("PDF disponível não deve emitir aviso: %q %v", none.String(), err)
	}
	var out bytes.Buffer
	if err := writeInvestigationPDFWarningJSON(&out, errors.New("ERR_PDF_DEPENDENCY: diretório de relatórios indisponível (read-only file system)")); err != nil {
		t.Fatal(err)
	}
	if strings.Count(out.String(), "\n") != 1 {
		t.Fatalf("aviso deve ocupar uma linha JSON: %q", out.String())
	}
	var record struct{ Type, Code, Message string }
	if err := json.Unmarshal(out.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record.Type != "warning" || record.Code != "ERR_PDF_DEPENDENCY" || !strings.Contains(record.Message, "diretório de relatórios") {
		t.Fatalf("registro inesperado: %+v", record)
	}
}

// A composição usada por `pocket investigate` e `refresh` expõe o mesmo erro
// de dependência que o doctor, sem exigir credencial ou rede. O PDF é gerado
// no processo; só um diretório de relatórios inutilizável o deixa indisponível.
func TestInvestigationCompositionKeepsPDFDependencyErrorForPlanSummary(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	data, err := pocketpath.DataDir()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(data, "investigations")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "reports"), []byte("não é diretório"), 0600); err != nil {
		t.Fatal(err)
	}
	services, err := newInvestigationServicesWithSecrets(secrets.NewSessionStore())
	if err != nil {
		t.Fatal(err)
	}
	if services.reportError == nil || services.report != nil {
		t.Fatalf("diretório inutilizável deveria deixar PDF indisponível: err=%v report=%v", services.reportError, services.report)
	}
	if status := investigationPDFStatus(services.reportError); !strings.Contains(status, "diretório de relatórios indisponível") {
		t.Fatalf("motivo ausente no resumo: %q", status)
	}
}
