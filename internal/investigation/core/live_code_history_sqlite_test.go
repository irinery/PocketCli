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
	chromium := os.Getenv("POCKETCLI_CHROMIUM")
	var firstPDF *report.Finalizer
	if chromium == "" {
		c.SetFinalizer(&fakeFinalizer{core: c})
	} else {
		firstPDF, err = report.New(c, filepath.Join(store.Root, "reports"), chromium, false, nil, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		c.SetFinalizer(firstPDF)
	}
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
	waitPublished := func(core *Core, current contract.Run) contract.Run {
		deadline := time.Now().Add(250 * time.Second)
		for time.Now().Before(deadline) {
			current, err = core.GetRun(context.Background(), current.ID)
			if err != nil {
				t.Fatal(err)
			}
			switch current.State {
			case "completed", "completed_partial":
				return current
			case "failed", "paused", "report_failed", "cancelled":
				t.Fatalf("run não concluiu: estado=%s erro=%+v", current.State, current.LastError)
			}
			time.Sleep(100 * time.Millisecond)
		}
		_, _ = core.CancelRun(context.Background(), current.ID)
		t.Fatal("run excedeu 250 segundos")
		return contract.Run{}
	}
	run, err := c.StartRun(context.Background(), contract.RunRequest{PlanID: plan.ID, ConfirmedPlanHash: plan.Hash, IdempotencyKey: "code-history-live"})
	if err != nil {
		t.Fatal(err)
	}
	run = waitPublished(c, run)
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
	firstSnapshot, err := c.GetSnapshot(context.Background(), run.SnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	firstStats, err := c.RunStats(context.Background(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	secondCore, err := NewCore(CoreConfig{Store: store, Secrets: ss})
	if err != nil {
		t.Fatal(err)
	}
	var secondPDF *report.Finalizer
	if chromium == "" {
		secondCore.SetFinalizer(&fakeFinalizer{core: secondCore})
	} else {
		secondPDF, err = report.New(secondCore, filepath.Join(store.Root, "reports"), chromium, false, nil, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		secondCore.SetFinalizer(secondPDF)
	}
	input.InvestigationID = plan.InvestigationID
	secondPlan, err := secondCore.PreparePlan(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	secondRun, err := secondCore.StartRun(context.Background(), contract.RunRequest{PlanID: secondPlan.ID, ConfirmedPlanHash: secondPlan.Hash, IdempotencyKey: "code-history-live-refresh"})
	if err != nil {
		t.Fatal(err)
	}
	secondRun = waitPublished(secondCore, secondRun)
	secondStats, err := secondCore.RunStats(context.Background(), secondRun.ID)
	if err != nil {
		t.Fatal(err)
	}
	unchangedSnapshot, err := secondCore.GetSnapshot(context.Background(), run.SnapshotID)
	if err != nil || unchangedSnapshot.DatasetHash != firstSnapshot.DatasetHash {
		t.Fatalf("snapshot anterior mudou ou ficou indisponível: err=%v", err)
	}
	if secondStats.ReuseCounter <= firstStats.ReuseCounter {
		t.Fatalf("refresh não demonstrou reuso: %d -> %d", firstStats.ReuseCounter, secondStats.ReuseCounter)
	}
	if firstStats.GitRefBytesAdded <= 0 || secondStats.GitRefBytesAdded != 0 {
		t.Fatalf("ref padrão idêntica baixada outra vez ou contagem ausente: first=%d refresh=%d", firstStats.GitRefBytesAdded, secondStats.GitRefBytesAdded)
	}
	if firstPDF != nil {
		firstArtifacts := firstPDF.List(run.SnapshotID)
		secondArtifacts := secondPDF.List(secondRun.SnapshotID)
		retained := secondPDF.List(run.SnapshotID)
		if len(firstArtifacts) != 1 || len(secondArtifacts) != 1 || len(retained) != 1 || firstArtifacts[0].PageCount < 1 || secondArtifacts[0].PageCount < 1 || retained[0].SHA256 != firstArtifacts[0].SHA256 {
			t.Fatalf("PDF real ausente ou primeira revisão alterada: first=%+v refresh=%+v retained=%+v", firstArtifacts, secondArtifacts, retained)
		}
		file, err := secondPDF.Open(secondArtifacts[0].ID)
		if err != nil {
			t.Fatal(err)
		}
		var signature [5]byte
		_, readErr := io.ReadFull(file, signature[:])
		closeErr := file.Close()
		if readErr != nil || closeErr != nil || string(signature[:]) != "%PDF-" {
			t.Fatalf("PDF real inválido: signature=%q read=%v close=%v", signature, readErr, closeErr)
		}
		t.Logf("PDF real: primeira=%d páginas refresh=%d páginas achados=%d/%d", firstArtifacts[0].PageCount, secondArtifacts[0].PageCount, firstArtifacts[0].FindingCount, secondArtifacts[0].FindingCount)
	}
	t.Logf("primeira rodada: requests=%d bytes=%d registros=%d coberturas=%d git_ref_bytes=%d; refresh: requests=%d bytes=%d reuso=%d->%d git_ref_bytes=%d git_branch_bytes=%d git_tree_bytes=%d", run.Requests, run.Bytes, run.Items, len(d.Coverage), firstStats.GitRefBytesAdded, secondRun.Requests, secondRun.Bytes, firstStats.ReuseCounter, secondStats.ReuseCounter, secondStats.GitRefBytesAdded, secondStats.GitBranchBytesAdded, secondStats.GitTreeBytesAdded)
}
