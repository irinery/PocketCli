package collect

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/secrets"
	"pocketcli/internal/investigation/storage"
)

func TestRetryAfter429DoesNotRequestDuringWait(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var attempts []time.Time
	client := secretClient(t, func(*http.Request) (*http.Response, error) {
		mu.Lock()
		attempts = append(attempts, time.Now())
		count := len(attempts)
		mu.Unlock()
		if count == 1 {
			res := response(http.StatusTooManyRequests, `{"message":"rate limited"}`)
			res.Header.Set("Retry-After", "1")
			return res, nil
		}
		return response(http.StatusOK, `[]`), nil
	})
	client.SetCacheStore(store)
	data, _, status, wireBytes, err := client.GetGitHubWithBytes(WithRunID(context.Background(), "run-rate-limit"), "https://api.github.com/user/repos")
	if err != nil || status != http.StatusOK || string(data) != "[]" || wireBytes != int64(len(`{"message":"rate limited"}`)+2) {
		t.Fatalf("retry: status=%d bytes=%d data=%s err=%v", status, wireBytes, data, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(attempts) != 2 || attempts[1].Sub(attempts[0]) < 900*time.Millisecond {
		t.Fatalf("Retry-After ignorado: attempts=%v", attempts)
	}
	receipts := receiptsFor(t, store, "run-rate-limit")
	if len(receipts) != 2 {
		t.Fatalf("esperados dois recibos, got=%d", len(receipts))
	}
}

func TestCompressedResponseExpansionStopsAtTenMiB(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	requests := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Encoding", "gzip")
		w.WriteHeader(http.StatusOK)
		zipped := gzip.NewWriter(w)
		_, _ = zipped.Write(bytes.Repeat([]byte("x"), 11*1024*1024))
		_ = zipped.Close()
	}))
	defer server.Close()
	secretStore := secrets.NewSessionStore()
	if _, err := secretStore.Put(context.Background(), contract.CredentialInput{Source: "github", Secret: "test-secret", Persistence: "session"}); err != nil {
		t.Fatal(err)
	}
	client := NewClient(server.Client(), secretStore)
	client.GitHubURL = server.URL
	client.SetCacheStore(store)
	_, _, _, wireBytes, err := client.GetGitHubWithBytes(WithRunID(context.Background(), "run-compressed"), server.URL+"/repos/acme/api/pulls")
	var coded *contract.ErrorResponse
	if !errors.As(err, &coded) || coded.Code != "ERR_RESOURCE_EXHAUSTED" || requests != 1 || wireBytes != 10*1024*1024+1 {
		t.Fatalf("resposta expandida: requests=%d bytes=%d err=%v", requests, wireBytes, err)
	}
	receipts := receiptsFor(t, store, "run-compressed")
	if len(receipts) != 1 || receipts[0].Status != http.StatusOK || receipts[0].WireBytes != wireBytes || receipts[0].RecordCount != 0 {
		t.Fatalf("recibo do limite: %+v", receipts)
	}
}

func TestLinkInCommentBodyIsNeverFollowed(t *testing.T) {
	requests := 0
	client := secretClient(t, func(r *http.Request) (*http.Response, error) {
		requests++
		if r.URL.Host != "api.github.com" {
			t.Fatalf("link do comentário virou request: %s", r.URL)
		}
		return response(http.StatusOK, `[{"id":1,"body":"<https://evil.invalid/steal>; rel=next"}]`), nil
	})
	items, _, err := client.GitHubList(context.Background(), "/repos/acme/api/issues/comments", 2)
	if err != nil || len(items) != 1 || requests != 1 || !strings.Contains(string(items[0]), "evil.invalid") {
		t.Fatalf("requests=%d items=%s err=%v", requests, items, err)
	}
}

