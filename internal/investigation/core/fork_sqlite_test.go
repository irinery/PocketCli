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

func TestForkDiscoveryRecordsUpstream(t *testing.T) {
	store, _ := storage.Open(t.TempDir())
	ss := secrets.NewSessionStore()
	ss.Put(context.Background(), contract.CredentialInput{Source: "github", Secret: "github-secret", Persistence: "session"})
	transport := transportFunc(func(r *http.Request) (*http.Response, error) {
		body := `{"id":42,"full_name":"acme/sdk","fork":true,"parent":{"full_name":"vendor/sdk"},"created_at":"2023-08-25T00:08:12Z","pushed_at":"2023-06-07T16:01:06Z"}`
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	c, err := NewCore(CoreConfig{Store: store, Secrets: ss, HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	scope := &contract.Scope{Mode: "selected", Repositories: []string{"acme/sdk"}}
	plan, err := c.PreparePlan(context.Background(), contract.PlanInput{SchemaVersion: contract.Version, GitHubEnabled: true, Scope: scope, Period: contract.Period{Preset: "30d", Timezone: "UTC"}, Limits: contract.Limits{GitHubRPH: 3600000}})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Resources) != 1 || !plan.Resources[0].Fork || plan.Resources[0].Parent != "vendor/sdk" {
		t.Fatalf("fork não registrado no plano: %+v", plan.Resources)
	}
	if got := contract.ForkReason(plan.Resources[0]); !strings.Contains(got, "fork de vendor/sdk") {
		t.Fatalf("motivo do fork=%q", got)
	}
}
