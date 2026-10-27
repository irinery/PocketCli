package core_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/core"
	"pocketcli/internal/investigation/queries"
	"pocketcli/internal/investigation/report"
	"pocketcli/internal/investigation/storage"
	"pocketcli/internal/investigation/web"
)

type deniedExternalTransport struct{ attempts *atomic.Int64 }

func (d deniedExternalTransport) RoundTrip(*http.Request) (*http.Response, error) {
	d.attempts.Add(1)
	return nil, errors.New("external network disabled for offline smoke")
}

// Opt-in offline product flow: typed import -> plan confirmation -> snapshot -> SQL -> real PDF.
func TestOfflineRuntimeToPDF(t *testing.T) {
	chromium := os.Getenv("POCKETCLI_CHROMIUM")
	if chromium == "" {
		t.Skip("defina POCKETCLI_CHROMIUM para validar o fluxo PDF")
	}
	for _, name := range []string{"POCKETCLI_INVESTIGATE_GITHUB_TOKEN", "POCKETCLI_INVESTIGATE_LINEAR_KEY", "ANTHROPIC_API_KEY"} {
		t.Setenv(name, "")
	}
	var externalAttempts, proxyAttempts atomic.Int64
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		proxyAttempts.Add(1)
		http.Error(w, "offline", http.StatusBadGateway)
	}))
	defer proxy.Close()
	for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY"} {
		t.Setenv(name, proxy.URL)
	}
	root := t.TempDir()
	store, err := storage.Open(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	clock := func() time.Time { return time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC) }
	c, err := core.NewCore(core.CoreConfig{Store: store, Clock: clock, HTTPClient: &http.Client{Transport: deniedExternalTransport{attempts: &externalAttempts}}})
	if err != nil {
		t.Fatal(err)
	}
	finalizer, err := report.New(c, filepath.Join(root, "reports"), chromium, false, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	c.SetFinalizer(finalizer)
	input := contract.PlanInput{SchemaVersion: contract.Version, Name: "offline-runtime", Modules: contract.Modules{RuntimeImport: true}, Period: contract.Period{Preset: "30d", Timezone: "UTC"}}
	first, err := c.PreparePlan(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.ImportRuntime(context.Background(), first.InvestigationID, []byte(`{"schema_version":"pocketcli.runtime.v1","observed_at":"2026-01-08T00:00:00Z","environment":"production","components":[{"name":"api","enabled":true,"revision_sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},{"name":"worker","enabled":false}]}`))
	if err != nil {
		t.Fatal(err)
	}
	input.InvestigationID = first.InvestigationID
	plan, err := c.PreparePlan(context.Background(), input)
	if err != nil || plan.Hash == first.Hash {
		t.Fatalf("plano não fixou importação: %+v %v", plan, err)
	}
	run, err := c.StartRun(context.Background(), contract.RunRequest{PlanID: plan.ID, ConfirmedPlanHash: plan.Hash, IdempotencyKey: "offline-runtime-pdf"})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		run, err = c.GetRun(context.Background(), run.ID)
		if err != nil {
			t.Fatal(err)
		}
		if run.State == "completed" || run.State == "completed_partial" {
			break
		}
		if run.State == "failed" || run.State == "report_failed" || run.State == "cancelled" {
			t.Fatalf("run falhou: %+v", run)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if run.SnapshotID == "" || (run.State != "completed" && run.State != "completed_partial") {
		t.Fatalf("run não finalizou: %+v", run)
	}
	result, err := c.Query(context.Background(), contract.QueryRequest{SnapshotID: run.SnapshotID, SQL: "SELECT COUNT(*) FROM v_components", MaxRows: 10, TimeoutMS: 1000})
	if err != nil || len(result.Rows) != 1 || len(result.Rows[0]) != 1 || result.Rows[0][0].Value != "2" {
		t.Fatalf("SQL offline inválido: %+v %v", result, err)
	}
	artifacts := finalizer.List(run.SnapshotID)
	if len(artifacts) != 1 || artifacts[0].PageCount < 1 || artifacts[0].FindingCount < 1 {
		t.Fatalf("PDF incompleto: %+v", artifacts)
	}
	queryStore, err := queries.Open(filepath.Join(root, "queries"))
	if err != nil {
		t.Fatal(err)
	}
	handler, code, err := web.NewHandler(c, c.Setup(), nil, nil, nil, finalizer, web.PreviewOptions{InvestigationID: plan.InvestigationID, SnapshotID: run.SnapshotID, BindHost: "127.0.0.1", QueryHistory: queryStore})
	if err != nil {
		t.Fatal(err)
	}
	unlock := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/v1/session/unlock", strings.NewReader(`{"code":"`+code+`"}`))
	unlock.Header.Set("Origin", "http://127.0.0.1")
	unlockResult := httptest.NewRecorder()
	handler.ServeHTTP(unlockResult, unlock)
	if unlockResult.Code != http.StatusOK || len(unlockResult.Result().Cookies()) != 1 {
		t.Fatalf("desbloqueio local: %d %s", unlockResult.Code, unlockResult.Body.String())
	}
	cookie := unlockResult.Result().Cookies()[0]
	session := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/v1/session", nil)
	session.AddCookie(cookie)
	sessionResult := httptest.NewRecorder()
	handler.ServeHTTP(sessionResult, session)
	var sessionData struct {
		CSRF string `json:"csrf_token"`
	}
	if sessionResult.Code != http.StatusOK || json.Unmarshal(sessionResult.Body.Bytes(), &sessionData) != nil || sessionData.CSRF == "" {
		t.Fatalf("sessão local: %d %s", sessionResult.Code, sessionResult.Body.String())
	}
	call := func(method, path string, body []byte) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, "http://127.0.0.1"+path, bytes.NewReader(body))
		request.AddCookie(cookie)
		if method != http.MethodGet {
			request.Header.Set("Origin", "http://127.0.0.1")
			request.Header.Set("X-CSRF-Token", sessionData.CSRF)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	if page := call(http.MethodGet, "/", nil); page.Code != http.StatusOK {
		t.Fatalf("HTML offline: %d", page.Code)
	}
	if snapshot := call(http.MethodGet, "/api/v1/snapshots/"+run.SnapshotID, nil); snapshot.Code != http.StatusOK {
		t.Fatalf("snapshot offline: %d %s", snapshot.Code, snapshot.Body.String())
	}
	queryBody, err := json.Marshal(contract.QueryRequest{SnapshotID: run.SnapshotID, SQL: "SELECT COUNT(*) FROM v_components", MaxRows: 10, TimeoutMS: 1000})
	if err != nil {
		t.Fatal(err)
	}
	queryResult := call(http.MethodPost, "/api/v1/snapshots/"+run.SnapshotID+"/query", queryBody)
	var webQuery contract.QueryResult
	if queryResult.Code != http.StatusOK || json.Unmarshal(queryResult.Body.Bytes(), &webQuery) != nil || len(webQuery.Rows) != 1 || webQuery.Rows[0][0].Value != "2" {
		t.Fatalf("SQL via servidor offline: %d %s", queryResult.Code, queryResult.Body.String())
	}
	defaultQueryBody, err := json.Marshal(contract.QueryRequest{
		SnapshotID: run.SnapshotID,
		SQL:        "SELECT id, window_start, window_end FROM v_snapshot",
		MaxRows:    200,
		TimeoutMS:  5000,
	})
	if err != nil {
		t.Fatal(err)
	}
	defaultQueryResult := call(http.MethodPost, "/api/v1/snapshots/"+run.SnapshotID+"/query", defaultQueryBody)
	var defaultWebQuery contract.QueryResult
	if defaultQueryResult.Code != http.StatusOK || json.Unmarshal(defaultQueryResult.Body.Bytes(), &defaultWebQuery) != nil ||
		len(defaultWebQuery.Rows) != 1 || defaultWebQuery.Rows[0][0].Value != run.SnapshotID {
		t.Fatalf("consulta padrão da UI via servidor offline: %d %s", defaultQueryResult.Code, defaultQueryResult.Body.String())
	}
	pdf := call(http.MethodGet, "/api/v1/reports/"+artifacts[0].ID+"/download", nil)
	if pdf.Code != http.StatusOK || !bytes.HasPrefix(pdf.Body.Bytes(), []byte("%PDF-")) {
		t.Fatalf("PDF via servidor offline: %d %s", pdf.Code, pdf.Header().Get("Content-Type"))
	}
	if externalAttempts.Load() != 0 || proxyAttempts.Load() != 0 {
		t.Fatalf("fluxo offline tentou rede externa: transport=%d proxy=%d", externalAttempts.Load(), proxyAttempts.Load())
	}
	t.Logf("fluxo offline: snapshot=%s PDF=%d páginas transporte=%d proxy=%d", run.SnapshotID, artifacts[0].PageCount, externalAttempts.Load(), proxyAttempts.Load())
}
