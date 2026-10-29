//go:build investigate_sqlite && cgo

package core

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/query"
	"pocketcli/internal/investigation/secrets"
	"pocketcli/internal/investigation/storage"
)

// One GraphQL batch brings reviews and skips irrelevant PR timelines. Issue
// timelines still need REST because GraphQL's state events omit dependencies.
func TestGitHubChildBatchReadsReviewsAndSkipsIrrelevantTimelines(t *testing.T) {
	batches := 0
	var rest []string
	timelines := map[int]string{
		7: `[{"__typename":"ReadyForReviewEvent","createdAt":"2026-01-02T00:00:00Z"}]`,
		9: `[]`,
		8: `[{"__typename":"ClosedEvent","createdAt":"2025-12-01T00:00:00Z"}]`,
	}
	reviews := map[int]string{
		7: `[{"databaseId":13,"state":"CHANGES_REQUESTED","submittedAt":"2026-01-03T00:00:00Z","author":{"__typename":"User","databaseId":2}},{"databaseId":12,"state":"APPROVED","submittedAt":"2026-01-02T12:00:00Z","author":{"__typename":"Bot","databaseId":3}}]`,
		9: `[]`,
	}
	transport := transportFunc(func(r *http.Request) (*http.Response, error) {
		body := ""
		switch r.URL.Path {
		case "/graphql":
			batches++
			var input struct {
				Query     string         `json:"query"`
				Variables map[string]any `json:"variables"`
			}
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(input.Query, "query PocketCliChildrenBatch(") {
				t.Fatalf("consulta GraphQL inesperada: %s", input.Query)
			}
			items := make([]string, 0, len(input.Variables))
			for i := 0; ; i++ {
				value, ok := input.Variables[fmt.Sprintf("n%d", i)]
				if !ok {
					break
				}
				number := int(value.(float64))
				fields := []string{fmt.Sprintf(`"timelineItems":{"nodes":%s,"pageInfo":{"hasNextPage":false}}`, timelines[number])}
				if review, ok := reviews[number]; ok {
					fields = append(fields, fmt.Sprintf(`"reviews":{"nodes":%s,"pageInfo":{"hasNextPage":false}}`, review))
				}
				items = append(items, fmt.Sprintf(`"t%d":{%s}`, i, strings.Join(fields, ",")))
			}
			body = `{"data":{"repository":{` + strings.Join(items, ",") + `}}}`
		case "/repos/acme/api/pulls":
			body = `[{"id":100,"number":7,"title":"PR","state":"closed","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-05T00:00:00Z","user":{"id":1,"type":"User"}},{"id":101,"number":9,"title":"PR","state":"open","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-05T00:00:00Z","user":{"id":1,"type":"User"}}]`
		case "/repos/acme/api/issues":
			body = `[{"id":200,"number":8,"title":"Issue","state":"open","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-05T00:00:00Z"}]`
		case "/repos/acme/api/issues/7/timeline":
			rest = append(rest, r.URL.Path)
			body = `[{"id":21,"event":"ready_for_review","created_at":"2026-01-02T00:00:00Z","actor":{"id":1}}]`
		default:
			if !strings.HasSuffix(r.URL.Path, "/comments") {
				rest = append(rest, r.URL.Path)
			}
			body = "[]"
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
	plan := contract.Plan{ID: "plan-batch", WindowStart: "2026-01-01T00:00:00Z", WindowEnd: "2026-01-10T00:00:00Z", Input: contract.PlanInput{Modules: contract.Modules{GitHubIssues: true}, Limits: contract.Limits{MaxTransferBytes: 1 << 20}}}
	d := query.Dataset{Snapshot: contract.Snapshot{WindowStart: plan.WindowStart, WindowEnd: plan.WindowEnd}}
	if err := c.collectRepository(context.Background(), plan, contract.Resource{Source: "github", ID: "42", Name: "acme/api"}, contract.Snapshot{}, &d, &contract.Run{ID: "run-batch"}); err != nil {
		t.Fatal(err)
	}
	sort.Strings(rest)
	if batches != 1 || strings.Join(rest, ",") != "/repos/acme/api/issues/7/timeline,/repos/acme/api/issues/8/timeline" {
		t.Fatalf("lotes=%d leituras REST por item=%v", batches, rest)
	}
	var pr query.PullRequest
	for _, p := range d.PullRequests {
		if p.Number == 7 {
			pr = p
		}
	}
	if pr.ReadyAt != "2026-01-02T00:00:00Z" || pr.FirstHumanReviewAt != "2026-01-03T00:00:00Z" {
		t.Fatalf("revisões do lote não alimentaram o PR: %+v", pr)
	}
	want := map[string]string{"evt-" + storage.Hash([]byte("acme/api:review:13"))[:24]: "human_reviewed", "evt-" + storage.Hash([]byte("acme/api:review:12"))[:24]: "bot_reviewed"}
	for _, event := range d.Events {
		if kind, ok := want[event.ID]; ok && event.Type == kind {
			delete(want, event.ID)
		}
	}
	if len(want) != 0 {
		t.Fatalf("eventos de revisão sem o ID do REST: faltam %v em %+v", want, d.Events)
	}
	for _, coverage := range d.Coverage {
		if (coverage.Collection == "reviews" || coverage.Collection == "timeline") && coverage.Status != "complete" {
			t.Fatalf("cobertura após lote: %+v", coverage)
		}
	}
}
