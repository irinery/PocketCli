//go:build investigate_sqlite && cgo

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

func TestPrincipalResolutionConsumesDiscoveryBudgetOnlyOnFirstUse(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	secretStore := secrets.NewSessionStore()
	if _, err := secretStore.Put(context.Background(), contract.CredentialInput{Source: "github", Secret: "test-secret", Persistence: "session"}); err != nil {
		t.Fatal(err)
	}
	identityCalls, versionCalls, repoCalls := 0, 0, 0
	transport := transportFunc(func(r *http.Request) (*http.Response, error) {
		body := `{"id":42}`
		if r.URL.Path == "/user" {
			identityCalls++
		} else if r.URL.Path == "/versions" {
			versionCalls++
			body = `["2022-11-28","2026-03-10"]`
		} else if r.URL.Path == "/repos/acme/api" {
			repoCalls++
			body = `{"id":99,"full_name":"acme/api"}`
		} else {
			t.Fatalf("unexpected endpoint: %s", r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	c, err := NewCore(CoreConfig{Store: store, Secrets: secretStore, HTTPClient: &http.Client{Transport: transport}, ResolvePrincipals: true})
	if err != nil {
		t.Fatal(err)
	}
	input := contract.PlanInput{SchemaVersion: contract.Version, GitHubEnabled: true, Scope: &contract.Scope{Mode: "selected", Repositories: []string{"acme/api"}}, Period: contract.Period{Preset: "30d", Timezone: "UTC"}, Limits: contract.Limits{GitHubRPH: 3600000}}
	if _, err := c.PreparePlan(collect.WithDiscoveryBudget(context.Background(), 2), input); err == nil || !strings.Contains(err.Error(), "ERR_RESOURCE_EXHAUSTED") || identityCalls != 0 || versionCalls != 0 || repoCalls != 0 {
		t.Fatalf("budget before network: identity=%d versions=%d repos=%d err=%v", identityCalls, versionCalls, repoCalls, err)
	}
	plan, err := c.PreparePlan(collect.WithDiscoveryBudget(context.Background(), 3), input)
	if err != nil || identityCalls != 1 || versionCalls != 1 || repoCalls != 1 {
		t.Fatalf("first plan: identity=%d versions=%d repos=%d err=%v", identityCalls, versionCalls, repoCalls, err)
	}
	if len(plan.Warnings) < 2 || !strings.Contains(strings.Join(plan.Warnings, " "), "ID 42") || !strings.Contains(strings.Join(plan.Warnings, " "), "2026-03-10") {
		t.Fatalf("verified account missing from plan: %v", plan.Warnings)
	}
	if _, err := c.PreparePlan(collect.WithDiscoveryBudget(context.Background(), 1), input); err != nil || identityCalls != 1 || versionCalls != 1 || repoCalls != 2 {
		t.Fatalf("cached identity and version: identity=%d versions=%d repos=%d err=%v", identityCalls, versionCalls, repoCalls, err)
	}
}

func TestUnsupportedGitHubVersionStopsPlanBeforeRepositoryDiscovery(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	secretStore := secrets.NewSessionStore()
	if _, err := secretStore.Put(context.Background(), contract.CredentialInput{Source: "github", Secret: "test-secret", Persistence: "session"}); err != nil {
		t.Fatal(err)
	}
	repoCalls := 0
	transport := transportFunc(func(r *http.Request) (*http.Response, error) {
		body := `{"id":42}`
		if r.URL.Path == "/versions" {
			body = `["2022-11-28"]`
		} else if r.URL.Path == "/repos/acme/api" {
			repoCalls++
			body = `{"id":99,"full_name":"acme/api"}`
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	c, err := NewCore(CoreConfig{Store: store, Secrets: secretStore, HTTPClient: &http.Client{Transport: transport}, ResolvePrincipals: true})
	if err != nil {
		t.Fatal(err)
	}
	input := contract.PlanInput{SchemaVersion: contract.Version, GitHubEnabled: true, Scope: &contract.Scope{Mode: "selected", Repositories: []string{"acme/api"}}, Period: contract.Period{Preset: "30d", Timezone: "UTC"}, Limits: contract.Limits{GitHubRPH: 3600000}}
	_, err = c.PreparePlan(collect.WithDiscoveryBudget(context.Background(), 3), input)
	if err == nil || !strings.Contains(err.Error(), "ERR_SCHEMA_VERSION_UNSUPPORTED") || repoCalls != 0 {
		t.Fatalf("unsupported version continued discovery: repos=%d err=%v", repoCalls, err)
	}
}
