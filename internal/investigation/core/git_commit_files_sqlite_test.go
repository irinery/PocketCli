//go:build investigate_sqlite && cgo

package core

import (
	"context"
	"strings"
	"testing"
	"time"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/gitmirror"
	"pocketcli/internal/investigation/query"
	"pocketcli/internal/investigation/secrets"
	"pocketcli/internal/investigation/storage"
)

func TestLocalCommitFilesMapToManifestAtThatTree(t *testing.T) {
	for _, ambiguous := range []bool{false, true} {
		name := "unique"
		if ambiguous {
			name = "ambiguous"
		}
		t.Run(name, func(t *testing.T) {
			store, err := storage.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			ss := secrets.NewSessionStore()
			if _, err := ss.Put(context.Background(), contract.CredentialInput{Source: "github", Secret: "test-secret", Persistence: "session"}); err != nil {
				t.Fatal(err)
			}
			c, err := NewCore(CoreConfig{Store: store, Secrets: ss, Clock: func() time.Time { return time.Date(2026, 1, 20, 0, 0, 0, 0, time.UTC) }})
			if err != nil {
				t.Fatal(err)
			}
			commitOID := strings.Repeat("a", 40)
			treeOID := strings.Repeat("b", 40)
			blobOID := strings.Repeat("c", 40)
			commitAt := "2026-01-10T00:00:00Z"
			entries := []gitmirror.TreeEntry{{Path: "services/api/go.mod", Mode: "100644", Type: "blob", OID: blobOID}}
			if ambiguous {
				entries = append(entries, gitmirror.TreeEntry{Path: "services/api/Dockerfile", Mode: "100644", Type: "blob", OID: blobOID})
			}
			observer := &fakeGitObserver{
				result:  gitmirror.Observation{Ref: "refs/heads/main", NewOID: commitOID, GraphComplete: true},
				commits: []gitmirror.Commit{{OID: commitOID, CommittedAt: commitAt}},
				changes: map[string][]gitmirror.ChangedFile{commitOID: {{Path: "services/api/handler.go", Status: "M", TreeOID: treeOID}}},
				trees:   map[string][]gitmirror.TreeEntry{treeOID: entries},
			}
			c.gitMirror = func(string) (gitObserver, error) { return observer, nil }
			r := contract.Resource{Source: "github", ID: "42", Name: "acme/api"}
			plan := contract.Plan{ID: "commit-files-plan", InvestigationID: "commit-files-investigation", Input: contract.PlanInput{Limits: contract.Limits{MaxTransferBytes: 1 << 20, MaxLocalBytes: 1 << 20}}}
			journalID := storage.Hash([]byte(plan.ID + ":" + r.ID + ":git_ref"))
			if err := store.Put("gitobservations", journalID, gitRunObservation{Observation: observer.result, ObservedAt: "2026-01-20T00:00:00Z"}); err != nil {
				t.Fatal(err)
			}
			if err := store.Put("gitbranchobservations", storage.Hash([]byte(plan.ID+":"+r.ID+":git_branches")), gitBranchObservation{Complete: true, ObservedAt: "2026-01-20T00:00:00Z"}); err != nil {
				t.Fatal(err)
			}
			if err := store.Put("planauth", plan.ID, planAuth{GitHub: "identity"}); err != nil {
				t.Fatal(err)
			}
			reachable := query.Event{ID: "evt-" + storage.Hash([]byte(r.ID + ":commit:" + commitOID))[:24], EntityID: "repo-" + r.ID, Type: "commit_reachable_observed", EventAt: commitAt, ObservedAt: "2026-01-20T00:00:00Z", NewValue: commitOID}
			d := query.Dataset{Events: []query.Event{reachable}, Coverage: []contract.Coverage{{Source: "git", ResourceID: r.ID, Collection: "git_commits", Status: "complete"}}}
			// A árvore já presente no espelho permite mapear sem orçamento de rede restante.
			run := contract.Run{ID: "commit-files-run", Bytes: plan.Input.Limits.MaxTransferBytes}
			if err := c.collectGitCommitFiles(context.Background(), plan, r, &d, &run); err != nil {
				t.Fatal(err)
			}
			wantStatus, wantLinks := "complete", 2
			if ambiguous {
				wantStatus, wantLinks = "partial", 1
			}
			if len(d.Links) != wantLinks || len(d.Components) != wantLinks || d.Coverage[1].Status != wantStatus || len(d.Events) != 2 || d.Events[1].OldValue != wantStatus || run.Requests != 0 {
				t.Fatalf("mapeamento commit=%+v run=%+v", d, run)
			}
			if d.Links[0].SourceID != reachable.ID || d.Links[0].Relation != "modifies_file" || d.Links[0].Status != "direct" {
				t.Fatalf("arquivo não ligado ao commit: %+v", d.Links)
			}
			if !ambiguous && (d.Links[1].Relation != "maps_to_component" || d.Components[1].Path != "services/api/go.mod") {
				t.Fatalf("manifesto incorreto: links=%+v components=%+v", d.Links, d.Components)
			}
			second := query.Dataset{Events: append([]query.Event(nil), d.Events...), Evidence: append([]contract.Evidence(nil), d.Evidence...), Components: append([]contract.ComponentRecord(nil), d.Components...), Links: append([]contract.LinkRecord(nil), d.Links...), Coverage: []contract.Coverage{{Source: "git", ResourceID: r.ID, Collection: "git_commits", Status: "complete"}}}
			secondRun := contract.Run{ID: "commit-files-run-two"}
			if err := c.collectGitCommitFiles(context.Background(), plan, r, &second, &secondRun); err != nil {
				t.Fatal(err)
			}
			if len(second.Links) != wantLinks || second.Coverage[1].Status != wantStatus || (!ambiguous && secondRun.Items != 0) || (ambiguous && secondRun.Items == 0) {
				t.Fatalf("refresh repetiu mapeamento: %+v run=%+v", second, secondRun)
			}
		})
	}
}
