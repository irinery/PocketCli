package web

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"testing"

	"pocketcli/internal/investigation/insightcontract"
	"pocketcli/internal/investigation/web/mock"
)

type insightFake struct {
	report                              insightcontract.ExecutiveReport
	starts, lists, gets, pages, details int
}

func (f *insightFake) GetExecutiveReport(_ context.Context, snapshot, analysis string) (insightcontract.ExecutiveReport, error) {
	f.gets++
	r := f.report
	r.SnapshotID = snapshot
	r.AnalysisID = analysis
	return r, nil
}
func (f *insightFake) GetInsightCase(_ context.Context, snapshot, analysis, id string) (insightcontract.CaseDetail, error) {
	f.details++
	return insightcontract.CaseDetail{SnapshotID: snapshot, AnalysisID: analysis, Case: insightcontract.DecisionCase{ID: id}}, nil
}
func (f *insightFake) ListInsightCases(_ context.Context, snapshot, analysis, cursor string, limit int) (insightcontract.CasePage, error) {
	f.pages++
	return insightcontract.CasePage{SnapshotID: snapshot, AnalysisID: analysis, Total: 1, Cases: []insightcontract.DecisionCase{{ID: "case-1"}}}, nil
}
func (f *insightFake) StartAnalysis(_ context.Context, req insightcontract.AnalysisRequest) (insightcontract.AnalysisRun, error) {
	f.starts++
	id := "analysis-1"
	return insightcontract.AnalysisRun{ID: "run-1", SnapshotID: req.SnapshotID, State: "queued", Phase: "source", AnalysisID: &id}, nil
}
func (f *insightFake) GetAnalysisRun(_ context.Context, id string) (insightcontract.AnalysisRun, error) {
	return insightcontract.AnalysisRun{ID: id, SnapshotID: "snap-demo-001", State: "running", Phase: "facts"}, nil
}
func (f *insightFake) CancelAnalysis(_ context.Context, id string) (insightcontract.AnalysisRun, error) {
	return insightcontract.AnalysisRun{ID: id, SnapshotID: "snap-demo-001", State: "cancelled", Phase: "idle"}, nil
}
func (f *insightFake) ListAnalyses(_ context.Context, snapshot, cursor string, limit int) (insightcontract.AnalysisPage, error) {
	f.lists++
	return insightcontract.AnalysisPage{SnapshotID: snapshot, Total: 1, Items: []insightcontract.AnalysisSummary{{ID: "analysis-1", SnapshotID: snapshot, Status: "complete"}}}, nil
}

func TestInsightReadRoutesAreScopedAndValidateReturnedIdentity(t *testing.T) {
	fake := &insightFake{report: insightcontract.ExecutiveReport{Version: insightcontract.Version, PublicationStatus: "ready", AnalysisStatus: "complete", SufficiencyStatus: "sufficient", TotalCases: 1, Cases: []insightcontract.DecisionCase{{ID: "case-1"}}, CaseIDs: []string{"case-1"}}}
	core := &scopeCore{Core: mock.New()}
	client := newUnlockedClient(t, core, nil, PreviewOptions{InvestigationID: "investigation-demo", InsightReader: fake, AnalysisController: fake})
	if got := client.do(http.MethodGet, "/snapshots/snap-demo-001/insights/analysis-1", "", ""); got.Code != http.StatusNotFound {
		t.Fatalf("unlisted analysis status: %d", got.Code)
	}
	if fake.gets != 0 {
		t.Fatalf("unlisted analysis reached reader: %d calls", fake.gets)
	}
	if got := client.do(http.MethodGet, "/snapshots/snap-demo-001/insights?limit=25", "", ""); got.Code != http.StatusOK {
		t.Fatalf("list analyses: %d %s", got.Code, got.Body.String())
	}
	if fake.starts != 0 || fake.lists != 1 {
		t.Fatalf("opening snapshot started analysis: starts=%d lists=%d", fake.starts, fake.lists)
	}
	if got := client.do(http.MethodGet, "/snapshots/snap-demo-001/insights?limit=201", "", ""); got.Code != http.StatusBadRequest {
		t.Fatalf("out-of-bounds limit: %d", got.Code)
	}
	if got := client.do(http.MethodGet, "/snapshots/snap-demo-001/insights/analysis-1", "", ""); got.Code != http.StatusOK {
		t.Fatalf("report: %d %s", got.Code, got.Body.String())
	}
	if got := client.do(http.MethodGet, "/snapshots/snap-demo-001/insights/analysis-1/cases?limit=10", "", ""); got.Code != http.StatusOK {
		t.Fatalf("cases: %d %s", got.Code, got.Body.String())
	}
	if got := client.do(http.MethodGet, "/snapshots/snap-demo-001/insights/analysis-1/cases/case-1", "", ""); got.Code != http.StatusOK {
		t.Fatalf("case: %d %s", got.Code, got.Body.String())
	}
	if fake.gets != 1 || fake.pages != 1 || fake.details != 1 {
		t.Fatalf("calls: report=%d page=%d detail=%d", fake.gets, fake.pages, fake.details)
	}
	if got := client.do(http.MethodGet, "/snapshots/snap-other/insights", "", ""); got.Code != http.StatusNotFound {
		t.Fatalf("foreign snapshot status: %d", got.Code)
	}
	if fake.lists != 1 {
		t.Fatalf("foreign snapshot reached insights service: %d calls", fake.lists)
	}
}

