//go:build investigate_sqlite && cgo

package core

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/query"
	"pocketcli/internal/investigation/rules"
	"pocketcli/internal/investigation/secrets"
	"pocketcli/internal/investigation/storage"
)

func TestGitHubReviewsAndTimelineFeedDeterministicRules(t *testing.T) {
	responses := map[string]string{
		"/repos/acme/api/pulls":             `[ {"id":100,"number":7,"title":"PR","state":"open","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-05T00:00:00Z","user":{"id":1}} ]`,
		"/repos/acme/api/issues":            `[ {"id":200,"number":8,"title":"Issue","state":"open","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-05T00:00:00Z"} ]`,
		"/repos/acme/api/pulls/7/reviews":   `[ {"id":14,"submitted_at":"2026-01-01T12:00:00Z","state":"COMMENTED","user":{"id":2,"type":"User"}}, {"id":11,"submitted_at":"2026-01-02T06:00:00Z","state":"APPROVED","user":{"id":1,"type":"User"}}, {"id":12,"submitted_at":"2026-01-02T12:00:00Z","state":"APPROVED","user":{"id":3,"type":"Bot"}}, {"id":13,"submitted_at":"2026-01-03T00:00:00Z","state":"CHANGES_REQUESTED","user":{"id":2,"type":"User"}} ]`,
		"/repos/acme/api/issues/7/timeline": `[ {"sha":"abc","event":"committed","created_at":"2026-01-02T00:00:00Z"}, {"event":"cross-referenced","created_at":"2026-01-02T00:00:00Z"}, {"id":21,"event":"ready_for_review","created_at":"2026-01-02T00:00:00Z","actor":{"id":1}} ]`,
		"/repos/acme/api/issues/8/timeline": `[ {"id":22,"event":"reopened","created_at":"2026-01-04T00:00:00Z","actor":{"id":2}} ]`,
	}
	transport := transportFunc(func(r *http.Request) (*http.Response, error) {
		body, ok := responses[r.URL.Path]
		if !ok {
			if strings.HasSuffix(r.URL.Path, "/comments") {
				body = "[]"
			} else {
				t.Errorf("unexpected endpoint %s", r.URL)
				body = "[]"
			}
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ss := secrets.NewSessionStore()
	ss.Put(context.Background(), contract.CredentialInput{Source: "github", Secret: "test-secret", Persistence: "session"})
	c, err := NewCore(CoreConfig{Store: store, Secrets: ss, HTTPClient: &http.Client{Transport: transport}, Clock: func() time.Time { return time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	plan := contract.Plan{ID: "plan-child", WindowStart: "2026-01-01T00:00:00Z", WindowEnd: "2026-01-10T00:00:00Z", Input: contract.PlanInput{Modules: contract.Modules{GitHubIssues: true}, Limits: contract.Limits{MaxTransferBytes: 1 << 20}}}
	resource := contract.Resource{Source: "github", ID: "42", Name: "acme/api"}
	d := query.Dataset{}
	run := contract.Run{ID: "run-child"}
	if err := c.collectRepository(context.Background(), plan, resource, contract.Snapshot{}, &d, &run); err != nil {
		t.Fatal(err)
	}
	if len(d.PullRequests) != 1 || d.PullRequests[0].ReadyAt != "2026-01-02T00:00:00Z" || d.PullRequests[0].FirstHumanReviewAt != "2026-01-03T00:00:00Z" || d.PullRequests[0].ReviewWaitSeconds == nil || *d.PullRequests[0].ReviewWaitSeconds != 86400 {
		t.Fatalf("PR review projection: %+v", d.PullRequests)
	}
	if len(d.Issues) != 1 || d.Issues[0].ReopenCount != 1 {
		t.Fatalf("issue reopen projection: %+v", d.Issues)
	}
	for _, coverage := range d.Coverage {
		if (coverage.Collection == "reviews" || coverage.Collection == "timeline") && coverage.Status != "complete" {
			t.Fatalf("child coverage: %+v", coverage)
		}
	}
	d = rules.Evaluate(d)
	var flow, reopen bool
	for _, f := range d.Findings {
		flow = flow || f.RuleID == "R-FLOW-001"
		reopen = reopen || f.RuleID == "R-REOPEN-001"
	}
	if !flow || !reopen {
		t.Fatalf("findings flow=%t reopen=%t: %+v", flow, reopen, d.Findings)
	}
}
