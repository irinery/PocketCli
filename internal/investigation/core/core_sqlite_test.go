//go:build investigate_sqlite && cgo

package core

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/gitmirror"
	"pocketcli/internal/investigation/query"
	"pocketcli/internal/investigation/rules"
	"pocketcli/internal/investigation/secrets"
	"pocketcli/internal/investigation/storage"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type fakeTransport struct {
	mu       sync.Mutex
	title    string
	requests int
}
type fakeFinalizer struct {
	core  *Core
	calls atomic.Int32
}

type fakeGitObserver struct {
	priors            []gitmirror.RefState
	result            gitmirror.Observation
	commits           []gitmirror.Commit
	commitsByHead     map[string][]gitmirror.Commit
	changes           map[string][]gitmirror.ChangedFile
	trees             map[string][]gitmirror.TreeEntry
	heads             []gitmirror.HeadRef
	headsIncomplete   bool
	headListCalls     int
	branchFetches     int
	branchErrors      map[string]error
	revertTargets     map[string]string
	revertUninspected map[string]bool
	ancestorResults   map[string]bool
}

func (f *fakeGitObserver) IsAncestor(_ context.Context, ancestor, descendant string) (bool, bool, error) {
	result, known := f.ancestorResults[ancestor+":"+descendant]
	return result, known, nil
}

func (f *fakeGitObserver) Observe(_ context.Context, _ string, _ []byte, prior gitmirror.RefState, _ int64) (gitmirror.Observation, error) {
	f.priors = append(f.priors, prior)
	f.result.OldOID = prior.OID
	return f.result, nil
}

func (f *fakeGitObserver) ObserveHead(_ context.Context, _ string, _ []byte, head gitmirror.HeadRef, prior gitmirror.RefState, _ int64) (gitmirror.Observation, error) {
	f.branchFetches++
	if err := f.branchErrors[head.Name]; err != nil {
		return gitmirror.Observation{}, err
	}
	f.priors = append(f.priors, prior)
	result := f.result
	result.Ref, result.OldOID, result.NewOID = head.Name, prior.OID, head.OID
	if prior.OID == "" {
		result.Relationship = "initial"
	} else if prior.OID == head.OID {
		result.Relationship = "unchanged"
	} else {
		result.Relationship = "advanced"
	}
	return result, nil
}

func (f *fakeGitObserver) WalkCommits(_ context.Context, head string, visit func(gitmirror.Commit) (bool, error)) (bool, error) {
	commits := f.commits
	if f.commitsByHead != nil {
		commits = f.commitsByHead[head]
	}
	for _, commit := range commits {
		proceed, err := visit(commit)
		if err != nil || !proceed {
			return false, err
		}
	}
	return true, nil
}

func (f *fakeGitObserver) DeclaredRevertTarget(_ context.Context, oid string) (string, bool, error) {
	if f.revertUninspected[oid] {
		return "", false, nil
	}
	return f.revertTargets[oid], true, nil
}

func (f *fakeGitObserver) EnsureTrees(context.Context, string, []byte, string, string, int64) (int64, error) {
	return 0, nil
}

func (f *fakeGitObserver) ChangedFiles(_ context.Context, oid string, _ int) ([]gitmirror.ChangedFile, bool, error) {
	return f.changes[oid], true, nil
}

func (f *fakeGitObserver) WalkTree(_ context.Context, oid string, visit func(gitmirror.TreeEntry) (bool, error)) (bool, error) {
	for _, entry := range f.trees[oid] {
		proceed, err := visit(entry)
		if err != nil || !proceed {
			return false, err
		}
	}
	return true, nil
}

func (f *fakeGitObserver) ListHeads(context.Context, string, []byte, int, int64) ([]gitmirror.HeadRef, bool, int64, error) {
	f.headListCalls++
	return append([]gitmirror.HeadRef(nil), f.heads...), !f.headsIncomplete, 0, nil
}

