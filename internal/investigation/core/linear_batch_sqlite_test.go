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

	"pocketcli/internal/investigation/collect"
	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/query"
	"pocketcli/internal/investigation/secrets"
	"pocketcli/internal/investigation/storage"
)

func TestLinearChildrenDrainBeyondOneHundredAndResumeWithoutRefetch(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ss := secrets.NewSessionStore()
	_, _ = ss.Put(context.Background(), contract.CredentialInput{Source: "linear", Secret: "synthetic-token", Persistence: "session"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	batches := 0
	seen := map[string]int{}
	transport := transportFunc(func(r *http.Request) (*http.Response, error) {
		var input struct {
			Query     string
			Variables map[string]string
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Fatal(err)
		}
		body := `{"data":{"__type":{"fields":[{"name":"history"},{"name":"comments"}]}}}`
		if strings.Contains(input.Query, "IssueChildrenBatch") {
			batches++
			data := map[string]any{}
			for key, id := range input.Variables {
				seen[id]++
				page := map[string]any{"nodes": []any{}, "pageInfo": map[string]any{"hasNextPage": false}}
				data[key] = map[string]any{"id": id, "history": page, "comments": page}
			}
			encoded, _ := json.Marshal(map[string]any{"data": data})
			body = string(encoded)
			if batches == 1 {
				cancel()
			}
		} else if !strings.Contains(input.Query, "IssueFields") {
			t.Fatalf("unexpected request: %s", input.Query)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	c, err := NewCore(CoreConfig{Store: store, Secrets: ss, HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	plan := contract.Plan{ID: "resume-plan", InvestigationID: "synthetic-investigation", WindowEnd: "2026-02-01T00:00:00Z", Input: contract.PlanInput{Limits: contract.Limits{MaxTransferBytes: 1 << 20}}}
	r := contract.Resource{Source: "linear", ID: "team-test"}
	issues := make([]query.Issue, 125)
	for i := range issues {
		issues[i] = query.Issue{ID: fmt.Sprintf("linear-item-%03d", i), Source: "linear", TeamID: r.ID}
	}
	d := query.Dataset{Issues: issues}
	run := &contract.Run{ID: "resume-run"}
	if err := c.collectLinearChildren(ctx, plan, r, &d, run); err == nil {
		t.Fatal("canceled run continued")
	}
	d = query.Dataset{Issues: issues}
	if err := c.collectLinearChildren(context.Background(), plan, r, &d, run); err != nil {
		t.Fatal(err)
	}
	if len(seen) != len(issues) || batches != 13 {
		t.Fatalf("issues=%d batches=%d", len(seen), batches)
	}
	for _, n := range seen {
		if n != 1 {
			t.Fatal("resume fetched an already journaled issue")
		}
	}
	for _, cov := range d.Coverage {
		if (cov.Collection == "history" || cov.Collection == "comments") && cov.Status != "complete" {
			t.Fatalf("queue not drained: %+v", cov)
		}
	}
}

func TestLinearRejectedCredentialStopsBatchWithoutPerIssueFallback(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ss := secrets.NewSessionStore()
	_, _ = ss.Put(context.Background(), contract.CredentialInput{Source: "linear", Secret: "synthetic-token", Persistence: "session"})
	calls := 0
	c, err := NewCore(CoreConfig{Store: store, Secrets: ss, HTTPClient: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		body, _ := io.ReadAll(r.Body)
		status, data := 200, `{"data":{"__type":{"fields":[{"name":"history"}]}}}`
		if !strings.Contains(string(body), "IssueFields") {
			status, data = 401, `{"message":"synthetic denial"}`
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(data))}, nil
	})}})
	if err != nil {
		t.Fatal(err)
	}
	d := query.Dataset{Issues: []query.Issue{{ID: "linear-one", Source: "linear", TeamID: "test"}, {ID: "linear-two", Source: "linear", TeamID: "test"}}}
	err = c.collectLinearChildren(context.Background(), contract.Plan{ID: "auth-plan", Input: contract.PlanInput{Limits: contract.Limits{MaxTransferBytes: 1 << 20}}}, contract.Resource{ID: "test", Source: "linear"}, &d, &contract.Run{ID: "auth-run"})
	if !collect.AuthenticationRejected(err) || calls != 2 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}

func TestLinearSchemaSharedAcrossTeamsWithinPlan(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ss := secrets.NewSessionStore()
	_, _ = ss.Put(context.Background(), contract.CredentialInput{Source: "linear", Secret: "synthetic-token", Persistence: "session"})
	calls := 0
	c, err := NewCore(CoreConfig{Store: store, Secrets: ss, HTTPClient: &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"data":{"__type":{"fields":[{"name":"history"}]}}}`))}, nil
	})}})
	if err != nil {
		t.Fatal(err)
	}
	plan := contract.Plan{ID: "schema-plan", Input: contract.PlanInput{Limits: contract.Limits{MaxTransferBytes: 1 << 20}}}
	for _, team := range []string{"team-one", "team-two"} {
		if _, err := c.linearSchema(context.Background(), plan, contract.Resource{ID: team}, &contract.Run{ID: "schema-run"}); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatalf("schema requests=%d", calls)
	}
	plan.ID = "another-plan"
	if _, err := c.linearSchema(context.Background(), plan, contract.Resource{ID: "team-one"}, &contract.Run{ID: "schema-run"}); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatal("schema leaked across plans")
	}
}

// One batch reads the first history page of every issue. An issue whose page
// continues, or that the batch did not return, falls back to the single-issue
// query from the right cursor; a failed batch falls back for every issue.
func TestLinearChildBatchFallsBackPerIssue(t *testing.T) {
	for _, test := range []struct {
		name      string
		batchBody string
		want      []string
	}{
		{
			name:      "partial",
			batchBody: `{"data":{"i0":{"id":"issue-1","history":{"nodes":[{"id":"h-1","createdAt":"2026-01-03T00:00:00Z"}],"pageInfo":{"hasNextPage":false}}},"i1":{"id":"issue-2","history":{"nodes":[{"id":"h-2","createdAt":"2026-01-03T00:00:00Z"}],"pageInfo":{"hasNextPage":true,"endCursor":"c-2"}}},"i2":null}}`,
			want:      []string{"issue-2:c-2", "issue-3:"},
		},
		{
			name:      "failed",
			batchBody: `{"errors":[{"message":"complexity"}]}`,
			want:      []string{"issue-1:", "issue-2:", "issue-3:"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, err := storage.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			ss := secrets.NewSessionStore()
			if _, err := ss.Put(context.Background(), contract.CredentialInput{Source: "linear", Secret: "test-linear", Persistence: "session"}); err != nil {
				t.Fatal(err)
			}
			batches := 0
			var single []string
			transport := transportFunc(func(r *http.Request) (*http.Response, error) {
				var input struct {
					Query     string            `json:"query"`
					Variables map[string]string `json:"variables"`
				}
				if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
					t.Fatal(err)
				}
				response := ""
				switch {
				case strings.Contains(input.Query, "TeamIssues"):
					nodes := make([]string, 0, 3)
					for i := 1; i <= 3; i++ {
						nodes = append(nodes, fmt.Sprintf(`{"id":"issue-%d","identifier":"T-%d","title":"Issue","createdAt":"2026-01-02T00:00:00Z","updatedAt":"2026-01-04T00:00:00Z","state":{"name":"Started","type":"started"}}`, i, i))
					}
					response = `{"data":{"issues":{"nodes":[` + strings.Join(nodes, ",") + `],"pageInfo":{"hasNextPage":false}}}}`
				case strings.Contains(input.Query, "IssueFields"):
					response = `{"data":{"__type":{"fields":[{"name":"history"}]}}}`
				case strings.Contains(input.Query, "IssueChildrenBatch"):
					batches++
					response = test.batchBody
				case strings.Contains(input.Query, "IssueHistory"):
					single = append(single, input.Variables["id"]+":"+input.Variables["after"])
					response = fmt.Sprintf(`{"data":{"issue":{"id":%q,"history":{"nodes":[],"pageInfo":{"hasNextPage":false}}}}}`, input.Variables["id"])
				case strings.Contains(input.Query, "TeamProjects"):
					response = `{"data":{"projects":{"nodes":[],"pageInfo":{"hasNextPage":false}}}}`
				default:
					t.Fatalf("consulta Linear inesperada: %s", input.Query)
				}
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(response))}, nil
			})
			c, err := NewCore(CoreConfig{Store: store, Secrets: ss, HTTPClient: &http.Client{Transport: transport}})
			if err != nil {
				t.Fatal(err)
			}
			plan := contract.Plan{ID: "batch-plan-" + test.name, InvestigationID: "batch-investigation", WindowStart: "2026-01-01T00:00:00Z", WindowEnd: "2026-02-01T00:00:00Z", Input: contract.PlanInput{LinearEnabled: true, Limits: contract.Limits{MaxTransferBytes: 1 << 20}}}
			d := query.Dataset{}
			if err := c.collectTeam(context.Background(), plan, contract.Resource{Source: "linear", ID: "team-1", Name: "T"}, &d, &contract.Run{ID: "batch-run"}); err != nil {
				t.Fatal(err)
			}
			sort.Strings(single)
			if batches != 1 || strings.Join(single, ",") != strings.Join(test.want, ",") {
				t.Fatalf("lote=%d consultas por issue=%v, esperado %v", batches, single, test.want)
			}
			for _, coverage := range d.Coverage {
				if coverage.Collection == "history" && coverage.Status != "complete" {
					t.Fatalf("histórico incompleto após o fallback: %+v", coverage)
				}
			}
		})
	}
}