func TestAnalysisRunStartsOnlyThroughCanonicalExplicitRoute(t *testing.T) {
	fake := &insightFake{report: insightcontract.ExecutiveReport{Version: insightcontract.Version, PublicationStatus: "ready", AnalysisStatus: "complete", SufficiencyStatus: "sufficient", Cases: []insightcontract.DecisionCase{}, CaseIDs: []string{}}}
	client := newUnlockedClient(t, &scopeCore{Core: mock.New()}, nil, PreviewOptions{InvestigationID: "investigation-demo", AnalysisController: fake, InsightReader: fake})
	config := insightcontract.AnalysisConfig{Version: insightcontract.Version, RequestedScope: insightcontract.Scope{Resources: []insightcontract.ResourceRef{}}, Objectives: []insightcontract.Objective{}, Policy: insightcontract.Policy{}, Budget: insightcontract.Budget{}, Provider: insightcontract.ProviderConfig{Kind: "none"}}
	raw, _ := json.Marshal(insightcontract.AnalysisRequest{SnapshotID: "snap-demo-001", Config: config, IdempotencyKey: "test-run"})
	if got := client.do(http.MethodPost, "/insights/runs", string(raw), ""); got.Code != http.StatusAccepted {
		t.Fatalf("start: %d %s", got.Code, got.Body.String())
	}
	if fake.starts != 1 {
		t.Fatalf("explicit route calls=%d", fake.starts)
	}
	if got := client.do(http.MethodGet, "/snapshots/snap-demo-001/insights/analysis-1", "", ""); got.Code != http.StatusOK {
		t.Fatalf("run-returned analysis not authorized: %d %s", got.Code, got.Body.String())
	}
	if got := client.do(http.MethodGet, "/insights/runs/run-1", "", ""); got.Code != http.StatusOK {
		t.Fatalf("get run: %d %s", got.Code, got.Body.String())
	}
	if got := client.do(http.MethodPost, "/insights/runs/run-1/cancel", `{}`, ""); got.Code != http.StatusOK {
		t.Fatalf("cancel run: %d %s", got.Code, got.Body.String())
	}
	if got := client.do(http.MethodPost, "/insights/runs", `{"snapshot_id":"snap-demo-001","config":{"version":"wrong"},"idempotency_key":"x"}`, ""); got.Code != http.StatusBadRequest {
		t.Fatalf("invalid config status: %d %s", got.Code, got.Body.String())
	}
	if fake.starts != 1 {
		t.Fatalf("invalid config reached controller: %d", fake.starts)
	}
}

type executiveExportFake struct {
	artifact         insightcontract.ExecutiveArtifact
	exported, opened int
}

func (f *executiveExportFake) ExportExecutive(_ context.Context, req insightcontract.ExecutiveExportRequest) (insightcontract.ExecutiveArtifact, error) {
	f.exported++
	f.artifact = insightcontract.ExecutiveArtifact{ID: "artifact-1", SnapshotID: req.SnapshotID, AnalysisID: req.AnalysisID, Mode: req.Mode, DownloadPath: "/api/v1/insights/exports/artifact-1/download"}
	return f.artifact, nil
}
func (f *executiveExportFake) GetExecutiveArtifact(_ context.Context, id string) (insightcontract.ExecutiveArtifact, error) {
	if id != f.artifact.ID {
		return insightcontract.ExecutiveArtifact{}, os.ErrNotExist
	}
	return f.artifact, nil
}
func (f *executiveExportFake) OpenExecutiveArtifact(id string) (*os.File, error) {
	f.opened++
	return nil, os.ErrNotExist
}

func TestExecutiveExportRouteUsesArtifactIDAndChecksScope(t *testing.T) {
	fake := &executiveExportFake{}
	reader := &insightFake{report: insightcontract.ExecutiveReport{Version: insightcontract.Version, TotalCases: 0, Cases: []insightcontract.DecisionCase{}, CaseIDs: []string{}}}
	client := newUnlockedClient(t, &scopeCore{Core: mock.New()}, nil, PreviewOptions{InvestigationID: "investigation-demo", InsightReader: reader, AnalysisController: reader, ExecutiveExporter: fake})
	if got := client.do(http.MethodGet, "/snapshots/snap-demo-001/insights", "", ""); got.Code != http.StatusOK {
		t.Fatalf("list analyses: %d %s", got.Code, got.Body.String())
	}
	body := `{"snapshot_id":"snap-demo-001","analysis_id":"analysis-1","mode":"executive","idempotency_key":"key-1"}`
	if got := client.do(http.MethodPost, "/insights/exports", body, ""); got.Code != http.StatusAccepted {
		t.Fatalf("export: %d %s", got.Code, got.Body.String())
	}
	if fake.exported != 1 {
		t.Fatalf("export calls=%d", fake.exported)
	}
	if got := client.do(http.MethodGet, "/insights/exports/artifact-1", "", ""); got.Code != http.StatusOK {
		t.Fatalf("artifact: %d %s", got.Code, got.Body.String())
	}
	if got := client.do(http.MethodGet, "/insights/exports/unknown/download", "", ""); got.Code == http.StatusOK {
		t.Fatal("unknown artifact downloaded")
	}
}
