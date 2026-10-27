package collect

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/secrets"
	"pocketcli/internal/investigation/storage"
)

func TestGitHub304ReusesPersistedRepresentationWithoutTransferBytes(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const endpoint = "https://api.github.com/repos/acme/api/pulls"
	const body = `[{"id":1}]`
	calls := 0
	transport := transportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			if got := r.Header.Get("If-None-Match"); got != "" {
				t.Fatalf("primeira request condicional: %q", got)
			}
			res := response(200, body)
			res.Header.Set("ETag", `"v1"`)
			res.Header.Set("Link", `<https://api.github.com/repos/acme/api/pulls?page=2>; rel="next"`)
			return res, nil
		}
		if got := r.Header.Get("If-None-Match"); got != `"v1"` {
			t.Fatalf("refresh sem ETag persistido: %q", got)
		}
		return response(304, ""), nil
	})
	first := secretClient(t, transport)
	first.SetCacheStore(store)
	data, _, status, transferred, err := first.GetGitHubWithBytes(context.Background(), endpoint)
	if err != nil || status != 200 || string(data) != body || transferred != int64(len(body)) {
		t.Fatalf("primeira request: status=%d bytes=%d data=%s err=%v", status, transferred, data, err)
	}
	second := secretClient(t, transport) // Fresh client, as on a new CLI invocation.
	second.SetCacheStore(store)
	runCtx := WithRunID(context.Background(), "run-refresh")
	data, headers, status, transferred, err := second.GetGitHubWithBytes(runCtx, endpoint)
	if err != nil || status != 200 || string(data) != body || transferred != 0 || calls != 2 || headers.Get("Link") == "" {
		t.Fatalf("304 persistido: status=%d bytes=%d calls=%d headers=%v data=%s err=%v", status, transferred, calls, headers, data, err)
	}
	var committedBytes int64 = -1
	_, err = second.GitHubWalk(runCtx, "/repos/acme/api/pulls", 1, "", func(_ []json.RawMessage, _ string, n int64) error {
		committedBytes = n
		return nil
	})
	if err == nil || committedBytes != 0 {
		// The cached Link advertises a second page; maxPages=1 must report
		// incomplete, while the committed first page transfers zero bytes.
		t.Fatalf("walk 304: committed=%d err=%v", committedBytes, err)
	}
	var stats RunStats
	if err := store.Get(runStatsKind, "run-refresh", &stats); err != nil || stats.ReuseCounter != 2 {
		t.Fatalf("contador de 304: %+v err=%v", stats, err)
	}
	if err := RecordReuse(store, "run-refresh"); err != nil {
		t.Fatal(err)
	}
	if err := store.Get(runStatsKind, "run-refresh", &stats); err != nil || stats.ReuseCounter != 3 {
		t.Fatalf("contador persistido após objeto Git: %+v err=%v", stats, err)
	}
}

func TestLegacyCacheEchoIsDiscardedBeforeNetworkOrReuse(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const endpoint = "https://api.github.com/user/repos"
	body := []byte(`[{"title":"topsecret"}]`)
	keyHash := sha256.Sum256([]byte("github:" + endpoint + ":topsecret"))
	key := hex.EncodeToString(keyHash[:])
	bodyHash := sha256.Sum256(body)
	if err := store.Put(cacheKind, cacheSlot(key), persistedETagEntry{Key: key, ETag: `"old"`, Body: body, SHA256: hex.EncodeToString(bodyHash[:])}); err != nil {
		t.Fatal(err)
	}
	client := secretClient(t, func(*http.Request) (*http.Response, error) {
		t.Fatal("cache com credencial não deveria chegar à rede")
		return nil, nil
	})
	client.SetCacheStore(store)
	data, _, _, _, err := client.GetGitHubWithBytes(context.Background(), endpoint)
	if err == nil || !strings.Contains(err.Error(), "ERR_AUDIT_FAILED") || data != nil || strings.Contains(err.Error(), "topsecret") {
		t.Fatalf("cache legado não foi bloqueado: data=%s err=%v", data, err)
	}
	content, err := os.ReadFile(filepath.Join(store.Root, cacheKind, cacheSlot(key)+".json"))
	if err != nil || strings.Contains(string(content), "topsecret") {
		t.Fatalf("slot de cache não foi descartado: %s err=%v", content, err)
	}
}

func TestLegacyBase64BlobCacheEchoIsDiscarded(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const endpoint = "https://api.github.com/repos/acme/api/git/blobs/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	encoded := base64.StdEncoding.EncodeToString([]byte("module example.com/acme/api\n// topsecret\n"))
	body := []byte(`{"encoding":"base64","content":"` + encoded + `"}`)
	keyHash := sha256.Sum256([]byte("github:" + endpoint + ":topsecret"))
	key := hex.EncodeToString(keyHash[:])
	bodyHash := sha256.Sum256(body)
	if err := store.Put(cacheKind, cacheSlot(key), persistedETagEntry{Key: key, ETag: `"old"`, Body: body, SHA256: hex.EncodeToString(bodyHash[:])}); err != nil {
		t.Fatal(err)
	}
	client := secretClient(t, func(*http.Request) (*http.Response, error) {
		t.Fatal("cache antigo com blob contendo credencial chegou à rede")
		return nil, nil
	})
	client.SetCacheStore(store)
	data, _, _, _, err := client.GetGitHubWithBytes(context.Background(), endpoint)
	if err == nil || !strings.Contains(err.Error(), "ERR_AUDIT_FAILED") || data != nil {
		t.Fatalf("cache de blob não foi bloqueado: data=%s err=%v", data, err)
	}
	var discarded persistedETagEntry
	if err := store.Get(cacheKind, cacheSlot(key), &discarded); err != nil || discarded.Key != "" || len(discarded.Body) != 0 {
		t.Fatalf("cache de blob não foi descartado: %+v err=%v", discarded, err)
	}
}

