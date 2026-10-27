package collect

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"pocketcli/internal/investigation/contract"
)

func TestDiscoveryBudgetCountsAttemptsPerSource(t *testing.T) {
	githubCalls, linearCalls := 0, 0
	client := secretClient(t, func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "api.github.com" {
			githubCalls++
			return response(200, `[]`), nil
		}
		linearCalls++
		return response(200, `{"data":{"teams":{"nodes":[]}}}`), nil
	})
	ctx := WithDiscoveryBudget(context.Background(), 1)
	if _, _, _, err := client.GetGitHub(ctx, "https://api.github.com/user/repos"); err != nil {
		t.Fatal(err)
	}
	_, _, _, err := client.GetGitHub(ctx, "https://api.github.com/user/repos")
	if coded, ok := err.(*contract.ErrorResponse); !ok || coded.Code != "ERR_RESOURCE_EXHAUSTED" {
		t.Fatalf("second GitHub attempt: %v", err)
	}
	if _, err := client.LinearQuery(ctx, "query Teams { teams { nodes { id } } }", nil); err != nil {
		t.Fatal(err)
	}
	if githubCalls != 1 || linearCalls != 1 {
		t.Fatalf("network calls github=%d linear=%d", githubCalls, linearCalls)
	}
}

func TestDiscoveryBudgetIncludesRetries(t *testing.T) {
	calls := 0
	client := secretClient(t, func(*http.Request) (*http.Response, error) {
		calls++
		return response(500, `{"message":"temporary"}`), nil
	})
	_, _, _, err := client.GetGitHub(WithDiscoveryBudget(context.Background(), 1), "https://api.github.com/user/repos")
	if err == nil || !strings.Contains(err.Error(), "ERR_RESOURCE_EXHAUSTED") || calls != 1 {
		t.Fatalf("retry crossed budget: calls=%d err=%v", calls, err)
	}
}
