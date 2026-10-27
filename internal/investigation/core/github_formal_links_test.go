//go:build investigate_sqlite && cgo

package core

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/query"
	"pocketcli/internal/investigation/secrets"
	"pocketcli/internal/investigation/storage"
)

func TestGitHubFormalLinksRequireCurrentConnectionAndCompletePages(t *testing.T) {
	pageCalls := 0
	transport := transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != "POST" || r.URL.Path != "/graphql" || r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("unexpected GraphQL request: %s %s", r.Method, r.URL)
		}
		var request struct {
			Variables map[string]json.RawMessage `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		pageCalls++
		body := `{"data":{"repository":{"issue":{"timelineItems":{"nodes":[{"__typename":"ConnectedEvent","id":"conn-1","createdAt":"2026-01-02T00:00:00Z","source":{"__typename":"Issue","number":8,"repository":{"nameWithOwner":"acme/api"}},"subject":{"__typename":"PullRequest","number":7,"repository":{"nameWithOwner":"acme/api"}}},{"__typename":"ConnectedEvent","id":"conn-2","createdAt":"2026-01-02T00:00:00Z","source":{"__typename":"Issue","number":8,"repository":{"nameWithOwner":"acme/api"}},"subject":{"__typename":"PullRequest","number":9,"repository":{"nameWithOwner":"acme/api"}}}],"pageInfo":{"hasNextPage":true,"endCursor":"next"}}}}}}`
		if string(request.Variables["after"]) == `"next"` {
			body = `{"data":{"repository":{"issue":{"timelineItems":{"nodes":[{"__typename":"DisconnectedEvent","id":"disc-2","createdAt":"2026-01-03T00:00:00Z","source":{"__typename":"Issue","number":8,"repository":{"nameWithOwner":"acme/api"}},"subject":{"__typename":"PullRequest","number":9,"repository":{"nameWithOwner":"acme/api"}}},{"__typename":"ConnectedEvent","id":"future","createdAt":"2026-01-12T00:00:00Z","source":{"__typename":"Issue","number":8,"repository":{"nameWithOwner":"acme/api"}},"subject":{"__typename":"PullRequest","number":9,"repository":{"nameWithOwner":"acme/api"}}}],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}}}`
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ss := secrets.NewSessionStore()
	if _, err := ss.Put(context.Background(), contract.CredentialInput{Source: "github", Secret: "test-secret", Persistence: "session"}); err != nil {
		t.Fatal(err)
	}
	c, err := NewCore(CoreConfig{Store: store, Secrets: ss, HTTPClient: &http.Client{Transport: transport}, Clock: func() time.Time { return time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	plan := contract.Plan{ID: "formal-plan", WindowEnd: "2026-01-10T00:00:00Z", Input: contract.PlanInput{Modules: contract.Modules{GitHubIssues: true}, Limits: contract.Limits{MaxTransferBytes: 1 << 20}}}
	r := contract.Resource{Source: "github", ID: "repo-1", Name: "acme/api"}
	d := query.Dataset{Issues: []query.Issue{{ID: "issue-8", Source: "github", Identifier: "acme/api#8"}}, PullRequests: []query.PullRequest{{ID: "pr-7", Repository: "acme/api", Number: 7}, {ID: "pr-9", Repository: "acme/api", Number: 9}}}
	run := contract.Run{ID: "formal-run"}
	if err := c.collectGitHubFormalLinks(context.Background(), plan, r, &d, &run); err != nil {
		t.Fatal(err)
	}
	if pageCalls != 2 || len(d.Links) != 1 || d.Links[0].Relation != "references_pr" || d.Links[0].TargetID != "pr-7" || d.Links[0].EvidenceID == "" || len(d.Evidence) != 1 {
		t.Fatalf("calls=%d links=%+v evidence=%+v", pageCalls, d.Links, d.Evidence)
	}
	if len(d.Coverage) != 1 || d.Coverage[0].Status != "complete" {
		t.Fatalf("coverage=%+v", d.Coverage)
	}
	if err := c.collectGitHubFormalLinks(context.Background(), plan, r, &d, &run); err != nil {
		t.Fatal(err)
	}
	if pageCalls != 2 || len(d.Links) != 1 {
		t.Fatalf("journal did not resume: calls=%d links=%+v", pageCalls, d.Links)
	}
}

func TestGitHubFormalLinksDoNotPublishLinkFromIncompleteTimeline(t *testing.T) {
	transport := transportFunc(func(*http.Request) (*http.Response, error) {
		body := `{"data":{"repository":{"issue":{"timelineItems":{"nodes":[{"__typename":"ConnectedEvent","id":"conn-1","createdAt":"2026-01-02T00:00:00Z","source":{"__typename":"Issue","number":8,"repository":{"nameWithOwner":"acme/api"}},"subject":{"__typename":"PullRequest","number":7,"repository":{"nameWithOwner":"acme/api"}}}],"pageInfo":{"hasNextPage":true,"endCursor":null}}}}}}`
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ss := secrets.NewSessionStore()
	ss.Put(context.Background(), contract.CredentialInput{Source: "github", Secret: "test-secret", Persistence: "session"})
	c, err := NewCore(CoreConfig{Store: store, Secrets: ss, HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	plan := contract.Plan{ID: "incomplete-plan", WindowEnd: "2026-01-10T00:00:00Z", Input: contract.PlanInput{Modules: contract.Modules{GitHubIssues: true}, Limits: contract.Limits{MaxTransferBytes: 1 << 20}}}
	d := query.Dataset{Issues: []query.Issue{{ID: "issue-8", Source: "github", Identifier: "acme/api#8"}}, PullRequests: []query.PullRequest{{ID: "pr-7", Repository: "acme/api", Number: 7}}}
	if err := c.collectGitHubFormalLinks(context.Background(), plan, contract.Resource{Source: "github", ID: "repo-1", Name: "acme/api"}, &d, &contract.Run{ID: "incomplete-run"}); err != nil {
		t.Fatal(err)
	}
	if len(d.Links) != 0 || len(d.Coverage) != 1 || d.Coverage[0].Status != "partial" {
		t.Fatalf("incomplete timeline promoted a link: links=%+v coverage=%+v", d.Links, d.Coverage)
	}
}
