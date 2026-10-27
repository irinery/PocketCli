//go:build investigate_sqlite && cgo

package core

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/query"
	"pocketcli/internal/investigation/rules"
	"pocketcli/internal/investigation/secrets"
	"pocketcli/internal/investigation/storage"
)

func TestLinearBlockingRelationPaginatesBothDirectionsAndFeedsRule(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	secretStore := secrets.NewSessionStore()
	if _, err := secretStore.Put(context.Background(), contract.CredentialInput{Source: "linear", Secret: "test-linear", Persistence: "session"}); err != nil {
		t.Fatal(err)
	}
	blocks := `{"id":"relation-blocks","type":"blocks","createdAt":"2026-01-03T00:00:00Z","updatedAt":"2026-01-04T00:00:00Z","issue":{"id":"blocker"},"relatedIssue":{"id":"dependent"}}`
	related := `{"id":"relation-related","type":"related","createdAt":"2026-01-03T00:00:00Z","updatedAt":"2026-01-04T00:00:00Z","issue":{"id":"blocker"},"relatedIssue":{"id":"dependent"}}`
	calls := make(map[string]int)
	transport := transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "test-linear" {
			t.Fatal("chave Linear ausente")
		}
		body, _ := io.ReadAll(r.Body)
		var input struct {
			Query     string            `json:"query"`
			Variables map[string]string `json:"variables"`
		}
		if err := json.Unmarshal(body, &input); err != nil {
			t.Fatal(err)
		}
		response := ""
		switch {
		case strings.Contains(input.Query, "IssueFields"):
			response = `{"data":{"__type":{"fields":[{"name":"relations"},{"name":"inverseRelations"}]}}}`
		case strings.Contains(input.Query, "IssueInverseRelations"):
			if !strings.Contains(input.Query, "includeArchived: false") {
				t.Fatal("relação arquivada poderia parecer bloqueio atual")
			}
			calls["inverse"]++
			nodes := ""
			if input.Variables["id"] == "dependent" {
				nodes = blocks
			}
			response = `{"data":{"issue":{"id":"` + input.Variables["id"] + `","inverseRelations":{"nodes":[` + nodes + `],"pageInfo":{"hasNextPage":false}}}}}`
		case strings.Contains(input.Query, "IssueRelations"):
			if !strings.Contains(input.Query, "includeArchived: false") {
				t.Fatal("relação arquivada poderia parecer bloqueio atual")
			}
			calls["outgoing"]++
			nodes, next := "", "false"
			cursor := ""
			if input.Variables["id"] == "blocker" {
				if input.Variables["after"] == "" {
					nodes, next, cursor = blocks, "true", `,"endCursor":"page-2"`
				} else if input.Variables["after"] == "page-2" {
					nodes = related
				} else {
					t.Fatalf("cursor inesperado: %+v", input.Variables)
				}
			}
			response = `{"data":{"issue":{"id":"` + input.Variables["id"] + `","relations":{"nodes":[` + nodes + `],"pageInfo":{"hasNextPage":` + next + cursor + `}}}}}`
		case strings.Contains(input.Query, "TeamProjects"):
			response = `{"data":{"projects":{"nodes":[],"pageInfo":{"hasNextPage":false}}}}`
		default:
			response = `{"data":{"issues":{"nodes":[{"id":"blocker","identifier":"T-1","title":"Bloqueador","createdAt":"2026-01-02T00:00:00Z","updatedAt":"2026-01-04T00:00:00Z","state":{"name":"In Progress","type":"started"}},{"id":"dependent","identifier":"T-2","title":"Dependente","createdAt":"2026-01-02T00:00:00Z","updatedAt":"2026-01-04T00:00:00Z","state":{"name":"Backlog","type":"backlog"}}],"pageInfo":{"hasNextPage":false}}}}`
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(response))}, nil
	})
	c, err := NewCore(CoreConfig{Store: store, Secrets: secretStore, HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	plan := contract.Plan{ID: "linear-relations-plan", InvestigationID: "linear-relations-investigation", WindowStart: "2026-01-01T00:00:00Z", WindowEnd: "2026-02-01T00:00:00Z", Input: contract.PlanInput{LinearEnabled: true, Limits: contract.Limits{MaxTransferBytes: 1 << 20}}}
	d := query.Dataset{}
	if err := c.collectTeam(context.Background(), plan, contract.Resource{Source: "linear", ID: "team-1", Name: "T"}, &d, &contract.Run{ID: "linear-relations-run"}); err != nil {
		t.Fatal(err)
	}
	if calls["outgoing"] != 3 || calls["inverse"] != 2 || len(d.Links) != 1 {
		t.Fatalf("paginação ou deduplicação das relações falhou: calls=%+v links=%+v", calls, d.Links)
	}
	link := d.Links[0]
	if link.SourceID != "linear-dependent" || link.TargetID != "linear-blocker" || link.Relation != "blocked_by" || link.Status != "direct" || link.EvidenceID == "" {
		t.Fatalf("sentido da dependência incorreto: %+v", link)
	}
	for _, coverage := range d.Coverage {
		if (coverage.Collection == "relations" || coverage.Collection == "inverseRelations") && coverage.Status != "complete" {
			t.Fatalf("cobertura de relação incompleta: %+v", coverage)
		}
	}
	var blocked int
	for _, finding := range rules.Evaluate(d).Findings {
		if finding.RuleID == "R-BLOCK-001" {
			blocked++
			if finding.SubjectID != "linear-dependent" || len(finding.EvidenceIDs) != 3 {
				t.Fatalf("achado sem estados e relação observados: %+v", finding)
			}
		}
	}
	if blocked != 1 {
		t.Fatalf("bloqueio direto não chegou à regra: %+v", d.Links)
	}
	late := strings.Replace(blocks, `"updatedAt":"2026-01-04T00:00:00Z"`, `"updatedAt":"2026-02-03T00:00:00Z"`, 1)
	valid, err := c.projectLinearRelation(plan, d.Issues[0], "relations", json.RawMessage(late), &d, &contract.Run{ID: "late-relation"})
	if err != nil || valid || len(d.Links) != 1 {
		t.Fatalf("relação posterior ao snapshot entrou na análise: valid=%v err=%v links=%+v", valid, err, d.Links)
	}
}

func TestLinearAttachmentPaginatesAndConfirmsOnlyCollectedPullRequest(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	secretStore := secrets.NewSessionStore()
	if _, err := secretStore.Put(context.Background(), contract.CredentialInput{Source: "linear", Secret: "test-linear", Persistence: "session"}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	transport := transportFunc(func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		var input struct {
			Query     string            `json:"query"`
			Variables map[string]string `json:"variables"`
		}
		if err := json.Unmarshal(body, &input); err != nil {
			t.Fatal(err)
		}
		response := `{"data":{"__type":{"fields":[{"name":"attachments"}]}}}`
		if strings.Contains(input.Query, "IssueAttachments") {
			calls++
			if input.Variables["id"] != "issue-1" {
				t.Fatalf("issue inesperada: %+v", input.Variables)
			}
			if input.Variables["after"] == "" {
				response = `{"data":{"issue":{"id":"issue-1","attachments":{"nodes":[{"id":"a-1","url":"https://github.com/acme/api/pull/7","createdAt":"2026-01-02T00:00:00Z","updatedAt":"2026-01-03T00:00:00Z"}],"pageInfo":{"hasNextPage":true,"endCursor":"next"}}}}}`
			} else if input.Variables["after"] == "next" {
				response = `{"data":{"issue":{"id":"issue-1","attachments":{"nodes":[{"id":"a-2","url":"https://github.com/acme/api/pull/8","createdAt":"2026-01-02T00:00:00Z","updatedAt":"2026-01-03T00:00:00Z"}],"pageInfo":{"hasNextPage":false}}}}}`
			} else {
				t.Fatalf("cursor inesperado: %+v", input.Variables)
			}
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(response))}, nil
	})
	c, err := NewCore(CoreConfig{Store: store, Secrets: secretStore, HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"issue-evidence", "pr-evidence"} {
		if err := store.Put("objects", id, json.RawMessage(`{"body":""}`)); err != nil {
			t.Fatal(err)
		}
	}
	plan := contract.Plan{ID: "attachment-plan", InvestigationID: "attachment-investigation", WindowStart: "2026-01-01T00:00:00Z", WindowEnd: "2026-02-01T00:00:00Z", Input: contract.PlanInput{Limits: contract.Limits{MaxTransferBytes: 1 << 20}}}
	d := query.Dataset{Issues: []query.Issue{{ID: "linear-issue-1", Source: "linear", Identifier: "OPS-1", TeamID: "team-1", EvidenceID: "issue-evidence"}}, PullRequests: []query.PullRequest{{ID: "pr-7", Repository: "acme/api", Number: 7, EvidenceID: "pr-evidence"}}}
	run := &contract.Run{ID: "attachment-run"}
	if err := c.collectLinearChildren(context.Background(), plan, contract.Resource{Source: "linear", ID: "team-1"}, &d, run); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(d.Attachments) != 2 {
		t.Fatalf("paginação de anexos falhou: calls=%d attachments=%+v", calls, d.Attachments)
	}
	if err := c.linkDataset(plan, &d); err != nil {
		t.Fatal(err)
	}
	if len(d.Links) != 1 || d.Links[0].SourceID != "linear-issue-1" || d.Links[0].TargetID != "pr-7" || d.Links[0].Relation != "references_pr" || d.Links[0].EvidenceID == "" {
		t.Fatalf("anexo não confirmou somente PR coletado: %+v", d.Links)
	}
	foundAttachments := false
	for _, cov := range d.Coverage {
		if cov.Collection == "attachments" && (cov.Status != "complete" || cov.Observed != 2) {
			t.Fatalf("cobertura de anexos incorreta: %+v", cov)
		}
		foundAttachments = foundAttachments || cov.Collection == "attachments"
	}
	if !foundAttachments {
		t.Fatal("cobertura de anexos ausente")
	}
	late := json.RawMessage(`{"id":"a-3","url":"https://github.com/acme/api/pull/7","createdAt":"2026-01-02T00:00:00Z","updatedAt":"2026-03-01T00:00:00Z"}`)
	if valid, err := c.projectLinearAttachment(plan, d.Issues[0], late, &d, run); err != nil || valid || len(d.Attachments) != 2 {
		t.Fatalf("anexo posterior ao snapshot foi promovido: valid=%v err=%v attachments=%+v", valid, err, d.Attachments)
	}
}
