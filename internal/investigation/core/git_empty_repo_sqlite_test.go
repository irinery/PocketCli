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

// An empty repository has nothing to fetch. That is a complete observation
// the reports can list, not a failure: a real fetch error stays partial.
func TestEmptyRepositoryIsCompleteCoverageAndFailureStaysPartial(t *testing.T) {
	for _, test := range []struct {
		name   string
		err    error
		status string
		empty  bool
	}{
		{"empty", gitmirror.ErrEmptyRemote, "complete", true},
		{"failed", gitmirror.ErrUnavailable, "partial", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, _ := storage.Open(t.TempDir())
			ss := secrets.NewSessionStore()
			ss.Put(context.Background(), contract.CredentialInput{Source: "github", Secret: "test-secret", Persistence: "session"})
			c, err := NewCore(CoreConfig{Store: store, Secrets: ss, Clock: func() time.Time { return time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC) }})
			if err != nil {
				t.Fatal(err)
			}
			plan := contract.Plan{ID: "empty-plan-" + test.name, InvestigationID: "empty-investigation", Input: contract.PlanInput{GitHubEnabled: true, Limits: contract.Limits{MaxTransferBytes: 1 << 20, MaxLocalBytes: 1 << 20}}}
			if err := store.Put("planauth", plan.ID, planAuth{GitHub: "identity"}); err != nil {
				t.Fatal(err)
			}
			c.gitMirror = func(string) (gitObserver, error) { return &fakeGitObserver{observeErr: test.err}, nil }
			d := query.Dataset{}
			run := contract.Run{ID: "empty-run-" + test.name}
			if err := c.collectGitRef(context.Background(), plan, contract.Resource{Source: "github", ID: "7", Name: "acme/empty"}, &d, &run); err != nil {
				t.Fatal(err)
			}
			if len(d.Coverage) != 2 {
				t.Fatalf("cobertura inesperada: %+v", d.Coverage)
			}
			for _, coverage := range d.Coverage {
				if coverage.Status != test.status || contract.IsEmptyRepository(coverage) != test.empty {
					t.Fatalf("status/vazio incorretos: %+v", coverage)
				}
				if test.empty && !strings.HasPrefix(coverage.Reason, contract.EmptyRepositoryReason) {
					t.Fatalf("motivo sem o texto padrão: %q", coverage.Reason)
				}
			}
		})
	}
}
