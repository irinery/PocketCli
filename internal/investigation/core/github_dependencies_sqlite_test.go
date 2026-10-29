//go:build investigate_sqlite && cgo

package core

import (
	"encoding/json"
	"testing"
	"time"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/query"
	"pocketcli/internal/investigation/storage"
)

func TestGitHubDependencyTimelineAddsRemovesAndMirrors(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCore(CoreConfig{Store: store, Clock: func() time.Time { return time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	plan := contract.Plan{WindowEnd: "2026-01-10T00:00:00Z"}
	r := contract.Resource{Source: "github", ID: "repo", Name: "acme/api"}
	d := query.Dataset{Issues: []query.Issue{{ID: "issue-8", Identifier: "acme/api#8"}, {ID: "issue-9", Identifier: "acme/api#9"}, {ID: "issue-10", Identifier: "acme/api#10"}}}
	items := func(raw ...string) []json.RawMessage {
		out := make([]json.RawMessage, len(raw))
		for i := range raw {
			out[i] = json.RawMessage(raw[i])
		}
		return out
	}
	from8, observed, invalid := parseGitHubDependencyEvents(plan, r, githubChildTarget{Number: 8, EntityID: "issue-8"}, items(
		`{"id":1,"event":"blocked_by_added","created_at":"2026-01-01T00:00:00Z","blocked_by":{"number":9,"repository":{"full_name":"acme/api"}}}`,
		`{"id":3,"event":"blocking_added","created_at":"2026-01-02T00:00:00Z","blocking":{"number":10,"repository":{"full_name":"acme/api"}}}`,
		`{"id":5,"event":"blocked_by_removed","created_at":"2026-01-03T00:00:00Z","blocked_by":{"number":9,"repository":{"full_name":"acme/api"}}}`,
	), &d)
	if invalid || observed != 3 {
		t.Fatalf("parse: invalid=%t observed=%d", invalid, observed)
	}
	from10, _, invalid := parseGitHubDependencyEvents(plan, r, githubChildTarget{Number: 10, EntityID: "issue-10"}, items(
		`{"id":4,"event":"blocked_by_added","created_at":"2026-01-02T00:00:01Z","blocked_by":{"number":8,"repository":{"full_name":"acme/api"}}}`,
	), &d)
	if invalid {
		t.Fatal("mirror invalid")
	}
	run := contract.Run{ID: "dependency-run"}
	if err := c.projectGitHubDependencyEvents(r, append(from8, from10...), &d, &run); err != nil {
		t.Fatal(err)
	}
	if len(d.Links) != 1 || d.Links[0].SourceID != "issue-10" || d.Links[0].TargetID != "issue-8" || d.Links[0].Relation != "blocked_by" || d.Links[0].Status != "direct" || d.Links[0].EvidenceID == "" {
		t.Fatalf("links: %+v", d.Links)
	}
	if len(d.Evidence) != 1 || run.Items != 1 {
		t.Fatalf("evidence=%+v run=%+v", d.Evidence, run)
	}
	_, _, invalid = parseGitHubDependencyEvents(plan, r, githubChildTarget{Number: 8, EntityID: "issue-8"}, items(`{"id":6,"event":"blocked_by_added","created_at":"2026-01-04T00:00:00Z","blocked_by":{"number":999,"repository":{"full_name":"acme/api"}}}`), &d)
	if !invalid {
		t.Fatal("out-of-scope endpoint must reduce coverage")
	}
}
