package web

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/web/mock"
)

type purgingCore struct {
	contract.Core
	purged []string
	err    error
}

func (c *purgingCore) PurgeInvestigation(_ context.Context, id string) (contract.PurgeResult, error) {
	c.purged = append(c.purged, id)
	return contract.PurgeResult{InvestigationID: id, Files: 12, Snapshots: 1, CachesCleared: true}, c.err
}

type fileReports struct {
	scopeReports
	artifact contract.ReportArtifact
	path     string
}

func (f *fileReports) Get(string) (contract.ReportArtifact, error) { return f.artifact, nil }
func (f *fileReports) Open(string) (*os.File, error)               { return os.Open(f.path) }

// Approving the consolidated PDF keeps a verified copy outside the store
// first, and only then removes the data, and only when the operator confirms
// the snapshot by its identifier.
func TestApproveConsolidatedReportArchivesThenRemovesData(t *testing.T) {
	root := t.TempDir()
	pdf := filepath.Join(root, "store-report.pdf")
	data := []byte("%PDF-1.4 consolidado")
	if err := os.WriteFile(pdf, data, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	artifact := contract.ReportArtifact{ID: "report-1", SnapshotID: "snap-demo-001", Revision: 3, Status: "completed", SHA256: hex.EncodeToString(sum[:]), Consolidated: true}
	reports := &fileReports{artifact: artifact, path: pdf}
	core := &purgingCore{Core: mock.New()}
	approved := filepath.Join(root, "approved")
	handler, code, err := NewHandler(core, nil, nil, nil, nil, reports, PreviewOptions{InvestigationID: "investigation-demo", BindHost: "127.0.0.1", ApprovedReportsDir: approved})
	if err != nil {
		t.Fatal(err)
	}
	cookie, csrf := unlockSession(t, handler, code)
	call := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/v1/reports/report-1/approve", strings.NewReader(body))
		req.AddCookie(cookie)
		req.Header.Set("Origin", "http://127.0.0.1")
		req.Header.Set("X-CSRF-Token", csrf)
		result := httptest.NewRecorder()
		handler.ServeHTTP(result, req)
		return result
	}

	if r := call(`{"remove_data":true,"confirm_snapshot_id":"outro"}`); r.Code != http.StatusBadRequest || len(core.purged) != 0 {
		t.Fatalf("sem confirmação do snapshot: %d %s purged=%v", r.Code, r.Body.String(), core.purged)
	}
	if entries, _ := os.ReadDir(approved); len(entries) != 0 {
		t.Fatal("nada deve ser arquivado sem confirmação")
	}

	r := call(`{"remove_data":true,"confirm_snapshot_id":"snap-demo-001"}`)
	var out ApprovalResult
	if r.Code != http.StatusOK || json.Unmarshal(r.Body.Bytes(), &out) != nil {
		t.Fatalf("aprovação: %d %s", r.Code, r.Body.String())
	}
	copied, err := os.ReadFile(out.ApprovedPath)
	if err != nil || string(copied) != string(data) || filepath.Dir(out.ApprovedPath) != approved || !strings.HasSuffix(out.ApprovedPath, "snap-demo-001-consolidado-r3.pdf") {
		t.Fatalf("cópia aprovada: %q err=%v", out.ApprovedPath, err)
	}
	if len(core.purged) != 1 || core.purged[0] != "investigation-demo" || out.Purge == nil || out.Purge.Files != 12 {
		t.Fatalf("remoção: purged=%v result=%+v", core.purged, out.Purge)
	}

	// Approval without removal only archives the PDF.
	core.purged = nil
	if r := call(`{}`); r.Code != http.StatusOK || len(core.purged) != 0 {
		t.Fatalf("aprovar sem remover: %d %s", r.Code, r.Body.String())
	}

	// A PDF that no longer matches its recorded digest blocks the removal.
	if err := os.WriteFile(pdf, []byte("corrompido"), 0o600); err != nil {
		t.Fatal(err)
	}
	if r := call(`{"remove_data":true,"confirm_snapshot_id":"snap-demo-001"}`); r.Code == http.StatusOK || len(core.purged) != 0 {
		t.Fatalf("PDF adulterado não pode liberar a remoção: %d %s", r.Code, r.Body.String())
	}

	// Only the consolidated PDF can be approved.
	reports.artifact.Consolidated = false
	if r := call(`{"remove_data":true,"confirm_snapshot_id":"snap-demo-001"}`); r.Code != http.StatusConflict || len(core.purged) != 0 {
		t.Fatalf("PDF comum: %d %s", r.Code, r.Body.String())
	}
}

func unlockSession(t *testing.T, handler http.Handler, code string) (*http.Cookie, string) {
	t.Helper()
	unlock := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/v1/session/unlock", strings.NewReader(`{"code":"`+code+`"}`))
	unlock.Header.Set("Origin", "http://127.0.0.1")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, unlock)
	if response.Code != 200 {
		t.Fatalf("unlock: %d %s", response.Code, response.Body.String())
	}
	cookie := response.Result().Cookies()[0]
	session := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/v1/session", nil)
	session.AddCookie(cookie)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, session)
	var token struct {
		CSRF string `json:"csrf_token"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &token); err != nil || token.CSRF == "" {
		t.Fatalf("session token: %s %v", response.Body.String(), err)
	}
	return cookie, token.CSRF
}
