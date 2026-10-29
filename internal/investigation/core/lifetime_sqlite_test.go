//go:build investigate_sqlite && cgo

package core

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/secrets"
	"pocketcli/internal/investigation/storage"
)

// The "all" period is each source's real lifetime: a repository from its
// creation to its last push, Linear from the organization's creation unless
// a team was targeted. A bounded period records none of it.
func TestAllPeriodResolvesSourceLifetimes(t *testing.T) {
	for _, test := range []struct {
		name         string
		preset       string
		teams        []string
		githubFrom   string
		githubTo     string
		linearFrom   string
		wantFrom     string
		wantTo       string
		wantLifetime bool
	}{
		{"organization", "all", nil, "2021-03-01T10:00:00Z", "2026-09-30T08:30:00Z", "2019-05-02T00:00:00Z", "2019-05-02T00:00:00Z", "windowEnd", true},
		{"targeted team", "all", []string{"T"}, "2021-03-01T10:00:00Z", "2026-09-30T08:30:00Z", "2022-07-10T12:00:00Z", "2021-03-01T10:00:00Z", "windowEnd", true},
		{"bounded period", "30d", nil, "", "", "", "", "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, _ := storage.Open(t.TempDir())
			ss := secrets.NewSessionStore()
			ss.Put(context.Background(), contract.CredentialInput{Source: "github", Secret: "github-secret", Persistence: "session"})
			ss.Put(context.Background(), contract.CredentialInput{Source: "linear", Secret: "linear-secret", Persistence: "session"})
			transport := transportFunc(func(r *http.Request) (*http.Response, error) {
				body := `{"data":{"organization":{"createdAt":"2019-05-02T00:00:00.000Z"},"teams":{"nodes":[{"id":"team-1","key":"T","name":"Team","createdAt":"2022-07-10T12:00:00.000Z"}],"pageInfo":{"hasNextPage":false}}}}`
				if r.URL.Host == "api.github.com" {
					body = `{"id":42,"full_name":"acme/api","created_at":"2021-03-01T10:00:00Z","pushed_at":"2026-09-30T08:30:00Z","updated_at":"2026-10-20T00:00:00Z"}`
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			c, err := NewCore(CoreConfig{Store: store, Secrets: ss, HTTPClient: &http.Client{Transport: transport}})
			if err != nil {
				t.Fatal(err)
			}
			scope := &contract.Scope{Mode: "selected", Repositories: []string{"acme/api"}, Teams: test.teams}
			plan, err := c.PreparePlan(context.Background(), contract.PlanInput{SchemaVersion: contract.Version, GitHubEnabled: true, LinearEnabled: true, Scope: scope, Period: contract.Period{Preset: test.preset, Timezone: "UTC"}, Limits: contract.Limits{GitHubRPH: 3600000, LinearRPH: 3600000}})
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range plan.Resources {
				want := [2]string{}
				if test.preset == "all" && r.Source == "github" {
					want = [2]string{test.githubFrom, test.githubTo}
				}
				if test.preset == "all" && r.Source == "linear" {
					want = [2]string{test.linearFrom, ""}
				}
				if got := [2]string{r.CreatedAt, r.LastChangeAt}; got != want {
					t.Fatalf("%s %s: tempo de vida %v, esperado %v", r.Source, r.Name, got, want)
				}
			}
			from, to, lifetime := contract.EffectiveWindow(plan.WindowStart, plan.WindowEnd, plan.Resources)
			wantTo := test.wantTo
			if wantTo == "windowEnd" {
				wantTo = plan.WindowEnd
			}
			if wantTo == "" {
				wantTo = plan.WindowEnd
			}
			if from != test.wantFrom && test.wantLifetime || to != wantTo || lifetime != test.wantLifetime {
				t.Fatalf("janela efetiva %q → %q (vida=%v), esperado %q → %q", from, to, lifetime, test.wantFrom, wantTo)
			}
		})
	}
}
