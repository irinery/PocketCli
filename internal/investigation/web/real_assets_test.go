package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pocketcli/internal/investigation/web/mock"
)

func TestRealServerServesSelectedInvestigationAndSnapshot(t *testing.T) {
	if _, _, err := NewHandler(mock.New(), nil, nil, nil, nil, nil, PreviewOptions{BindHost: "127.0.0.1"}); err == nil {
		t.Fatal("real server accepted missing investigation id")
	}
	handler, code, err := NewHandler(mock.New(), nil, nil, nil, nil, nil, PreviewOptions{
		BindHost: "127.0.0.1", InvestigationID: "investigation-demo", SnapshotID: "snap-demo-001",
	})
	if err != nil {
		t.Fatal(err)
	}
	asset := httptest.NewRecorder()
	handler.ServeHTTP(asset, httptest.NewRequest(http.MethodGet, "http://127.0.0.1/?asset=app.js", nil))
	if asset.Code != http.StatusOK {
		t.Fatalf("asset status: %d", asset.Code)
	}
	if strings.Contains(asset.Body.String(), "investigation-demo") || !strings.Contains(asset.Body.String(), "session/unlock") {
		t.Fatal("unauthenticated asset exposed investigation id or cannot unlock")
	}
	unlock := httptest.NewRecorder()
	handler.ServeHTTP(unlock, httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/v1/session/unlock", strings.NewReader(`{"code":"`+code+`"}`)))
	if unlock.Code != http.StatusForbidden {
		t.Fatalf("unlock without origin: got %d", unlock.Code)
	}
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/v1/session/unlock", strings.NewReader(`{"code":"`+code+`"}`))
	request.Header.Set("Origin", "http://127.0.0.1")
	unlock = httptest.NewRecorder()
	handler.ServeHTTP(unlock, request)
	if unlock.Code != http.StatusOK || len(unlock.Result().Cookies()) != 1 {
		t.Fatalf("unlock failed: %d %s", unlock.Code, unlock.Body.String())
	}
	assetRequest := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/?asset=app.js", nil)
	assetRequest.AddCookie(unlock.Result().Cookies()[0])
	asset = httptest.NewRecorder()
	handler.ServeHTTP(asset, assetRequest)
	if !strings.Contains(asset.Body.String(), `const investigationId = "investigation-demo"`) ||
		!strings.Contains(asset.Body.String(), `const requestedSnapshotId = "snap-demo-001"`) ||
		strings.Contains(asset.Body.String(), "Resultado determinístico do snapshot de demonstração") {
		t.Fatal("real asset is missing selected ids or contains demo copy")
	}
	pageRequest := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/", nil)
	pageRequest.AddCookie(unlock.Result().Cookies()[0])
	page := httptest.NewRecorder()
	handler.ServeHTTP(page, pageRequest)
	if !strings.Contains(page.Body.String(), "INVESTIGAÇÃO LOCAL") || strings.Contains(page.Body.String(), "Carregando dados de demonstração") {
		t.Fatal("real page still presents demo content")
	}
	apiRequest := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/v1/investigations/investigation-demo/snapshots", nil)
	apiRequest.AddCookie(unlock.Result().Cookies()[0])
	apiResponse := httptest.NewRecorder()
	handler.ServeHTTP(apiResponse, apiRequest)
	var snapshots []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(apiResponse.Body.Bytes(), &snapshots); err != nil || len(snapshots) != 1 || snapshots[0].ID != "snap-demo-001" {
		t.Fatalf("snapshot API: status=%d body=%s error=%v", apiResponse.Code, apiResponse.Body.String(), err)
	}
}

func TestRealAssetQuotesIDsWithoutRecursiveReplacement(t *testing.T) {
	js := realAppJS(`owner/\"<script>__SNAPSHOT_ID__`, "snap-__INVESTIGATION_ID__")
	if !strings.Contains(js, `const investigationId = "owner/\\\"\u003cscript\u003e__SNAPSHOT_ID__"`) ||
		!strings.Contains(js, `const requestedSnapshotId = "snap-__INVESTIGATION_ID__"`) {
		t.Fatal("IDs were not safely quoted or were substituted recursively")
	}
}

func TestExecutiveOverviewDoesNotPreloadLegacyCollectionsOrAllReportPages(t *testing.T) {
	js := realAppJS("investigation-demo", "snap-demo-001")
	start := strings.Index(js, "async function chooseSnapshot(id)")
	end := strings.Index(js[start:], "  async function init()")
	if start < 0 || end < 0 {
		t.Fatal("snapshot selection function was not found")
	}
	choose := js[start : start+end]
	if strings.Contains(choose, "/findings") || strings.Contains(choose, "/queries") || strings.Contains(choose, "/report") {
		t.Fatal("opening a snapshot preloads a legacy collection")
	}
	if strings.Contains(js, "loadReportRecords") || !strings.Contains(js, "async function loadReportPage(cursor)") {
		t.Fatal("legacy map is not paginated")
	}
	if !strings.Contains(js, "write('/insights/runs'") || !strings.Contains(js, "attempts < 60") || !strings.Contains(js, "Cancelar análise") {
		t.Fatal("explicit bounded analysis workflow is incomplete")
	}
}

func TestExecutiveUIShowsMetricContextAndEvidenceIntegrityFields(t *testing.T) {
	js := realAppJS("investigation-demo", "snap-demo-001")
	for _, required := range []string{"item.cohort_id", "item.method", "item.limitations", "item.content_hash", "item.excerpt_hash", "item.sanitization_version", "item.source_updated_at", "labelPt(value.state) + ' · ' + labelPt(value.level"} {
		if !strings.Contains(js, required) {
			t.Fatalf("executive UI omits %s", required)
		}
	}
	if !strings.Contains(js, "Recursos solicitados:") || !strings.Contains(js, "requestedScope.report_window") || !strings.Contains(js, "requestedScope.history") {
		t.Fatal("executive UI omits requested scope windows or resources")
	}
}

func TestAnalysisUIHasExplicitEmptyStartAndBoundedTerminalPolling(t *testing.T) {
	js := realAppJS("investigation-demo", "snap-demo-001")
	for _, required := range []string{
		"Este snapshot ainda não tem análise executiva",
		"somente pelo botão abaixo",
		"attempts < 60",
		"Date.now() < deadline",
		"['completed', 'completed_partial', 'failed', 'cancelled']",
		"if (current.analysis_id)",
		"AnalysisID indisponível; selecione uma versão listada",
	} {
		if !strings.Contains(js, required) {
			t.Fatalf("analysis UI omits behavior marker %q", required)
		}
	}
}

func TestExecutiveListsRejectCursorCyclesAndDuplicateIDsAcrossPages(t *testing.T) {
	js := realAppJS("investigation-demo", "snap-demo-001")
	for _, required := range []string{
		"analysisPages.requested.has(page.next_cursor)",
		"analysisPages.ids.has(item.id)",
		"pageState.requested.has(page.next_cursor)",
		"pageState.ids.has(item.id)",
		"pageState.lastID",
		"O total de análises mudou durante a paginação.",
	} {
		if !strings.Contains(js, required) {
			t.Fatalf("paginated executive UI omits guard %q", required)
		}
	}
}

func TestAnalysisConfigInputWhitelistsCanonicalFieldsBeforeRequest(t *testing.T) {
	js := realAppJS("investigation-demo", "snap-demo-001")
	for _, required := range []string{
		"const analysisConfigFromFile = raw =>",
		"onlyKeys(raw.provider, ['kind', 'model', 'endpoint_profile', 'adapter_version']",
		"credenciais e campos extras não são enviados",
		"analysisConfigFromFile(JSON.parse(await configFile.files[0].text()))",
		"write('/insights/runs', {snapshot_id: snapshot.id, config, idempotency_key",
	} {
		if !strings.Contains(js, required) {
			t.Fatalf("config input flow omits %q", required)
		}
	}
}

func TestProgressiveNavigationShowsLoadingEmptyAndIncompletePageErrors(t *testing.T) {
	js := realAppJS("investigation-demo", "snap-demo-001")
	for _, required := range []string{
		"Carregando snapshot e análises disponíveis…",
		"Carregando relatório executivo…",
		"Carregando casos…",
		"Carregando evidências e timeline do caso…",
		"Não há casos nesta página.",
		"A paginação terminou antes de carregar todas as análises.",
		"A paginação terminou antes de carregar todos os casos.",
		"Gerando PDF e validando os dados da análise…",
	} {
		if !strings.Contains(js, required) {
			t.Fatalf("progressive navigation omits state %q", required)
		}
	}
}

func TestRealSQLConsoleUsesSupportedQueryDefaults(t *testing.T) {
	js := realAppJS("investigation-demo", "snap-demo-001")
	if !strings.Contains(js, "const manualSQL = 'SELECT id, window_start, window_end FROM v_snapshot'") ||
		!strings.Contains(js, "editor.value = manualSQL") ||
		!strings.Contains(js, "max_rows: 200, timeout_ms: 5000") ||
		strings.Contains(js, "sqlite_master") {
		t.Fatal("SQL console default is outside the read-only query contract")
	}
}

func TestPreviewKeepsDemoAsset(t *testing.T) {
	handler, _, err := NewHandler(mock.New(), nil, nil, nil, nil, nil, PreviewOptions{Demo: true, BindHost: "127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://127.0.0.1/?asset=app.js", nil))
	if !strings.Contains(response.Body.String(), "Resultado determinístico do snapshot de demonstração") {
		t.Fatal("demo asset changed")
	}
}
