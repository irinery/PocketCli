package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pocketcli/internal/investigation/annotations"
	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/editorial"
	"pocketcli/internal/investigation/queries"
	"pocketcli/internal/investigation/web/mock"
)

type scopeCore struct {
	contract.Core
	queries, starts, plans int
	planInput              contract.PlanInput
}

func (c *scopeCore) GetSnapshot(ctx context.Context, id string) (contract.Snapshot, error) {
	if id == "snap-other" {
		return contract.Snapshot{ID: id, InvestigationID: "investigation-other"}, nil
	}
	return c.Core.GetSnapshot(ctx, id)
}
func (c *scopeCore) LoadPlan(id string) (contract.Plan, error) {
	return contract.Plan{ID: id, InvestigationID: "investigation-other"}, nil
}
func (c *scopeCore) GetRun(_ context.Context, id string) (contract.Run, error) {
	return contract.Run{ID: id, PlanID: "plan-other"}, nil
}
func (c *scopeCore) Query(context.Context, contract.QueryRequest) (contract.QueryResult, error) {
	c.queries++
	return contract.QueryResult{}, nil
}
func (c *scopeCore) StartRun(context.Context, contract.RunRequest) (contract.Run, error) {
	c.starts++
	return contract.Run{}, nil
}
func (c *scopeCore) PreparePlan(_ context.Context, in contract.PlanInput) (contract.Plan, error) {
	c.plans++
	c.planInput = in
	return contract.Plan{}, nil
}

type scopeReports struct{ finalized, listed, opened int }

func (s *scopeReports) Finalize(context.Context, contract.FinalizeRequest) (contract.ReportArtifact, error) {
	s.finalized++
	return contract.ReportArtifact{}, nil
}
func (s *scopeReports) Get(id string) (contract.ReportArtifact, error) {
	return contract.ReportArtifact{ID: id, SnapshotID: "snap-other", Status: "completed"}, nil
}
func (s *scopeReports) List(string) []contract.ReportArtifact {
	s.listed++
	return nil
}
func (s *scopeReports) Open(string) (*os.File, error) {
	s.opened++
	return nil, os.ErrNotExist
}

type scopeEditorial struct{ previews, generations, reviews int }

func (*scopeEditorial) Models(context.Context) ([]editorial.Model, error) { return nil, nil }
func (s *scopeEditorial) Preview(context.Context, editorial.Selection) (editorial.Preview, error) {
	s.previews++
	return editorial.Preview{}, nil
}
func (s *scopeEditorial) Generate(context.Context, editorial.GenerateInput) (editorial.Draft, error) {
	s.generations++
	return editorial.Draft{}, nil
}
func (s *scopeEditorial) Review(string, int, bool) (editorial.Draft, error) {
	s.reviews++
	return editorial.Draft{}, nil
}
func (*scopeEditorial) Drafts(string) []editorial.Draft { return nil }
func (*scopeEditorial) Draft(string) (editorial.Draft, bool) {
	return editorial.Draft{SnapshotID: "snap-other"}, true
}
func (*scopeEditorial) PreviewInfo(string) (editorial.Preview, bool) {
	return editorial.Preview{SnapshotID: "snap-other"}, true
}

