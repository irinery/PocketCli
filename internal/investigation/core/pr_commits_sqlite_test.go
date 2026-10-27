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
	"pocketcli/internal/investigation/secrets"
	"pocketcli/internal/investigation/storage"
)

type prCommitTransport struct{ body string }

func (f *prCommitTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Path != "/repos/acme/api/pulls/7/commits" || req.Header.Get("Authorization") != "Bearer test-secret" {
		return &http.Response{StatusCode: 404, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"message":"not found"}`))}, nil
	}
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(f.body))}, nil
}

func TestPRCommitMembershipClosesOnlyAfterCompleteRefresh(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ss := secrets.NewSessionStore()
	if _, err := ss.Put(context.Background(), contract.CredentialInput{Source: "github", Secret: "test-secret", Persistence: "session"}); err != nil {
		t.Fatal(err)
	}
	transport := &prCommitTransport{body: `[{"sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},{"sha":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}]`}
	now := time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC)
	c, err := NewCore(CoreConfig{Store: store, Secrets: ss, HTTPClient: &http.Client{Transport: transport}, Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	c.collector.SetRPH("github", 3600000)
	r := contract.Resource{Source: "github", ID: "42", Name: "acme/api"}
	pr := query.PullRequest{ID: "github-pr-100", Repository: r.Name, Number: 7, HeadSHA: strings.Repeat("b", 40)}
	first := query.Dataset{PullRequests: []query.PullRequest{pr}}
	plan := contract.Plan{ID: "pr-commit-plan-1", InvestigationID: "pr-commit-inv", WindowEnd: "2026-02-01T00:00:00Z", Input: contract.PlanInput{Limits: contract.Limits{MaxTransferBytes: 1 << 20}}}
	if err := c.collectPRCommits(context.Background(), plan, r, &first, &contract.Run{ID: "pr-commit-run-1"}); err != nil {
		t.Fatal(err)
	}
	if len(first.Events) != 2 || len(first.Links) != 2 || first.Coverage[0].Status != "complete" {
		t.Fatalf("primeira coleta: %+v", first)
	}
	for _, link := range first.Links {
		if link.Relation != "includes_commit" || link.Status != "direct" || link.ValidTo != "" || link.EvidenceID == "" {
			t.Fatalf("vínculo inicial inválido: %+v", link)
		}
	}

	now = now.Add(24 * time.Hour)
	partial := query.Dataset{PullRequests: []query.PullRequest{{ID: pr.ID, Repository: pr.Repository, Number: pr.Number, HeadSHA: strings.Repeat("d", 40)}}}
	reusePriorPRCommitGraph(&partial, first, plan.WindowEnd, make(map[string]bool))
	transport.body = `[{"sha":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},{"sha":"cccccccccccccccccccccccccccccccccccccccc"}]`
	partialPlan := plan
	partialPlan.ID = "pr-commit-plan-partial"
	if err := c.collectPRCommits(context.Background(), partialPlan, r, &partial, &contract.Run{ID: "pr-commit-run-partial"}); err != nil {
		t.Fatal(err)
	}
	if partial.Coverage[0].Status != "partial" || len(partial.Links) != 3 {
		t.Fatalf("lista com head inconsistente deveria ficar parcial: %+v", partial)
	}
	for _, link := range partial.Links {
		if link.ValidTo != "" {
			t.Fatalf("lista parcial fechou vínculo antigo: %+v", link)
		}
	}

	pr.HeadSHA = strings.Repeat("c", 40)
	second := query.Dataset{PullRequests: []query.PullRequest{pr}}
	referenced := make(map[string]bool)
	reusePriorPRCommitGraph(&second, first, plan.WindowEnd, referenced)
	if len(second.Links) != 2 || len(second.Events) != 2 || len(referenced) != 2 {
		t.Fatalf("histórico não reaproveitado: %+v", second)
	}
	transport.body = `[{"sha":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},{"sha":"cccccccccccccccccccccccccccccccccccccccc"}]`
	plan.ID = "pr-commit-plan-2"
	if err := c.collectPRCommits(context.Background(), plan, r, &second, &contract.Run{ID: "pr-commit-run-2"}); err != nil {
		t.Fatal(err)
	}
	if len(second.Links) != 3 || len(second.Events) != 4 || second.Coverage[0].Status != "complete" {
		t.Fatalf("refresh não reconciliou commits: %+v", second)
	}
	active, closed := 0, 0
	for _, link := range second.Links {
		if link.ValidTo == "" {
			active++
		} else {
			closed++
			if link.ValidTo != now.Format(time.RFC3339) {
				t.Fatalf("fim de validade incorreto: %+v", link)
			}
		}
	}
	if active != 2 || closed != 1 || first.Links[0].ValidTo != "" {
		t.Fatalf("vínculos atuais/históricos incorretos: primeiro=%+v segundo=%+v", first.Links, second.Links)
	}
	third := query.Dataset{PullRequests: []query.PullRequest{pr}}
	thirdEvidence := make(map[string]bool)
	reusePriorPRCommitGraph(&third, second, plan.WindowEnd, thirdEvidence)
	if len(third.Links) != 3 || len(third.Events) != 4 || len(thirdEvidence) != 4 {
		t.Fatalf("refresh perdeu evidência da remoção: %+v evidências=%+v", third, thirdEvidence)
	}
}
