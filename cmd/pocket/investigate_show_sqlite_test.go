//go:build investigate_sqlite

package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/core"
)

// The terminal view shows what a real run extracted, read from the snapshot
// views, without the web UI.
func TestShowPrintsWhatTheRunCollected(t *testing.T) {
	c, err := core.NewCore(core.CoreConfig{Root: t.TempDir(), Clock: func() time.Time { return time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.StopActiveRuns(context.Background()) })
	c.SetFinalizer(&jsonRunFinalizer{core: c})
	input := contract.PlanInput{SchemaVersion: contract.Version, Name: "show", Modules: contract.Modules{RuntimeImport: true}, Period: contract.Period{Preset: "30d", Timezone: "UTC"}}
	first, err := c.PreparePlan(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.ImportRuntime(context.Background(), first.InvestigationID, []byte(`{"schema_version":"pocketcli.runtime.v1","observed_at":"2026-01-08T00:00:00Z","environment":"production","components":[{"name":"api","enabled":true,"revision_sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},{"name":"worker","enabled":false}]}`)); err != nil {
		t.Fatal(err)
	}
	input.InvestigationID = first.InvestigationID
	plan, err := c.PreparePlan(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	run, err := c.StartRun(context.Background(), contract.RunRequest{PlanID: plan.ID, ConfirmedPlanHash: plan.Hash, IdempotencyKey: "show"})
	if err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline) && run.SnapshotID == ""; time.Sleep(20 * time.Millisecond) {
		if run, err = c.GetRun(context.Background(), run.ID); err != nil {
			t.Fatal(err)
		}
	}
	if run.SnapshotID == "" {
		t.Fatalf("run sem snapshot: %+v", run)
	}
	snapshot, err := c.GetSnapshot(context.Background(), run.SnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	var summary bytes.Buffer
	if err := showSummary(context.Background(), &summary, palette{}, c, snapshot, plan.InvestigationID, showFlags{limit: 20}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Dados coletados", snapshot.ID, "2 componentes", "Cobertura", "show " + plan.InvestigationID} {
		if !strings.Contains(summary.String(), want) {
			t.Fatalf("resumo sem %q:\n%s", want, summary.String())
		}
	}
	var table bytes.Buffer
	if err := showQuery(context.Background(), &table, palette{}, c, snapshot, "SQL", `SELECT name, kind FROM v_components ORDER BY name`, showFlags{limit: 1}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"name", "api", "1 linhas", "cortado em 1"} {
		if !strings.Contains(table.String(), want) {
			t.Fatalf("tabela sem %q:\n%s", want, table.String())
		}
	}
	if strings.Contains(table.String(), "worker") {
		t.Fatalf("--limit 1 mostrou a segunda linha:\n%s", table.String())
	}
	rows := collectedRows(context.Background(), c, snapshot.ID, palette{})
	if len(rows) != 3 || rows[0][0] != "Coletado" || !strings.Contains(rows[0][1], "2 componentes") || rows[2][0] != "Cobertura" {
		t.Fatalf("linhas finais: %+v", rows)
	}
	if err := showQuery(context.Background(), &table, palette{}, c, snapshot, "SQL", `DELETE FROM v_components`, showFlags{limit: 5}); err == nil {
		t.Fatal("o terminal aceitou SQL que não é SELECT")
	}
}
