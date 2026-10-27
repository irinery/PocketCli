package collect

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/secrets"
	"pocketcli/internal/investigation/storage"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func secretClient(t *testing.T, fn transportFunc) *Client {
	t.Helper()
	store := secrets.NewSessionStore()
	for _, source := range []string{"github", "linear"} {
		if _, err := store.Put(context.Background(), contract.CredentialInput{Source: source, Secret: "topsecret", Persistence: "session"}); err != nil {
			t.Fatal(err)
		}
	}
	return NewClient(&http.Client{Transport: fn}, store)
}
func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func TestLinearRateLimitedEvenWithHTTP400(t *testing.T) {
	c := secretClient(t, func(r *http.Request) (*http.Response, error) {
		if r.Method != "POST" || r.Header.Get("Authorization") != "topsecret" {
			t.Fatal("invalid Linear request")
		}
		return response(400, `{"errors":[{"message":"rate","extensions":{"code":"RATELIMITED"}}]}`), nil
	})
	_, err := c.LinearQuery(context.Background(), "query Teams { teams { nodes { id } } }", nil)
	if err == nil || !strings.Contains(err.Error(), "ERR_RATE_LIMITED") {
		t.Fatalf("err=%v", err)
	}
}

func TestGitHubPaginationRejectsExternalHostAndSerializes(t *testing.T) {
	var mu sync.Mutex
	active, maxActive, calls := 0, 0, 0
	c := secretClient(t, func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		active++
		calls++
		if active > maxActive {
			maxActive = active
		}
		mu.Unlock()
		time.Sleep(10 * time.Millisecond)
		mu.Lock()
		active--
		mu.Unlock()
		res := response(200, `[]`)
		res.Header.Set("Link", `<https://evil.invalid/steal>; rel="next"`)
		return res, nil
	})
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := c.GitHubList(context.Background(), "/user/repos", 2)
			if err == nil || !strings.Contains(err.Error(), "ERR_UNSUPPORTED_SOURCE") {
				t.Errorf("err=%v", err)
			}
		}()
	}
	wg.Wait()
	if maxActive != 1 || calls != 2 {
		t.Fatalf("active=%d calls=%d", maxActive, calls)
	}
}

func TestGitHubForbiddenIsNotEmptyList(t *testing.T) {
	c := secretClient(t, func(*http.Request) (*http.Response, error) {
		return response(403, `{"message":"denied topsecret"}`), nil
	})
	items, _, err := c.GitHubList(context.Background(), "/user/repos", 1)
	if err == nil || len(items) != 0 || strings.Contains(err.Error(), "topsecret") {
		t.Fatalf("items=%d err=%v", len(items), err)
	}
}

func TestCredentialEchoCannotReachPersistentCacheOrCaller(t *testing.T) {
	for _, tc := range []struct {
		name, body, header string
		status             int
	}{
		{"body", `[{"title":"topsecret"}]`, "", 200},
		{"escaped JSON", `[{"title":"topse\u0063ret"}]`, "", 200},
		{"pagination header", `[]`, `<https://api.github.com/user/repos?token=topsecret>; rel="next"`, 200},
		{"error body", `{"message":"denied topsecret"}`, "", 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			store, err := storage.Open(root)
			if err != nil {
				t.Fatal(err)
			}
			client := secretClient(t, func(*http.Request) (*http.Response, error) {
				res := response(tc.status, tc.body)
				res.Header.Set("ETag", `"echoed"`)
				if tc.header != "" {
					res.Header.Set("Link", tc.header)
				}
				return res, nil
			})
			client.SetCacheStore(store)
			data, headers, _, _, err := client.GetGitHubWithBytes(WithRunID(context.Background(), "run-echo"), "https://api.github.com/user/repos")
			expectedCode := "ERR_UNAVAILABLE"
			if tc.status == http.StatusForbidden {
				expectedCode = "ERR_FORBIDDEN"
			}
			if err == nil || !strings.Contains(err.Error(), expectedCode) || strings.Contains(err.Error(), "topsecret") || data != nil || headers != nil {
				t.Fatalf("resposta com credencial chegou ao chamador: data=%q headers=%v err=%v", data, headers, err)
			}
			if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if entry.IsDir() {
					return nil
				}
				content, readErr := os.ReadFile(path)
				if readErr != nil {
					return readErr
				}
				if strings.Contains(string(content), "topsecret") || strings.Contains(string(content), `topse\u0063ret`) {
					t.Errorf("credencial persistida em %s", path)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestGitHubWalkStopsBeforeNextRequestWhenCommitFails(t *testing.T) {
	calls := 0
	c := secretClient(t, func(r *http.Request) (*http.Response, error) {
		calls++
		res := response(200, `[{"id":1}]`)
		res.Header.Set("Link", `<https://api.github.com/repos/acme/api/pulls?page=2>; rel="next"`)
		return res, nil
	})
	count, err := c.GitHubWalk(context.Background(), "/repos/acme/api/pulls", 2, "", func(_ []json.RawMessage, _ string, _ int64) error {
		return errors.New("disk full")
	})
	if err == nil || count != 1 || calls != 1 {
		t.Fatalf("count=%d calls=%d err=%v", count, calls, err)
	}
}

func TestGitHub304UsesOnlyMatchingCachedRepresentation(t *testing.T) {
	calls := 0
	c := secretClient(t, func(r *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			if r.Header.Get("If-None-Match") != "" {
				t.Fatal("first request sent conditional header")
			}
			res := response(200, `[{"id":1}]`)
			res.Header.Set("ETag", `"version-one"`)
			return res, nil
		}
		if r.Header.Get("If-None-Match") != `"version-one"` {
			t.Fatalf("missing conditional header: %q", r.Header.Get("If-None-Match"))
		}
		return response(304, ""), nil
	})
	for i := 0; i < 2; i++ {
		items, _, err := c.GitHubList(context.Background(), "/repos/acme/api/pulls", 1)
		if err != nil || len(items) != 1 || string(items[0]) != `{"id":1}` {
			t.Fatalf("round %d: items=%s err=%v", i, items, err)
		}
	}
	if calls != 2 {
		t.Fatalf("calls=%d", calls)
	}
}

func TestGitHub304WithoutCacheRetriesUnconditionally(t *testing.T) {
	calls := 0
	c := secretClient(t, func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Header.Get("If-None-Match") != "" {
			t.Fatal("unseeded request used conditional header")
		}
		if calls == 1 {
			return response(304, ""), nil
		}
		return response(200, `[{"id":2}]`), nil
	})
	items, _, err := c.GitHubList(context.Background(), "/repos/acme/api/pulls", 1)
	if err != nil || calls != 2 || len(items) != 1 {
		t.Fatalf("calls=%d items=%s err=%v", calls, items, err)
	}
}