func TestGitObjectByteAuditPreservesReuseAndFailsClosed(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := RecordReuse(store, "run-git"); err != nil {
		t.Fatal(err)
	}
	if err := RecordGitObjectBytes(store, "run-git", GitRefObjects, 512); err != nil {
		t.Fatal(err)
	}
	if err := RecordGitObjectBytes(store, "run-git", GitBranchObjects, 128); err != nil {
		t.Fatal(err)
	}
	if err := RecordGitObjectBytes(store, "run-git", GitTreeObjects, 64); err != nil {
		t.Fatal(err)
	}
	var stats RunStats
	if err := store.Get(runStatsKind, "run-git", &stats); err != nil || stats.ReuseCounter != 1 || stats.GitObjectBytesAdded != 704 || stats.GitRefBytesAdded != 512 || stats.GitBranchBytesAdded != 128 || stats.GitTreeBytesAdded != 64 {
		t.Fatalf("contadores não preservados: %+v err=%v", stats, err)
	}
	if err := RecordGitObjectBytes(failingCacheStore{}, "run-git", GitRefObjects, 1); err == nil {
		t.Fatal("falha de persistência não interrompeu auditoria Git")
	}
	if err := RecordGitObjectBytes(store, "run-git", GitRefObjects, -1); err == nil {
		t.Fatal("contagem Git negativa aceita")
	}
	if err := RecordGitObjectBytes(store, "run-git", "unknown", 1); err == nil {
		t.Fatal("origem Git inválida aceita")
	}
}

func TestPersistedCacheIsScopedToCredentialAndRejectsCorruption(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const endpoint = "https://api.github.com/repos/acme/api/pulls"
	calls := 0
	transport := transportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Header.Get("If-None-Match") != "" {
			t.Fatal("credencial diferente reutilizou ETag")
		}
		res := response(200, `[{"id":1}]`)
		res.Header.Set("ETag", `"v1"`)
		return res, nil
	})
	first := secretClient(t, transport)
	first.SetCacheStore(store)
	if _, _, _, _, err := first.GetGitHubWithBytes(context.Background(), endpoint); err != nil {
		t.Fatal(err)
	}
	otherSecrets := secrets.NewSessionStore()
	if _, err := otherSecrets.Put(context.Background(), contract.CredentialInput{Source: "github", Secret: "other-secret", Persistence: "session"}); err != nil {
		t.Fatal(err)
	}
	other := NewClient(&http.Client{Transport: transport}, otherSecrets)
	other.SetCacheStore(store)
	if _, _, _, _, err := other.GetGitHubWithBytes(context.Background(), endpoint); err != nil || calls != 2 {
		t.Fatalf("outra credencial: calls=%d err=%v", calls, err)
	}
	keySum := sha256.Sum256([]byte("github:" + endpoint + ":topsecret"))
	key := hex.EncodeToString(keySum[:])
	var saved persistedETagEntry
	if err := store.Get(cacheKind, cacheSlot(key), &saved); err != nil {
		// A different credential may collide in the bounded slot. Restore
		// the original representation before testing integrity below.
		t.Fatal(err)
	}
	if saved.Key != key {
		first = secretClient(t, transport)
		first.SetCacheStore(store)
		if _, _, _, _, err := first.GetGitHubWithBytes(context.Background(), endpoint); err != nil {
			t.Fatal(err)
		}
		if err := store.Get(cacheKind, cacheSlot(key), &saved); err != nil {
			t.Fatal(err)
		}
	}
	saved.SHA256 = strings.Repeat("0", 64)
	if err := store.Put(cacheKind, cacheSlot(key), saved); err != nil {
		t.Fatal(err)
	}
	before := calls
	newClient := secretClient(t, transport)
	newClient.SetCacheStore(store)
	_, _, _, _, err = newClient.GetGitHubWithBytes(context.Background(), endpoint)
	var audit *contract.ErrorResponse
	if !errors.As(err, &audit) || audit.Code != "ERR_AUDIT_FAILED" || calls != before {
		t.Fatalf("cache corrompido não bloqueou antes da rede: calls=%d->%d err=%v", before, calls, err)
	}
}

type failingCacheStore struct{}

func (failingCacheStore) Get(string, string, any) error     { return os.ErrNotExist }
func (failingCacheStore) Put(string, string, any) error     { return errors.New("disk full") }
func (failingCacheStore) Lock(string) (func(), error)       { return func() {}, nil }
func (failingCacheStore) LockShared(string) (func(), error) { return func() {}, nil }

func TestCacheWriteFailureStopsPagination(t *testing.T) {
	calls := 0
	c := secretClient(t, func(*http.Request) (*http.Response, error) {
		calls++
		res := response(200, `[{"id":1}]`)
		res.Header.Set("ETag", `"v1"`)
		res.Header.Set("Link", `<https://api.github.com/repos/acme/api/pulls?page=2>; rel="next"`)
		return res, nil
	})
	c.SetCacheStore(failingCacheStore{})
	committed := 0
	_, err := c.GitHubWalk(context.Background(), "/repos/acme/api/pulls", 2, "", func([]json.RawMessage, string, int64) error {
		committed++
		return nil
	})
	var audit *contract.ErrorResponse
	if !errors.As(err, &audit) || audit.Code != "ERR_AUDIT_FAILED" || calls != 1 || committed != 0 {
		t.Fatalf("falha no cache não interrompeu coleta: calls=%d committed=%d err=%v", calls, committed, err)
	}
}
