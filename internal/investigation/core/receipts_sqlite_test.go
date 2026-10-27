//go:build investigate_sqlite && cgo

package core

import (
	"context"
	"net/http"
	"testing"
	"time"

	"pocketcli/internal/investigation/collect"
	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/secrets"
	"pocketcli/internal/investigation/storage"
)

func TestPlanAndRunHTTPRequestsHaveSeparateReceipts(t *testing.T) {
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
	plan, err := c.PreparePlan(context.Background(), contract.PlanInput{SchemaVersion: contract.Version, GitHubEnabled: true, Scope: &contract.Scope{Mode: "selected", Repositories: []string{"acme/api"}}, Period: contract.Period{Preset: "30d", Timezone: "UTC"}, Limits: contract.Limits{GitHubRPH: 3600000}})
	if err != nil {
		t.Fatal(err)
	}
	run, err := c.StartRun(context.Background(), contract.RunRequest{PlanID: plan.ID, ConfirmedPlanHash: plan.Hash, IdempotencyKey: "receipt-run"})
	if err != nil {
		t.Fatal(err)
	}
	run = waitRun(t, c, run.ID)
	if run.SnapshotID == "" {
		t.Fatalf("run sem snapshot: %+v", run)
	}
	ids, err := store.List("requestreceipts")
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, id := range ids {
		var receipt collect.RequestReceipt
		if err := store.Get("requestreceipts", id, &receipt); err != nil {
			t.Fatal(err)
		}
		counts[receipt.AuditID]++
	}
	if counts[plan.ID] == 0 || counts[run.ID] == 0 {
		t.Fatalf("recibos não vinculados a plano/run: %+v", counts)
	}
}
