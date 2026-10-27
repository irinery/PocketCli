//go:build investigate_sqlite && cgo

package core

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/secrets"
	"pocketcli/internal/investigation/storage"
)

func TestLinearTeamScopeAcceptsDiscoveredKeyOrID(t *testing.T) {
	for _, tc := range []struct {
		name, selected, response, wantCode string
	}{
		{"key", "smf", `{"data":{"teams":{"nodes":[{"id":"team-smf","key":"SMF","name":"Social Media Factory"},{"id":"team-ops","key":"OPS","name":"Operations"}],"pageInfo":{"hasNextPage":false}}}}`, ""},
		{"id", "team-smf", `{"data":{"teams":{"nodes":[{"id":"team-smf","key":"SMF","name":"Social Media Factory"},{"id":"team-ops","key":"OPS","name":"Operations"}],"pageInfo":{"hasNextPage":false}}}}`, ""},
		{"unknown", "other", `{"data":{"teams":{"nodes":[{"id":"team-smf","key":"SMF","name":"Social Media Factory"}],"pageInfo":{"hasNextPage":false}}}}`, "ERR_FORBIDDEN"},
		{"ambiguous", "OPS", `{"data":{"teams":{"nodes":[{"id":"OPS","key":"SMF","name":"Social Media Factory"},{"id":"team-ops","key":"OPS","name":"Operations"}],"pageInfo":{"hasNextPage":false}}}}`, "ERR_VALIDATION"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, err := storage.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			secretStore := secrets.NewSessionStore()
			if _, err := secretStore.Put(context.Background(), contract.CredentialInput{Source: "linear", Secret: "test-secret", Persistence: "session"}); err != nil {
				t.Fatal(err)
			}
			calls := 0
			transport := transportFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(tc.response))}, nil
			})
			c, err := NewCore(CoreConfig{Store: store, Secrets: secretStore, HTTPClient: &http.Client{Transport: transport}})
			if err != nil {
				t.Fatal(err)
			}
			plan, err := c.PreparePlan(context.Background(), contract.PlanInput{
				SchemaVersion: contract.Version, LinearEnabled: true,
				Scope:  &contract.Scope{Mode: "selected", Teams: []string{tc.selected}},
				Period: contract.Period{Preset: "30d", Timezone: "UTC"},
			})
			if calls != 1 {
				t.Fatalf("descoberta fez %d requests", calls)
			}
			if tc.wantCode != "" {
				var fault *contract.ErrorResponse
				if !errors.As(err, &fault) || fault.Code != tc.wantCode {
					t.Fatalf("escopo %q: err=%v, esperado %s", tc.selected, err, tc.wantCode)
				}
				return
			}
			if err != nil || len(plan.Resources) != 1 || plan.Resources[0].ID != "team-smf" || plan.Resources[0].Name != "SMF" {
				t.Fatalf("escopo %q: recursos=%+v err=%v", tc.selected, plan.Resources, err)
			}
		})
	}
}
