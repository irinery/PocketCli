package core

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"pocketcli/internal/investigation/collect"
	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/secrets"
	"pocketcli/internal/investigation/storage"
)

type versionTransportFunc func(*http.Request) (*http.Response, error)

func (f versionTransportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestPlanPinsGitHubAPIVersionInPrivateIdentity(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	secretStore := secrets.NewSessionStore()
	if _, err := secretStore.Put(context.Background(), contract.CredentialInput{Source: "github", Secret: "test-secret", Persistence: "session"}); err != nil {
		t.Fatal(err)
	}
	transport := versionTransportFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"id":99,"full_name":"acme/api"}`))}, nil
	})
	c, err := NewCore(CoreConfig{Store: store, Secrets: secretStore, HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	input := contract.PlanInput{SchemaVersion: contract.Version, GitHubEnabled: true, Scope: &contract.Scope{Mode: "selected", Repositories: []string{"acme/api"}}, Period: contract.Period{Preset: "30d", Timezone: "UTC"}, Limits: contract.Limits{GitHubRPH: 3600000}}
	plan, err := c.PreparePlan(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	var identity planAuth
	if err := store.Get("planauth", plan.ID, &identity); err != nil || identity.GitHubAPIVersion != collect.GitHubAPIVersion() {
		t.Fatalf("private plan identity version=%q err=%v", identity.GitHubAPIVersion, err)
	}
	identity.GitHubAPIVersion = "2022-11-28"
	if err := store.Put("planauth", plan.ID, identity); err != nil {
		t.Fatal(err)
	}
	if err := c.checkPlanAuth(context.Background(), plan); err == nil || !strings.Contains(err.Error(), "ERR_PLAN_CHANGED") {
		t.Fatalf("changed API version accepted: %v", err)
	}
}
