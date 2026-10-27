package web

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/web/mock"
)

// RFC 01 C.4: os cenários do preview só usam estados de cobertura do contrato.
func TestPreviewScenariosUseContractCoverageStates(t *testing.T) {
	allowed := map[string]bool{"complete": true, "partial": true, "pending": true, "unauthorized": true, "unavailable": true, "unsupported": true, "not_selected": true}
	for _, scenario := range []string{"complete", "empty", "partial", "unauthorized", "rate_limited", "offline", "pdf_unavailable", "large_report", "rewritten_refs", "llm_disabled", "llm_invalid_output"} {
		core := mock.NewScenario(scenario)
		snapshot, err := core.GetSnapshot(context.Background(), "snap-demo-001")
		if scenario == "unauthorized" {
			continue // GetSnapshot recusa acesso de propósito neste cenário.
		}
		if err != nil {
			t.Fatalf("%s: %v", scenario, err)
		}
		for _, coverage := range snapshot.Coverage {
			if !allowed[coverage.Status] {
				t.Fatalf("%s: estado de cobertura fora do contrato: %+v", scenario, coverage)
			}
		}
	}
	limited, _ := mock.NewScenario("rate_limited").GetSnapshot(context.Background(), "snap-demo-001")
	if limited.Status != "partial" || !strings.HasPrefix(limited.Coverage[0].Reason, "ERR_RATE_LIMITED") {
		t.Fatalf("rate_limited: %+v", limited.Coverage[0])
	}
	rewritten, _ := mock.NewScenario("rewritten_refs").GetSnapshot(context.Background(), "snap-demo-001")
	found := false
	for _, coverage := range rewritten.Coverage {
		if coverage.Collection == "code_history" {
			found = coverage.Status == "partial" && strings.Contains(coverage.Reason, "ref_diverged") && strings.Contains(coverage.Reason, "não comprova rebase")
		}
	}
	if !found || rewritten.Status != "partial" {
		t.Fatalf("rewritten_refs sem divergência honesta: %+v", rewritten.Coverage)
	}
}

// RFC 02 C.9 llm_disabled / L06-01: sem serviço editorial, as rotas Claude
// respondem ERR_LLM_DISABLED e o restante continua disponível.
func TestEditorialRoutesAnswerLLMDisabledWithoutService(t *testing.T) {
	client := newUnlockedClient(t, mock.NewScenario("llm_disabled"), &scopeReports{}, PreviewOptions{InvestigationID: "investigation-demo"})
	for _, request := range []struct{ method, path, body string }{
		{http.MethodGet, "/editorial/models", ""},
		{http.MethodPost, "/editorial/preview", `{"snapshot_id":"snap-demo-001","finding_ids":["f-1"],"annotation_ids":[],"purpose":"clarity","tone":"neutral","model_id":"x"}`},
	} {
		response := client.do(request.method, request.path, request.body, contract.Version)
		var body contract.ErrorResponse
		if response.Code != http.StatusServiceUnavailable || json.Unmarshal(response.Body.Bytes(), &body) != nil || body.Code != "ERR_LLM_DISABLED" {
			t.Fatalf("%s %s: %d %s", request.method, request.path, response.Code, response.Body.String())
		}
	}
	var capabilities map[string]any
	if err := json.Unmarshal(client.do(http.MethodGet, "/capabilities", "", contract.Version).Body.Bytes(), &capabilities); err != nil || capabilities["claude_available"] != false || capabilities["pdf_available"] != true {
		t.Fatalf("capabilities: %+v %v", capabilities, err)
	}
	if ok := client.do(http.MethodGet, "/snapshots/snap-demo-001/findings", "", contract.Version); ok.Code != http.StatusOK {
		t.Fatalf("achados deveriam continuar disponíveis: %d", ok.Code)
	}
	for _, asset := range []string{realAppJS("investigation-demo", ""), appJS} {
		if !strings.Contains(asset, "claude_available") || !strings.Contains(asset, "ERR_LLM_DISABLED") {
			t.Fatal("tela Claude não trata serviço desativado")
		}
	}
}
