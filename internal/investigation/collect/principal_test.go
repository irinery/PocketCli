package collect

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/secrets"
	"pocketcli/internal/investigation/storage"
)

func principalClient(t *testing.T, store *storage.Store, token string, transport http.RoundTripper) *Client {
	t.Helper()
	secretStore := secrets.NewSessionStore()
	if _, err := secretStore.Put(context.Background(), contract.CredentialInput{Source: "github", Secret: token, Persistence: "session"}); err != nil {
		t.Fatal(err)
	}
	client := NewClient(&http.Client{Transport: transport}, secretStore)
	client.SetCacheStore(store)
	client.SetRPH("github", 3600000)
	client.EnablePrincipalResolution()
	return client
}

func TestTwoCredentialsOfSameActorShareRequestQueue(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	active, maxActive, identityCalls, businessCalls := 0, 0, 0, 0
	transport := transportFunc(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		if r.URL.Path == "/user" {
			identityCalls++
			mu.Unlock()
			return response(200, `{"id":42}`), nil
		}
		businessCalls++
		active++
		if active > maxActive {
			maxActive = active
		}
		mu.Unlock()
		time.Sleep(35 * time.Millisecond)
		mu.Lock()
		active--
		mu.Unlock()
		return response(200, `[]`), nil
	})
	first := principalClient(t, store, "token-a", transport)
	second := principalClient(t, store, "token-b", transport)
	const endpoint = "https://api.github.com/user/repos"
	var wg sync.WaitGroup
	for _, client := range []*Client{first, second} {
		wg.Add(1)
		go func(client *Client) {
			defer wg.Done()
			if _, _, _, err := client.GetGitHub(context.Background(), endpoint); err != nil {
				t.Errorf("request: %v", err)
			}
		}(client)
	}
	wg.Wait()
	if maxActive != 1 || identityCalls != 2 || businessCalls != 2 {
		t.Fatalf("active=%d identity=%d business=%d", maxActive, identityCalls, businessCalls)
	}
	for _, client := range []*Client{first, second} {
		if id, err := client.PrincipalID(context.Background(), "github"); err != nil || id != "42" {
			t.Fatalf("principal id=%q err=%v", id, err)
		}
	}
}

func TestPrincipalLookupKeepsGitHubAPIPrefix(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var requested []string
	transport := transportFunc(func(r *http.Request) (*http.Response, error) {
		requested = append(requested, r.URL.Path)
		if r.URL.Path == "/api/v3/user" {
			return response(200, `{"id":42}`), nil
		}
		return response(200, `[]`), nil
	})
	client := principalClient(t, store, "token-a", transport)
	client.GitHubURL = "https://github.example.com/api/v3"
	if _, _, _, err := client.GetGitHub(context.Background(), "https://github.example.com/api/v3/user/repos"); err != nil {
		t.Fatal(err)
	}
	if len(requested) != 2 || requested[0] != "/api/v3/user" || requested[1] != "/api/v3/user/repos" {
		t.Fatalf("unexpected GitHub Enterprise API paths: %v", requested)
	}
}

func TestLegacyGitHubBindingRefreshesLoginOnce(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const token = "token-a"
	const host = "api.github.com"
	key := principalCredentialKey("github", host, []byte(token))
	legacy := principalBinding{CredentialKey: key, Source: "github", Host: host, PrincipalID: "42", PrincipalKey: principalHash("github", host, "42")}
	if err := store.Put(principalBindingKind, key, legacy); err != nil {
		t.Fatal(err)
	}
	identityCalls := 0
	transport := transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/user" {
			t.Fatalf("unexpected endpoint: %s", r.URL.Path)
		}
		identityCalls++
		return response(200, `{"id":42,"login":"octocat"}`), nil
	})
	client := principalClient(t, store, token, transport)
	needed, err := client.PrincipalLookupNeeded(context.Background(), "github")
	if err != nil || !needed {
		t.Fatalf("legacy lookup needed=%v err=%v", needed, err)
	}
	if err := client.ensurePrincipal(context.Background(), "github"); err != nil {
		t.Fatal(err)
	}
	if err := client.ensurePrincipal(context.Background(), "github"); err != nil {
		t.Fatal(err)
	}
	login, err := client.PrincipalLogin(context.Background(), "github")
	if err != nil || login != "octocat" || identityCalls != 1 {
		t.Fatalf("login=%q identity calls=%d err=%v", login, identityCalls, err)
	}
}

