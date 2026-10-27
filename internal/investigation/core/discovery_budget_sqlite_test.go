//go:build investigate_sqlite && cgo

package core

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"pocketcli/internal/investigation/collect"
	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/secrets"
	"pocketcli/internal/investigation/storage"
)

func TestSelectedDiscoveryRequiresApprovalBeyondTwentyCalls(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	secretStore := secrets.NewSessionStore()
	if _, err := secretStore.Put(context.Background(), contract.CredentialInput{Source: "github", Secret: "test-secret", Persistence: "session"}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	transport := transportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		name := strings.TrimPrefix(r.URL.Path, "/repos/")
		body := fmt.Sprintf(`{"id":%d,"full_name":%q}`, calls, name)
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	c, err := NewCore(CoreConfig{Store: store, Secrets: secretStore, HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	repos := make([]string, 21)
	for i := range repos {
		repos[i] = fmt.Sprintf("acme/repo-%02d", i)
	}
	input := contract.PlanInput{SchemaVersion: contract.Version, GitHubEnabled: true, Scope: &contract.Scope{Mode: "selected", Repositories: repos}, Period: contract.Period{Preset: "30d", Timezone: "UTC"}, Limits: contract.Limits{GitHubRPH: 3600000}}
	if _, err := c.PreparePlan(context.Background(), input); err == nil || !strings.Contains(err.Error(), "ERR_RESOURCE_EXHAUSTED") || calls != 0 {
		t.Fatalf("default budget: calls=%d err=%v", calls, err)
	}
	plan, err := c.PreparePlan(collect.WithDiscoveryBudget(context.Background(), 21), input)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 21 || len(plan.Resources) != 21 || len(plan.Warnings) == 0 || !strings.Contains(plan.Warnings[0], "21 chamadas") {
		t.Fatalf("approved budget: calls=%d resources=%d warnings=%v", calls, len(plan.Resources), plan.Warnings)
	}
}