func (f *fakeFinalizer) Finalize(ctx context.Context, in contract.FinalizeRequest) (contract.ReportArtifact, error) {
	f.calls.Add(1)
	count := int64(0)
	for cursor := ""; ; {
		page, err := f.core.ReadReportPage(ctx, in.SnapshotID, cursor, 1000)
		if err != nil {
			return contract.ReportArtifact{}, err
		}
		for _, r := range page.Records {
			if r.Kind == "finding" {
				count++
			}
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	return contract.ReportArtifact{SnapshotID: in.SnapshotID, Status: "completed", FindingCount: count}, nil
}

func (f *fakeTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests++
	if r.Header.Get("Authorization") != "Bearer test-secret" {
		return nil, fmt.Errorf("missing credential")
	}
	body := "[]"
	switch r.URL.Path {
	case "/repos/acme/api":
		body = `{"id":42,"full_name":"acme/api"}`
	case "/repos/acme/api/pulls":
		body = fmt.Sprintf(`[{"id":100,"number":7,"title":%q,"state":"open","created_at":"2026-01-02T00:00:00Z","updated_at":"2026-01-03T00:00:00Z","head":{"sha":"abc"}}]`, f.title)
	case "/repos/acme/api/issues":
		body = `[]`
	}
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
}

func waitRun(t *testing.T, c *Core, id string) contract.Run {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		r, e := c.GetRun(context.Background(), id)
		if e != nil {
			t.Fatal(e)
		}
		if r.State == "report_failed" || r.State == "failed" || r.State == "completed" || r.State == "completed_partial" {
			return r
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("run timed out")
	return contract.Run{}
}

func TestRunSnapshotAndVersionedObservation(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	secrets := secrets.NewSessionStore()
	if _, err := secrets.Put(context.Background(), contract.CredentialInput{Source: "github", Secret: "test-secret", Persistence: "session"}); err != nil {
		t.Fatal(err)
	}
	transport := &fakeTransport{title: "Original PR"}
	c, err := NewCore(CoreConfig{Store: store, Secrets: secrets, HTTPClient: &http.Client{Transport: transport}, Clock: func() time.Time { return time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	input := contract.PlanInput{SchemaVersion: contract.Version, Name: "test", GitHubEnabled: true, Scope: &contract.Scope{Mode: "selected", Repositories: []string{"acme/api"}}, Period: contract.Period{Preset: "30d", Timezone: "UTC"}, Modules: contract.Modules{GitHubIssues: true}, Limits: contract.Limits{GitHubRPH: 3600000}}
	plan, err := c.PreparePlan(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	run, err := c.StartRun(context.Background(), contract.RunRequest{PlanID: plan.ID, ConfirmedPlanHash: plan.Hash, IdempotencyKey: "one"})
	if err != nil {
		t.Fatal(err)
	}
	again, err := c.StartRun(context.Background(), contract.RunRequest{PlanID: plan.ID, ConfirmedPlanHash: plan.Hash, IdempotencyKey: "one"})
	if err != nil || again.ID != run.ID {
		t.Fatalf("idempotency run=%s again=%+v err=%v", run.ID, again, err)
	}
	run = waitRun(t, c, run.ID)
	if run.State != "report_failed" || run.SnapshotID == "" {
		t.Fatalf("run=%+v", run)
	}
	snapshot, err := c.GetSnapshot(context.Background(), run.SnapshotID)
	if err != nil || snapshot.RulesetVersion != rules.Version || snapshot.MappingVersion != "1.17.0" {
		t.Fatalf("published versions do not match executed rules and mappings: snapshot=%+v err=%v", snapshot, err)
	}
	if snapshot.WindowStart != plan.WindowStart || snapshot.WindowEnd != plan.WindowEnd || snapshot.AnalysisReferenceAt != plan.WindowEnd {
		t.Fatalf("published window differs from confirmed plan: snapshot=%+v plan=%+v", snapshot, plan)
	}
	windowRow, err := c.Query(context.Background(), contract.QueryRequest{SnapshotID: run.SnapshotID, SQL: "SELECT window_start, window_end FROM v_snapshot"})
	if err != nil || len(windowRow.Rows) != 1 || len(windowRow.Rows[0]) != 2 || windowRow.Rows[0][0].Value != plan.WindowStart || windowRow.Rows[0][1].Value != plan.WindowEnd {
		t.Fatalf("SQL window differs from confirmed plan: result=%+v err=%v", windowRow, err)
	}
	result, err := c.Query(context.Background(), contract.QueryRequest{SnapshotID: run.SnapshotID, SQL: "SELECT COUNT(*) FROM v_pull_requests"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Rows[0][0].Value != "1" {
		t.Fatalf("result=%+v", result)
	}
	first := run.SnapshotID
	transport.mu.Lock()
	transport.title = "Edited without rebase"
	transport.mu.Unlock()
	input.InvestigationID = plan.InvestigationID
	plan2, err := c.PreparePlan(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	run2, err := c.StartRun(context.Background(), contract.RunRequest{PlanID: plan2.ID, ConfirmedPlanHash: plan2.Hash, IdempotencyKey: "two"})
	if err != nil {
		t.Fatal(err)
	}
	run2 = waitRun(t, c, run2.ID)
	if run2.SnapshotID == first {
		t.Fatal("refresh overwrote snapshot")
	}
	old, err := c.Query(context.Background(), contract.QueryRequest{SnapshotID: first, SQL: "SELECT title FROM v_pull_requests"})
	if err != nil {
		t.Fatal(err)
	}
	if old.Rows[0][0].Value != "Original PR" {
		t.Fatalf("old=%+v", old)
	}
	newResult, err := c.Query(context.Background(), contract.QueryRequest{SnapshotID: run2.SnapshotID, SQL: "SELECT title FROM v_pull_requests"})
	if err != nil {
		t.Fatal(err)
	}
	if newResult.Rows[0][0].Value != "Edited without rebase" {
		t.Fatalf("new=%+v", newResult)
	}
	objects, err := store.List("objects")
	if err != nil {
		t.Fatal(err)
	}
	if len(objects) != 2 {
		t.Fatalf("object versions=%d", len(objects))
	}
}

func TestUnchangedRefreshReusesObjectsAcrossCoreRestart(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	secretStore := secrets.NewSessionStore()
	if _, err := secretStore.Put(context.Background(), contract.CredentialInput{Source: "github", Secret: "test-secret", Persistence: "session"}); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var objectDownloads atomic.Int32
	transport := transportFunc(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer test-secret" {
			return nil, fmt.Errorf("credencial ausente")
		}
		if r.Header.Get("If-None-Match") == `"stable"` {
			return &http.Response{StatusCode: http.StatusNotModified, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
		}
		body := "[]"
		switch r.URL.Path {
		case "/repos/acme/api":
			body = `{"id":42,"full_name":"acme/api"}`
		case "/repos/acme/api/pulls":
			body = `[{"id":100,"number":7,"title":"PR estável","state":"open","created_at":"2026-01-02T00:00:00Z","updated_at":"2026-01-03T00:00:00Z","head":{"sha":"abc"}}]`
			objectDownloads.Add(1)
		}
		res := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
		res.Header.Set("ETag", `"stable"`)
		return res, nil
	})
	now := func() time.Time { return time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC) }
	newCore := func() *Core {
		c, err := NewCore(CoreConfig{Store: store, Secrets: secretStore, HTTPClient: &http.Client{Transport: transport}, Clock: now})
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	input := contract.PlanInput{SchemaVersion: contract.Version, Name: "refresh sem mudança", GitHubEnabled: true,
		Scope:  &contract.Scope{Mode: "selected", Repositories: []string{"acme/api"}},
		Period: contract.Period{Preset: "30d", Timezone: "UTC"}, Modules: contract.Modules{GitHubIssues: true},
		Limits: contract.Limits{GitHubRPH: 3600000}}
	firstCore := newCore()
	firstPlan, err := firstCore.PreparePlan(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	firstRun, err := firstCore.StartRun(context.Background(), contract.RunRequest{PlanID: firstPlan.ID, ConfirmedPlanHash: firstPlan.Hash, IdempotencyKey: "first"})
	if err != nil {
		t.Fatal(err)
	}
	firstRun = waitRun(t, firstCore, firstRun.ID)
	if firstRun.SnapshotID == "" || firstRun.State != "report_failed" || objectDownloads.Load() != 1 {
		t.Fatalf("primeira rodada: %+v downloads=%d", firstRun, objectDownloads.Load())
	}
	firstStats, err := firstCore.RunStats(context.Background(), firstRun.ID)
	if err != nil {
		t.Fatal(err)
	}
	beforeObjects, err := store.List("objects")
	if err != nil {
		t.Fatal(err)
	}

	secondCore := newCore() // Simulates a separate pocket invocation.
	input.InvestigationID = firstPlan.InvestigationID
	secondPlan, err := secondCore.PreparePlan(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	secondRun, err := secondCore.StartRun(context.Background(), contract.RunRequest{PlanID: secondPlan.ID, ConfirmedPlanHash: secondPlan.Hash, IdempotencyKey: "second"})
	if err != nil {
		t.Fatal(err)
	}
	secondRun = waitRun(t, secondCore, secondRun.ID)
	secondStats, err := secondCore.RunStats(context.Background(), secondRun.ID)
	if err != nil {
		t.Fatal(err)
	}
	afterObjects, err := store.List("objects")
	if err != nil {
		t.Fatal(err)
	}
	if secondRun.SnapshotID == "" || secondStats.ReuseCounter <= firstStats.ReuseCounter || objectDownloads.Load() != 1 || len(afterObjects) != len(beforeObjects) {
		t.Fatalf("refresh sem reuso: run=%+v stats=%+v->%+v downloads=%d objects=%d->%d", secondRun, firstStats, secondStats, objectDownloads.Load(), len(beforeObjects), len(afterObjects))
	}
	if _, err := secondCore.GetSnapshot(context.Background(), firstRun.SnapshotID); err != nil {
		t.Fatalf("snapshot anterior indisponível: %v", err)
	}
}

func TestRunKeyWithoutRunRecoversSameID(t *testing.T) {
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
	req := contract.RunRequest{PlanID: plan.ID, ConfirmedPlanHash: plan.Hash, IdempotencyKey: "interrupted-before-run-write"}
	keyHash := storage.Hash([]byte(req.IdempotencyKey))
	if err := store.Put("runkeys", keyHash, runKey{RunID: "recovered-run", PlanID: plan.ID, Hash: plan.Hash}); err != nil {
		t.Fatal(err)
	}
	run, err := c.StartRun(context.Background(), req)
	if err != nil || run.ID != "recovered-run" {
		t.Fatalf("run=%+v err=%v", run, err)
	}
	again, err := c.StartRun(context.Background(), req)
	if err != nil || again.ID != run.ID {
		t.Fatalf("retry=%+v err=%v", again, err)
	}
	run = waitRun(t, c, run.ID)
	if run.State != "report_failed" || run.SnapshotID == "" {
		t.Fatalf("run=%+v", run)
	}
	stored, err := store.List("runs")
	if err != nil || len(stored) != 1 || stored[0] != run.ID {
		t.Fatalf("runs=%v err=%v", stored, err)
	}
}

func TestStartRunRestoresRequestPacingFromPreparedPlan(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	secretStore := secrets.NewSessionStore()
	if _, err := secretStore.Put(context.Background(), contract.CredentialInput{Source: "github", Secret: "test-secret", Persistence: "session"}); err != nil {
		t.Fatal(err)
	}
	base := &fakeTransport{title: "PR"}
	var mu sync.Mutex
	var pullsAt, issuesAt time.Time
	transport := transportFunc(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		switch r.URL.Path {
		case "/repos/acme/api/pulls":
			if pullsAt.IsZero() {
				pullsAt = time.Now()
			}
		case "/repos/acme/api/issues":
			if issuesAt.IsZero() {
				issuesAt = time.Now()
			}
		}
		mu.Unlock()
		return base.RoundTrip(r)
	})
	config := CoreConfig{Store: store, Secrets: secretStore, HTTPClient: &http.Client{Transport: transport}, Clock: func() time.Time { return time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC) }}
	preparer, err := NewCore(config)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := preparer.PreparePlan(context.Background(), contract.PlanInput{SchemaVersion: contract.Version, GitHubEnabled: true, Scope: &contract.Scope{Mode: "selected", Repositories: []string{"acme/api"}}, Period: contract.Period{Preset: "30d", Timezone: "UTC"}, Modules: contract.Modules{GitHubIssues: true}, Limits: contract.Limits{GitHubRPH: 36000, MaxSessionMS: 10000}})
	if err != nil {
		t.Fatal(err)
	}
	// Start in a new Core, as the separate CLI invocation does.
	runner, err := NewCore(config)
	if err != nil {
		t.Fatal(err)
	}
	run, err := runner.StartRun(context.Background(), contract.RunRequest{PlanID: plan.ID, ConfirmedPlanHash: plan.Hash, IdempotencyKey: "pacing-after-restart"})
	if err != nil {
		t.Fatal(err)
	}
	waitRun(t, runner, run.ID)
	mu.Lock()
	delay := issuesAt.Sub(pullsAt)
	missing := pullsAt.IsZero() || issuesAt.IsZero()
	mu.Unlock()
	if missing || delay < 80*time.Millisecond {
		t.Fatalf("limite de 100ms não aplicado: pulls=%v issues=%v delay=%v", pullsAt, issuesAt, delay)
	}
}

func TestRecoverPublishedSnapshotBeforePDF(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	plan := contract.Plan{ID: "plan-recovery", InvestigationID: "investigation-recovery"}
	if err := store.Put("plans", plan.ID, plan); err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"analysis_ready", "reporting"} {
		run := contract.Run{ID: "run-" + state, PlanID: plan.ID, State: state, SnapshotID: "snap-immutable"}
		if err := store.Put("runs", run.ID, run); err != nil {
			t.Fatal(err)
		}
	}
	c, err := NewCore(CoreConfig{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"analysis_ready", "reporting"} {
		run, err := c.GetRun(context.Background(), "run-"+state)
		if err != nil || run.State != "report_failed" || run.SnapshotID != "snap-immutable" || run.LastError == nil {
			t.Fatalf("estado %s não recuperado: %+v %v", state, run, err)
		}
	}
}

func TestResumeAfterSnapshotPublicationKeepsSameSnapshot(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ss := secrets.NewSessionStore()
	if _, err := ss.Put(context.Background(), contract.CredentialInput{Source: "github", Secret: "test-secret", Persistence: "session"}); err != nil {
		t.Fatal(err)
	}
	transport := &fakeTransport{title: "PR"}
	config := CoreConfig{Store: store, Secrets: ss, HTTPClient: &http.Client{Transport: transport}, Clock: func() time.Time { return time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC) }}
	firstCore, err := NewCore(config)
	if err != nil {
		t.Fatal(err)
	}
	firstCore.SetFinalizer(&fakeFinalizer{core: firstCore})
	plan, err := firstCore.PreparePlan(context.Background(), contract.PlanInput{SchemaVersion: contract.Version, GitHubEnabled: true, Scope: &contract.Scope{Mode: "selected", Repositories: []string{"acme/api"}}, Period: contract.Period{Preset: "30d", Timezone: "UTC"}, Limits: contract.Limits{GitHubRPH: 3600000}})
	if err != nil {
		t.Fatal(err)
	}
	run, err := firstCore.StartRun(context.Background(), contract.RunRequest{PlanID: plan.ID, ConfirmedPlanHash: plan.Hash, IdempotencyKey: "publication-interrupted"})
	if err != nil {
		t.Fatal(err)
	}
	run = waitRun(t, firstCore, run.ID)
	if run.State != "completed_partial" || run.SnapshotID == "" {
		t.Fatalf("primeiro run=%+v", run)
	}
	beforeID := run.SnapshotID
	transport.mu.Lock()
	beforeRequests := transport.requests
	transport.mu.Unlock()
	// Simulates the recovered state after publication and before the run's
	// analysis_ready write. The snapshot and projection already exist.
	run.State, run.Stage, run.SnapshotID = "paused", "building", ""
	if err := store.Put("runs", run.ID, run); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewCore(config)
	if err != nil {
		t.Fatal(err)
	}
	restarted.SetFinalizer(&fakeFinalizer{core: restarted})
	if _, err := restarted.ResumeRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	resumed := waitRun(t, restarted, run.ID)
	transport.mu.Lock()
	afterRequests := transport.requests
	transport.mu.Unlock()
	snapshots, err := store.List("snapshots")
	if err != nil || resumed.State != "completed_partial" || resumed.SnapshotID != beforeID || afterRequests != beforeRequests || len(snapshots) != 1 {
		t.Fatalf("retomada duplicou publicação: run=%+v requests=%d->%d snapshots=%v err=%v", resumed, beforeRequests, afterRequests, snapshots, err)
	}
	var published contract.Snapshot
	if err := store.Get("snapshots", beforeID, &published); err != nil {
		t.Fatal(err)
	}
	if err := restarted.query.VerifyPublished(beforeID, published.DatasetHash); err != nil {
		t.Fatalf("projeção publicada inválida: %v", err)
	}
	if err := restarted.query.VerifyPublished(beforeID, strings.Repeat("0", 64)); err == nil {
		t.Fatal("projeção aceitou hash de dataset divergente")
	}
	if _, err := restarted.Query(context.Background(), contract.QueryRequest{SnapshotID: beforeID, SQL: "SELECT * FROM projection_meta"}); err == nil {
		t.Fatal("metadados internos da projeção ficaram acessíveis por SQL público")
	}
	for _, kind := range []string{"datasets", "snapshots"} {
		path, err := store.Path(kind, beforeID)
		if err != nil {
			t.Fatalf("remoção da fixture %s: %v", kind, err)
		}
		if err := os.Remove(path); err != nil {
			t.Fatalf("remoção da fixture %s: %v", kind, err)
		}
	}
	if err := os.Remove(filepath.Join(store.Root, "projection", beforeID+".sqlite")); err != nil {
		t.Fatal(err)
	}
	resumed.State, resumed.Stage, resumed.SnapshotID = "paused", "building", ""
	if err := store.Put("runs", resumed.ID, resumed); err != nil {
		t.Fatal(err)
	}
	restartedAgain, err := NewCore(config)
	if err != nil {
		t.Fatal(err)
	}
	restartedAgain.SetFinalizer(&fakeFinalizer{core: restartedAgain})
	if _, err := restartedAgain.ResumeRun(context.Background(), resumed.ID); err != nil {
		t.Fatal(err)
	}
	republished := waitRun(t, restartedAgain, resumed.ID)
	transport.mu.Lock()
	afterRepublishRequests := transport.requests
	transport.mu.Unlock()
	if republished.State != "completed_partial" || republished.SnapshotID != beforeID || afterRepublishRequests != beforeRequests {
		t.Fatalf("build persistido não republicado: run=%+v requests=%d->%d", republished, beforeRequests, afterRepublishRequests)
	}
	if err := store.Put("datasets", beforeID, query.Dataset{}); err != nil {
		t.Fatal(err)
	}
	republished.State, republished.Stage, republished.SnapshotID = "paused", "building", ""
	if err := store.Put("runs", republished.ID, republished); err != nil {
		t.Fatal(err)
	}
	corrupted, err := NewCore(config)
	if err != nil {
		t.Fatal(err)
	}
	corrupted.SetFinalizer(&fakeFinalizer{core: corrupted})
	if _, err := corrupted.ResumeRun(context.Background(), republished.ID); err != nil {
		t.Fatal(err)
	}
	rejected := waitRun(t, corrupted, republished.ID)
	if rejected.State != "failed" || rejected.LastError == nil || rejected.LastError.Code != "ERR_AUDIT_FAILED" {
		t.Fatalf("dataset divergente não interrompeu retomada: %+v", rejected)
	}
}

func TestFinalizerTransitionsRunToCompletedPartialWithIncompleteLinks(t *testing.T) {
	store, _ := storage.Open(t.TempDir())
	secretStore := secrets.NewSessionStore()
	secretStore.Put(context.Background(), contract.CredentialInput{Source: "github", Secret: "test-secret", Persistence: "session"})
	c, err := NewCore(CoreConfig{Store: store, Secrets: secretStore, HTTPClient: &http.Client{Transport: &fakeTransport{title: "PR"}}, Clock: func() time.Time { return time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	finalizer := &fakeFinalizer{core: c}
	c.SetFinalizer(finalizer)
	plan, err := c.PreparePlan(context.Background(), contract.PlanInput{SchemaVersion: contract.Version, GitHubEnabled: true, Scope: &contract.Scope{Mode: "selected", Repositories: []string{"acme/api"}}, Period: contract.Period{Preset: "30d", Timezone: "UTC"}, Limits: contract.Limits{GitHubRPH: 3600000}})
	if err != nil {
		t.Fatal(err)
	}
	run, err := c.StartRun(context.Background(), contract.RunRequest{PlanID: plan.ID, ConfirmedPlanHash: plan.Hash, IdempotencyKey: "finalize"})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		run, _ = c.GetRun(context.Background(), run.ID)
		if run.State == "completed" || run.State == "completed_partial" || run.State == "failed" || run.State == "report_failed" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if run.State != "completed_partial" || finalizer.calls.Load() != 1 {
		t.Fatalf("run=%+v finalizer calls=%d", run, finalizer.calls.Load())
	}
}

func TestReportRetryClosesPartialRunWithoutRecollecting(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	secretStore := secrets.NewSessionStore()
	if _, err := secretStore.Put(context.Background(), contract.CredentialInput{Source: "github", Secret: "test-secret", Persistence: "session"}); err != nil {
		t.Fatal(err)
	}
	transport := &fakeTransport{title: "PR"}
	c, err := NewCore(CoreConfig{Store: store, Secrets: secretStore, HTTPClient: &http.Client{Transport: transport}, Clock: func() time.Time { return time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := c.PreparePlan(context.Background(), contract.PlanInput{SchemaVersion: contract.Version, GitHubEnabled: true, Scope: &contract.Scope{Mode: "selected", Repositories: []string{"acme/api"}}, Period: contract.Period{Preset: "30d", Timezone: "UTC"}, Limits: contract.Limits{GitHubRPH: 3600000}})
	if err != nil {
		t.Fatal(err)
	}
	run, err := c.StartRun(context.Background(), contract.RunRequest{PlanID: plan.ID, ConfirmedPlanHash: plan.Hash, IdempotencyKey: "retry-report"})
	if err != nil {
		t.Fatal(err)
	}
	run = waitRun(t, c, run.ID)
	if run.State != "report_failed" || run.SnapshotID == "" {
		t.Fatalf("expected published snapshot and failed report: %+v", run)
	}
	var d query.Dataset
	if err := store.Get("datasets", run.SnapshotID, &d); err != nil {
		t.Fatal(err)
	}
	before := transport.requests
	artifact := contract.ReportArtifact{ID: "report-retry", SnapshotID: run.SnapshotID, Status: "completed", FindingCount: int64(len(d.Findings))}
	bad := artifact
	bad.FindingCount++
	if err := c.CompleteReport(context.Background(), bad); err == nil {
		t.Fatal("incomplete PDF accepted")
	}
	if got, _ := c.GetRun(context.Background(), run.ID); got.State != "report_failed" {
		t.Fatalf("failed retry changed run: %+v", got)
	}
	deadline := time.Now().Add(time.Second)
	for {
		err = c.CompleteReport(context.Background(), artifact)
		if err == nil || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.GetRun(context.Background(), run.ID)
	if err != nil || got.State != "completed_partial" || got.LastError != nil || got.SnapshotID != run.SnapshotID || transport.requests != before {
		t.Fatalf("retry recollected or did not close run: %+v requests=%d before=%d err=%v", got, transport.requests, before, err)
	}
	if err := c.CompleteReport(context.Background(), artifact); err != nil {
		t.Fatalf("retry is not idempotent: %v", err)
	}
}

func TestCancelDuringCollectionDoesNotPublishSnapshot(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ss := secrets.NewSessionStore()
	ss.Put(context.Background(), contract.CredentialInput{Source: "github", Secret: "test-secret", Persistence: "session"})
	entered := make(chan struct{})
	transport := transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/repos/acme/api" {
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"id":42,"full_name":"acme/api"}`))}, nil
		}
		if r.URL.Path == "/repos/acme/api/pulls" {
			close(entered)
			<-r.Context().Done()
			return nil, r.Context().Err()
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`[]`))}, nil
	})
	c, err := NewCore(CoreConfig{Store: store, Secrets: ss, HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := c.PreparePlan(context.Background(), contract.PlanInput{SchemaVersion: contract.Version, GitHubEnabled: true, Scope: &contract.Scope{Mode: "selected", Repositories: []string{"acme/api"}}, Period: contract.Period{Preset: "30d", Timezone: "UTC"}, Limits: contract.Limits{GitHubRPH: 3600000}})
	if err != nil {
		t.Fatal(err)
	}
	run, err := c.StartRun(context.Background(), contract.RunRequest{PlanID: plan.ID, ConfirmedPlanHash: plan.Hash, IdempotencyKey: "cancel-collection"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("collector did not begin")
	}
	if _, err := c.CancelRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		run, err = c.GetRun(context.Background(), run.ID)
		if err != nil {
			t.Fatal(err)
		}
		if run.State == "cancelled" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if run.State != "cancelled" || run.SnapshotID != "" {
		t.Fatalf("cancelled run=%+v", run)
	}
	for time.Now().Before(deadline) {
		c.mu.Lock()
		_, running := c.runs[run.ID]
		c.mu.Unlock()
		if !running {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	c.mu.Lock()
	_, running := c.runs[run.ID]
	c.mu.Unlock()
	if running {
		t.Fatal("cancelled worker did not stop")
	}
	snapshots, err := c.ListSnapshots(context.Background(), plan.InvestigationID)
	if err != nil || len(snapshots) != 0 {
		t.Fatalf("snapshots=%+v err=%v", snapshots, err)
	}
}

func TestCompletedRunCannotBeCancelled(t *testing.T) {
	store, _ := storage.Open(t.TempDir())
	c, err := NewCore(CoreConfig{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put("runs", "done-run", contract.Run{ID: "done-run", State: "completed"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CancelRun(context.Background(), "done-run"); err == nil || !strings.Contains(err.Error(), "ERR_VALIDATION") {
		t.Fatalf("cancel err=%v", err)
	}
	run, _ := c.GetRun(context.Background(), "done-run")
	if run.State != "completed" {
		t.Fatalf("completed run mutated: %+v", run)
	}
}

func TestPeriodAndConfirmationBlockCollection(t *testing.T) {
	store, _ := storage.Open(t.TempDir())
	secretStore := secrets.NewSessionStore()
	secretStore.Put(context.Background(), contract.CredentialInput{Source: "github", Secret: "test-secret", Persistence: "session"})
	transport := &fakeTransport{title: "PR"}
	c, err := NewCore(CoreConfig{Store: store, Secrets: secretStore, HTTPClient: &http.Client{Transport: transport}, Clock: func() time.Time { return time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	input := contract.PlanInput{SchemaVersion: contract.Version, GitHubEnabled: true, Scope: &contract.Scope{Mode: "selected", Repositories: []string{"acme/api"}}}
	if _, err := c.PreparePlan(context.Background(), input); err == nil || !strings.Contains(err.Error(), "ERR_PERIOD_REQUIRED") {
		t.Fatalf("period err=%v", err)
	}
	transport.mu.Lock()
	calls := transport.requests
	transport.mu.Unlock()
	if calls != 0 {
		t.Fatalf("network before period: %d", calls)
	}
	input.Period = contract.Period{Preset: "30d", Timezone: "UTC"}
	input.Limits.GitHubRPH = 3600000
	plan, err := c.PreparePlan(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.StartRun(context.Background(), contract.RunRequest{PlanID: plan.ID, ConfirmedPlanHash: "old-hash", IdempotencyKey: "wrong"}); err == nil || !strings.Contains(err.Error(), "ERR_PLAN_CHANGED") {
		t.Fatalf("hash err=%v", err)
	}
	transport.mu.Lock()
	after := transport.requests
	transport.mu.Unlock()
	if after != 1 {
		t.Fatalf("collection began before confirmation: %d", after)
	}
}

func TestGitHubJournalResumesFromCommittedPage(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	secretStore := secrets.NewSessionStore()
	secretStore.Put(context.Background(), contract.CredentialInput{Source: "github", Secret: "test-secret", Persistence: "session"})
	pageOne, pageTwo := 0, 0
	transport := transportFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.URL.Path == "/repos/acme/api/pulls" && r.URL.Query().Get("page") == "":
			pageOne++
			res := &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`[{"id":1,"number":1,"title":"first","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-02T00:00:00Z"}]`))}
			res.Header.Set("Link", `<https://api.github.com/repos/acme/api/pulls?page=2>; rel="next"`)
			return res, nil
		case r.URL.Path == "/repos/acme/api/pulls" && r.URL.Query().Get("page") == "2":
			pageTwo++
			if pageTwo == 1 {
				return &http.Response{StatusCode: 403, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"message":"denied"}`))}, nil
			}
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`[{"id":2,"number":2,"title":"second","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-03T00:00:00Z"}]`))}, nil
		case r.URL.Path == "/repos/acme/api/issues/comments":
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`[]`))}, nil
		case r.URL.Path == "/repos/acme/api/pulls/comments":
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`[]`))}, nil
		case r.URL.Path == "/repos/acme/api/pulls/1/reviews" || r.URL.Path == "/repos/acme/api/pulls/1/timeline" || r.URL.Path == "/repos/acme/api/issues/1/timeline" || r.URL.Path == "/repos/acme/api/pulls/2/reviews" || r.URL.Path == "/repos/acme/api/issues/2/timeline":
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`[]`))}, nil
		}
		return nil, fmt.Errorf("unexpected endpoint %s", r.URL.String())
	})
	c, err := NewCore(CoreConfig{Store: store, Secrets: secretStore, HTTPClient: &http.Client{Transport: transport}, Clock: func() time.Time { return time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	plan := contract.Plan{ID: "plan-journal", WindowStart: "2026-01-01T00:00:00Z", WindowEnd: "2026-01-10T00:00:00Z", Input: contract.PlanInput{Limits: contract.Limits{MaxTransferBytes: 1 << 20}}}
	resource := contract.Resource{Source: "github", ID: "42", Name: "acme/api"}
	run := contract.Run{ID: "run-journal"}
	first := &query.Dataset{}
	if err := c.collectRepository(context.Background(), plan, resource, contract.Snapshot{}, first, &run); err != nil {
		t.Fatal(err)
	}
	if pageOne != 1 || pageTwo != 1 || len(first.PullRequests) != 1 || first.Coverage[0].Status != "unauthorized" {
		t.Fatalf("first pass pages=%d,%d prs=%d coverage=%+v", pageOne, pageTwo, len(first.PullRequests), first.Coverage)
	}
	cursor, err := c.githubCursor(context.Background(), plan, resource, "pull_requests")
	if err != nil || cursor.value.Watermark != "" || !strings.Contains(cursor.value.Checkpoint, "page=2") {
		t.Fatalf("cursor avançou após falha na segunda página: %+v %v", cursor.value, err)
	}
	second := &query.Dataset{}
	if err := c.collectRepository(context.Background(), plan, resource, contract.Snapshot{}, second, &run); err != nil {
		t.Fatal(err)
	}
	if pageOne != 1 || pageTwo != 2 || len(second.PullRequests) != 2 || second.Coverage[0].Status != "partial" {
		t.Fatalf("resume pages=%d,%d prs=%d coverage=%+v", pageOne, pageTwo, len(second.PullRequests), second.Coverage)
	}
	cursor, err = c.githubCursor(context.Background(), plan, resource, "pull_requests")
	if err != nil || cursor.value.Watermark != "" {
		t.Fatalf("lista paginada avançou watermark sem snapshot estável: %+v %v", cursor.value, err)
	}
}

func TestInterruptedRunBecomesPausedOnRestart(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	plan := contract.Plan{ID: "plan-recovery", InvestigationID: "investigation-recovery"}
	run := contract.Run{ID: "run-recovery", PlanID: plan.ID, State: "collecting"}
	if err := store.Put("plans", plan.ID, plan); err != nil {
		t.Fatal(err)
	}
	if err := store.Put("runs", run.ID, run); err != nil {
		t.Fatal(err)
	}
	c, err := NewCore(CoreConfig{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.GetRun(context.Background(), run.ID)
	if err != nil || got.State != "paused" || got.LastError == nil {
		t.Fatalf("run=%+v err=%v", got, err)
	}
}

func TestLinearJournalResumesFromCommittedCursor(t *testing.T) {
	store, _ := storage.Open(t.TempDir())
	secretStore := secrets.NewSessionStore()
	secretStore.Put(context.Background(), contract.CredentialInput{Source: "linear", Secret: "linear-secret", Persistence: "session"})
	firstCalls, secondCalls := 0, 0
	transport := transportFunc(func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		payload := ""
		status := 200
		if strings.Contains(string(body), `"after":"cursor-1"`) {
			secondCalls++
			if secondCalls == 1 {
				status = 403
				payload = `{"errors":[{"message":"denied"}]}`
			} else {
				payload = `{"data":{"issues":{"nodes":[{"id":"two","identifier":"T-2","title":"second","createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-03T00:00:00Z"}],"pageInfo":{"hasNextPage":false,"endCursor":"cursor-2"}}}}`
			}
		} else {
			firstCalls++
			payload = `{"data":{"issues":{"nodes":[{"id":"one","identifier":"T-1","title":"first","createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-02T00:00:00Z"}],"pageInfo":{"hasNextPage":true,"endCursor":"cursor-1"}}}}`
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(payload))}, nil
	})
	c, err := NewCore(CoreConfig{Store: store, Secrets: secretStore, HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	plan := contract.Plan{ID: "plan-linear", Input: contract.PlanInput{Limits: contract.Limits{MaxTransferBytes: 1 << 20}}}
	resource := contract.Resource{Source: "linear", ID: "team-1", Name: "T"}
	run := contract.Run{ID: "run-linear"}
	first := &query.Dataset{}
	if err := c.collectTeam(context.Background(), plan, resource, first, &run); err != nil {
		t.Fatal(err)
	}
	if firstCalls != 1 || secondCalls != 1 || len(first.Issues) != 1 || first.Coverage[0].Status != "partial" {
		t.Fatalf("first pass calls=%d,%d issues=%d coverage=%+v", firstCalls, secondCalls, len(first.Issues), first.Coverage)
	}
	second := &query.Dataset{}
	if err := c.collectTeam(context.Background(), plan, resource, second, &run); err != nil {
		t.Fatal(err)
	}
	if firstCalls != 1 || secondCalls != 2 || len(second.Issues) != 2 || second.Coverage[0].Status != "partial" {
		t.Fatalf("resume calls=%d,%d issues=%d coverage=%+v", firstCalls, secondCalls, len(second.Issues), second.Coverage)
	}
}

func TestLinearCancelledAndDuplicateIssuesDoNotBecomeDeliveries(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	secretStore := secrets.NewSessionStore()
	if _, err := secretStore.Put(context.Background(), contract.CredentialInput{Source: "linear", Secret: "linear-secret", Persistence: "session"}); err != nil {
		t.Fatal(err)
	}
	transport := transportFunc(func(r *http.Request) (*http.Response, error) {
		payload := `{"data":{"issues":{"nodes":[{"id":"done","identifier":"T-1","createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-10T00:00:00Z","completedAt":"2026-01-08T00:00:00Z","state":{"name":"Done","type":"completed"}},{"id":"cancelled","identifier":"T-2","createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-10T00:00:00Z","completedAt":"2026-01-09T00:00:00Z","state":{"name":"Won't Do","type":"canceled"}},{"id":"duplicate","identifier":"T-3","createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-10T00:00:00Z","completedAt":"2026-01-09T00:00:00Z","state":{"name":"Duplicate","type":"canceled"}}],"pageInfo":{"hasNextPage":false}}}}`
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(payload))}, nil
	})
	c, err := NewCore(CoreConfig{Store: store, Secrets: secretStore, HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	plan := contract.Plan{ID: "plan-linear-outcomes", WindowStart: "2026-01-01T00:00:00Z", WindowEnd: "2026-02-01T00:00:00Z", Input: contract.PlanInput{Limits: contract.Limits{MaxTransferBytes: 1 << 20}}}
	d := query.Dataset{Snapshot: contract.Snapshot{WindowStart: plan.WindowStart, WindowEnd: plan.WindowEnd}}
	if err := c.collectTeam(context.Background(), plan, contract.Resource{Source: "linear", ID: "team-1", Name: "T"}, &d, &contract.Run{ID: "run-linear-outcomes"}); err != nil {
		t.Fatal(err)
	}
	if len(d.Issues) != 3 || d.Issues[0].Outcome != "completed" || d.Issues[0].CompletedAt == "" || d.Issues[1].Outcome != "cancelled" || d.Issues[1].CompletedAt != "" || d.Issues[2].Outcome != "duplicate" || d.Issues[2].CompletedAt != "" {
		t.Fatalf("Linear outcome projection: %+v", d.Issues)
	}
	for _, metric := range rules.Evaluate(d).Metrics {
		if metric.ID == "metric-completed" {
			if metric.Value.Value != "1" {
				t.Fatalf("cancelled or duplicate counted as delivery: %+v", metric)
			}
			return
		}
	}
	t.Fatal("completion metric missing")
}

func TestRetainedIssueReclassifiesFromPrivateEvidence(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCore(CoreConfig{Store: store, Secrets: secrets.NewSessionStore()})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, source, raw, want string
	}{
		{"github-not-planned", "github", `{"state":"closed","state_reason":"not_planned","closed_at":"2026-01-09T00:00:00Z"}`, "not_planned"},
		{"linear-duplicate", "linear", `{"state":{"name":"Duplicate","type":"canceled"},"completedAt":"2026-01-09T00:00:00Z"}`, "duplicate"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			evidence, err := c.evidence(tc.source, tc.name, "2026-01-09T00:00:00Z", json.RawMessage(tc.raw))
			if err != nil {
				t.Fatal(err)
			}
			seconds := int64(3600)
			prior := query.Issue{ID: tc.name, Source: tc.source, EvidenceID: evidence.ID, StartedAt: "2026-01-08T00:00:00Z", CompletedAt: "2026-01-09T00:00:00Z", DurationValid: true, CycleSeconds: &seconds}
			got, err := c.classifyRetainedIssue(prior, evidence.SHA256)
			if err != nil || got.Outcome != tc.want || got.CompletedAt != "" || got.DurationValid || got.CycleSeconds != nil {
				t.Fatalf("old projection retained false delivery: issue=%+v err=%v", got, err)
			}
			if _, err := c.classifyRetainedIssue(prior, "wrong-hash"); err == nil || !strings.Contains(err.Error(), "ERR_AUDIT_FAILED") {
				t.Fatalf("tampered evidence accepted: %v", err)
			}
		})
	}
}

func TestRetainedGitHubActorKindsReclassifyWithoutNetwork(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCore(CoreConfig{Store: store, Secrets: secrets.NewSessionStore()})
	if err != nil {
		t.Fatal(err)
	}
	prEvidence, err := c.evidence("github", "acme/api#7", "2026-01-10T00:00:00Z", json.RawMessage(`{"id":100,"user":{"id":1,"type":"User"}}`))
	if err != nil {
		t.Fatal(err)
	}
	pr, err := c.classifyRetainedPR(query.PullRequest{ID: "github-pr-100", AuthorID: "github-1", EvidenceID: prEvidence.ID}, prEvidence.SHA256)
	if err != nil || pr.AuthorID != "github-1" || pr.AuthorKind != "user" {
		t.Fatalf("retained PR author type: %+v err=%v", pr, err)
	}
	for _, tc := range []struct {
		name, body, wantType, wantKind string
	}{
		{"bot", `{"id":10,"state":"APPROVED","user":{"id":2,"type":"Bot"}}`, "bot_reviewed", "bot"},
		{"unknown", `{"id":11,"state":"APPROVED","user":{"id":3}}`, "reviewed_unknown", ""},
		{"self", `{"id":12,"state":"APPROVED","user":{"id":1,"type":"User"}}`, "reviewed", "user"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			evidence, err := c.evidence("github", "acme/api:review:"+tc.name, "2026-01-10T00:00:00Z", json.RawMessage(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			event, err := c.classifyRetainedActorEvent(query.Event{ID: tc.name, EntityID: pr.ID, Type: "reviewed", ActorID: "github-2", EvidenceID: evidence.ID}, []query.PullRequest{pr}, evidence.SHA256)
			if err != nil || event.Type != tc.wantType || event.ActorKind != tc.wantKind {
				t.Fatalf("retained review classification: %+v err=%v", event, err)
			}
		})
	}
}

func TestLinearCursorUsesUpdatedAtAfterConfirmedCoverage(t *testing.T) {
	store, _ := storage.Open(t.TempDir())
	ss := secrets.NewSessionStore()
	ss.Put(context.Background(), contract.CredentialInput{Source: "linear", Secret: "linear-secret", Persistence: "session"})
	queries := []string{}
	sinceValues := []string{}
	untilValues := []string{}
	transport := transportFunc(func(r *http.Request) (*http.Response, error) {
		var request struct {
			Query     string            `json:"query"`
			Variables map[string]string `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		queries = append(queries, request.Query)
		sinceValues = append(sinceValues, request.Variables["since"])
		untilValues = append(untilValues, request.Variables["until"])
		if strings.Contains(request.Query, "TeamIssuesGap") {
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"data":{"issues":{"nodes":[{"id":"issue-old","identifier":"T-OLD","title":"Old issue","createdAt":"2025-12-10T00:00:00Z","updatedAt":"2025-12-20T00:00:00Z"}],"pageInfo":{"hasNextPage":false,"endCursor":""}}}}`))}, nil
		}
		updated := "2026-01-03T00:00:00Z"
		if len(queries) > 1 {
			updated = "2026-01-11T00:00:00Z"
		}
		body := fmt.Sprintf(`{"data":{"issues":{"nodes":[{"id":"issue-1","identifier":"T-1","title":"Issue","createdAt":"2026-01-01T00:00:00Z","updatedAt":%q}],"pageInfo":{"hasNextPage":false,"endCursor":""}}}}`, updated)
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	c, err := NewCore(CoreConfig{Store: store, Secrets: ss, HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	r := contract.Resource{Source: "linear", ID: "team-1", Name: "T"}
	run := contract.Run{ID: "linear-cursor-run"}
	first := contract.Plan{ID: "linear-cursor-1", InvestigationID: "linear-cursor-investigation", WindowStart: "2026-01-01T00:00:00Z", WindowEnd: "2026-01-10T00:00:00Z", Input: contract.PlanInput{Limits: contract.Limits{MaxTransferBytes: 1 << 20}}}
	if err := c.collectTeam(context.Background(), first, r, &query.Dataset{}, &run); err != nil {
		t.Fatal(err)
	}
	second := first
	second.ID = "linear-cursor-2"
	second.WindowStart = "2026-01-05T00:00:00Z"
	second.WindowEnd = "2026-01-12T00:00:00Z"
	if err := c.collectTeam(context.Background(), second, r, &query.Dataset{}, &run); err != nil {
		t.Fatal(err)
	}
	third := second
	third.ID = "linear-cursor-3"
	third.WindowStart = "2025-12-01T00:00:00Z"
	third.WindowEnd = "2026-01-13T00:00:00Z"
	thirdData := &query.Dataset{}
	if err := c.collectTeam(context.Background(), third, r, thirdData, &run); err != nil {
		t.Fatal(err)
	}
	if len(queries) != 4 || strings.Contains(queries[0], "TeamIssuesDelta") || !strings.Contains(queries[1], "TeamIssuesDelta") || !strings.Contains(queries[2], "TeamIssuesGap") || !strings.Contains(queries[3], "TeamIssuesDelta") || sinceValues[0] != "" || sinceValues[1] != "2026-01-08T00:00:00Z" || sinceValues[2] != "2025-12-01T00:00:00Z" || untilValues[2] != "2026-01-01T00:00:00Z" || sinceValues[3] != "2026-01-10T00:00:00Z" {
		t.Fatalf("seletores Linear inválidos: since=%v until=%v queries=%v", sinceValues, untilValues, queries)
	}
	if len(thirdData.Issues) != 2 || thirdData.Issues[0].ID != "linear-issue-old" || thirdData.Issues[1].ID != "linear-issue-1" {
		t.Fatalf("gap e delta não chegaram ao dataset: %+v", thirdData.Issues)
	}
}

func TestLinearHistoricalGapResumesFromCommittedPage(t *testing.T) {
	store, _ := storage.Open(t.TempDir())
	ss := secrets.NewSessionStore()
	ss.Put(context.Background(), contract.CredentialInput{Source: "linear", Secret: "linear-secret", Persistence: "session"})
	failedSecondPage := false
	var requests []string
	transport := transportFunc(func(r *http.Request) (*http.Response, error) {
		var request struct {
			Query     string            `json:"query"`
			Variables map[string]string `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		kind := "delta"
		if strings.Contains(request.Query, "TeamIssuesGap") {
			kind = "gap"
		}
		requests = append(requests, kind+":"+request.Variables["after"])
		body := `{"data":{"issues":{"nodes":[],"pageInfo":{"hasNextPage":false,"endCursor":""}}}}`
		if kind == "gap" && request.Variables["after"] == "" {
			body = `{"data":{"issues":{"nodes":[{"id":"old-1","identifier":"T-1","createdAt":"2025-12-01T00:00:00Z","updatedAt":"2025-12-02T00:00:00Z"}],"pageInfo":{"hasNextPage":true,"endCursor":"next"}}}}`
		} else if kind == "gap" && !failedSecondPage {
			failedSecondPage = true
			body = `{"errors":[{"message":"transient"}]}`
		} else if kind == "gap" {
			body = `{"data":{"issues":{"nodes":[{"id":"old-2","identifier":"T-2","createdAt":"2025-12-03T00:00:00Z","updatedAt":"2025-12-04T00:00:00Z"}],"pageInfo":{"hasNextPage":false,"endCursor":""}}}}`
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	c, err := NewCore(CoreConfig{Store: store, Secrets: ss, HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	r := contract.Resource{Source: "linear", ID: "team-1", Name: "T"}
	oldPlan := contract.Plan{ID: "old-window", InvestigationID: "gap-resume", WindowStart: "2026-01-01T00:00:00Z", WindowEnd: "2026-01-10T00:00:00Z"}
	cursor, err := c.linearCursor(context.Background(), oldPlan, r, "issues")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.finishSourceCursor(&cursor, oldPlan, "complete"); err != nil {
		t.Fatal(err)
	}
	plan := oldPlan
	plan.ID = "expanded-window"
	plan.WindowStart = "2025-12-01T00:00:00Z"
	plan.WindowEnd = "2026-01-12T00:00:00Z"
	plan.Input.Limits.MaxTransferBytes = 1 << 20
	run := contract.Run{ID: "gap-resume-run"}
	first := &query.Dataset{}
	if err := c.collectTeam(context.Background(), plan, r, first, &run); err != nil || first.Coverage[0].Status != "partial" {
		t.Fatalf("primeira coleta: coverage=%+v err=%v", first.Coverage, err)
	}
	second := &query.Dataset{}
	if err := c.collectTeam(context.Background(), plan, r, second, &run); err != nil {
		t.Fatal(err)
	}
	if strings.Join(requests, ",") != "gap:,gap:next,delta:,gap:next" || len(second.Issues) != 2 || second.Coverage[0].Status != "partial" {
		t.Fatalf("retomada: requests=%v issues=%+v coverage=%+v", requests, second.Issues, second.Coverage)
	}
	cursor, err = c.linearCursor(context.Background(), plan, r, "issues")
	if err != nil || cursor.value.Watermark != oldPlan.WindowEnd {
		t.Fatalf("watermark avançou na paginação parcial: %+v err=%v", cursor.value, err)
	}
}

func TestCommentEditPreservesBothObservedVersions(t *testing.T) {
	store, _ := storage.Open(t.TempDir())
	secretStore := secrets.NewSessionStore()
	secretStore.Put(context.Background(), contract.CredentialInput{Source: "github", Secret: "test-secret", Persistence: "session"})
	version := 1
	var lastSince string
	transport := transportFunc(func(r *http.Request) (*http.Response, error) {
		lastSince = r.URL.Query().Get("since")
		body := `[{"id":8,"issue_url":"https://api.github.com/repos/acme/api/issues/7","created_at":"2025-12-10T00:00:00Z","updated_at":"2026-01-02T00:00:00Z","body":"first"}]`
		if version == 2 {
			body = `[{"id":8,"issue_url":"https://api.github.com/repos/acme/api/issues/7","created_at":"2025-12-10T00:00:00Z","updated_at":"2026-01-08T00:00:00Z","body":"edited"}]`
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	c, err := NewCore(CoreConfig{Store: store, Secrets: secretStore, HTTPClient: &http.Client{Transport: transport}, Clock: func() time.Time { return time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	resource := contract.Resource{Source: "github", ID: "42", Name: "acme/api"}
	firstPlan := contract.Plan{ID: "plan-comment-one", WindowStart: "2025-12-01T00:00:00Z", Input: contract.PlanInput{Limits: contract.Limits{MaxTransferBytes: 1 << 20}}}
	first := &query.Dataset{}
	run := contract.Run{ID: "run-comment"}
	if err := c.collectComments(context.Background(), firstPlan, resource, contract.Snapshot{}, first, &run); err != nil {
		t.Fatal(err)
	}
	if len(first.Evidence) != 1 || first.Coverage[0].Status != "complete" || lastSince == "" {
		t.Fatalf("first=%+v since=%q", first.Coverage, lastSince)
	}
	version = 2
	secondPlan := firstPlan
	secondPlan.ID = "plan-comment-two"
	prior := contract.Snapshot{WindowStart: firstPlan.WindowStart, ObservationFinishedAt: "2026-01-05T00:00:00Z", Coverage: first.Coverage}
	second := &query.Dataset{}
	if err := c.collectComments(context.Background(), secondPlan, resource, prior, second, &run); err != nil {
		t.Fatal(err)
	}
	if len(second.Evidence) != 1 || first.Evidence[0].SHA256 == second.Evidence[0].SHA256 || lastSince != "2026-01-02T23:59:59Z" {
		t.Fatalf("first=%+v second=%+v since=%q", first.Evidence, second.Evidence, lastSince)
	}
	objects, err := store.List("objects")
	if err != nil || len(objects) != 2 {
		t.Fatalf("objects=%v err=%v", objects, err)
	}
}

func TestExplicitlyDisabledGitHubIsNotQueried(t *testing.T) {
	store, _ := storage.Open(t.TempDir())
	secretStore := secrets.NewSessionStore()
	secretStore.Put(context.Background(), contract.CredentialInput{Source: "linear", Secret: "linear-secret", Persistence: "session"})
	githubCalls, linearCalls := 0, 0
	transport := transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "api.github.com" {
			githubCalls++
			return nil, fmt.Errorf("GitHub should not be called")
		}
		linearCalls++
		body := `{"data":{"teams":{"nodes":[{"id":"team-1","key":"T","name":"Team"}],"pageInfo":{"hasNextPage":false}}}}`
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	c, err := NewCore(CoreConfig{Store: store, Secrets: secretStore, HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	input := contract.PlanInput{SchemaVersion: contract.Version, GitHubEnabled: false, LinearEnabled: true, Scope: &contract.Scope{Mode: "selected", Teams: []string{"T"}}, Period: contract.Period{Preset: "30d", Timezone: "UTC"}}
	plan, err := c.PreparePlan(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if githubCalls != 0 || linearCalls != 1 || plan.Input.GitHubEnabled || len(plan.Resources) != 1 || plan.Resources[0].Source != "linear" {
		t.Fatalf("github=%d linear=%d plan=%+v", githubCalls, linearCalls, plan)
	}
}

func TestRepositorySelectorKeepsLinearEnabled(t *testing.T) {
	store, _ := storage.Open(t.TempDir())
	ss := secrets.NewSessionStore()
	ss.Put(context.Background(), contract.CredentialInput{Source: "github", Secret: "github-secret", Persistence: "session"})
	ss.Put(context.Background(), contract.CredentialInput{Source: "linear", Secret: "linear-secret", Persistence: "session"})
	githubCalls, linearCalls := 0, 0
	transport := transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "api.github.com" {
			githubCalls++
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"id":42,"full_name":"acme/api"}`))}, nil
		}
		linearCalls++
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"data":{"teams":{"nodes":[{"id":"team-1","key":"T","name":"Team"}],"pageInfo":{"hasNextPage":false}}}}`))}, nil
	})
	c, err := NewCore(CoreConfig{Store: store, Secrets: ss, HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := c.PreparePlan(context.Background(), contract.PlanInput{SchemaVersion: contract.Version, GitHubEnabled: true, LinearEnabled: true, Scope: &contract.Scope{Mode: "selected", Repositories: []string{"acme/api"}}, Period: contract.Period{Preset: "30d", Timezone: "UTC"}, Limits: contract.Limits{GitHubRPH: 3600000, LinearRPH: 3600000}})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Input.GitHubEnabled || !plan.Input.LinearEnabled || githubCalls != 1 || linearCalls != 1 || len(plan.Resources) != 2 {
		t.Fatalf("github=%d linear=%d plan=%+v", githubCalls, linearCalls, plan)
	}
}

func TestRepositoryDiscoveryRejectsUnsafeReturnedName(t *testing.T) {
	store, _ := storage.Open(t.TempDir())
	ss := secrets.NewSessionStore()
	ss.Put(context.Background(), contract.CredentialInput{Source: "github", Secret: "github-secret", Persistence: "session"})
	transport := transportFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"id":42,"full_name":"../../users"}`))}, nil
	})
	c, err := NewCore(CoreConfig{Store: store, Secrets: ss, HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.PreparePlan(context.Background(), contract.PlanInput{SchemaVersion: contract.Version, GitHubEnabled: true, Scope: &contract.Scope{Mode: "selected", Repositories: []string{"acme/api"}}, Period: contract.Period{Preset: "30d", Timezone: "UTC"}})
	if err == nil || !strings.Contains(err.Error(), "ERR_UNAVAILABLE") {
		t.Fatalf("unsafe discovery err=%v", err)
	}
}

func TestGitRefObservationFeedsSnapshotEvidenceAndNextRound(t *testing.T) {
	store, _ := storage.Open(t.TempDir())
	ss := secrets.NewSessionStore()
	ss.Put(context.Background(), contract.CredentialInput{Source: "github", Secret: "test-secret", Persistence: "session"})
	c, err := NewCore(CoreConfig{Store: store, Secrets: ss, Clock: func() time.Time { return time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	plan := contract.Plan{ID: "git-plan", InvestigationID: "git-investigation", Input: contract.PlanInput{GitHubEnabled: true, Limits: contract.Limits{MaxTransferBytes: 1 << 20, MaxLocalBytes: 1 << 20}}}
	if err := store.Put("planauth", plan.ID, planAuth{GitHub: "identity"}); err != nil {
		t.Fatal(err)
	}
	observer := &fakeGitObserver{result: gitmirror.Observation{Ref: "refs/heads/main", NewOID: strings.Repeat("a", 40), Relationship: "initial", GraphComplete: true, LocalBytes: 300}}
	c.gitMirror = func(string) (gitObserver, error) { return observer, nil }
	resource := contract.Resource{Source: "github", ID: "42", Name: "acme/api"}
	run := contract.Run{ID: "git-run"}
	first := query.Dataset{}
	if err := c.collectGitRef(context.Background(), plan, resource, &first, &run); err != nil {
		t.Fatal(err)
	}
	if len(first.Evidence) != 1 || len(first.Events) != 1 || len(first.Coverage) != 3 || first.Coverage[0].Status != "complete" || first.Coverage[1].Status != "partial" || run.Bytes != 300 {
		t.Fatalf("first=%+v run=%+v", first, run)
	}
	firstStats, err := c.RunStats(context.Background(), run.ID)
	if err != nil || firstStats.GitObjectBytesAdded != 300 || firstStats.GitRefBytesAdded != 300 {
		t.Fatalf("Git object bytes after first fetch=%+v err=%v", firstStats, err)
	}
	observer.result = gitmirror.Observation{Ref: "refs/heads/main", NewOID: strings.Repeat("b", 40), Relationship: "advanced", GraphComplete: true, LocalBytes: 200}
	retry := query.Dataset{}
	if err := c.collectGitRef(context.Background(), plan, resource, &retry, &run); err != nil {
		t.Fatal(err)
	}
	if len(observer.priors) != 1 || len(retry.Events) != 1 || retry.Events[0].Type != "ref_initial_observed" || run.Bytes != 300 {
		t.Fatalf("journal não foi reutilizado: prior=%+v retry=%+v run=%+v", observer.priors, retry, run)
	}
	plan.ID = "git-plan-two"
	if err := store.Put("planauth", plan.ID, planAuth{GitHub: "identity"}); err != nil {
		t.Fatal(err)
	}
	second := query.Dataset{}
	if err := c.collectGitRef(context.Background(), plan, resource, &second, &run); err != nil {
		t.Fatal(err)
	}
	if len(observer.priors) != 2 || observer.priors[1].OID != strings.Repeat("a", 40) || second.Events[0].Type != "ref_advanced_observed" || second.Events[0].EvidenceID == first.Events[0].EvidenceID || run.Bytes != 500 {
		t.Fatalf("prior=%+v second=%+v run=%+v", observer.priors, second, run)
	}
	secondStats, err := c.RunStats(context.Background(), run.ID)
	if err != nil || secondStats.GitObjectBytesAdded != 500 || secondStats.GitRefBytesAdded != 500 {
		t.Fatalf("Git object bytes after second fetch=%+v err=%v", secondStats, err)
	}
}

func TestManifestPathsArePinnedCachedAndBounded(t *testing.T) {
	store, _ := storage.Open(t.TempDir())
	ss := secrets.NewSessionStore()
	ss.Put(context.Background(), contract.CredentialInput{Source: "github", Secret: "test-secret", Persistence: "session"})
	treeOID := strings.Repeat("a", 40)
	blobOID := strings.Repeat("b", 40)
	calls := 0
	transport := transportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != "GET" || r.URL.Path != "/repos/acme/api/git/trees/"+treeOID || r.URL.Query().Get("recursive") != "1" || r.Header.Get("Authorization") != "Bearer test-secret" {
			t.Fatalf("request inesperada: %s %s", r.Method, r.URL)
		}
		body := fmt.Sprintf(`{"sha":%q,"truncated":true,"tree":[{"path":"services/api/go.mod","mode":"100644","type":"blob","sha":%q,"size":3000000},{"path":"../evil/package.json","mode":"100644","type":"blob","sha":%q},{"path":"vendor/Dockerfile","mode":"120000","type":"blob","sha":%q}]}`, treeOID, blobOID, blobOID, blobOID)
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	c, err := NewCore(CoreConfig{Store: store, Secrets: ss, HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	plan := contract.Plan{ID: "manifest-plan", Input: contract.PlanInput{Limits: contract.Limits{MaxTransferBytes: 1 << 20, MaxLocalBytes: 1 << 20}}}
	r := contract.Resource{Source: "github", ID: "42", Name: "acme/api"}
	run := contract.Run{ID: "manifest-run"}
	first := query.Dataset{}
	if err := c.collectManifestPaths(context.Background(), plan, r, treeOID, "identity", "2026-01-10T00:00:00Z", &first, &run); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || len(first.Components) != 1 || first.Components[0].Kind != "go_module" || first.Components[0].ImplementationStatus != "identified" || len(first.Links) != 1 || len(first.Evidence) != 1 || first.Coverage[0].Status != "partial" {
		t.Fatalf("first=%+v calls=%d", first, calls)
	}
	second := query.Dataset{}
	if err := c.collectManifestPaths(context.Background(), plan, r, treeOID, "identity", "2026-01-10T00:00:00Z", &second, &run); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || len(second.Components) != 1 || second.Evidence[0].ID != first.Evidence[0].ID {
		t.Fatalf("cache não reutilizado: second=%+v calls=%d", second, calls)
	}
	reused := query.Dataset{Components: append([]contract.ComponentRecord(nil), first.Components...), Links: append([]contract.LinkRecord(nil), first.Links...)}
	if err := c.collectManifestPaths(context.Background(), plan, r, treeOID, "identity", "2026-01-10T00:00:00Z", &reused, &run); err != nil {
		t.Fatal(err)
	}
	if len(reused.Components) != 1 || len(reused.Links) != 1 {
		t.Fatalf("manifesto reaproveitado foi duplicado: componentes=%d vínculos=%d", len(reused.Components), len(reused.Links))
	}
}

func TestManifestBlobIsVerifiedParsedAndCached(t *testing.T) {
	store, _ := storage.Open(t.TempDir())
	ss := secrets.NewSessionStore()
	ss.Put(context.Background(), contract.CredentialInput{Source: "github", Secret: "test-secret", Persistence: "session"})
	treeOID := strings.Repeat("a", 40)
	content := []byte("module example.com/acme/api\n\ngo 1.22\n")
	object := append([]byte(fmt.Sprintf("blob %d\x00", len(content))), content...)
	sum := sha1.Sum(object)
	blobOID := hex.EncodeToString(sum[:])
	calls := 0
	transport := transportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		body := ""
		switch r.URL.Path {
		case "/repos/acme/api/git/trees/" + treeOID:
			body = fmt.Sprintf(`{"sha":%q,"truncated":false,"tree":[{"path":"services/api/go.mod","mode":"100644","type":"blob","sha":%q,"size":%d}]}`, treeOID, blobOID, len(content))
		case "/repos/acme/api/git/blobs/" + blobOID:
			body = fmt.Sprintf(`{"sha":%q,"encoding":"base64","content":%q,"size":%d}`, blobOID, base64.StdEncoding.EncodeToString(content), len(content))
		default:
			t.Fatalf("request inesperada: %s", r.URL)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	c, err := NewCore(CoreConfig{Store: store, Secrets: ss, HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	plan := contract.Plan{ID: "manifest-content-plan", Input: contract.PlanInput{Limits: contract.Limits{MaxTransferBytes: 1 << 20, MaxLocalBytes: 1 << 20}}}
	r := contract.Resource{Source: "github", ID: "42", Name: "acme/api"}
	run := contract.Run{ID: "manifest-content-run"}
	d := query.Dataset{}
	if err := c.collectManifestPaths(context.Background(), plan, r, treeOID, "identity", "2026-01-10T00:00:00Z", &d, &run); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || run.Requests != 2 || len(d.Components) != 1 || d.Components[0].Name != "example.com/acme/api" || len(d.Events) != 1 || len(d.Evidence) != 2 || d.Coverage[0].Status != "complete" || d.Coverage[1].Status != "complete" {
		t.Fatalf("manifest=%+v run=%+v calls=%d", d, run, calls)
	}
	firstBytes := run.Bytes
	var stats struct {
		ReuseCounter int64 `json:"reuse_counter"`
	}
	if err := store.Get("runstats", run.ID, &stats); !os.IsNotExist(err) {
		t.Fatalf("primeira coleta já contou cache: %+v err=%v", stats, err)
	}
	retry := query.Dataset{}
	if err := c.collectManifestPaths(context.Background(), plan, r, treeOID, "identity", "2026-01-10T00:00:00Z", &retry, &run); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || run.Requests != 2 || run.Bytes != firstBytes || len(retry.Components) != 1 || retry.Events[0].EvidenceID != d.Events[0].EvidenceID {
		t.Fatalf("cache de blob inválido: retry=%+v run=%+v calls=%d", retry, run, calls)
	}
	if err := store.Get("runstats", run.ID, &stats); err != nil || stats.ReuseCounter != 2 {
		t.Fatalf("reuso de árvore e blob: %+v err=%v", stats, err)
	}
}

func TestComposeDependenciesLinkOnlyVerifiedServices(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ss := secrets.NewSessionStore()
	if _, err := ss.Put(context.Background(), contract.CredentialInput{Source: "github", Secret: "test-secret", Persistence: "session"}); err != nil {
		t.Fatal(err)
	}
	treeOID := strings.Repeat("a", 40)
	content := []byte("services:\n  api:\n    depends_on: [db]\n  worker:\n    depends_on:\n      db:\n        condition: service_healthy\n      external_service:\n        condition: service_started\n  db:\n    image: postgres:18\n")
	object := append([]byte(fmt.Sprintf("blob %d\x00", len(content))), content...)
	sum := sha1.Sum(object)
	blobOID := hex.EncodeToString(sum[:])
	calls := 0
	transport := transportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		var body string
		switch r.URL.Path {
		case "/repos/acme/api/git/trees/" + treeOID:
			body = fmt.Sprintf(`{"sha":%q,"truncated":false,"tree":[{"path":"deploy/compose.yaml","mode":"100644","type":"blob","sha":%q,"size":%d}]}`, treeOID, blobOID, len(content))
		case "/repos/acme/api/git/blobs/" + blobOID:
			body = fmt.Sprintf(`{"sha":%q,"encoding":"base64","content":%q,"size":%d}`, blobOID, base64.StdEncoding.EncodeToString(content), len(content))
		default:
			t.Fatalf("request inesperada: %s", r.URL)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	c, err := NewCore(CoreConfig{Store: store, Secrets: ss, HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	plan := contract.Plan{ID: "compose-dependencies", Input: contract.PlanInput{Limits: contract.Limits{MaxTransferBytes: 1 << 20, MaxLocalBytes: 1 << 20}}}
	r := contract.Resource{Source: "github", ID: "42", Name: "acme/api"}
	run := contract.Run{ID: "compose-dependencies-run"}
	d := query.Dataset{}
	if err := c.collectManifestPaths(context.Background(), plan, r, treeOID, "identity", "2026-01-10T00:00:00Z", &d, &run); err != nil {
		t.Fatal(err)
	}
	serviceNames := make(map[string]string)
	for _, component := range d.Components {
		if component.Kind == "compose_service" {
			serviceNames[component.ID] = component.Name
		}
	}
	dependencies := make(map[string]bool)
	for _, link := range d.Links {
		if link.Relation != "depends_on" {
			continue
		}
		if link.Status != "direct" || link.EvidenceID == "" {
			t.Fatalf("dependência sem evidência verificada: %+v", link)
		}
		dependencies[serviceNames[link.SourceID]+"->"+serviceNames[link.TargetID]] = true
	}
	if calls != 2 || len(serviceNames) != 3 || len(dependencies) != 2 || !dependencies["api->db"] || !dependencies["worker->db"] || d.Coverage[1].Status != "partial" {
		t.Fatalf("vínculos ou cobertura Compose incorretos: components=%+v links=%+v coverage=%+v calls=%d", d.Components, d.Links, d.Coverage, calls)
	}
}

func TestBase64GitBlobEchoCannotEnterManifestStorage(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ss := secrets.NewSessionStore()
	if _, err := ss.Put(context.Background(), contract.CredentialInput{Source: "github", Secret: "test-secret", Persistence: "session"}); err != nil {
		t.Fatal(err)
	}
	treeOID := strings.Repeat("a", 40)
	content := []byte("module example.com/acme/api\n// test-secret\n")
	encoded := base64.StdEncoding.EncodeToString(content)
	object := append([]byte(fmt.Sprintf("blob %d\x00", len(content))), content...)
	sum := sha1.Sum(object)
	blobOID := hex.EncodeToString(sum[:])
	calls := 0
	transport := transportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		var body string
		switch r.URL.Path {
		case "/repos/acme/api/git/trees/" + treeOID:
			body = fmt.Sprintf(`{"sha":%q,"truncated":false,"tree":[{"path":"go.mod","mode":"100644","type":"blob","sha":%q,"size":%d}]}`, treeOID, blobOID, len(content))
		case "/repos/acme/api/git/blobs/" + blobOID:
			body = fmt.Sprintf(`{"sha":%q,"encoding":"base64","content":%q,"size":%d}`, blobOID, encoded, len(content))
		default:
			t.Fatalf("request inesperada: %s", r.URL)
		}
		res := &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
		res.Header.Set("ETag", `"blob-version"`)
		return res, nil
	})
	c, err := NewCore(CoreConfig{Store: store, Secrets: ss, HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	plan := contract.Plan{ID: "secret-blob-plan", Input: contract.PlanInput{Limits: contract.Limits{MaxTransferBytes: 1 << 20, MaxLocalBytes: 1 << 20}}}
	r := contract.Resource{Source: "github", ID: "42", Name: "acme/api"}
	run := contract.Run{ID: "secret-blob-run"}
	d := query.Dataset{}
	if err := c.collectManifestPaths(context.Background(), plan, r, treeOID, "identity", "2026-01-10T00:00:00Z", &d, &run); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(d.Components) != 1 || len(d.Events) != 0 || len(d.Coverage) != 2 || d.Coverage[1].Status != "partial" {
		t.Fatalf("blob com credencial não ficou parcial: components=%+v events=%+v coverage=%+v calls=%d", d.Components, d.Events, d.Coverage, calls)
	}
	if err := filepath.WalkDir(store.Root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(raw), "test-secret") || strings.Contains(string(raw), encoded) {
			t.Errorf("blob com credencial persistido em %s", path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestPRFilesLinkExactObservedPathWithoutAssumingComponent(t *testing.T) {
	store, _ := storage.Open(t.TempDir())
	ss := secrets.NewSessionStore()
	ss.Put(context.Background(), contract.CredentialInput{Source: "github", Secret: "test-secret", Persistence: "session"})
	calls := 0
	transport := transportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Path != "/repos/acme/api/pulls/7/files" {
			t.Fatalf("endpoint inesperado: %s", r.URL)
		}
		body := `[{"filename":"services/api/new.go","previous_filename":"services/api/old.go","status":"renamed","sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]`
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	c, err := NewCore(CoreConfig{Store: store, Secrets: ss, HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	plan := contract.Plan{ID: "pr-files-plan", Input: contract.PlanInput{Limits: contract.Limits{MaxTransferBytes: 1 << 20}}}
	r := contract.Resource{Source: "github", ID: "42", Name: "acme/api"}
	run := contract.Run{ID: "pr-files-run"}
	d := query.Dataset{PullRequests: []query.PullRequest{{ID: "github-pr-100", Number: 7, Repository: "acme/api", HeadSHA: strings.Repeat("b", 40), UpdatedAt: "2026-01-04T00:00:00Z"}}}
	if err := c.collectPRFiles(context.Background(), plan, r, &d, &run); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || len(d.Components) != 1 || d.Components[0].Kind != "file_change" || len(d.Links) != 1 || d.Links[0].Relation != "modifies_file" || len(d.Events) != 1 || d.Events[0].Type != "file_rename_reported" || d.Coverage[0].Status != "complete" {
		t.Fatalf("arquivos PR=%+v calls=%d", d, calls)
	}
	if d.Events[0].EventAt != "" || d.Events[0].ObservedAt == "" || d.Events[0].ObservedAt != d.Evidence[0].ObservedAt || d.Events[0].OldValue != "services/api/old.go" || d.Events[0].NewValue != "services/api/new.go" {
		t.Fatalf("rename recebeu data inferida ou perdeu evidência de observação: %+v", d.Events[0])
	}
	if d.Evidence[0].SourceUpdatedAt != "" {
		t.Fatalf("arquivo de PR recebeu source_updated_at inferido: %+v", d.Evidence[0])
	}
	retry := query.Dataset{PullRequests: d.PullRequests}
	if err := c.collectPRFiles(context.Background(), plan, r, &retry, &run); err != nil || calls != 1 {
		t.Fatalf("journal de arquivos não reutilizado: %+v %v calls=%d", retry, err, calls)
	}
}

func TestRefreshKeepsPreviouslyObservedPRFileLinks(t *testing.T) {
	store, _ := storage.Open(t.TempDir())
	ss := secrets.NewSessionStore()
	ss.Put(context.Background(), contract.CredentialInput{Source: "github", Secret: "test-secret", Persistence: "session"})
	transport := transportFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`[]`))}, nil
	})
	c, err := NewCore(CoreConfig{Store: store, Secrets: ss, HTTPClient: &http.Client{Transport: transport}, Clock: func() time.Time { return time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	modules := contract.Modules{CodeHistory: true}
	r := contract.Resource{Source: "github", ID: "42", Name: "acme/api"}
	prior := contract.Snapshot{ID: "prior-file-snapshot", PlanID: "prior-file-plan", InvestigationID: "file-investigation", Resources: []contract.Resource{r}, Modules: modules, WindowStart: "2026-01-01T00:00:00Z", WindowEnd: "2026-02-01T00:00:00Z", ObservationFinishedAt: "2026-01-09T00:00:00Z"}
	priorData := query.Dataset{PullRequests: []query.PullRequest{{ID: "github-pr-100", Number: 7, Repository: r.Name, State: "open", CreatedAt: "2026-01-02T00:00:00Z", UpdatedAt: "2026-01-03T00:00:00Z", EvidenceID: "ev-pr"}}, Components: []contract.ComponentRecord{{ID: "file-old", Name: "api.go", Repository: r.Name, Path: "api.go", Kind: "file_change"}, {ID: "manifest-old", Name: "go.mod", Repository: r.Name, Path: "go.mod", Kind: "go_module"}}, Links: []contract.LinkRecord{{ID: "link-file-old", SourceID: "github-pr-100", Relation: "modifies_file", TargetID: "file-old", Status: "direct", EvidenceID: "ev-file"}, {ID: "link-map-old", SourceID: "file-old", Relation: "maps_to_component", TargetID: "manifest-old", Status: "direct", ValidFrom: "2026-01-03T00:00:00Z", EvidenceID: "ev-map"}}, Events: []query.Event{{ID: "old-rename", EntityID: "file-old", Type: "file_rename_reported", EventAt: "2026-01-03T00:00:00Z", ObservedAt: "2026-01-09T00:00:00Z", OldValue: "old-api.go", NewValue: "api.go", EvidenceID: "ev-file"}}, Evidence: []contract.Evidence{{ID: "ev-pr", Source: "github", SourceUpdatedAt: "2026-01-03T00:00:00Z"}, {ID: "ev-file", Source: "github", SourceUpdatedAt: "2026-01-03T00:00:00Z"}, {ID: "ev-map", Source: "git", ObservedAt: "2026-01-09T00:00:00Z", SourceUpdatedAt: "2026-01-03T00:00:00Z"}}}
	if err := store.Put("snapshots", prior.ID, prior); err != nil {
		t.Fatal(err)
	}
	if err := store.Put("datasets", prior.ID, priorData); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"ev-pr", "ev-file", "ev-map"} {
		if err := store.Put("objects", id, json.RawMessage(`{"body":""}`)); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{prior.PlanID, "current-file-plan"} {
		if err := store.Put("planauth", id, planAuth{GitHub: "identity"}); err != nil {
			t.Fatal(err)
		}
	}
	c.gitMirror = func(string) (gitObserver, error) {
		return &fakeGitObserver{result: gitmirror.Observation{Ref: "refs/heads/main", NewOID: strings.Repeat("a", 40), Relationship: "initial", GraphComplete: true}}, nil
	}
	plan := contract.Plan{ID: "current-file-plan", InvestigationID: prior.InvestigationID, Resources: []contract.Resource{r}, WindowStart: prior.WindowStart, WindowEnd: prior.WindowEnd, Input: contract.PlanInput{GitHubEnabled: true, Modules: modules, Limits: contract.Limits{MaxTransferBytes: 1 << 20, MaxLocalBytes: 1 << 20}}}
	d, err := c.collectDataset(context.Background(), plan, &contract.Run{ID: "current-file-run"})
	if err != nil {
		t.Fatal(err)
	}
	fileFound, manifestFound, linkFound, mappingFound, evidenceFound, mappingEvidenceFound, renameFound := false, false, false, false, false, false, false
	for _, component := range d.Components {
		fileFound = fileFound || component.ID == "file-old"
		manifestFound = manifestFound || component.ID == "manifest-old"
	}
	for _, link := range d.Links {
		linkFound = linkFound || link.ID == "link-file-old"
		mappingFound = mappingFound || link.ID == "link-map-old" && link.ValidFrom == "2026-01-09T00:00:00Z"
	}
	for _, evidence := range d.Evidence {
		evidenceFound = evidenceFound || evidence.ID == "ev-file" && evidence.SourceUpdatedAt == ""
		mappingEvidenceFound = mappingEvidenceFound || evidence.ID == "ev-map" && evidence.SourceUpdatedAt == ""
	}
	for _, event := range d.Events {
		if event.ID == "old-rename" {
			renameFound = event.EventAt == "" && event.ObservedAt == "2026-01-09T00:00:00Z" && event.EntityID == "file-old"
		}
	}
	if !fileFound || !manifestFound || !linkFound || !mappingFound || !evidenceFound || !mappingEvidenceFound || !renameFound {
		t.Fatalf("observação temporal anterior desapareceu: file=%v manifest=%v file_link=%v mapping=%v file_evidence=%v mapping_evidence=%v rename=%v", fileFound, manifestFound, linkFound, mappingFound, evidenceFound, mappingEvidenceFound, renameFound)
	}
}

func TestFileMapsToComponentOnlyAtExactObservedVersion(t *testing.T) {
	store, _ := storage.Open(t.TempDir())
	c, err := NewCore(CoreConfig{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	plan := contract.Plan{ID: "file-map-plan"}
	r := contract.Resource{Source: "github", ID: "42", Name: "acme/api"}
	oid := strings.Repeat("a", 40)
	journalID := storage.Hash([]byte(plan.ID + ":" + r.ID + ":git_ref"))
	if err := store.Put("gitobservations", journalID, gitRunObservation{Observation: gitmirror.Observation{Ref: "refs/heads/main", NewOID: oid}, ObservedAt: "2026-01-10T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	makeDataset := func(head string) query.Dataset {
		fileID := prFileComponentID(r.ID, head, "services/api/handler.go")
		return query.Dataset{PullRequests: []query.PullRequest{{ID: "pr-1", Repository: r.Name, HeadSHA: head}}, Components: []contract.ComponentRecord{{ID: "cmp-root", Repository: r.Name, Path: "go.mod", Kind: "go_module"}, {ID: "cmp-api", Repository: r.Name, Path: "services/api/go.mod", Kind: "go_module"}, {ID: fileID, Repository: r.Name, Path: "services/api/handler.go", Kind: "file_change"}}, Links: []contract.LinkRecord{{ID: "file-link", SourceID: "pr-1", Relation: "modifies_file", TargetID: fileID, EvidenceID: "ev-file"}}}
	}
	d := makeDataset(oid)
	if err := c.mapCurrentRefFiles(context.Background(), plan, r, &d); err != nil {
		t.Fatal(err)
	}
	if len(d.Links) != 2 || d.Links[1].Relation != "maps_to_component" || d.Links[1].TargetID != "cmp-api" || d.Links[1].Status != "direct" || len(d.Evidence) != 1 || d.Coverage[0].Status != "complete" {
		t.Fatalf("mapeamento exato=%+v", d)
	}
	other := makeDataset(strings.Repeat("b", 40))
	if err := c.mapCurrentRefFiles(context.Background(), plan, r, &other); err != nil || len(other.Links) != 1 || other.Coverage[0].Status != "complete" {
		t.Fatalf("versão diferente mapeada: %+v %v", other, err)
	}
	ambiguous := makeDataset(oid)
	ambiguous.Components = append(ambiguous.Components, contract.ComponentRecord{ID: "cmp-api-container", Repository: r.Name, Path: "services/api/Dockerfile", Kind: "container"})
	if err := c.mapCurrentRefFiles(context.Background(), plan, r, &ambiguous); err != nil || len(ambiguous.Links) != 1 || ambiguous.Coverage[0].Status != "partial" {
		t.Fatalf("manifestos ambíguos mapeados: %+v %v", ambiguous, err)
	}
	forcePushed := makeDataset(oid)
	oldFileID := prFileComponentID(r.ID, strings.Repeat("c", 40), "services/api/old.go")
	forcePushed.Components = append(forcePushed.Components, contract.ComponentRecord{ID: oldFileID, Repository: r.Name, Path: "services/api/old.go", Kind: "file_change"})
	forcePushed.Links = append(forcePushed.Links, contract.LinkRecord{ID: "old-file-link", SourceID: "pr-1", Relation: "modifies_file", TargetID: oldFileID})
	if err := c.mapCurrentRefFiles(context.Background(), plan, r, &forcePushed); err != nil || len(forcePushed.Links) != 3 {
		t.Fatalf("arquivo de head anterior mapeado após force-push: %+v %v", forcePushed, err)
	}
}

func TestPRHeadFileMappingUsesManifestFromThatCommit(t *testing.T) {
	store, _ := storage.Open(t.TempDir())
	ss := secrets.NewSessionStore()
	ss.Put(context.Background(), contract.CredentialInput{Source: "github", Secret: "test-secret", Persistence: "session"})
	head := strings.Repeat("a", 40)
	treeSHA := strings.Repeat("b", 40)
	blobSHA := strings.Repeat("c", 40)
	calls := 0
	transport := transportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		var body string
		switch r.URL.Path {
		case "/repos/acme/api/git/commits/" + head:
			body = fmt.Sprintf(`{"sha":%q,"tree":{"sha":%q}}`, head, treeSHA)
		case "/repos/acme/api/git/trees/" + treeSHA:
			body = fmt.Sprintf(`{"sha":%q,"truncated":false,"tree":[{"path":"services/api/go.mod","mode":"100644","type":"blob","sha":%q}]}`, treeSHA, blobSHA)
		default:
			t.Fatalf("endpoint inesperado: %s", r.URL)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	observedAt := time.Date(2026, time.January, 20, 0, 0, 0, 0, time.UTC)
	c, err := NewCore(CoreConfig{Store: store, Secrets: ss, HTTPClient: &http.Client{Transport: transport}, Clock: func() time.Time { return observedAt }})
	if err != nil {
		t.Fatal(err)
	}
	plan := contract.Plan{ID: "pr-head-plan", InvestigationID: "investigation-pr-head", Input: contract.PlanInput{Limits: contract.Limits{MaxTransferBytes: 1 << 20, MaxLocalBytes: 1 << 20}}}
	if err := store.Put("planauth", plan.ID, planAuth{GitHub: "test-auth"}); err != nil {
		t.Fatal(err)
	}
	r := contract.Resource{Source: "github", ID: "42", Name: "acme/api"}
	run := contract.Run{ID: "pr-head-run"}
	fileID := prFileComponentID(r.ID, head, "services/api/handler.go")
	oldFileID := prFileComponentID(r.ID, strings.Repeat("d", 40), "services/api/old.go")
	d := query.Dataset{PullRequests: []query.PullRequest{{ID: "pr-7", Repository: r.Name, Number: 7, HeadSHA: head, UpdatedAt: "2026-01-10T00:00:00Z"}}, Components: []contract.ComponentRecord{{ID: "cmp-current", Repository: r.Name, Path: "services/other/go.mod", Kind: "go_module"}, {ID: fileID, Repository: r.Name, Path: "services/api/handler.go", Kind: "file_change"}, {ID: oldFileID, Repository: r.Name, Path: "services/api/old.go", Kind: "file_change"}}, Links: []contract.LinkRecord{{ID: "file-link-7", SourceID: "pr-7", Relation: "modifies_file", TargetID: fileID, EvidenceID: "ev-file-7"}, {ID: "old-file-link", SourceID: "pr-7", Relation: "modifies_file", TargetID: oldFileID}}}
	if err := c.mapPRHeadFiles(context.Background(), plan, r, &d, &run); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(d.Links) != 3 || d.Links[2].Relation != "maps_to_component" || d.Links[2].SourceID != fileID || d.Links[2].TargetID == "cmp-current" || d.Coverage[0].Status != "complete" {
		t.Fatalf("mapeamento temporal=%+v calls=%d", d, calls)
	}
	if len(d.Evidence) != 1 || d.Evidence[0].SourceUpdatedAt != "" || d.Links[2].ValidFrom != d.Evidence[0].ObservedAt {
		t.Fatalf("vínculo recebeu data inferida do PR: link=%+v evidências=%+v", d.Links[2], d.Evidence)
	}
	linked := false
	for _, component := range d.Components {
		if component.ID == d.Links[2].TargetID && component.Path == "services/api/go.mod" {
			linked = true
		}
	}
	if !linked {
		t.Fatalf("manifesto do head não encontrado: %+v", d.Components)
	}
	observedAt = observedAt.Add(24 * time.Hour)
	if err := c.mapPRHeadFiles(context.Background(), plan, r, &d, &run); err != nil || calls != 2 || len(d.Links) != 3 {
		t.Fatalf("cache da árvore não reutilizado: calls=%d links=%+v err=%v", calls, d.Links, err)
	}
	if d.Links[2].ValidFrom != d.Evidence[0].ObservedAt || d.Links[2].ValidFrom != "2026-01-20T00:00:00Z" {
		t.Fatalf("refresh mudou início da observação do vínculo: %+v", d.Links[2])
	}
	oldOnly := query.Dataset{PullRequests: d.PullRequests, Components: []contract.ComponentRecord{{ID: oldFileID, Repository: r.Name, Path: "services/api/old.go", Kind: "file_change"}}, Links: []contract.LinkRecord{{ID: "old-file-link", SourceID: "pr-7", Relation: "modifies_file", TargetID: oldFileID}}}
	if err := c.mapPRHeadFiles(context.Background(), plan, r, &oldOnly, &run); err != nil || len(oldOnly.Links) != 1 || oldOnly.Coverage[0].Status != "partial" {
		t.Fatalf("head novo sem inventário de arquivos tratado como completo: %+v %v", oldOnly, err)
	}
}

func TestPRHeadMappingRejectsCommitFromAnotherSHA(t *testing.T) {
	store, _ := storage.Open(t.TempDir())
	ss := secrets.NewSessionStore()
	ss.Put(context.Background(), contract.CredentialInput{Source: "github", Secret: "test-secret", Persistence: "session"})
	head := strings.Repeat("a", 40)
	other := strings.Repeat("b", 40)
	transport := transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/repos/acme/api/git/commits/"+head {
			t.Fatalf("árvore consultada após commit incompatível: %s", r.URL)
		}
		body := fmt.Sprintf(`{"sha":%q,"tree":{"sha":%q}}`, other, other)
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	c, err := NewCore(CoreConfig{Store: store, Secrets: ss, HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	plan := contract.Plan{ID: "bad-head-plan", InvestigationID: "investigation-bad-head", Input: contract.PlanInput{Limits: contract.Limits{MaxTransferBytes: 1 << 20, MaxLocalBytes: 1 << 20}}}
	if err := store.Put("planauth", plan.ID, planAuth{GitHub: "test-auth"}); err != nil {
		t.Fatal(err)
	}
	r := contract.Resource{Source: "github", ID: "42", Name: "acme/api"}
	run := contract.Run{ID: "bad-head-run"}
	fileID := prFileComponentID(r.ID, head, "services/api/handler.go")
	d := query.Dataset{PullRequests: []query.PullRequest{{ID: "pr-7", Repository: r.Name, Number: 7, HeadSHA: head}}, Components: []contract.ComponentRecord{{ID: fileID, Repository: r.Name, Path: "services/api/handler.go", Kind: "file_change"}}, Links: []contract.LinkRecord{{ID: "file-link-7", SourceID: "pr-7", Relation: "modifies_file", TargetID: fileID}}}
	if err := c.mapPRHeadFiles(context.Background(), plan, r, &d, &run); err != nil {
		t.Fatal(err)
	}
	if len(d.Links) != 1 || d.Coverage[0].Status != "partial" {
		t.Fatalf("commit incompatível gerou vínculo: %+v", d)
	}
	ids, err := store.List("gitcommits")
	if err != nil || len(ids) != 0 {
		t.Fatalf("commit incompatível entrou no cache: %v %v", ids, err)
	}
}

func TestActionsCollectsPagedRunsWithoutInferringDeployment(t *testing.T) {
	store, _ := storage.Open(t.TempDir())
	ss := secrets.NewSessionStore()
	ss.Put(context.Background(), contract.CredentialInput{Source: "github", Secret: "test-secret", Persistence: "session"})
	calls := 0
	transport := transportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Header.Get("Authorization") != "Bearer test-secret" {
			t.Fatal("GitHub sem Bearer")
		}
		headers := make(http.Header)
		body := `{"workflow_runs":[{"id":10,"run_attempt":1,"head_sha":"aaa","status":"completed","conclusion":"success","created_at":"2026-01-03T00:00:00Z","updated_at":"2026-01-03T00:01:00Z"}]}`
		if calls == 1 {
			headers.Set("Link", `<https://api.github.com/repos/acme/api/actions/runs?per_page=100&page=2>; rel="next"`)
		} else {
			body = `{"workflow_runs":[{"id":11,"run_attempt":1,"head_sha":"bbb","status":"completed","conclusion":"failure","created_at":"2026-01-04T00:00:00Z","updated_at":"2026-01-04T00:01:00Z"}]}`
		}
		return &http.Response{StatusCode: 200, Header: headers, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	c, err := NewCore(CoreConfig{Store: store, Secrets: ss, HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	plan := contract.Plan{ID: "ci-plan", WindowStart: "2026-01-01T00:00:00Z", WindowEnd: "2026-02-01T00:00:00Z", Input: contract.PlanInput{Limits: contract.Limits{MaxTransferBytes: 1 << 20}}}
	r := contract.Resource{Source: "github", ID: "42", Name: "acme/api"}
	run := contract.Run{ID: "ci-run"}
	d := query.Dataset{}
	if err := c.collectActions(context.Background(), plan, r, &d, &run); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(d.Events) != 2 || len(d.Evidence) != 2 || len(d.Deliveries) != 0 || d.Coverage[0].Status != "complete" || run.Requests != 2 {
		t.Fatalf("CI=%+v run=%+v calls=%d", d, run, calls)
	}
	retry := query.Dataset{}
	if err := c.collectActions(context.Background(), plan, r, &retry, &run); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(retry.Events) != 2 {
		t.Fatalf("journal CI não reutilizado: calls=%d retry=%+v", calls, retry)
	}
}

func TestChecksCollectsPagedRunsAtPRHead(t *testing.T) {
	store, _ := storage.Open(t.TempDir())
	ss := secrets.NewSessionStore()
	ss.Put(context.Background(), contract.CredentialInput{Source: "github", Secret: "test-secret", Persistence: "session"})
	sha := strings.Repeat("a", 40)
	calls := 0
	transport := transportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Header.Get("Authorization") != "Bearer test-secret" || r.URL.Path != "/repos/acme/api/commits/"+sha+"/check-runs" {
			t.Fatalf("request inesperado: %s", r.URL)
		}
		headers := make(http.Header)
		body := fmt.Sprintf(`{"check_runs":[{"id":10,"head_sha":%q,"name":"build","status":"completed","conclusion":"success","started_at":"2026-01-03T00:00:00Z","completed_at":"2026-01-03T00:01:00Z"}]}`, sha)
		if calls == 1 {
			headers.Set("Link", fmt.Sprintf(`<https://api.github.com/repos/acme/api/commits/%s/check-runs?per_page=100&page=2>; rel="next"`, sha))
		} else {
			body = fmt.Sprintf(`{"check_runs":[{"id":11,"head_sha":%q,"name":"lint","status":"completed","conclusion":"failure","started_at":"2026-01-04T00:00:00Z","completed_at":"2026-01-04T00:01:00Z"}]}`, sha)
		}
		return &http.Response{StatusCode: 200, Header: headers, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	c, err := NewCore(CoreConfig{Store: store, Secrets: ss, HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	plan := contract.Plan{ID: "checks-plan", WindowStart: "2026-01-01T00:00:00Z", WindowEnd: "2026-02-01T00:00:00Z", Input: contract.PlanInput{Limits: contract.Limits{MaxTransferBytes: 1 << 20}}}
	r := contract.Resource{Source: "github", ID: "42", Name: "acme/api"}
	run := contract.Run{ID: "checks-run"}
	d := query.Dataset{PullRequests: []query.PullRequest{{ID: "github-pr-1", Repository: r.Name, Number: 1, HeadSHA: sha}}}
	if err := c.collectChecks(context.Background(), plan, r, &d, &run); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(d.Events) != 2 || len(d.Evidence) != 2 || len(d.Deliveries) != 0 || d.Coverage[0].Status != "complete" || run.Requests != 2 || d.Events[0].EntityID != "github-pr-1" {
		t.Fatalf("checks=%+v run=%+v calls=%d", d, run, calls)
	}
	retry := query.Dataset{PullRequests: d.PullRequests}
	if err := c.collectChecks(context.Background(), plan, r, &retry, &run); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(retry.Events) != 2 {
		t.Fatalf("journal checks não reutilizado: calls=%d retry=%+v", calls, retry)
	}
}

func TestDeploymentSuccessIsDeclaredProvenance(t *testing.T) {
	store, _ := storage.Open(t.TempDir())
	ss := secrets.NewSessionStore()
	ss.Put(context.Background(), contract.CredentialInput{Source: "github", Secret: "test-secret", Persistence: "session"})
	sha := strings.Repeat("a", 40)
	calls := 0
	transport := transportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		body := ""
		switch r.URL.Path {
		case "/repos/acme/api/deployments":
			body = fmt.Sprintf(`[{"id":17,"sha":%q,"environment":"production","created_at":"2026-01-03T00:00:00Z","updated_at":"2026-01-03T00:01:00Z"}]`, sha)
		case "/repos/acme/api/deployments/17/statuses":
			body = `[{"id":19,"state":"success","created_at":"2026-01-03T00:05:00Z"}]`
		default:
			t.Fatalf("endpoint inesperado: %s", r.URL)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	c, err := NewCore(CoreConfig{Store: store, Secrets: ss, HTTPClient: &http.Client{Transport: transport}, Clock: func() time.Time { return time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	plan := contract.Plan{ID: "deploy-plan", WindowStart: "2026-01-01T00:00:00Z", WindowEnd: "2026-02-01T00:00:00Z", Input: contract.PlanInput{Limits: contract.Limits{MaxTransferBytes: 1 << 20}}}
	r := contract.Resource{Source: "github", ID: "42", Name: "acme/api"}
	run := contract.Run{ID: "deploy-run"}
	d := query.Dataset{}
	if err := c.collectDeployments(context.Background(), plan, r, &d, &run); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(d.Events) != 2 || len(d.Deliveries) != 1 || d.Deliveries[0].ProvenanceStatus != "declared" || d.Deliveries[0].RevisionSHA != sha || d.Coverage[0].Status != "complete" {
		t.Fatalf("deployment=%+v calls=%d", d, calls)
	}
}

func TestRuntimeImportIsTypedAndPinnedByPlan(t *testing.T) {
	store, _ := storage.Open(t.TempDir())
	c, err := NewCore(CoreConfig{Store: store, Clock: func() time.Time { return time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	input := contract.PlanInput{SchemaVersion: contract.Version, Name: "runtime", Period: contract.Period{Preset: "30d", Timezone: "UTC"}, Modules: contract.Modules{RuntimeImport: true}}
	first, err := c.PreparePlan(context.Background(), input)
	if err != nil || first.Resources[0].ID != "runtime-pending" {
		t.Fatalf("plano pendente: %+v %v", first, err)
	}
	raw := []byte(`{"schema_version":"pocketcli.runtime.v1","observed_at":"2026-01-08T00:00:00Z","environment":"production","components":[{"name":"api","repository":"acme/api","path":"services/api/go.mod","enabled":true,"revision_sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},{"name":"worker","enabled":false}]}`)
	if _, err := c.ImportRuntime(context.Background(), first.InvestigationID, []byte(`{"schema_version":"pocketcli.runtime.v1","schema_version":"pocketcli.runtime.v1"}`)); err == nil {
		t.Fatal("chave duplicada aceita")
	}
	if _, err := c.ImportRuntime(context.Background(), first.InvestigationID, []byte(`{"schema_version":"pocketcli.runtime.v1","secret":"x"}`)); err == nil {
		t.Fatal("campo secreto aceito")
	}
	if _, err := c.ImportRuntime(context.Background(), first.InvestigationID, []byte(`{"schema_version":"pocketcli.runtime.v1","observed_at":"2026-01-08T00:00:00Z","environment":"production","components":[{"name":"api","enabled":false},{"name":"api","enabled":true}]}`)); err == nil {
		t.Fatal("componentes contraditórios com o mesmo nome aceitos")
	}
	hash, err := c.ImportRuntime(context.Background(), first.InvestigationID, raw)
	if err != nil {
		t.Fatal(err)
	}
	input.InvestigationID = first.InvestigationID
	second, err := c.PreparePlan(context.Background(), input)
	if err != nil || second.Resources[0].ID != hash || second.Hash == first.Hash {
		t.Fatalf("plano importado: %+v %v", second, err)
	}
	d := query.Dataset{Components: []contract.ComponentRecord{{ID: "file-api", Name: "go.mod", Repository: "acme/api", Path: "services/api/go.mod", Kind: "file_change", DeploymentStatus: "unknown"}, {ID: "cmp-api", Name: "api", Repository: "acme/api", Path: "services/api/go.mod", Kind: "go_module", DeploymentStatus: "unknown"}}}
	run := contract.Run{ID: "runtime-run"}
	if err := c.collectRuntime(context.Background(), second.Resources[0], &d, &run); err != nil {
		t.Fatal(err)
	}
	if len(d.Components) != 3 || d.Components[0].DeploymentStatus != "unknown" || d.Components[1].DeploymentStatus != "observed" || len(d.Deliveries) != 1 || d.Deliveries[0].ComponentID != "cmp-api" || len(d.Events) != 2 || d.Coverage[0].Status != "complete" {
		t.Fatalf("runtime=%+v", d)
	}
	old := query.Dataset{}
	if err := c.collectRuntime(context.Background(), first.Resources[0], &old, &run); err != nil || old.Coverage[0].Status != "pending" || len(old.Components) != 0 {
		t.Fatalf("plano antigo alterado: %+v %v", old, err)
	}
}

func TestLinearHistoryAndCommentsPaginateWithEvidence(t *testing.T) {
	store, _ := storage.Open(t.TempDir())
	ss := secrets.NewSessionStore()
	ss.Put(context.Background(), contract.CredentialInput{Source: "linear", Secret: "test-linear", Persistence: "session"})
	calls := make(map[string]int)
	transport := transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "test-linear" {
			t.Fatal("Linear sem chave")
		}
		body, _ := io.ReadAll(r.Body)
		var input struct {
			Query     string            `json:"query"`
			Variables map[string]string `json:"variables"`
		}
		if json.Unmarshal(body, &input) != nil {
			t.Fatal("query inválida")
		}
		kind := "issues"
		response := `{"data":{"issues":{"nodes":[{"id":"issue-1","identifier":"T-1","title":"Ticket","createdAt":"2026-01-02T00:00:00Z","updatedAt":"2026-01-08T00:00:00Z","startedAt":"2026-01-03T00:00:00Z","state":{"name":"In Progress","type":"started"},"project":{"id":"project-1","name":"API"}}],"pageInfo":{"hasNextPage":false}}}}`
		if strings.Contains(input.Query, "IssueFields") {
			kind = "schema"
			response = `{"data":{"__type":{"fields":[{"name":"history"},{"name":"comments"}]}}}`
		} else if strings.Contains(input.Query, "IssueHistory") {
			kind = "history"
			response = `{"data":{"issue":{"id":"issue-1","history":{"nodes":[{"id":"history-1","createdAt":"2026-01-04T00:00:00Z","fromState":{"name":"Done","type":"completed"},"toState":{"name":"In Progress","type":"started"},"fromEstimate":3,"toEstimate":5,"actor":{"id":"user-1"}}],"pageInfo":{"hasNextPage":false}}}}}`
		} else if strings.Contains(input.Query, "IssueComments") {
			kind = "comments"
			response = `{"data":{"issue":{"id":"issue-1","comments":{"nodes":[{"id":"comment-1","body":"revisado","createdAt":"2026-01-05T00:00:00Z","updatedAt":"2026-01-06T00:00:00Z","user":{"id":"user-2"}}],"pageInfo":{"hasNextPage":false}}}}}`
		} else if strings.Contains(input.Query, "TeamProjects") {
			kind = "projects"
			response = `{"data":{"projects":{"nodes":[{"id":"project-1","name":"API","createdAt":"2026-01-02T00:00:00Z","updatedAt":"2026-01-07T00:00:00Z"},{"id":"project-2","name":"Sem issues","createdAt":"2026-01-02T00:00:00Z","updatedAt":"2026-01-07T00:00:00Z"}],"pageInfo":{"hasNextPage":false}}}}`
		}
		calls[kind]++
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(response))}, nil
	})
	c, err := NewCore(CoreConfig{Store: store, Secrets: ss, HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	plan := contract.Plan{ID: "linear-plan", InvestigationID: "linear-investigation", WindowStart: "2026-01-01T00:00:00Z", WindowEnd: "2026-02-01T00:00:00Z", Input: contract.PlanInput{LinearEnabled: true, Limits: contract.Limits{MaxTransferBytes: 1 << 20}}}
	r := contract.Resource{Source: "linear", ID: "team-1", Name: "T"}
	run := contract.Run{ID: "linear-run"}
	d := query.Dataset{}
	if err := c.collectTeam(context.Background(), plan, r, &d, &run); err != nil {
		t.Fatal(err)
	}
	if calls["issues"] != 1 || calls["schema"] != 1 || calls["history"] != 1 || calls["comments"] != 1 || calls["projects"] != 1 || len(d.Issues) != 1 || d.Issues[0].State != "started" || len(d.Estimates) != 1 || d.Estimates[0].Timing != "after_start" || len(d.Events) != 2 || d.Events[0].OldValue != "completed" || d.Events[0].NewValue != "started" || len(d.Evidence) != 5 || len(d.Components) != 2 || len(d.Links) != 1 {
		t.Fatalf("linear=%+v calls=%+v", d, calls)
	}
	for _, coverage := range d.Coverage {
		if coverage.Collection == "history" || coverage.Collection == "comments" {
			if coverage.Status != "complete" {
				t.Fatalf("cobertura Linear: %+v", coverage)
			}
		}
		if coverage.Collection == "relations" || coverage.Collection == "inverseRelations" {
			if coverage.Status != "unsupported" {
				t.Fatalf("campo de relação ausente virou cobertura completa: %+v", coverage)
			}
		}
	}
}

func TestYAMLAndFormProduceSamePlanHash(t *testing.T) {
	store, _ := storage.Open(t.TempDir())
	secretStore := secrets.NewSessionStore()
	secretStore.Put(context.Background(), contract.CredentialInput{Source: "github", Secret: "test-secret", Persistence: "session"})
	c, err := NewCore(CoreConfig{Store: store, Secrets: secretStore, HTTPClient: &http.Client{Transport: &fakeTransport{}}, Clock: func() time.Time { return time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	content := "schema_version: investigate.v1\nname: example\nsources:\n  github: true\n  linear: false\nscope:\n  mode: selected\n  repositories: [acme/api]\nperiod:\n  preset: 30d\nlimits:\n  github_requests_per_hour: 3600000\n"
	var plans []contract.Plan
	for _, filename := range []string{"scope.yml", "scope.yaml"} {
		parsed, err := c.Setup().Parse(context.Background(), contract.SetupParseRequest{Filename: filename, Content: content})
		if err != nil {
			t.Fatal(err)
		}
		plan, err := c.PreparePlan(context.Background(), parsed.Input)
		if err != nil {
			t.Fatal(err)
		}
		plans = append(plans, plan)
	}
	form := plans[0].Input
	form.InvestigationID = ""
	form.Period.Timezone = "UTC"
	plan, err := c.PreparePlan(context.Background(), form)
	if err != nil {
		t.Fatal(err)
	}
	plans = append(plans, plan)
	for _, p := range plans[1:] {
		if p.Hash != plans[0].Hash || p.ID == plans[0].ID || p.InvestigationID == plans[0].InvestigationID {
			t.Fatalf("plans not normalized: first=%+v other=%+v", plans[0], p)
		}
	}
}

func TestTextReferenceCreatesOnlyMentionForExactCollectedPR(t *testing.T) {
	store, _ := storage.Open(t.TempDir())
	c, err := NewCore(CoreConfig{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	for id, body := range map[string]string{
		"ev-issue-one": `{"body":"Implementado em https://github.com/acme/api/pull/7"}`,
		"ev-issue-two": `{"body":"Talvez PR #70"}`,
		"ev-pr":        `{"body":"Sem referência de issue"}`,
	} {
		if err := store.Put("objects", id, json.RawMessage(body)); err != nil {
			t.Fatal(err)
		}
	}
	d := query.Dataset{Issues: []query.Issue{{ID: "issue-1", Identifier: "T-1", EvidenceID: "ev-issue-one"}, {ID: "issue-2", Identifier: "T-2", EvidenceID: "ev-issue-two"}}, PullRequests: []query.PullRequest{{ID: "pr-7", Repository: "acme/api", Number: 7, EvidenceID: "ev-pr"}}, Coverage: []contract.Coverage{{Source: "github", Collection: "pull_requests", Status: "complete"}, {Source: "linear", Collection: "issues", Status: "complete"}}}
	if err := c.linkDataset(contract.Plan{InvestigationID: "inv-1"}, &d); err != nil {
		t.Fatal(err)
	}
	if len(d.Links) != 1 || d.Links[0].SourceID != "issue-1" || d.Links[0].TargetID != "pr-7" || d.Links[0].Relation != "mentions" || d.Coverage[len(d.Coverage)-1].Status != "partial" {
		t.Fatalf("links=%+v coverage=%+v", d.Links, d.Coverage)
	}
}

func TestIssueCommentCreatesDirectPRMention(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCore(CoreConfig{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	for id, raw := range map[string]string{
		"ev-issue":   `{"body":"No link here"}`,
		"ev-pr":      `{"body":"No issue here"}`,
		"ev-comment": `{"body":"See PR #7","issue_url":"https://api.github.com/repos/acme/api/issues/8"}`,
	} {
		if err := store.Put("objects", id, json.RawMessage(raw)); err != nil {
			t.Fatal(err)
		}
	}
	d := query.Dataset{Issues: []query.Issue{{ID: "issue-8", Identifier: "acme/api#8", EvidenceID: "ev-issue"}}, PullRequests: []query.PullRequest{{ID: "pr-7", Repository: "acme/api", Number: 7, EvidenceID: "ev-pr"}}, Evidence: []contract.Evidence{{ID: "ev-comment"}}, Coverage: []contract.Coverage{{Source: "github", Collection: "pull_requests", Status: "complete"}, {Source: "github", Collection: "issues", Status: "complete"}, {Source: "github", Collection: "comments", Status: "complete"}}}
	if err := c.linkDataset(contract.Plan{InvestigationID: "inv-comments"}, &d); err != nil {
		t.Fatal(err)
	}
	if len(d.Links) != 1 || d.Links[0].EvidenceID != "ev-comment" || d.Links[0].SourceID != "issue-8" || d.Links[0].TargetID != "pr-7" || d.Links[0].Relation != "mentions" {
		t.Fatalf("comment link=%+v", d.Links)
	}
}

func TestFixesKeywordCreatesDeclaredFixLinkOnlyForExactIssue(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCore(CoreConfig{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	for id, raw := range map[string]string{
		"ev-issue":   `{"body":"Issue details"}`,
		"ev-fixes":   `{"body":"Fixes #8"}`,
		"ev-closes":  `{"body":"Closes #8"}`,
		"ev-vague":   `{"body":"fix the issue and touch the same file"}`,
		"ev-foreign": `{"body":"Fixes #80"}`,
	} {
		if err := store.Put("objects", id, json.RawMessage(raw)); err != nil {
			t.Fatal(err)
		}
	}
	d := query.Dataset{
		Snapshot: contract.Snapshot{WindowStart: "2026-01-01T00:00:00Z", WindowEnd: "2026-02-01T00:00:00Z"},
		Issues:   []query.Issue{{ID: "issue-8", Identifier: "acme/api#8", Source: "github", EvidenceID: "ev-issue"}},
		PullRequests: []query.PullRequest{
			{ID: "pr-fixes", Repository: "acme/api", Number: 7, MergedAt: "2026-01-20T00:00:00Z", EvidenceID: "ev-fixes"},
			{ID: "pr-closes", Repository: "acme/api", Number: 9, MergedAt: "2026-01-21T00:00:00Z", EvidenceID: "ev-closes"},
			{ID: "pr-vague", Repository: "acme/api", Number: 10, MergedAt: "2026-01-22T00:00:00Z", EvidenceID: "ev-vague"},
			{ID: "pr-foreign", Repository: "acme/api", Number: 11, MergedAt: "2026-01-23T00:00:00Z", EvidenceID: "ev-foreign"},
		},
		Events:   []query.Event{{ID: "reopened", EntityID: "issue-8", Type: "reopened", OldValue: "done", NewValue: "open", EventAt: "2026-01-10T00:00:00Z", EvidenceID: "ev-reopen"}},
		Coverage: []contract.Coverage{{Source: "github", Collection: "pull_requests", Status: "complete"}, {Source: "github", Collection: "issues", Status: "complete"}},
	}
	if err := c.linkDataset(contract.Plan{InvestigationID: "inv-fixes"}, &d); err != nil {
		t.Fatal(err)
	}
	declared, generic := 0, 0
	for _, link := range d.Links {
		if link.Relation == "declares_fix_of" {
			declared++
			if link.SourceID != "pr-fixes" || link.TargetID != "issue-8" || link.EvidenceID != "ev-fixes" {
				t.Fatalf("wrong fix declaration: %+v", link)
			}
		}
		if link.Relation == "mentions" {
			generic++
		}
	}
	if declared != 1 || generic != 2 {
		t.Fatalf("fix declaration confused with generic reference: links=%+v", d.Links)
	}
	analyzed := rules.Evaluate(d)
	corrections := 0
	for _, record := range analyzed.Rework {
		if record.Kind == "declared_correction" {
			corrections++
		}
	}
	if corrections != 1 {
		t.Fatalf("explicit Fixes link did not reach rework analysis: %+v", analyzed.Rework)
	}
}

func TestLinearTeamDiscoveryFollowsCursor(t *testing.T) {
	store, _ := storage.Open(t.TempDir())
	secretStore := secrets.NewSessionStore()
	secretStore.Put(context.Background(), contract.CredentialInput{Source: "linear", Secret: "linear-secret", Persistence: "session"})
	calls := 0
	transport := transportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		body, _ := io.ReadAll(r.Body)
		payload := `{"data":{"teams":{"nodes":[{"id":"a","key":"A","name":"A"}],"pageInfo":{"hasNextPage":true,"endCursor":"cursor-a"}}}}`
		if strings.Contains(string(body), `"after":"cursor-a"`) {
			payload = `{"data":{"teams":{"nodes":[{"id":"b","key":"B","name":"B"}],"pageInfo":{"hasNextPage":false,"endCursor":"cursor-b"}}}}`
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(payload))}, nil
	})
	c, err := NewCore(CoreConfig{Store: store, Secrets: secretStore, HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := c.PreparePlan(context.Background(), contract.PlanInput{SchemaVersion: contract.Version, LinearEnabled: true, Scope: &contract.Scope{Mode: "all_authorized"}, Period: contract.Period{Preset: "30d", Timezone: "UTC"}, Limits: contract.Limits{LinearRPH: 3600000}})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(plan.Resources) != 2 || plan.Resources[0].ID != "a" || plan.Resources[1].ID != "b" {
		t.Fatalf("calls=%d resources=%+v", calls, plan.Resources)
	}
}

func TestPlanRejectsDifferentCredentialOnStartAndReuse(t *testing.T) {
	store, _ := storage.Open(t.TempDir())
	secretStore := secrets.NewSessionStore()
	secretStore.Put(context.Background(), contract.CredentialInput{Source: "github", Secret: "token-one", Persistence: "session"})
	transport := transportFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"id":42,"full_name":"acme/api"}`))}, nil
	})
	c, err := NewCore(CoreConfig{Store: store, Secrets: secretStore, HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	input := contract.PlanInput{SchemaVersion: contract.Version, GitHubEnabled: true, Scope: &contract.Scope{Mode: "selected", Repositories: []string{"acme/api"}}, Period: contract.Period{Preset: "30d", Timezone: "UTC"}, Limits: contract.Limits{GitHubRPH: 3600000}}
	first, err := c.PreparePlan(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	secretStore.Put(context.Background(), contract.CredentialInput{Source: "github", Secret: "token-two", Persistence: "session"})
	if _, err := c.StartRun(context.Background(), contract.RunRequest{PlanID: first.ID, ConfirmedPlanHash: first.Hash, IdempotencyKey: "auth-change"}); err == nil || !strings.Contains(err.Error(), "ERR_PLAN_CHANGED") {
		t.Fatalf("start with changed credential: %v", err)
	}
	input.InvestigationID = first.InvestigationID
	second, err := c.PreparePlan(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	canReuse, err := c.canReuse(first.ID, second.ID)
	if err != nil || canReuse || first.Hash == second.Hash {
		t.Fatalf("reuse=%v err=%v hashes=%q,%q", canReuse, err, first.Hash, second.Hash)
	}
}

func TestCorruptSnapshotDoesNotDisappearFromHistory(t *testing.T) {
	store, _ := storage.Open(t.TempDir())
	path, err := store.Path("snapshots", "broken")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{invalid"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := NewCore(CoreConfig{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.ListSnapshots(context.Background(), "inv"); err == nil || !strings.Contains(err.Error(), "ERR_AUDIT_FAILED") {
		t.Fatalf("corrupt snapshot silently skipped: %v", err)
	}
}

func TestEvidenceCacheRejectsCorruptionWithoutOverwriting(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCore(CoreConfig{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	raw := json.RawMessage("{ \"id\": 7, \"title\": \"example\" }")
	first, err := c.evidence("github", "acme/api#7", "2026-01-01T00:00:00Z", raw)
	if err != nil {
		t.Fatal(err)
	}
	second, err := c.evidence("github", "acme/api#7", "2026-01-01T00:00:00Z", raw)
	if err != nil || second.ID != first.ID {
		t.Fatalf("evidência idêntica não reutilizada: %+v %v", second, err)
	}
	path, err := store.Path("objects", first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := c.evidence("github", "acme/api#7", "2026-01-01T00:00:00Z", raw); err == nil || !strings.Contains(err.Error(), "ERR_AUDIT_FAILED") {
		t.Fatalf("objeto corrompido aceito: %v", err)
	}
	stored, err := os.ReadFile(path)
	if err != nil || string(stored) != "corrupt" {
		t.Fatalf("objeto corrompido sobrescrito: %q %v", stored, err)
	}
}

func TestRefreshDoesNotCarryRemovedResource(t *testing.T) {
	store, _ := storage.Open(t.TempDir())
	secretStore := secrets.NewSessionStore()
	secretStore.Put(context.Background(), contract.CredentialInput{Source: "github", Secret: "test-secret", Persistence: "session"})
	transport := transportFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`[]`))}, nil
	})
	c, err := NewCore(CoreConfig{Store: store, Secrets: secretStore, HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	prior := contract.Snapshot{ID: "snap-prior", PlanID: "plan-prior", InvestigationID: "inv-scope", Resources: []contract.Resource{{Source: "github", ID: "1", Name: "acme/api"}}}
	if err := store.Put("snapshots", prior.ID, prior); err != nil {
		t.Fatal(err)
	}
	if err := store.Put("datasets", prior.ID, query.Dataset{Issues: []query.Issue{{ID: "old", Identifier: "acme/api#1", Source: "github"}}}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"plan-prior", "plan-current"} {
		if err := store.Put("planauth", id, planAuth{GitHub: "same-credential"}); err != nil {
			t.Fatal(err)
		}
	}
	current := contract.Plan{ID: "plan-current", InvestigationID: "inv-scope", Resources: []contract.Resource{{Source: "github", ID: "2", Name: "other/repo"}}, Input: contract.PlanInput{Limits: contract.Limits{MaxTransferBytes: 1 << 20}}}
	d, err := c.collectDataset(context.Background(), current, &contract.Run{ID: "run-scope"})
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Issues) != 0 || len(d.Evidence) != 0 {
		t.Fatalf("removed resource survived refresh: issues=%+v evidence=%+v", d.Issues, d.Evidence)
	}
}
