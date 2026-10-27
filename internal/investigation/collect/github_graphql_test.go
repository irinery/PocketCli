package collect

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestGitHubGraphQLRejectsMutationAndPartialErrors(t *testing.T) {
	calls := 0
	c := secretClient(t, func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != "POST" || r.URL.Path != "/graphql" || r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("unexpected GraphQL request: %s %s", r.Method, r.URL)
		}
		return response(200, `{"data":{"repository":{"issue":null}},"errors":[{"message":"topsecret hidden"}]}`), nil
	})
	if _, _, err := c.GitHubQueryWithBytes(context.Background(), "mutation Bad { x }", nil); err == nil || calls != 0 {
		t.Fatalf("mutation reached network: calls=%d err=%v", calls, err)
	}
	_, transferred, err := c.GitHubQueryWithBytes(context.Background(), "query Read { viewer { login } }", nil)
	if err == nil || transferred == 0 || strings.Contains(err.Error(), "topsecret") || calls != 1 {
		t.Fatalf("partial GraphQL error: calls=%d bytes=%d err=%v", calls, transferred, err)
	}
}

func TestGitHubGraphQLRateLimitCodeIsRecognized(t *testing.T) {
	if !githubGraphQLRateLimited([]byte(`{"errors":[{"type":"RATE_LIMITED"}]}`)) || !githubGraphQLRateLimited([]byte(`{"errors":[{"extensions":{"code":"RATE_LIMITED"}}]}`)) || githubGraphQLRateLimited([]byte(`{"errors":[{"type":"FORBIDDEN"}]}`)) {
		t.Fatal("GraphQL rate limit classification")
	}
}