func TestSameCredentialRequestsAreSerialAcrossClients(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	active, maximum, calls := 0, 0, 0
	transport := transportFunc(func(*http.Request) (*http.Response, error) {
		mu.Lock()
		active++
		calls++
		if active > maximum {
			maximum = active
		}
		mu.Unlock()
		time.Sleep(60 * time.Millisecond)
		mu.Lock()
		active--
		mu.Unlock()
		return response(http.StatusOK, `[]`), nil
	})
	clients := []*Client{secretClient(t, transport), secretClient(t, transport)}
	for _, client := range clients {
		client.SetCacheStore(store)
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i, client := range clients {
		wg.Add(1)
		go func(i int, client *Client) {
			defer wg.Done()
			<-start
			_, _, _, _, err := client.GetGitHubWithBytes(WithRunID(context.Background(), "run-concurrent-"+string(rune('0'+i))), "https://api.github.com/user/repos")
			if err != nil {
				t.Errorf("cliente %d: %v", i, err)
			}
		}(i, client)
	}
	close(start)
	wg.Wait()
	if maximum != 1 || calls != 2 {
		t.Fatalf("concorrência por credencial: max=%d calls=%d", maximum, calls)
	}
}

func TestPrincipalLockCancellationStopsBeforeNetwork(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	release, err := store.Lock("http-" + rateHoldKey("github", []byte("topsecret")))
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	calls := 0
	client := secretClient(t, func(*http.Request) (*http.Response, error) {
		calls++
		return response(200, `[]`), nil
	})
	client.SetCacheStore(store)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, _, _, _, err = client.GetGitHubWithBytes(ctx, "https://api.github.com/user/repos")
	if !errors.Is(err, context.DeadlineExceeded) || calls != 0 {
		t.Fatalf("request atravessou lock: calls=%d err=%v", calls, err)
	}
}

func TestPacingIsSharedAcrossClientRestarts(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var attempts []time.Time
	transport := transportFunc(func(*http.Request) (*http.Response, error) {
		attempts = append(attempts, time.Now())
		return response(200, `[]`), nil
	})
	const endpoint = "https://api.github.com/user/repos"
	first := secretClient(t, transport)
	first.SetCacheStore(store)
	first.SetRPH("github", 36000) // One request per 100 ms.
	if _, _, _, _, err := first.GetGitHubWithBytes(context.Background(), endpoint); err != nil {
		t.Fatal(err)
	}
	second := secretClient(t, transport)
	second.SetCacheStore(store)
	second.SetRPH("github", 36000)
	if _, _, _, _, err := second.GetGitHubWithBytes(context.Background(), endpoint); err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 2 || attempts[1].Sub(attempts[0]) < 90*time.Millisecond {
		t.Fatalf("pacing perdido entre clientes: %v", attempts)
	}
}

type slowFirstRateHoldStore struct {
	*storage.Store
	mu    sync.Mutex
	wrote bool
}

func (s *slowFirstRateHoldStore) Put(kind, id string, value any) error {
	if kind == rateHoldKind {
		s.mu.Lock()
		first := !s.wrote
		s.wrote = true
		s.mu.Unlock()
		if first {
			time.Sleep(80 * time.Millisecond)
		}
	}
	return s.Store.Put(kind, id, value)
}

func TestPacingUsesActualRequestStartAfterSlowStorage(t *testing.T) {
	base, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store := &slowFirstRateHoldStore{Store: base}
	var attempts []time.Time
	transport := transportFunc(func(*http.Request) (*http.Response, error) {
		attempts = append(attempts, time.Now())
		return response(200, `[]`), nil
	})
	const endpoint = "https://api.github.com/user/repos"
	for i := 0; i < 2; i++ {
		client := secretClient(t, transport)
		client.SetCacheStore(store)
		client.SetRPH("github", 36000) // 100 ms between request starts.
		if _, _, _, _, err := client.GetGitHubWithBytes(context.Background(), endpoint); err != nil {
			t.Fatal(err)
		}
	}
	if len(attempts) != 2 || attempts[1].Sub(attempts[0]) < 90*time.Millisecond {
		t.Fatalf("slow storage shortened pacing: %v", attempts)
	}
}
