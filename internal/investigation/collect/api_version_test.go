package collect

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/storage"
)

func TestGitHubVersionProbeIsCachedAcrossClients(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	identityCalls, versionCalls := 0, 0
	transport := transportFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/user":
			identityCalls++
			return response(200, `{"id":42}`), nil
		case "/versions":
			versionCalls++
			res := response(200, `["2022-11-28","2026-03-10"]`)
			res.Header.Set("X-Github-Api-Version-Selected", "2026-03-10")
			return res, nil
		default:
			t.Fatalf("unexpected API path: %s", r.URL.Path)
			return nil, nil
		}
	})
	first := principalClient(t, store, "token-a", transport)
	if version, err := first.VerifyGitHubVersion(WithAuditID(context.Background(), "version-probe")); err != nil || version != githubAPIVersion {
		t.Fatalf("first version=%q err=%v", version, err)
	}
	receipts := receiptsFor(t, store, "version-probe")
	if len(receipts) != 2 {
		t.Fatalf("identity and version receipts=%d", len(receipts))
	}
	seenVersion := false
	for _, receipt := range receipts {
		if receipt.Endpoint == "/versions" && receipt.SelectedAPIVersion == githubAPIVersion {
			seenVersion = true
		}
	}
	if !seenVersion {
		t.Fatalf("version endpoint missing from receipts: %+v", receipts)
	}
	second := principalClient(t, store, "token-a", transport)
	if needed, err := second.GitHubVersionLookupNeeded(context.Background()); err != nil || needed {
		t.Fatalf("cached version needed=%v err=%v", needed, err)
	}
	if version, err := second.VerifyGitHubVersion(context.Background()); err != nil || version != githubAPIVersion {
		t.Fatalf("second version=%q err=%v", version, err)
	}
	if identityCalls != 1 || versionCalls != 1 {
		t.Fatalf("uncached probes: identity=%d version=%d", identityCalls, versionCalls)
	}
}

func TestGitHubVersionProbeRejectsUnsupportedAndWrongSelection(t *testing.T) {
	for _, tc := range []struct {
		name, body, selected string
	}{
		{"unsupported", `["2022-11-28"]`, "2022-11-28"},
		{"wrong selection", `["2026-03-10"]`, "2022-11-28"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, err := storage.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			transport := transportFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/user" {
					return response(200, `{"id":42}`), nil
				}
				res := response(200, tc.body)
				res.Header.Set("X-Github-Api-Version-Selected", tc.selected)
				return res, nil
			})
			client := principalClient(t, store, "token-a", transport)
			_, err = client.VerifyGitHubVersion(context.Background())
			var failure *contract.ErrorResponse
			if !errors.As(err, &failure) || failure.Code != "ERR_SCHEMA_VERSION_UNSUPPORTED" {
				t.Fatalf("version mismatch accepted: %v", err)
			}
			if needed, err := client.GitHubVersionLookupNeeded(context.Background()); err != nil || !needed {
				t.Fatalf("unsupported version cached: needed=%v err=%v", needed, err)
			}
		})
	}
}

func TestGitHubVersionProbeKeepsEnterpriseAPIPrefix(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	transport := transportFunc(func(r *http.Request) (*http.Response, error) {
		paths = append(paths, r.URL.Path)
		if r.URL.Path == "/api/v3/user" {
			return response(200, `{"id":42}`), nil
		}
		return response(200, `["2026-03-10"]`), nil
	})
	client := principalClient(t, store, "token-a", transport)
	client.GitHubURL = "https://github.example.com/api/v3"
	if _, err := client.VerifyGitHubVersion(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 || paths[0] != "/api/v3/user" || paths[1] != "/api/v3/versions" {
		t.Fatalf("enterprise API paths: %v", paths)
	}
}

func TestCorruptGitHubVersionCacheStopsBeforeNetwork(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	client := principalClient(t, store, "token-a", transportFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("corrupt version cache reached the network")
		return nil, nil
	}))
	key := principalHash("github-version", client.GitHubURL, githubAPIVersion)
	if err := store.Put(githubVersionCheckKind, key, githubVersionCheck{BaseURL: client.GitHubURL, Version: "2022-11-28", CheckedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	_, err = client.VerifyGitHubVersion(context.Background())
	var failure *contract.ErrorResponse
	if !errors.As(err, &failure) || failure.Code != "ERR_AUDIT_FAILED" {
		t.Fatalf("corrupt version cache accepted: %v", err)
	}
}
