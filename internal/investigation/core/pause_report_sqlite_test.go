//go:build investigate_sqlite && cgo

package core

import (
	"context"
	"net/http"
	"testing"
	"time"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/secrets"
	"pocketcli/internal/investigation/storage"
)

type waitingFinalizer struct {
	entered chan struct{}
	exited  chan struct{}
}

func (f *waitingFinalizer) Finalize(ctx context.Context, _ contract.FinalizeRequest) (contract.ReportArtifact, error) {
	close(f.entered)
	<-ctx.Done()
	close(f.exited)
	return contract.ReportArtifact{}, ctx.Err()
}

func TestPauseDuringPDFKeepsSnapshotReadyForReportRetry(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	secretStore := secrets.NewSessionStore()
	if _, err := secretStore.Put(context.Background(), contract.CredentialInput{Source: "github", Secret: "test-secret", Persistence: "session"}); err != nil {
		t.Fatal(err)
	}
	c, err := NewCore(CoreConfig{Store: store, Secrets: secretStore, HTTPClient: &http.Client{Transport: &fakeTransport{title: "PR"}}, Clock: func() time.Time { return time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	finalizer := &waitingFinalizer{entered: make(chan struct{}), exited: make(chan struct{})}
	c.SetFinalizer(finalizer)
	plan, err := c.PreparePlan(context.Background(), contract.PlanInput{SchemaVersion: contract.Version, GitHubEnabled: true, Scope: &contract.Scope{Mode: "selected", Repositories: []string{"acme/api"}}, Period: contract.Period{Preset: "30d", Timezone: "UTC"}, Limits: contract.Limits{GitHubRPH: 3600000}})
	if err != nil {
		t.Fatal(err)
	}
	run, err := c.StartRun(context.Background(), contract.RunRequest{PlanID: plan.ID, ConfirmedPlanHash: plan.Hash, IdempotencyKey: "pause-during-pdf"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-finalizer.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("finalizer não iniciou")
	}
	paused, err := c.PauseRun(context.Background(), run.ID)
	if err != nil || paused.State != "report_failed" || paused.SnapshotID == "" {
		t.Fatalf("interrupção do PDF não preservou snapshot: %+v err=%v", paused, err)
	}
	select {
	case <-finalizer.exited:
	case <-time.After(5 * time.Second):
		t.Fatal("finalizer não recebeu cancelamento")
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.WaitRunStopped(shutdown, run.ID); err != nil {
		t.Fatalf("worker não encerrou após cancelamento: %v", err)
	}
	got, err := c.GetRun(context.Background(), run.ID)
	if err != nil || got.State != "report_failed" || got.SnapshotID != paused.SnapshotID || got.LastError == nil || got.LastError.Code != "ERR_PDF_FAILED" {
		t.Fatalf("estado após interrupção do PDF: %+v err=%v", got, err)
	}
	if _, err := c.GetSnapshot(context.Background(), got.SnapshotID); err != nil {
		t.Fatalf("snapshot publicado ficou indisponível: %v", err)
	}
	if _, err := c.Query(context.Background(), contract.QueryRequest{SnapshotID: got.SnapshotID, SQL: "SELECT COUNT(*) FROM v_findings"}); err != nil {
		t.Fatalf("SQL do snapshot ficou indisponível: %v", err)
	}
}
