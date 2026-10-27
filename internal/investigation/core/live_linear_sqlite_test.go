//go:build investigate_sqlite && cgo

package core

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/report"
	"pocketcli/internal/investigation/secrets"
	"pocketcli/internal/investigation/storage"
)

// Opt-in smoke: one explicitly selected Linear team, read-only and bounded.
// No personal credential or organization data is used by regular test runs.
func TestRealLinearToSnapshot(t *testing.T) {
	team := os.Getenv("POCKETCLI_INVESTIGATE_REAL_LINEAR_TEAM")
	if team == "" {
		t.Skip("configure POCKETCLI_INVESTIGATE_REAL_LINEAR_TEAM para smoke real")
	}
	key := os.Getenv("POCKETCLI_INVESTIGATE_LINEAR_KEY")
	if key == "" {
		t.Fatal("POCKETCLI_INVESTIGATE_LINEAR_KEY ausente")
	}
	secretStore := secrets.NewSessionStore()
	defer secretStore.Delete(context.Background(), "linear")
	if _, err := secretStore.Put(context.Background(), contract.CredentialInput{Source: "linear", Secret: key, Persistence: "session"}); err != nil {
		t.Fatal(err)
	}
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCore(CoreConfig{Store: store, Secrets: secretStore})
	if err != nil {
		t.Fatal(err)
	}
	pdf, err := report.New(c, filepath.Join(store.Root, "reports"), false, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	c.SetFinalizer(pdf)
	now := time.Now().UTC()
	plan, err := c.PreparePlan(context.Background(), contract.PlanInput{
		SchemaVersion: contract.Version,
		Name:          "read-only Linear smoke",
		LinearEnabled: true,
		Scope:         &contract.Scope{Mode: "selected", Teams: []string{team}},
		Period: contract.Period{
			Preset: "custom", FromDate: now.AddDate(0, 0, -1).Format("2006-01-02"),
			ToDate: now.Format("2006-01-02"), Timezone: "UTC",
		},
		Limits: contract.Limits{
			LinearRPH: 180, MaxSessionMS: 240000,
			MaxTransferBytes: 16 << 20, MaxLocalBytes: 64 << 20,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Resources) != 1 || plan.Resources[0].Source != "linear" {
		t.Fatalf("escopo Linear inesperado: %d recursos", len(plan.Resources))
	}
	run, err := c.StartRun(context.Background(), contract.RunRequest{
		PlanID: plan.ID, ConfirmedPlanHash: plan.Hash, IdempotencyKey: "linear-live-one-team",
	})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(250 * time.Second)
	for time.Now().Before(deadline) {
		run, err = c.GetRun(context.Background(), run.ID)
		if err != nil {
			t.Fatal(err)
		}
		switch run.State {
		case "completed", "completed_partial":
			goto published
		case "failed", "paused", "report_failed", "cancelled":
			t.Fatalf("run Linear não concluiu: estado=%s erro=%+v", run.State, run.LastError)
		}
		time.Sleep(200 * time.Millisecond)
	}
	_, _ = c.CancelRun(context.Background(), run.ID)
	t.Fatal("run Linear excedeu 250 segundos")

published:
	if run.SnapshotID == "" {
		t.Fatal("snapshot Linear ausente")
	}
	d, err := c.dataset(run.SnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	hasIssues := false
	for _, coverage := range d.Coverage {
		if coverage.Source == "linear" && coverage.ResourceID == plan.Resources[0].ID &&
			coverage.Collection == "issues" && coverage.Status != "unsupported" && coverage.Status != "unauthorized" && coverage.Status != "unavailable" {
			hasIssues = true
		}
	}
	if !hasIssues {
		t.Fatalf("issues Linear não foram observadas: %d entradas de cobertura", len(d.Coverage))
	}
	result, err := c.Query(context.Background(), contract.QueryRequest{
		SnapshotID: run.SnapshotID,
		SQL:        "SELECT COUNT(*) FROM v_issues WHERE source = 'linear'",
		MaxRows:    10,
	})
	if err != nil || len(result.Rows) != 1 || len(result.Rows[0]) != 1 || result.Rows[0][0].Kind != "integer" {
		t.Fatalf("SQL Linear falhou: rows=%d err=%v", len(result.Rows), err)
	}
	if pdf != nil {
		artifacts := pdf.List(run.SnapshotID)
		if len(artifacts) != 1 || artifacts[0].PageCount < 1 {
			t.Fatalf("PDF Linear ausente ou vazio: %d artefatos", len(artifacts))
		}
		file, err := pdf.Open(artifacts[0].ID)
		if err != nil {
			t.Fatal(err)
		}
		var signature [5]byte
		_, readErr := io.ReadFull(file, signature[:])
		closeErr := file.Close()
		if readErr != nil || closeErr != nil || string(signature[:]) != "%PDF-" {
			t.Fatalf("assinatura do PDF Linear inválida: read=%v close=%v", readErr, closeErr)
		}
	}
	t.Logf("smoke Linear: requests=%d bytes=%d issues=%s cobertura=%s PDF real=%t", run.Requests, run.Bytes, result.Rows[0][0].Value, run.Coverage, pdf != nil)
}
