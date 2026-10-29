package web

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"pocketcli/internal/investigation/insightcontract"
	"pocketcli/internal/investigation/web/mock"
)

type insightFake struct {
	report                              insightcontract.ExecutiveReport
	starts, lists, gets, pages, details int
	analysisPages                       map[string]insightcontract.AnalysisPage
	omitRunAnalysisID                   bool
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
	if f.omitRunAnalysisID {
		return insightcontract.AnalysisRun{ID: "run-1", SnapshotID: req.SnapshotID, State: "queued", Phase: "source"}, nil
	}
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
	if f.analysisPages != nil {
		return f.analysisPages[cursor], nil
	}
	return insightcontract.AnalysisPage{SnapshotID: snapshot, Total: 1, Items: []insightcontract.AnalysisSummary{{ID: "analysis-1", SnapshotID: snapshot, Status: "complete"}}}, nil
}

func TestAnalysisListingPagesAuthorizeOnlyReturnedVersions(t *testing.T) {
	fake := &insightFake{
		report: insightcontract.ExecutiveReport{Version: insightcontract.Version, PublicationStatus: "ready", AnalysisStatus: "complete", SufficiencyStatus: "sufficient", Cases: []insightcontract.DecisionCase{}, CaseIDs: []string{}},
		analysisPages: map[string]insightcontract.AnalysisPage{
			"":       {SnapshotID: "snap-demo-001", Total: 2, Items: []insightcontract.AnalysisSummary{{ID: "analysis-a", SnapshotID: "snap-demo-001", Status: "complete"}}, NextCursor: "page-2"},
			"page-2": {SnapshotID: "snap-demo-001", Total: 2, Items: []insightcontract.AnalysisSummary{{ID: "analysis-b", SnapshotID: "snap-demo-001", Status: "partial"}}},
		},
	}
	client := newUnlockedClient(t, &scopeCore{Core: mock.New()}, nil, PreviewOptions{InvestigationID: "investigation-demo", InsightReader: fake, AnalysisController: fake})
	first := client.do(http.MethodGet, "/snapshots/snap-demo-001/insights?limit=1", "", "")
	if first.Code != http.StatusOK || !strings.Contains(first.Body.String(), `"next_cursor":"page-2"`) {
		t.Fatalf("first analysis page: %d %s", first.Code, first.Body.String())
	}
	if got := client.do(http.MethodGet, "/snapshots/snap-demo-001/insights/analysis-b", "", ""); got.Code != http.StatusNotFound {
		t.Fatalf("unlisted analysis became authorized: %d", got.Code)
	}
	second := client.do(http.MethodGet, "/snapshots/snap-demo-001/insights?limit=1&cursor=page-2", "", "")
	if second.Code != http.StatusOK || !strings.Contains(second.Body.String(), "analysis-b") {
		t.Fatalf("second analysis page: %d %s", second.Code, second.Body.String())
	}
	if got := client.do(http.MethodGet, "/snapshots/snap-demo-001/insights/analysis-b", "", ""); got.Code != http.StatusOK {
		t.Fatalf("listed second-page analysis not authorized: %d %s", got.Code, got.Body.String())
	}
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
	withCredentialField := `{"snapshot_id":"snap-demo-001","config":{"version":"investigate.insights.v1","provider":{"kind":"configured","api_key":"synthetic-secret"}},"idempotency_key":"credential-field"}`
	if got := client.do(http.MethodPost, "/insights/runs", withCredentialField, ""); got.Code != http.StatusBadRequest {
		t.Fatalf("credential-like unknown config field status: %d %s", got.Code, got.Body.String())
	}
	if fake.starts != 1 {
		t.Fatalf("invalid config reached controller: %d", fake.starts)
	}
}

func TestRunWithoutAnalysisIDDoesNotAuthorizeInventedReport(t *testing.T) {
	fake := &insightFake{omitRunAnalysisID: true}
	client := newUnlockedClient(t, &scopeCore{Core: mock.New()}, nil, PreviewOptions{InvestigationID: "investigation-demo", InsightReader: fake, AnalysisController: fake})
	config := insightcontract.AnalysisConfig{Version: insightcontract.Version, RequestedScope: insightcontract.Scope{Resources: []insightcontract.ResourceRef{}}, Objectives: []insightcontract.Objective{}, Policy: insightcontract.Policy{}, Budget: insightcontract.Budget{}, Provider: insightcontract.ProviderConfig{Kind: "none"}}
	raw, _ := json.Marshal(insightcontract.AnalysisRequest{SnapshotID: "snap-demo-001", Config: config, IdempotencyKey: "without-analysis-id"})
	start := client.do(http.MethodPost, "/insights/runs", string(raw), "")
	if start.Code != http.StatusAccepted {
		t.Fatalf("start without analysis ID: %d %s", start.Code, start.Body.String())
	}
	if got := client.do(http.MethodGet, "/snapshots/snap-demo-001/insights/analysis-guessed", "", ""); got.Code != http.StatusNotFound {
		t.Fatalf("invented report ID became authorized: %d", got.Code)
	}
}

func TestRunEnumsAcceptEveryTerminalAndIntermediateState(t *testing.T) {
	for _, state := range []string{"queued", "running", "completed", "completed_partial", "failed", "cancelled"} {
		if !validRunState(state) {
			t.Errorf("valid run state rejected: %s", state)
		}
	}
	for _, phase := range []string{"source", "facts", "reasoning", "presentation", "persisting", "idle"} {
		if !validRunPhase(phase) {
			t.Errorf("valid run phase rejected: %s", phase)
		}
	}
	if validRunState("paused") || validRunPhase("unknown") {
		t.Fatal("unknown run enum accepted")
	}
}

type executiveExportFake struct {
	artifact         insightcontract.ExecutiveArtifact
	exported, opened int
}

func (f *executiveExportFake) ExportExecutive(_ context.Context, req insightcontract.ExecutiveExportRequest) (insightcontract.ExecutiveArtifact, error) {
	f.exported++
	f.artifact = insightcontract.ExecutiveArtifact{
		ID: "artifact-1", SnapshotID: req.SnapshotID, AnalysisID: req.AnalysisID, Mode: req.Mode,
		Revision: 1, PageCount: 1, ContentHash: strings.Repeat("a", 64), AnalysisHash: strings.Repeat("b", 64),
		RendererVersion: "pdf-native/1", CreatedAt: "2026-10-28T00:00:00Z",
		DownloadPath: "/api/v1/insights/exports/artifact-1/download",
	}
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

func TestExecutiveArtifactMetadataMustBeCompleteAndRouteRelative(t *testing.T) {
	artifact := insightcontract.ExecutiveArtifact{
		ID: "artifact-1", SnapshotID: "snapshot-1", AnalysisID: "analysis-1", Mode: "executive",
		Revision: 1, PageCount: 1, ContentHash: strings.Repeat("a", 64), AnalysisHash: strings.Repeat("b", 64),
		RendererVersion: "pdf-native/1", CreatedAt: "2026-10-28T00:00:00Z", DownloadPath: "/api/v1/insights/exports/artifact-1/download",
	}
	if !validExecutiveArtifact(artifact) {
		t.Fatal("valid artifact rejected")
	}
	artifact.DownloadPath = "/tmp/report.pdf"
	if validExecutiveArtifact(artifact) {
		t.Fatal("filesystem path accepted as download path")
	}
}
