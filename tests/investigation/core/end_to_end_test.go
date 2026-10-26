package core_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/core"
	"pocketcli/internal/investigation/report"
	"pocketcli/internal/investigation/storage"
)

// Opt-in offline product flow: typed import -> plan confirmation -> snapshot -> SQL -> real PDF.
func TestOfflineRuntimeToPDF(t *testing.T) {
	chromium := os.Getenv("POCKETCLI_CHROMIUM")
	if chromium == "" {
		t.Skip("defina POCKETCLI_CHROMIUM para validar o fluxo PDF")
	}
	root := t.TempDir()
	store, err := storage.Open(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	clock := func() time.Time { return time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC) }
	c, err := core.NewCore(core.CoreConfig{Store: store, Clock: clock})
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
}
