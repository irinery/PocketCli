//go:build investigate_sqlite && cgo

package core

import (
	"context"
	"strings"
	"testing"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/query"
	"pocketcli/internal/investigation/rules"
	"pocketcli/internal/investigation/storage"
)

func TestDeliveryAncestryChangesFindingOnlyWhenGitProofIsKnown(t *testing.T) {
	merge := strings.Repeat("a", 40)
	revision := strings.Repeat("b", 40)
	for _, scenario := range []struct {
		name, relation, coverage string
		known, contained         bool
		finding                  bool
	}{
		{"contained", "contains_merge_commit", "complete", true, true, false},
		{"not-ancestor", "merge_commit_not_ancestor", "complete", true, false, true},
		{"unknown", "", "partial", false, false, true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			store, err := storage.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			c, err := NewCore(CoreConfig{Store: store})
			if err != nil {
				t.Fatal(err)
			}
			observer := &fakeGitObserver{}
			if scenario.known {
				observer.ancestorResults = map[string]bool{merge + ":" + revision: scenario.contained}
			}
			c.gitMirror = func(string) (gitObserver, error) { return observer, nil }
			plan := contract.Plan{ID: "plan-ancestry", InvestigationID: "inv-ancestry", WindowStart: "2026-01-01T00:00:00Z", WindowEnd: "2026-01-10T00:00:00Z", Resources: []contract.Resource{{Source: "github", ID: "repo-1", Name: "acme/api"}}, Input: contract.PlanInput{Modules: contract.Modules{CodeHistory: true}}}
			if err := store.Put("planauth", plan.ID, planAuth{GitHub: "account"}); err != nil {
				t.Fatal(err)
			}
			d := query.Dataset{
				Snapshot:     contract.Snapshot{WindowStart: plan.WindowStart, WindowEnd: plan.WindowEnd, AnalysisReferenceAt: plan.WindowEnd},
				Components:   []contract.ComponentRecord{{ID: "cmp-1", Repository: "acme/api"}},
				Issues:       []query.Issue{{ID: "issue-1", EvidenceID: "ev-issue"}},
				PullRequests: []query.PullRequest{{ID: "pr-1", Repository: "acme/api", MergeSHA: merge, MergedAt: "2026-01-05T00:00:00Z", EvidenceID: "ev-pr"}},
				Links: []contract.LinkRecord{
					{ID: "modifies", SourceID: "pr-1", Relation: "modifies_component", TargetID: "cmp-1", Status: "direct", EvidenceID: "ev-file"},
					{ID: "fix", SourceID: "pr-1", Relation: "declares_fix_of", TargetID: "issue-1", Status: "direct", EvidenceID: "ev-fix"},
				},
				Deliveries: []contract.DeliveryRecord{{ID: "art-1", ComponentID: "cmp-1", RevisionSHA: revision, ObservedAt: "2026-01-06T00:00:00Z", ProvenanceStatus: "declared", EvidenceID: "ev-artifact"}},
			}
			run := contract.Run{ID: "run-ancestry"}
			if err := c.mapDeliveryAncestry(context.Background(), plan, &d, &run); err != nil {
				t.Fatal(err)
			}
			if len(d.Coverage) != 1 || d.Coverage[0].Status != scenario.coverage {
				t.Fatalf("coverage=%+v", d.Coverage)
			}
			relationFound := false
			for _, link := range d.Links {
				relationFound = relationFound || link.Relation == scenario.relation && scenario.relation != "" && link.EvidenceID != ""
			}
			if relationFound != scenario.known {
				t.Fatalf("relation not recorded as expected: links=%+v", d.Links)
			}
			findingFound, ancestryEvidenceFound := false, false
			for _, finding := range rules.Evaluate(d).Findings {
				if finding.RuleID != "R-DELIVERY-001" {
					continue
				}
				findingFound = true
				if scenario.relation == "merge_commit_not_ancestor" {
					for _, link := range d.Links {
						if link.Relation == scenario.relation {
							for _, id := range finding.EvidenceIDs {
								ancestryEvidenceFound = ancestryEvidenceFound || id == link.EvidenceID
							}
						}
					}
				}
			}
			if findingFound != scenario.finding || (scenario.relation == "merge_commit_not_ancestor" && !ancestryEvidenceFound) {
				t.Fatalf("finding=%t ancestryEvidence=%t links=%+v", findingFound, ancestryEvidenceFound, d.Links)
			}
		})
	}
}