func TestRealServerRejectsOtherInvestigationBeforeReadingOrWriting(t *testing.T) {
	root := t.TempDir()
	notes, err := annotations.Open(filepath.Join(root, "annotations"))
	if err != nil {
		t.Fatal(err)
	}
	foreignNote, err := notes.Create(annotations.CreateInput{InvestigationID: "investigation-other", SnapshotID: "snap-other", TargetType: annotations.Snapshot, TargetID: "snap-other", Body: "foreign", OperatorLabel: "tester"})
	if err != nil {
		t.Fatal(err)
	}
	queryStore, err := queries.Open(filepath.Join(root, "queries"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := queryStore.Record(contract.QueryRequest{SnapshotID: "snap-other", SQL: "SELECT 1"}, contract.QueryResult{QueryID: "foreign-query"}, nil); err != nil {
		t.Fatal(err)
	}
	foreignQuery, err := queryStore.Create(queries.CreateInput{Name: "foreign", QueryID: "foreign-query"}, "investigation-other", "snap-other")
	if err != nil {
		t.Fatal(err)
	}
	core := &scopeCore{Core: mock.New()}
	reports := &scopeReports{}
	edit := &scopeEditorial{}
	handler, code, err := NewHandler(core, nil, nil, edit, notes, reports, PreviewOptions{InvestigationID: "investigation-demo", BindHost: "127.0.0.1", QueryHistory: queryStore})
	if err != nil {
		t.Fatal(err)
	}
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
	call := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "http://127.0.0.1/api/v1"+path, strings.NewReader(body))
		req.AddCookie(cookie)
		if method != http.MethodGet {
			req.Header.Set("Origin", "http://127.0.0.1")
			req.Header.Set("X-CSRF-Token", token.CSRF)
		}
		result := httptest.NewRecorder()
		handler.ServeHTTP(result, req)
		return result
	}
	if own := call(http.MethodGet, "/snapshots/snap-demo-001", ""); own.Code != http.StatusOK {
		t.Fatalf("own snapshot unavailable: %d %s", own.Code, own.Body.String())
	}
	if own := call(http.MethodPost, "/plans", `{}`); own.Code != http.StatusOK || core.planInput.InvestigationID != "investigation-demo" {
		t.Fatalf("plan was not bound to this investigation: status=%d input=%+v", own.Code, core.planInput)
	}
	core.plans = 0
	queryResponse := call(http.MethodPost, "/snapshots/snap-demo-001/query", `{"snapshot_id":"snap-demo-001","sql":"SELECT 1"}`)
	var ownQuery contract.QueryResult
	if queryResponse.Code != http.StatusOK || json.Unmarshal(queryResponse.Body.Bytes(), &ownQuery) != nil || ownQuery.QueryID == "" {
		t.Fatalf("own query has no saved execution id: %d %s", queryResponse.Code, queryResponse.Body.String())
	}
	if saved := call(http.MethodPost, "/queries/saved?snapshot_id=snap-demo-001", `{"name":"own","query_id":"`+ownQuery.QueryID+`"}`); saved.Code != http.StatusOK {
		t.Fatalf("own query could not be saved: %d %s", saved.Code, saved.Body.String())
	}
	core.queries = 0
	cases := []struct{ method, path, body string }{
		{"GET", "/investigations/investigation-other/snapshots", ""},
		{"GET", "/snapshots/snap-other", ""},
		{"GET", "/snapshots/snap-other/queries", ""},
		{"GET", "/snapshots/snap-other/findings", ""},
		{"GET", "/snapshots/snap-other/findings/f-1", ""},
		{"GET", "/snapshots/snap-other/evidence/e-1", ""},
		{"GET", "/snapshots/snap-other/report", ""},
		{"POST", "/snapshots/snap-other/query", `{"snapshot_id":"snap-other","sql":"SELECT 1"}`},
		{"GET", "/queries/history?snapshot_id=snap-other", ""},
		{"GET", "/queries/saved?snapshot_id=snap-other", ""},
		{"POST", "/queries/saved?snapshot_id=snap-other", `{"name":"x","query_id":"foreign-query"}`},
		{"POST", "/queries/saved?snapshot_id=snap-demo-001", `{"name":"x","query_id":"foreign-query"}`},
		{"PATCH", "/queries/saved/" + foreignQuery.ID, `{"expected_revision":1,"name":"changed"}`},
		{"GET", "/reports?snapshot_id=snap-other", ""},
		{"POST", "/reports", `{"snapshot_id":"snap-other"}`},
		{"GET", "/reports/report-other", ""},
		{"GET", "/reports/report-other/download", ""},
		{"GET", "/editorial/drafts?snapshot_id=snap-other", ""},
		{"POST", "/editorial/preview", `{"snapshot_id":"snap-other"}`},
		{"POST", "/editorial/generate", `{"preview_id":"preview-other"}`},
		{"POST", "/editorial/draft-other/accept", `{"expected_revision":1}`},
		{"GET", "/annotations?snapshot_id=snap-other", ""},
		{"POST", "/annotations", `{"investigation_id":"investigation-other","snapshot_id":"snap-other","target_type":"snapshot","target_id":"snap-other","body":"x","operator_label":"tester"}`},
		{"PATCH", "/annotations/" + foreignNote.ID, `{"expected_revision":1,"body":"changed"}`},
		{"POST", "/annotations/" + foreignNote.ID + "/resolve", `{"expected_revision":1}`},
		{"POST", "/plans", `{"investigation_id":"investigation-other"}`},
		{"POST", "/runs", `{"plan_id":"plan-other"}`},
		{"GET", "/runs/run-other", ""},
		{"POST", "/runs/run-other/pause", `{}`},
	}
	for _, tc := range cases {
		result := call(tc.method, tc.path, tc.body)
		if result.Code != http.StatusNotFound {
			t.Errorf("%s %s: status=%d body=%s", tc.method, tc.path, result.Code, result.Body.String())
		}
	}
	if core.queries != 0 || core.starts != 0 || core.plans != 0 || reports.finalized != 0 || reports.listed != 0 || reports.opened != 0 || edit.previews != 0 || edit.generations != 0 || edit.reviews != 0 {
		t.Fatalf("foreign operation reached a service: core=%+v reports=%+v editorial=%+v", core, reports, edit)
	}
	if note, ok := notes.Get(foreignNote.ID); !ok || note.Revision != 1 {
		t.Fatalf("foreign annotation changed: %+v", note)
	}
	if saved, ok := queryStore.GetSaved(foreignQuery.ID); !ok || saved.Revision != 1 {
		t.Fatalf("foreign saved query changed: %+v", saved)
	}
	if rows := queryStore.List("snap-other"); len(rows) != 1 {
		t.Fatalf("foreign query was copied into this investigation: %+v", rows)
	}
}
