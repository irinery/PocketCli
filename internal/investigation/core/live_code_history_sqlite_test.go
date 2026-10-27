//go:build investigate_sqlite && cgo

package core

import (
	"context"
	"os"
	"testing"
	"time"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/secrets"
	"pocketcli/internal/investigation/storage"
)

// Opt-in, read-only smoke against one explicitly selected GitHub repository.
// The private dataset and mirror are removed with the test's temporary directory.
func TestRealCodeHistoryToSnapshot(t *testing.T) {
	repo := os.Getenv("POCKETCLI_INVESTIGATE_REAL_GIT_REPO")
	if repo == "" {
		t.Skip("configure POCKETCLI_INVESTIGATE_REAL_GIT_REPO para smoke real")
	}
	token := os.Getenv("POCKETCLI_INVESTIGATE_GITHUB_TOKEN")
	if token == "" {
		t.Fatal("POCKETCLI_INVESTIGATE_GITHUB_TOKEN ausente")
	}
	ss := secrets.NewSessionStore()
	if _, err := ss.Put(context.Background(), contract.CredentialInput{Source: "github", Secret: token, Persistence: "session"}); err != nil {
		t.Fatal(err)
	}
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCore(CoreConfig{Store: store, Secrets: ss})
	if err != nil {
		t.Fatal(err)
	}
	c.SetFinalizer(&fakeFinalizer{core: c})
	now := time.Now().UTC()
	input := contract.PlanInput{
		SchemaVersion: contract.Version,
		Name:          "read-only code-history smoke",
		GitHubEnabled: true,
		Scope:         &contract.Scope{Mode: "selected", Repositories: []string{repo}},
		Period: contract.Period{Preset: "custom", FromDate: now.AddDate(0, 0, -1).Format("2006-01-02"),
			ToDate: now.Format("2006-01-02"), Timezone: "UTC"},
		Modules: contract.Modules{GitHubIssues: true, CodeHistory: true},
		Limits: contract.Limits{GitHubRPH: 3600, MaxSessionMS: 240000,
			MaxTransferBytes: 128 << 20, MaxLocalBytes: 256 << 20},
	}
	plan, err := c.PreparePlan(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Resources) != 1 || plan.Resources[0].Name != repo {
		t.Fatalf("escopo inesperado: %d recursos", len(plan.Resources))
	}
	run, err := c.StartRun(context.Background(), contract.RunRequest{PlanID: plan.ID, ConfirmedPlanHash: plan.Hash, IdempotencyKey: "code-history-live"})
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
			goto completed
		case "failed", "paused", "report_failed", "cancelled":
			t.Fatalf("run não concluiu: estado=%s erro=%+v", run.State, run.LastError)
		}
		time.Sleep(100 * time.Millisecond)
	}
	_, _ = c.CancelRun(context.Background(), run.ID)
	t.Fatal("run excedeu 250 segundos")

completed:
	if run.SnapshotID == "" {
		t.Fatal("snapshot ausente")
	}
	d, err := c.dataset(run.SnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	hasRef, hasIssues, hasFormalLinks := false, false, false
	for _, coverage := range d.Coverage {
		if coverage.Source == "git" && coverage.Collection == "git_refs" && coverage.Status != "unsupported" {
			hasRef = true
		}
		if coverage.Source == "github" && coverage.Collection == "issues" && coverage.Status != "unsupported" {
			hasIssues = true
		}
		if coverage.Source == "github" && coverage.Collection == "formal_links" && coverage.Status != "unsupported" {
			hasFormalLinks = true
		}
	}
	if !hasRef || !hasIssues || !hasFormalLinks {
		t.Fatalf("histórico Git, issues ou vínculos formais não observados: ref=%t issues=%t formal_links=%t coberturas=%d", hasRef, hasIssues, hasFormalLinks, len(d.Coverage))
	}
	result, err := c.Query(context.Background(), contract.QueryRequest{SnapshotID: run.SnapshotID, SQL: "SELECT COUNT(*) FROM v_coverage WHERE source = 'git'", MaxRows: 10})
	if err != nil || len(result.Rows) != 1 || len(result.Rows[0]) != 1 || result.Rows[0][0].Kind != "integer" {
		t.Fatalf("consulta do snapshot falhou: %+v %v", result, err)
	}
	t.Logf("snapshot publicado: requests=%d bytes=%d registros=%d coberturas=%d", run.Requests, run.Bytes, run.Items, len(d.Coverage))
}
