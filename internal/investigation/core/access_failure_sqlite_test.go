//go:build investigate_sqlite && cgo

package core

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"pocketcli/internal/investigation/collect"
	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/query"
	"pocketcli/internal/investigation/secrets"
	"pocketcli/internal/investigation/storage"
)

func TestRejectedCredentialStopsSourceBeforeNextCollection(t *testing.T) {
	for _, source := range []string{"github", "linear"} {
		t.Run(source, func(t *testing.T) {
			store, err := storage.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			ss := secrets.NewSessionStore()
			_, _ = ss.Put(context.Background(), contract.CredentialInput{Source: source, Secret: "synthetic-token", Persistence: "session"})
			calls := 0
			c, err := NewCore(CoreConfig{Store: store, Secrets: ss, HTTPClient: &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: 401, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"message":"synthetic-denial-body"}`))}, nil
			})}})
			if err != nil {
				t.Fatal(err)
			}
			plan := contract.Plan{ID: "access-plan", InvestigationID: "test-investigation", WindowEnd: "2026-02-01T00:00:00Z", Input: contract.PlanInput{GitHubEnabled: source == "github", LinearEnabled: source == "linear", Modules: contract.Modules{GitHubIssues: true}, Limits: contract.Limits{MaxTransferBytes: 1 << 20}}}
			r := contract.Resource{ID: "test-resource", Source: source, Name: "example/repository"}
			d, run := query.Dataset{}, &contract.Run{ID: "access-run"}
			if source == "github" {
				err = c.collectRepository(context.Background(), plan, r, contract.Snapshot{}, &d, run)
			} else {
				err = c.collectTeam(context.Background(), plan, r, &d, run)
			}
			var coded *contract.ErrorResponse
			if !collect.AuthenticationRejected(err) || !errors.As(err, &coded) || coded.Code != "ERR_FORBIDDEN" || calls != 1 {
				t.Fatalf("calls=%d err=%v", calls, err)
			}
			if strings.Contains(err.Error(), "synthetic-denial-body") {
				t.Fatal("error reflected external response")
			}
		})
	}
}

func TestSelectedRepositoryNotVisibleExplainsPATAccess(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ss := secrets.NewSessionStore()
	_, _ = ss.Put(context.Background(), contract.CredentialInput{Source: "github", Secret: "synthetic-token", Persistence: "session"})
	c, err := NewCore(CoreConfig{Store: store, Secrets: ss, HTTPClient: &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 404, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"message":"not found"}`))}, nil
	})}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.discoverRepository(context.Background(), "example/repository", false)
	var coded *contract.ErrorResponse
	if !errors.As(err, &coded) || coded.Code != "ERR_FORBIDDEN" || !strings.Contains(err.Error(), "404") || !strings.Contains(err.Error(), "PAT") {
		t.Fatalf("err=%v", err)
	}
}
