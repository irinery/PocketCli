//go:build investigate_sqlite && cgo

package core

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pocketcli/internal/investigation/collect"
	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/query"
	"pocketcli/internal/investigation/secrets"
	"pocketcli/internal/investigation/storage"
)

func TestPaginatedLinearIssuesStillCollectObservedHistoryAsPartial(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	secretStore := secrets.NewSessionStore()
	if _, err := secretStore.Put(context.Background(), contract.CredentialInput{Source: "linear", Secret: "test-linear", Persistence: "session"}); err != nil {
		t.Fatal(err)
	}
	historyCalls, batchCalls := 0, 0
	transport := transportFunc(func(r *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		var input struct {
			Query     string            `json:"query"`
			Variables map[string]string `json:"variables"`
		}
		if err := json.Unmarshal(body, &input); err != nil {
			t.Fatal(err)
		}
		response := ""
		switch {
		case strings.Contains(input.Query, "TeamIssues"):
			if input.Variables["after"] == "" {
				response = `{"data":{"issues":{"nodes":[{"id":"issue-1","identifier":"SMF-1","title":"Primeira","createdAt":"2026-01-02T00:00:00Z","updatedAt":"2026-01-04T00:00:00Z","state":{"name":"In Progress","type":"started"}}],"pageInfo":{"hasNextPage":true,"endCursor":"page-2"}}}}`
			} else if input.Variables["after"] == "page-2" {
				response = `{"data":{"issues":{"nodes":[{"id":"issue-2","identifier":"SMF-2","title":"Segunda","createdAt":"2026-01-02T00:00:00Z","updatedAt":"2026-01-04T00:00:00Z","state":{"name":"Backlog","type":"backlog"}}],"pageInfo":{"hasNextPage":false}}}}`
			} else {
				t.Fatalf("cursor inesperado: %+v", input.Variables)
			}
		case strings.Contains(input.Query, "IssueFields"):
			response = `{"data":{"__type":{"fields":[{"name":"history"}]}}}`
		case strings.Contains(input.Query, "IssueChildrenBatch"):
			batchCalls++
			response = `{"data":{"i0":{"id":"issue-1","history":{"nodes":[{"id":"transition-1","createdAt":"2026-01-03T00:00:00Z","fromState":{"name":"Backlog","type":"backlog"},"toState":{"name":"In Progress","type":"started"}}],"pageInfo":{"hasNextPage":false}}},"i1":{"id":"issue-2","history":{"nodes":[],"pageInfo":{"hasNextPage":false}}}}}`
		case strings.Contains(input.Query, "IssueHistory"):
			historyCalls++
			if input.Variables["id"] == "issue-1" {
				response = `{"data":{"issue":{"id":"issue-1","history":{"nodes":[{"id":"transition-1","createdAt":"2026-01-03T00:00:00Z","fromState":{"name":"Backlog","type":"backlog"},"toState":{"name":"In Progress","type":"started"}}],"pageInfo":{"hasNextPage":false}}}}}`
			} else {
				response = `{"data":{"issue":{"id":"issue-2","history":{"nodes":[],"pageInfo":{"hasNextPage":false}}}}}`
			}
		case strings.Contains(input.Query, "TeamProjects"):
			response = `{"data":{"projects":{"nodes":[],"pageInfo":{"hasNextPage":false}}}}`
		default:
			t.Fatalf("consulta Linear inesperada: %s", input.Query)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(response))}, nil
	})
	c, err := NewCore(CoreConfig{Store: store, Secrets: secretStore, HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	plan := contract.Plan{ID: "paginated-linear-plan", InvestigationID: "paginated-linear-investigation", WindowStart: "2026-01-01T00:00:00Z", WindowEnd: "2026-02-01T00:00:00Z", Input: contract.PlanInput{LinearEnabled: true, Limits: contract.Limits{MaxTransferBytes: 1 << 20}}}
	d := query.Dataset{}
	if err := c.collectTeam(context.Background(), plan, contract.Resource{Source: "linear", ID: "team-smf", Name: "SMF"}, &d, &contract.Run{ID: "paginated-linear-run"}); err != nil {
		t.Fatal(err)
	}
	if batchCalls != 1 || historyCalls != 0 || len(d.Events) != 1 || d.Events[0].EntityID != "linear-issue-1" {
		t.Fatalf("histórico das issues observadas não coletado: calls=%d events=%+v", historyCalls, d.Events)
	}
	statuses := map[string]string{}
	for _, coverage := range d.Coverage {
		statuses[coverage.Collection] = coverage.Status
	}
	if statuses["issues"] != "partial" || statuses["history"] != "partial" {
		t.Fatalf("paginação foi declarada completa: %+v", d.Coverage)
	}
}

func TestLinearAuditFailureStopsBeforeAnotherRequest(t *testing.T) {
	for _, failAt := range []string{"issues", "projects"} {
		t.Run(failAt, func(t *testing.T) {
			store, err := storage.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			secretStore := secrets.NewSessionStore()
			if _, err := secretStore.Put(context.Background(), contract.CredentialInput{Source: "linear", Secret: "test-linear", Persistence: "session"}); err != nil {
				t.Fatal(err)
			}
			requests := 0
			transport := transportFunc(func(r *http.Request) (*http.Response, error) {
				requests++
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Fatal(err)
				}
				response := `{"data":{"issues":{"nodes":[],"pageInfo":{"hasNextPage":false}}}}`
				if failAt == "projects" {
					response = `{"data":{"issues":{"nodes":[],"pageInfo":{"hasNextPage":true}}}}`
				}
				if strings.Contains(string(body), "TeamProjects") {
					response = `{"data":{"projects":{"nodes":[],"pageInfo":{"hasNextPage":false}}}}`
				}
				if (failAt == "issues" && requests == 1) || (failAt == "projects" && requests == 2) {
					receiptPath := filepath.Join(store.Root, "requestreceipts")
					if failAt == "projects" {
						if err := os.Rename(receiptPath, receiptPath+".saved"); err != nil {
							t.Fatal(err)
						}
					}
					if err := os.WriteFile(receiptPath, []byte("blocked"), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(response))}, nil
			})
			c, err := NewCore(CoreConfig{Store: store, Secrets: secretStore, HTTPClient: &http.Client{Transport: transport}})
			if err != nil {
				t.Fatal(err)
			}
			plan := contract.Plan{ID: "linear-audit-plan", InvestigationID: "linear-audit-investigation", WindowStart: "2026-01-01T00:00:00Z", WindowEnd: "2026-02-01T00:00:00Z", Input: contract.PlanInput{LinearEnabled: true, Limits: contract.Limits{MaxTransferBytes: 1 << 20}}}
			d := query.Dataset{}
			err = c.collectTeam(collect.WithRunID(context.Background(), "linear-audit-run"), plan, contract.Resource{Source: "linear", ID: "team-smf"}, &d, &contract.Run{ID: "linear-audit-run"})
			var coded *contract.ErrorResponse
			if !errors.As(err, &coded) || coded.Code != "ERR_AUDIT_FAILED" {
				t.Fatalf("falha de auditoria foi transformada em cobertura parcial: err=%v coverage=%+v", err, d.Coverage)
			}
			wantRequests := 1
			if failAt == "projects" {
				wantRequests = 2
			}
			if requests != wantRequests {
				t.Fatalf("coleta continuou depois da falha: requests=%d want=%d", requests, wantRequests)
			}
		})
	}
}
