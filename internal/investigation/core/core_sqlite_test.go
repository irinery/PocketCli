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
	priors []gitmirror.RefState
	result gitmirror.Observation
}

func (f *fakeGitObserver) Observe(_ context.Context, _ string, _ []byte, prior gitmirror.RefState, _ int64) (gitmirror.Observation, error) {
	f.priors = append(f.priors, prior)
	f.result.OldOID = prior.OID
	return f.result, nil
}

func (f *fakeFinalizer) Finalize(ctx context.Context, in contract.FinalizeRequest) (contract.ReportArtifact, error) {
	f.calls.Add(1)
	page, err := f.core.ReadReportPage(ctx, in.SnapshotID, "", 1000)
	if err != nil {
		return contract.ReportArtifact{}, err
	}
	count := int64(0)
	for _, r := range page.Records {
		if r.Kind == "finding" {
			count++
		}
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
		if r.State == "report_failed" || r.State == "failed" || r.State == "completed" {
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
	transport := &fakeTransport{title: "Original test-secret"}
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
	if old.Rows[0][0].Value != "Original [REDACTED]" {
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

func TestFinalizerTransitionsRunToCompleted(t *testing.T) {
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
	if run.State != "completed" || finalizer.calls.Load() != 1 {
		t.Fatalf("run=%+v finalizer calls=%d", run, finalizer.calls.Load())
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
	plan := contract.Plan{ID: "plan-journal", Input: contract.PlanInput{Limits: contract.Limits{MaxTransferBytes: 1 << 20}}}
	resource := contract.Resource{Source: "github", ID: "42", Name: "acme/api"}
	run := contract.Run{ID: "run-journal"}
	first := &query.Dataset{}
	if err := c.collectRepository(context.Background(), plan, resource, contract.Snapshot{}, first, &run); err != nil {
		t.Fatal(err)
	}
	if pageOne != 1 || pageTwo != 1 || len(first.PullRequests) != 1 || first.Coverage[0].Status != "unauthorized" {
		t.Fatalf("first pass pages=%d,%d prs=%d coverage=%+v", pageOne, pageTwo, len(first.PullRequests), first.Coverage)
	}
	second := &query.Dataset{}
	if err := c.collectRepository(context.Background(), plan, resource, contract.Snapshot{}, second, &run); err != nil {
		t.Fatal(err)
	}
	if pageOne != 1 || pageTwo != 2 || len(second.PullRequests) != 2 || second.Coverage[0].Status != "partial" {
		t.Fatalf("resume pages=%d,%d prs=%d coverage=%+v", pageOne, pageTwo, len(second.PullRequests), second.Coverage)
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
	if firstCalls != 1 || secondCalls != 2 || len(second.Issues) != 2 || second.Coverage[0].Status != "complete" {
		t.Fatalf("resume calls=%d,%d issues=%d coverage=%+v", firstCalls, secondCalls, len(second.Issues), second.Coverage)
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
	retry := query.Dataset{}
	if err := c.collectManifestPaths(context.Background(), plan, r, treeOID, "identity", "2026-01-10T00:00:00Z", &retry, &run); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(retry.Components) != 1 || retry.Events[0].EvidenceID != d.Events[0].EvidenceID {
		t.Fatalf("cache de blob inválido: retry=%+v calls=%d", retry, calls)
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
	priorData := query.Dataset{PullRequests: []query.PullRequest{{ID: "github-pr-100", Number: 7, Repository: r.Name, State: "open", CreatedAt: "2026-01-02T00:00:00Z", UpdatedAt: "2026-01-03T00:00:00Z", EvidenceID: "ev-pr"}}, Components: []contract.ComponentRecord{{ID: "file-old", Name: "api.go", Repository: r.Name, Path: "api.go", Kind: "file_change"}}, Links: []contract.LinkRecord{{ID: "link-file-old", SourceID: "github-pr-100", Relation: "modifies_file", TargetID: "file-old", Status: "direct", EvidenceID: "ev-file"}}, Evidence: []contract.Evidence{{ID: "ev-pr", Source: "github", SourceUpdatedAt: "2026-01-03T00:00:00Z"}, {ID: "ev-file", Source: "github", SourceUpdatedAt: "2026-01-03T00:00:00Z"}}}
	if err := store.Put("snapshots", prior.ID, prior); err != nil {
		t.Fatal(err)
	}
	if err := store.Put("datasets", prior.ID, priorData); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"ev-pr", "ev-file"} {
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
	fileFound, linkFound, evidenceFound := false, false, false
	for _, component := range d.Components {
		fileFound = fileFound || component.ID == "file-old"
	}
	for _, link := range d.Links {
		linkFound = linkFound || link.ID == "link-file-old"
	}
	for _, evidence := range d.Evidence {
		evidenceFound = evidenceFound || evidence.ID == "ev-file"
	}
	if !fileFound || !linkFound || !evidenceFound {
		t.Fatalf("observação anterior desapareceu: files=%v links=%v evidence=%v", fileFound, linkFound, evidenceFound)
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
	if len(d.Links) != 2 || d.Links[1].Relation != "maps_to_component" || d.Links[1].TargetID != "cmp-api" || d.Links[1].Status != "direct" || len(d.Evidence) != 1 || d.Coverage[0].Status != "partial" {
		t.Fatalf("mapeamento exato=%+v", d)
	}
	other := makeDataset(strings.Repeat("b", 40))
	if err := c.mapCurrentRefFiles(context.Background(), plan, r, &other); err != nil || len(other.Links) != 1 {
		t.Fatalf("versão diferente mapeada: %+v %v", other, err)
	}
	ambiguous := makeDataset(oid)
	ambiguous.Components = append(ambiguous.Components, contract.ComponentRecord{ID: "cmp-api-container", Repository: r.Name, Path: "services/api/Dockerfile", Kind: "container"})
	if err := c.mapCurrentRefFiles(context.Background(), plan, r, &ambiguous); err != nil || len(ambiguous.Links) != 1 {
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
	c, err := NewCore(CoreConfig{Store: store, Secrets: ss, HTTPClient: &http.Client{Transport: transport}})
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
	linked := false
	for _, component := range d.Components {
		if component.ID == d.Links[2].TargetID && component.Path == "services/api/go.mod" {
			linked = true
		}
	}
	if !linked {
		t.Fatalf("manifesto do head não encontrado: %+v", d.Components)
	}
	if err := c.mapPRHeadFiles(context.Background(), plan, r, &d, &run); err != nil || calls != 2 || len(d.Links) != 3 {
		t.Fatalf("cache da árvore não reutilizado: calls=%d links=%+v err=%v", calls, d.Links, err)
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

func TestLinksRequireExactCollectedPRReference(t *testing.T) {
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
	if len(d.Links) != 1 || d.Links[0].SourceID != "issue-1" || d.Links[0].TargetID != "pr-7" || d.Coverage[len(d.Coverage)-1].Status != "complete" {
		t.Fatalf("links=%+v coverage=%+v", d.Links, d.Coverage)
	}
}

func TestIssueCommentCreatesDirectPRLink(t *testing.T) {
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
	if len(d.Links) != 1 || d.Links[0].EvidenceID != "ev-comment" || d.Links[0].SourceID != "issue-8" || d.Links[0].TargetID != "pr-7" {
		t.Fatalf("comment link=%+v", d.Links)
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
