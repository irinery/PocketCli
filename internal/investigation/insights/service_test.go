package insights

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"pocketcli/internal/investigation/insightcontract"
	"pocketcli/internal/investigation/storage"
)

type sourceFunc func(context.Context, string, insightcontract.AnalysisConfig) (insightcontract.SourceBundle, error)

func (f sourceFunc) ReadInsightSource(ctx context.Context, id string, cfg insightcontract.AnalysisConfig) (insightcontract.SourceBundle, error) {
	return f(ctx, id, cfg)
}

func serviceConfig() insightcontract.AnalysisConfig {
	return insightcontract.AnalysisConfig{Version: insightcontract.Version, ID: "cfg", RequestedScope: insightcontract.Scope{History: insightcontract.Window{Start: "2022-10-28T00:00:00Z", End: "2026-10-28T00:00:00Z"}, ReportWindow: insightcontract.Window{Start: "2026-10-21T00:00:00Z", End: "2026-10-28T00:00:00Z"}, Resources: []insightcontract.ResourceRef{}}, Policy: insightcontract.Policy{ContentMode: "local_excerpts"}, Provider: insightcontract.ProviderConfig{Kind: "none"}}
}

func TestServiceRunsAsyncIdempotentlyAndPagesCases(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put("snapshots", "snap-1", struct {
		ID string `json:"id"`
	}{"snap-1"}); err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{}, 1)
	releaseSource := make(chan struct{})
	source := sourceFunc(func(ctx context.Context, id string, _ insightcontract.AnalysisConfig) (insightcontract.SourceBundle, error) {
		select {
		case entered <- struct{}{}:
		default:
		}
		select {
		case <-releaseSource:
		case <-ctx.Done():
			return insightcontract.SourceBundle{}, ctx.Err()
		}
		return insightcontract.SourceBundle{Version: insightcontract.Version, InputHash: "input", Snapshot: insightcontract.SnapshotRef{ID: id}, Limitations: []string{}}, nil
	})
	cfg := serviceConfig()
	deps := Dependencies{Source: source, Facts: func(bundle insightcontract.SourceBundle, _ insightcontract.AnalysisConfig) (insightcontract.FactsResult, error) {
		return insightcontract.FactsResult{SnapshotID: bundle.Snapshot.ID, InputHash: bundle.InputHash, Cases: []insightcontract.FactCase{{ID: "f1"}, {ID: "f2"}}}, nil
	},
		Decisions: func(ctx context.Context, bundle insightcontract.SourceBundle, facts insightcontract.FactsResult, _ insightcontract.AnalysisConfig, provider insightcontract.ReasoningProvider) (insightcontract.DecisionAnalysis, error) {
			if provider != nil {
				t.Fatal("provider real não esperado")
			}
			return insightcontract.DecisionAnalysis{SnapshotID: bundle.Snapshot.ID, Status: "complete", Cases: []insightcontract.DecisionCase{{ID: "case-b", FactCaseIDs: []string{"f2"}}, {ID: "case-a", FactCaseIDs: []string{"f1"}}}}, nil
		},
		Presentation: func(_ insightcontract.SourceBundle, _ insightcontract.FactsResult, d insightcontract.DecisionAnalysis, _ insightcontract.AnalysisConfig) (insightcontract.ExecutiveReport, error) {
			return insightcontract.ExecutiveReport{SnapshotID: d.SnapshotID, AnalysisStatus: "complete", SufficiencyStatus: "sufficient", Cases: []insightcontract.DecisionCase{d.Cases[1]}, TotalCases: 2}, nil
		}, Store: store, Now: func() time.Time { return time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC) }}
	svc, err := NewService(deps)
	if err != nil {
		t.Fatal(err)
	}
	req := insightcontract.AnalysisRequest{SnapshotID: "snap-1", Config: cfg, IdempotencyKey: "same"}
	type startResult struct {
		run insightcontract.AnalysisRun
		err error
	}
	started := make(chan startResult, 1)
	go func() {
		run, err := svc.StartAnalysis(context.Background(), req)
		started <- startResult{run: run, err: err}
	}()
	var result startResult
	select {
	case result = <-started:
	case <-time.After(time.Second):
		close(releaseSource)
		t.Fatal("StartAnalysis esperou callback bloqueante")
	}
	run, err := result.run, result.err
	if err != nil {
		t.Fatal(err)
	}
	if run.State != "queued" {
		t.Fatalf("start não retornou run queued: %+v", run)
	}
	<-entered
	close(releaseSource)
	replay, err := svc.StartAnalysis(context.Background(), req)
	if err != nil || replay.ID != run.ID {
		t.Fatalf("idempotência: %+v err=%v", replay, err)
	}
	changed := req
	changed.Config.Policy.ExcerptMaxRunes = 20
	if _, err := svc.StartAnalysis(context.Background(), changed); err == nil || !strings.Contains(err.Error(), "ERR_IDEMPOTENCY_CONFLICT") {
		t.Fatalf("conflito não recusado: %v", err)
	}
	_ = store.Put("snapshots", "snap-other", struct {
		ID string `json:"id"`
	}{"snap-other"})
	otherSnapshot := req
	otherSnapshot.SnapshotID = "snap-other"
	if _, err := svc.StartAnalysis(context.Background(), otherSnapshot); err == nil || !strings.Contains(err.Error(), "ERR_IDEMPOTENCY_CONFLICT") {
		t.Fatalf("chave reutilizada em outro snapshot não conflitou: %v", err)
	}
	completed := waitRun(t, svc, run.ID)
	if completed.State != "completed" || completed.AnalysisID == nil || completed.Phase != "idle" {
		t.Fatalf("run não concluiu: %+v", completed)
	}
	page, err := svc.ListInsightCases(context.Background(), "snap-1", *completed.AnalysisID, "", 1)
	_ = page
	_ = err
	all, err := svc.ListInsightCases(context.Background(), "snap-1", *completed.AnalysisID, "", 1)
	if err != nil || all.Total != 2 || len(all.Cases) != 1 || all.Cases[0].ID != "case-a" || all.NextCursor == "" {
		t.Fatalf("página inicial: %+v err=%v", all, err)
	}
	second, err := svc.ListInsightCases(context.Background(), "snap-1", *completed.AnalysisID, all.NextCursor, 1)
	if err != nil || len(second.Cases) != 1 || second.Cases[0].ID != "case-b" {
		t.Fatalf("segunda página: %+v err=%v", second, err)
	}
	if _, err := svc.ListInsightCases(context.Background(), "snap-1", "other-analysis", all.NextCursor, 1); err == nil {
		t.Fatal("cursor de outra análise aceito")
	}
	analyses, err := svc.ListAnalyses(context.Background(), "snap-1", "", 1)
	if err != nil || analyses.Total != 1 || len(analyses.Items) != 1 || analyses.Items[0].ID != *completed.AnalysisID {
		t.Fatalf("listagem de análises: %+v err=%v", analyses, err)
	}
	detail, err := svc.GetInsightCase(context.Background(), "snap-1", *completed.AnalysisID, "case-a")
	if err != nil || detail.Case.ID != "case-a" || len(detail.Facts) != 1 || detail.Facts[0].ID != "f1" {
		t.Fatalf("detalhe de caso: %+v err=%v", detail, err)
	}
	if _, err := svc.GetInsightCase(context.Background(), "different-snapshot", *completed.AnalysisID, "case-a"); err == nil {
		t.Fatal("leitura de análise em outro snapshot foi aceita")
	}
	report, err := svc.GetExecutiveReport(context.Background(), "snap-1", *completed.AnalysisID)
	if err != nil || report.AnalysisID != *completed.AnalysisID || report.TotalCases != 2 {
		t.Fatalf("report: %+v err=%v", report, err)
	}
	if err := svc.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestServiceCancelsAndRestartDoesNotReplay(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Put("snapshots", "snap-2", struct {
		ID string `json:"id"`
	}{"snap-2"})
	entered := make(chan struct{})
	source := sourceFunc(func(ctx context.Context, id string, _ insightcontract.AnalysisConfig) (insightcontract.SourceBundle, error) {
		close(entered)
		<-ctx.Done()
		return insightcontract.SourceBundle{}, ctx.Err()
	})
	calls := 0
	deps := Dependencies{Source: source, Facts: func(insightcontract.SourceBundle, insightcontract.AnalysisConfig) (insightcontract.FactsResult, error) {
		calls++
		return insightcontract.FactsResult{}, nil
	}, Decisions: func(context.Context, insightcontract.SourceBundle, insightcontract.FactsResult, insightcontract.AnalysisConfig, insightcontract.ReasoningProvider) (insightcontract.DecisionAnalysis, error) {
		calls++
		return insightcontract.DecisionAnalysis{}, nil
	}, Presentation: func(insightcontract.SourceBundle, insightcontract.FactsResult, insightcontract.DecisionAnalysis, insightcontract.AnalysisConfig) (insightcontract.ExecutiveReport, error) {
		calls++
		return insightcontract.ExecutiveReport{}, nil
	}, Store: store}
	svc, err := NewService(deps)
	if err != nil {
		t.Fatal(err)
	}
	run, err := svc.StartAnalysis(context.Background(), insightcontract.AnalysisRequest{SnapshotID: "snap-2", Config: serviceConfig(), IdempotencyKey: "cancel"})
	if err != nil {
		t.Fatal(err)
	}
	<-entered
	cancelled, err := svc.CancelAnalysis(context.Background(), run.ID)
	if err != nil || cancelled.State != "cancelled" {
		t.Fatalf("cancel: %+v err=%v", cancelled, err)
	}
	if err := svc.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("callbacks continuaram após cancelamento: %d", calls)
	}
	interrupted := insightcontract.AnalysisRun{ID: "interrupted", SnapshotID: "snap-2", State: "running", Phase: "facts"}
	if err := store.Put("insightruns", interrupted.ID, interrupted); err != nil {
		t.Fatal(err)
	}
	if _, err := NewService(deps); err != nil {
		t.Fatal(err)
	}
	var recovered insightcontract.AnalysisRun
	if err := store.Get("insightruns", interrupted.ID, &recovered); err != nil {
		t.Fatal(err)
	}
	if recovered.State != "failed" || recovered.ErrorCode == nil || *recovered.ErrorCode != "ERR_INTERRUPTED_NO_REPLAY" {
		t.Fatalf("restart não marcou terminal sem replay: %+v", recovered)
	}
}

