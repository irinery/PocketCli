package core

import (
	"testing"
	"time"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/query"
	"pocketcli/internal/investigation/storage"
)

func TestAttachmentToUncollectedRepositoryIsKeptAsOutOfScope(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c := &Core{store: store, setup: NewSetup(func() time.Time { return time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC) })}
	plan := contract.Plan{InvestigationID: "investigation-1", Input: contract.PlanInput{GitHubEnabled: true}, Resources: []contract.Resource{{Source: "github", ID: "repo-1", Name: "acme/site"}}}
	d := query.Dataset{
		PullRequests: []query.PullRequest{{ID: "pr-1", Repository: "acme/site", Number: 1}},
		Attachments: []query.Attachment{
			{IssueID: "linear-1", PullKey: "acme/site#1", EvidenceID: "ev-1"},
			{IssueID: "linear-1", PullKey: "acme/private#4", EvidenceID: "ev-2"},
			{IssueID: "linear-2", PullKey: "acme/private#5", EvidenceID: "ev-3"},
			{IssueID: "linear-3", PullKey: "acme/other#9", EvidenceID: "ev-4"},
		},
	}
	if err := c.linkDataset(plan, &d); err != nil {
		t.Fatal(err)
	}
	statuses := map[string]string{}
	for _, l := range d.Links {
		statuses[l.TargetID] = l.Status
	}
	if statuses["pr-1"] != "direct" || statuses["github-pr:acme/private#4"] != "out_of_scope" || statuses["github-pr:acme/other#9"] != "out_of_scope" {
		t.Fatalf("vínculos inesperados: %+v", d.Links)
	}
	repos := contract.UncollectedRepositories(d.Coverage)
	if len(repos) != 2 || repos[0].Name != "acme/private" || repos[0].References != 2 || repos[0].Status != "unauthorized" {
		t.Fatalf("repositórios citados não coletados: %+v", repos)
	}
	outside, total, insufficient := contract.PullReferenceGap(d.Coverage)
	if outside != 3 || total != 4 || !insufficient {
		t.Fatalf("lacuna de PRs=%d/%d insuficiente=%v", outside, total, insufficient)
	}
	if last := d.Coverage[len(d.Coverage)-1]; last.Collection != "links" {
		t.Fatalf("cobertura de vínculos deixou de ser a última: %+v", last)
	}

	scoped := plan
	scoped.Input.Scope = &contract.Scope{Mode: "selected", Repositories: []string{"acme/site"}}
	d2 := query.Dataset{Attachments: []query.Attachment{{IssueID: "linear-1", PullKey: "acme/private#4", EvidenceID: "ev-2"}}}
	if err := c.linkDataset(scoped, &d2); err != nil {
		t.Fatal(err)
	}
	if repos := contract.UncollectedRepositories(d2.Coverage); len(repos) != 1 || repos[0].Status != "not_selected" {
		t.Fatalf("repositório fora do escopo selecionado: %+v", repos)
	}
}
