package core

import (
	"encoding/json"
	"testing"
	"time"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/query"
	"pocketcli/internal/investigation/rules"
	"pocketcli/internal/investigation/storage"
)

func TestMixedSourceLinksDoNotClaimCompleteWithoutLinearFormalCollector(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c := &Core{store: store, setup: NewSetup(func() time.Time { return time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC) })}
	plan := contract.Plan{InvestigationID: "investigation-1", Resources: []contract.Resource{{Source: "github", ID: "repo-1", Name: "acme/api"}, {Source: "linear", ID: "team-1", Name: "Operations"}}}
	coverage := []contract.Coverage{
		{Source: "github", ResourceID: "repo-1", Collection: "issues", Status: "complete"},
		{Source: "github", ResourceID: "repo-1", Collection: "pull_requests", Status: "complete"},
		{Source: "github", ResourceID: "repo-1", Collection: "formal_links", Status: "complete"},
	}
	githubOnly := query.Dataset{Coverage: append([]contract.Coverage{}, coverage...)}
	if err := c.linkDataset(plan, &githubOnly); err != nil || githubOnly.Coverage[len(githubOnly.Coverage)-1].Status != "complete" {
		t.Fatalf("cobertura GitHub completa perdida: %+v err=%v", githubOnly.Coverage, err)
	}
	for _, id := range []string{"ev-github", "ev-linear"} {
		if err := store.Put("objects", id, json.RawMessage(`{"body":""}`)); err != nil {
			t.Fatal(err)
		}
	}
	mixed := query.Dataset{Snapshot: contract.Snapshot{Resources: plan.Resources}, Coverage: append([]contract.Coverage{}, coverage...), Issues: []query.Issue{{ID: "github-1", Source: "github", Identifier: "acme/api#1", EvidenceID: "ev-github"}, {ID: "linear-1", Source: "linear", Identifier: "OPS-1", TeamID: "team-1", EvidenceID: "ev-linear"}}}
	if err := c.linkDataset(plan, &mixed); err != nil || mixed.Coverage[len(mixed.Coverage)-1].Status != "partial" {
		t.Fatalf("vínculos Linear não coletados apresentados como completos: %+v err=%v", mixed.Coverage, err)
	}
	foundGitHub := false
	for _, finding := range rules.Evaluate(mixed).Findings {
		if finding.RuleID != "R-LINK-001" {
			continue
		}
		if finding.SubjectID != "github-1" {
			t.Fatalf("issue Linear recebeu ausência sem cobertura formal: %+v", finding)
		}
		foundGitHub = true
	}
	if !foundGitHub {
		t.Fatal("issue GitHub com cobertura formal completa perdeu achado elegível")
	}
}

func TestExactGitHubPullKeyRejectsAmbiguousAttachmentURL(t *testing.T) {
	for _, raw := range []string{
		"https://github.com/acme/api/pull/7.evil",
		"https://github.com/acme/api/pull/7/commits",
		"https://github.com.evil.test/acme/api/pull/7",
		"https://github.com@evil.test/acme/api/pull/7",
		"https://github.com/acme/api/pull/7?access_token=secret",
		"https://github.com/acme%2Fapi/pull/7",
	} {
		if key, ok := exactGitHubPullKey(raw); ok {
			t.Fatalf("URL ambígua aceita: %q -> %q", raw, key)
		}
	}
	if key, ok := exactGitHubPullKey("https://github.com/Acme/API/pull/7"); !ok || key != "acme/api#7" {
		t.Fatalf("PR exato rejeitado: key=%q ok=%v", key, ok)
	}
}