func TestShutdownCancelsAndWaitsForActiveJob(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Put("snapshots", "snap-shutdown", struct {
		ID string `json:"id"`
	}{"snap-shutdown"})
	entered := make(chan struct{})
	deps := Dependencies{Source: sourceFunc(func(ctx context.Context, id string, _ insightcontract.AnalysisConfig) (insightcontract.SourceBundle, error) {
		close(entered)
		<-ctx.Done()
		return insightcontract.SourceBundle{}, ctx.Err()
	}), Facts: func(insightcontract.SourceBundle, insightcontract.AnalysisConfig) (insightcontract.FactsResult, error) {
		return insightcontract.FactsResult{}, nil
	}, Decisions: func(context.Context, insightcontract.SourceBundle, insightcontract.FactsResult, insightcontract.AnalysisConfig, insightcontract.ReasoningProvider) (insightcontract.DecisionAnalysis, error) {
		return insightcontract.DecisionAnalysis{}, nil
	}, Presentation: func(insightcontract.SourceBundle, insightcontract.FactsResult, insightcontract.DecisionAnalysis, insightcontract.AnalysisConfig) (insightcontract.ExecutiveReport, error) {
		return insightcontract.ExecutiveReport{}, nil
	}, Store: store}
	svc, err := NewService(deps)
	if err != nil {
		t.Fatal(err)
	}
	run, err := svc.StartAnalysis(context.Background(), insightcontract.AnalysisRequest{SnapshotID: "snap-shutdown", Config: serviceConfig(), IdempotencyKey: "shutdown"})
	if err != nil {
		t.Fatal(err)
	}
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := svc.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	result, err := svc.GetAnalysisRun(context.Background(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "cancelled" {
		t.Fatalf("shutdown não encerrou run ativo: %+v", result)
	}
}

func TestServiceRejectsMissingSnapshotAndInvalidConfig(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	deps := Dependencies{Source: sourceFunc(func(context.Context, string, insightcontract.AnalysisConfig) (insightcontract.SourceBundle, error) {
		return insightcontract.SourceBundle{}, nil
	}), Facts: func(insightcontract.SourceBundle, insightcontract.AnalysisConfig) (insightcontract.FactsResult, error) {
		return insightcontract.FactsResult{}, nil
	}, Decisions: func(context.Context, insightcontract.SourceBundle, insightcontract.FactsResult, insightcontract.AnalysisConfig, insightcontract.ReasoningProvider) (insightcontract.DecisionAnalysis, error) {
		return insightcontract.DecisionAnalysis{}, nil
	}, Presentation: func(insightcontract.SourceBundle, insightcontract.FactsResult, insightcontract.DecisionAnalysis, insightcontract.AnalysisConfig) (insightcontract.ExecutiveReport, error) {
		return insightcontract.ExecutiveReport{}, nil
	}, Store: store}
	svc, err := NewService(deps)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.StartAnalysis(context.Background(), insightcontract.AnalysisRequest{SnapshotID: "missing", Config: serviceConfig(), IdempotencyKey: "k"}); err == nil {
		t.Fatal("snapshot ausente aceito")
	}
	bad := serviceConfig()
	bad.Version = "wrong"
	if _, err := svc.StartAnalysis(context.Background(), insightcontract.AnalysisRequest{SnapshotID: "missing", Config: bad, IdempotencyKey: "k"}); err == nil {
		t.Fatal("versão inválida aceita")
	}
}

func TestServicePersistsCallbackErrorsAtEveryPhase(t *testing.T) {
	for _, phase := range []string{"source", "facts", "reasoning", "presentation"} {
		t.Run(phase, func(t *testing.T) {
			store, err := storage.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			_ = store.Put("snapshots", "snap-error", struct {
				ID string `json:"id"`
			}{"snap-error"})
			deps := Dependencies{Store: store, Source: sourceFunc(func(ctx context.Context, id string, _ insightcontract.AnalysisConfig) (insightcontract.SourceBundle, error) {
				if phase == "source" {
					return insightcontract.SourceBundle{}, errors.New("fake source failure")
				}
				return insightcontract.SourceBundle{Version: insightcontract.Version, InputHash: "h", Snapshot: insightcontract.SnapshotRef{ID: id}}, nil
			}),
				Facts: func(bundle insightcontract.SourceBundle, _ insightcontract.AnalysisConfig) (insightcontract.FactsResult, error) {
					if phase == "facts" {
						return insightcontract.FactsResult{}, errors.New("fake facts failure")
					}
					return insightcontract.FactsResult{SnapshotID: bundle.Snapshot.ID, InputHash: bundle.InputHash}, nil
				},
				Decisions: func(ctx context.Context, bundle insightcontract.SourceBundle, facts insightcontract.FactsResult, _ insightcontract.AnalysisConfig, _ insightcontract.ReasoningProvider) (insightcontract.DecisionAnalysis, error) {
					if phase == "reasoning" {
						return insightcontract.DecisionAnalysis{}, errors.New("fake reasoning failure")
					}
					return insightcontract.DecisionAnalysis{SnapshotID: bundle.Snapshot.ID, Status: "complete"}, nil
				},
				Presentation: func(_ insightcontract.SourceBundle, _ insightcontract.FactsResult, decisions insightcontract.DecisionAnalysis, _ insightcontract.AnalysisConfig) (insightcontract.ExecutiveReport, error) {
					if phase == "presentation" {
						return insightcontract.ExecutiveReport{}, errors.New("fake presentation failure")
					}
					return insightcontract.ExecutiveReport{SnapshotID: decisions.SnapshotID, AnalysisStatus: "complete", SufficiencyStatus: "sufficient"}, nil
				}}
			svc, err := NewService(deps)
			if err != nil {
				t.Fatal(err)
			}
			run, err := svc.StartAnalysis(context.Background(), insightcontract.AnalysisRequest{SnapshotID: "snap-error", Config: serviceConfig(), IdempotencyKey: "error"})
			if err != nil {
				t.Fatal(err)
			}
			result := waitRun(t, svc, run.ID)
			if result.State != "failed" || result.ErrorCode == nil || !strings.Contains(*result.ErrorCode, "FAILED") {
				t.Fatalf("erro da fase %s não terminalizado: %+v", phase, result)
			}
			_ = svc.Shutdown(context.Background())
		})
	}
}

func waitRun(t *testing.T, svc *Service, id string) insightcontract.AnalysisRun {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		run, err := svc.GetAnalysisRun(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if isTerminal(run.State) {
			return run
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timeout esperando run terminal")
	return insightcontract.AnalysisRun{}
}

func TestRecoverInterruptedIgnoresPersistedArtifacts(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Put("insightruns", "old", insightcontract.AnalysisRun{ID: "old", State: "running", Phase: "reasoning"})
	deps := Dependencies{Source: sourceFunc(func(context.Context, string, insightcontract.AnalysisConfig) (insightcontract.SourceBundle, error) {
		return insightcontract.SourceBundle{}, errors.New("should not run")
	}), Facts: func(insightcontract.SourceBundle, insightcontract.AnalysisConfig) (insightcontract.FactsResult, error) {
		return insightcontract.FactsResult{}, nil
	}, Decisions: func(context.Context, insightcontract.SourceBundle, insightcontract.FactsResult, insightcontract.AnalysisConfig, insightcontract.ReasoningProvider) (insightcontract.DecisionAnalysis, error) {
		return insightcontract.DecisionAnalysis{}, nil
	}, Presentation: func(insightcontract.SourceBundle, insightcontract.FactsResult, insightcontract.DecisionAnalysis, insightcontract.AnalysisConfig) (insightcontract.ExecutiveReport, error) {
		return insightcontract.ExecutiveReport{}, nil
	}, Store: store}
	if _, err := NewService(deps); err != nil {
		t.Fatal(err)
	}
	var run insightcontract.AnalysisRun
	_ = store.Get("insightruns", "old", &run)
	if run.State != "failed" {
		t.Fatalf("run não terminalizado: %+v", run)
	}
	var raw json.RawMessage
	if err := store.Get("insightruns", "old", &raw); err != nil {
		t.Fatal(err)
	}
}