func TestLegacyLinearBindingRefreshesWorkspaceOnce(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const token = "linear-token"
	const host = "api.linear.app"
	key := principalCredentialKey("linear", host, []byte(token))
	legacy := principalBinding{CredentialKey: key, Source: "linear", Host: host, PrincipalID: "user-42", PrincipalKey: principalHash("linear", host, "user-42"), Version: 1}
	if err := store.Put(principalBindingKind, key, legacy); err != nil {
		t.Fatal(err)
	}
	identityCalls := 0
	transport := transportFunc(func(r *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, err
		}
		if !strings.Contains(string(body), "organization { id urlKey }") {
			t.Fatalf("workspace missing from identity query: %s", body)
		}
		identityCalls++
		return response(200, `{"data":{"viewer":{"id":"user-42"},"organization":{"id":"workspace-7","urlKey":"smf"}}}`), nil
	})
	secretStore := secrets.NewSessionStore()
	if _, err := secretStore.Put(context.Background(), contract.CredentialInput{Source: "linear", Secret: token, Persistence: "session"}); err != nil {
		t.Fatal(err)
	}
	client := NewClient(&http.Client{Transport: transport}, secretStore)
	client.SetCacheStore(store)
	client.SetRPH("linear", 3600000)
	client.EnablePrincipalResolution()
	needed, err := client.PrincipalLookupNeeded(context.Background(), "linear")
	if err != nil || !needed {
		t.Fatalf("legacy lookup needed=%v err=%v", needed, err)
	}
	if err := client.ensurePrincipal(context.Background(), "linear"); err != nil {
		t.Fatal(err)
	}
	if err := client.ensurePrincipal(context.Background(), "linear"); err != nil {
		t.Fatal(err)
	}
	id, workspace, err := client.PrincipalWorkspace(context.Background())
	if err != nil || id != "workspace-7" || workspace != "smf" || identityCalls != 1 {
		t.Fatalf("workspace id=%q key=%q calls=%d err=%v", id, workspace, identityCalls, err)
	}
}

func TestCredentialRotationDuringLookupStopsBeforeBusinessRequest(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	secretStore := secrets.NewSessionStore()
	if _, err := secretStore.Put(context.Background(), contract.CredentialInput{Source: "github", Secret: "token-a", Persistence: "session"}); err != nil {
		t.Fatal(err)
	}
	business := 0
	transport := transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/user" {
			if r.Header.Get("Authorization") == "Bearer token-a" {
				if _, err := secretStore.Put(context.Background(), contract.CredentialInput{Source: "github", Secret: "token-b", Persistence: "session"}); err != nil {
					t.Error(err)
				}
			}
			return response(200, `{"id":42}`), nil
		}
		business++
		return response(200, `[]`), nil
	})
	client := NewClient(&http.Client{Transport: transport}, secretStore)
	client.SetCacheStore(store)
	client.SetRPH("github", 3600000)
	client.EnablePrincipalResolution()
	_, _, _, err = client.GetGitHub(context.Background(), "https://api.github.com/user/repos")
	var changed *contract.ErrorResponse
	if !errors.As(err, &changed) || changed.Code != "ERR_PLAN_CHANGED" || business != 0 {
		t.Fatalf("rotated credential reached business request: business=%d err=%v", business, err)
	}
}

func TestSameActorSharesRateHoldAcrossCredentials(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	business := 0
	transport := transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/user" {
			return response(200, `{"id":42}`), nil
		}
		business++
		return response(200, `[]`), nil
	})
	first := principalClient(t, store, "token-a", transport)
	second := principalClient(t, store, "token-b", transport)
	for _, client := range []*Client{first, second} {
		if err := client.ensurePrincipal(context.Background(), "github"); err != nil {
			t.Fatal(err)
		}
	}
	if err := first.saveRateHold(context.Background(), "github", []byte("token-a"), 31*time.Second); err != nil {
		t.Fatal(err)
	}
	_, _, _, err = second.GetGitHub(context.Background(), "https://api.github.com/user/repos")
	var limited *contract.ErrorResponse
	if !errors.As(err, &limited) || limited.Code != "ERR_RATE_LIMITED" || business != 0 {
		t.Fatalf("shared hold: business=%d err=%v", business, err)
	}
}

func TestTwoLinearKeysOfSameViewerShareQueue(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	active, maxActive, identityCalls, businessCalls := 0, 0, 0, 0
	transport := transportFunc(func(r *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, err
		}
		if strings.Contains(string(body), "PocketCliViewer") {
			mu.Lock()
			identityCalls++
			mu.Unlock()
			return response(200, `{"data":{"viewer":{"id":"user-42"},"organization":{"id":"workspace-7","urlKey":"smf"}}}`), nil
		}
		mu.Lock()
		businessCalls++
		active++
		if active > maxActive {
			maxActive = active
		}
		mu.Unlock()
		time.Sleep(35 * time.Millisecond)
		mu.Lock()
		active--
		mu.Unlock()
		return response(200, `{"data":{"teams":{"nodes":[]}}}`), nil
	})
	clients := make([]*Client, 2)
	for i, token := range []string{"linear-a", "linear-b"} {
		secretStore := secrets.NewSessionStore()
		if _, err := secretStore.Put(context.Background(), contract.CredentialInput{Source: "linear", Secret: token, Persistence: "session"}); err != nil {
			t.Fatal(err)
		}
		client := NewClient(&http.Client{Transport: transport}, secretStore)
		client.SetCacheStore(store)
		client.SetRPH("linear", 3600000)
		client.EnablePrincipalResolution()
		clients[i] = client
	}
	var wg sync.WaitGroup
	for _, client := range clients {
		wg.Add(1)
		go func(client *Client) {
			defer wg.Done()
			if _, err := client.LinearQuery(context.Background(), "query Teams { teams { nodes { id } } }", nil); err != nil {
				t.Errorf("Linear request: %v", err)
			}
		}(client)
	}
	wg.Wait()
	if maxActive != 1 || identityCalls != 2 || businessCalls != 2 {
		t.Fatalf("active=%d identity=%d business=%d", maxActive, identityCalls, businessCalls)
	}
	for _, client := range clients {
		id, key, err := client.PrincipalWorkspace(context.Background())
		if err != nil || id != "workspace-7" || key != "smf" {
			t.Fatalf("workspace id=%q key=%q err=%v", id, key, err)
		}
	}
}

