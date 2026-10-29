package insightsintegration

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"pocketcli/internal/investigation/analysisfacts"
	"pocketcli/internal/investigation/decisionengine"
	"pocketcli/internal/investigation/insightcontract"
	"pocketcli/internal/investigation/insights"
	"pocketcli/internal/investigation/presentation"
	"pocketcli/internal/investigation/report"
	"pocketcli/internal/investigation/storage"
)

type fixturePackage struct {
	Config insightcontract.AnalysisConfig `json:"config"`
	Source insightcontract.SourceBundle   `json:"source"`
}

type fixtureSource struct {
	bundle insightcontract.SourceBundle
	reads  atomic.Int32
}

func (s *fixtureSource) ReadInsightSource(_ context.Context, snapshotID string, _ insightcontract.AnalysisConfig) (insightcontract.SourceBundle, error) {
	s.reads.Add(1)
	bundle := s.bundle
	bundle.Snapshot.ID = snapshotID
	return bundle, nil
}

func TestOfflineRuntimeToExecutiveAndDetailPDF(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "internal", "investigation", "insightcontract", "testdata", "fixture_go.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture fixturePackage
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	snapshotID := fixture.Source.Snapshot.ID
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put("snapshots", snapshotID, struct {
		ID                  string                        `json:"id"`
		SchemaVersion       string                        `json:"schema_version"`
		QueryCatalogVersion string                        `json:"query_catalog_version"`
		Resources           []insightcontract.ResourceRef `json:"resources"`
	}{ID: snapshotID, SchemaVersion: fixture.Source.Snapshot.SchemaVersion, QueryCatalogVersion: fixture.Source.Snapshot.QueryCatalogVersion, Resources: fixture.Source.ObservedResources}); err != nil {
		t.Fatal(err)
	}
	source := &fixtureSource{bundle: fixture.Source}
	fixture.Config.Policy.ExternalProcessingAllowed = true
	service, err := insights.NewService(insights.Dependencies{
		Source: source,
		Facts:  analysisfacts.Analyze,
		Decisions: func(ctx context.Context, bundle insightcontract.SourceBundle, facts insightcontract.FactsResult, cfg insightcontract.AnalysisConfig, _ insightcontract.ReasoningProvider) (insightcontract.DecisionAnalysis, error) {
			return decisionengine.Analyze(ctx, bundle, facts, cfg, nil)
		},
		Presentation: presentation.Build,
		Store:        store,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := service.Shutdown(ctx); err != nil {
			t.Errorf("shutdown: %v", err)
		}
	}()
	run, err := service.StartAnalysis(context.Background(), insightcontract.AnalysisRequest{SnapshotID: snapshotID, Config: fixture.Config, IdempotencyKey: "offline-integration"})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for run.State != "completed" && run.State != "completed_partial" && run.State != "failed" && run.State != "cancelled" {
		if time.Now().After(deadline) {
			t.Fatal("analysis did not finish")
		}
		time.Sleep(10 * time.Millisecond)
		run, err = service.GetAnalysisRun(context.Background(), run.ID)
		if err != nil {
			t.Fatal(err)
		}
	}
	if run.State == "failed" || run.State == "cancelled" {
		code := ""
		if run.ErrorCode != nil {
			code = *run.ErrorCode
		}
		t.Fatalf("analysis terminal state=%s error=%s", run.State, code)
	}
	if run.AnalysisID == nil || source.reads.Load() != 1 {
		t.Fatalf("run=%+v source reads=%d, want one source read and published analysis", run, source.reads.Load())
	}
	reportPath := filepath.Join(t.TempDir(), "executive")
	exporter, err := report.NewExecutiveFinalizer(service, reportPath)
	if err != nil {
		t.Fatal(err)
	}
	demoOutput := os.Getenv("POCKETCLI_INSIGHTS_DEMO_DIR")
	if demoOutput != "" {
		if err := os.MkdirAll(demoOutput, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, mode := range []string{"executive", "analysis_detail"} {
		artifact, err := exporter.ExportExecutive(context.Background(), insightcontract.ExecutiveExportRequest{SnapshotID: snapshotID, AnalysisID: *run.AnalysisID, Mode: mode, IdempotencyKey: "export-" + mode})
		if err != nil {
			t.Fatalf("export %s: %v", mode, err)
		}
		pdf, err := exporter.OpenExecutiveArtifact(artifact.ID)
		if err != nil {
			t.Fatalf("open %s: %v", mode, err)
		}
		contents, readErr := io.ReadAll(pdf)
		closeErr := pdf.Close()
		if readErr != nil || closeErr != nil {
			t.Fatalf("read %s PDF: read=%v close=%v", mode, readErr, closeErr)
		}
		if len(contents) < 8 || string(contents[:5]) != "%PDF-" {
			t.Fatalf("%s export is not a PDF", mode)
		}
		if demoOutput != "" {
			name := "pocketcli-insights-demo-executive.pdf"
			if mode == "analysis_detail" {
				name = "pocketcli-insights-demo-detail.pdf"
			}
			if err := os.WriteFile(filepath.Join(demoOutput, name), contents, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if mode == "analysis_detail" && artifact.SelectedCaseCount != artifact.TotalCaseCount {
			t.Fatalf("detail export is incomplete: selected=%d total=%d", artifact.SelectedCaseCount, artifact.TotalCaseCount)
		}
	}
}