func TestDifferentActorsCanRequestConcurrently(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	transport := transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/user" {
			if r.Header.Get("Authorization") == "Bearer token-a" {
				return response(200, `{"id":42}`), nil
			}
			return response(200, `{"id":43}`), nil
		}
		entered <- struct{}{}
		<-release
		return response(200, `[]`), nil
	})
	clients := []*Client{principalClient(t, store, "token-a", transport), principalClient(t, store, "token-b", transport)}
	for _, client := range clients {
		if err := client.ensurePrincipal(context.Background(), "github"); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	for _, client := range clients {
		wg.Add(1)
		go func(client *Client) {
			defer wg.Done()
			if _, _, _, err := client.GetGitHub(context.Background(), "https://api.github.com/user/repos"); err != nil {
				t.Errorf("request: %v", err)
			}
		}(client)
	}
	concurrent := true
	for i := 0; i < 2; i++ {
		select {
		case <-entered:
		case <-time.After(2 * time.Second):
			concurrent = false
		}
	}
	close(release)
	wg.Wait()
	if !concurrent {
		t.Fatal("different actors were serialized by the provider barrier")
	}
}

func TestUnknownCredentialDoesNotProbeDuringProviderHold(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	identityCalls, businessCalls := 0, 0
	transport := transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/user" {
			identityCalls++
			return response(200, `{"id":42}`), nil
		}
		businessCalls++
		res := response(429, `{"message":"rate limited"}`)
		res.Header.Set("Retry-After", "31")
		return res, nil
	})
	first := principalClient(t, store, "token-a", transport)
	second := principalClient(t, store, "token-b", transport)
	_, _, _, err = first.GetGitHub(context.Background(), "https://api.github.com/user/repos")
	var limited *contract.ErrorResponse
	if !errors.As(err, &limited) || limited.Code != "ERR_RATE_LIMITED" {
		t.Fatalf("first rate limit: %v", err)
	}
	_, _, _, err = second.GetGitHub(context.Background(), "https://api.github.com/user/repos")
	if !errors.As(err, &limited) || limited.Code != "ERR_RATE_LIMITED" || identityCalls != 1 || businessCalls != 1 {
		t.Fatalf("unknown credential probed during hold: identity=%d business=%d err=%v", identityCalls, businessCalls, err)
	}
}

// A refused key names the source and the cause, so the operator knows which
// of two keys to fix and whether it is wrong (401) or short on permission (403).
func TestDeniedKeyNamesSourceAndCause(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		status int
		want   []string
	}{
		{401, []string{"GitHub recusou a chave", "HTTP 401", "revogada"}},
		{403, []string{"GitHub negou a leitura", "HTTP 403", "SSO"}},
	} {
		client := principalClient(t, store, "token-denied", transportFunc(func(r *http.Request) (*http.Response, error) {
			return response(test.status, `{"message":"denied"}`), nil
		}))
		_, _, _, _, err := client.GetGitHubWithBytes(context.Background(), "https://api.github.com/user")
		var coded *contract.ErrorResponse
		if !errors.As(err, &coded) || coded.Code != "ERR_FORBIDDEN" {
			t.Fatalf("HTTP %d: erro %v", test.status, err)
		}
		for _, want := range test.want {
			if !strings.Contains(coded.Message, want) {
				t.Fatalf("HTTP %d: mensagem %q sem %q", test.status, coded.Message, want)
			}
		}
	}
	linear := secrets.NewSessionStore()
	if _, err := linear.Put(context.Background(), contract.CredentialInput{Source: "linear", Secret: "lin_api_revoked", Persistence: "session"}); err != nil {
		t.Fatal(err)
	}
	client := NewClient(&http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		return response(200, `{"errors":[{"message":"Authentication required","extensions":{"code":"AUTHENTICATION_ERROR"}}]}`), nil
	})}, linear)
	_, err = client.LinearQuery(context.Background(), "query Viewer { viewer { id } }", nil)
	var coded *contract.ErrorResponse
	if !errors.As(err, &coded) || coded.Code != "ERR_FORBIDDEN" || !strings.Contains(coded.Message, "Linear recusou a chave") {
		t.Fatalf("Linear AUTHENTICATION_ERROR: %v", err)
	}
}
