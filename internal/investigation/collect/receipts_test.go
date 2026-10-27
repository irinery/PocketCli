package collect

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/storage"
)

func receiptsFor(t *testing.T, store *storage.Store, auditID string) []RequestReceipt {
	t.Helper()
	ids, err := store.List(requestReceiptKind)
	if err != nil {
		t.Fatal(err)
	}
	var receipts []RequestReceipt
	for _, id := range ids {
		var receipt RequestReceipt
		if err := store.Get(requestReceiptKind, id, &receipt); err != nil {
			t.Fatal(err)
		}
		if receipt.AuditID == auditID {
			receipts = append(receipts, receipt)
		}
	}
	return receipts
}

func TestRequestReceiptsPersist200And304WithoutSecrets(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const endpoint = "https://api.github.com/repos/acme/api/pulls?page=1&token=topsecret"
	calls := 0
	client := secretClient(t, func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Header.Get("X-GitHub-Api-Version") != "2026-03-10" {
			t.Fatalf("versão REST não enviada: %q", r.Header.Get("X-GitHub-Api-Version"))
		}
		if calls == 1 {
			res := response(200, `[{"id":1},{"id":2}]`)
			res.Header.Set("ETag", `"v1"`)
			res.Header.Set("X-Github-Api-Version-Selected", "2026-03-10")
			return res, nil
		}
		res := response(304, "")
		res.Header.Set("X-Github-Api-Version-Selected", "unexpected-private-header-value")
		return res, nil
	})
	client.SetCacheStore(store)
	ctx := WithRunID(context.Background(), "run-receipts")
	for i := 0; i < 2; i++ {
		if _, _, _, _, err := client.GetGitHubWithBytes(ctx, endpoint); err != nil {
			t.Fatal(err)
		}
	}
	receipts := receiptsFor(t, store, "run-receipts")
	if calls != 2 || len(receipts) != 2 {
		t.Fatalf("calls=%d receipts=%d", calls, len(receipts))
	}
	seen := map[int]RequestReceipt{}
	for _, receipt := range receipts {
		seen[receipt.Status] = receipt
		if receipt.Method != "GET" || receipt.Source != "github" || receipt.Endpoint != "/repos/{id}/{id}/pulls" || receipt.RecordCount != 2 || receipt.StartedAt.IsZero() || receipt.DurationMS < 1 {
			t.Fatalf("recibo inválido: %+v", receipt)
		}
		content, err := os.ReadFile(filepath.Join(store.Root, requestReceiptKind, receipt.ID+".json"))
		if err != nil || strings.Contains(string(content), "topsecret") || strings.Contains(string(content), "page=1") || strings.Contains(string(content), "Authorization") || strings.Contains(string(content), "unexpected-private-header-value") {
			t.Fatalf("recibo expôs conteúdo privado: %s err=%v", content, err)
		}
	}
	if seen[200].CacheHit || seen[200].WireBytes != int64(len(`[{"id":1},{"id":2}]`)) || !seen[304].CacheHit || seen[304].WireBytes != 0 {
		t.Fatalf("contagem 200/304: %+v", seen)
	}
	if seen[200].RequestedAPIVersion != "2026-03-10" || seen[200].SelectedAPIVersion != "2026-03-10" || seen[304].RequestedAPIVersion != "2026-03-10" || seen[304].SelectedAPIVersion != "" {
		t.Fatalf("versões REST do recibo: %+v", seen)
	}
}

func TestRequestReceiptPersistsEachRetryAndForbiddenWithoutBody(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	client := secretClient(t, func(*http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			res := response(503, `{"message":"topsecret"}`)
			res.Header.Set("Retry-After", "0")
			return res, nil
		}
		return response(401, `{"message":"topsecret"}`), nil
	})
	client.SetCacheStore(store)
	_, _, _, _, err = client.GetGitHubWithBytes(WithRunID(context.Background(), "run-failure"), "https://api.github.com/user/repos")
	var forbidden *contract.ErrorResponse
	if !errors.As(err, &forbidden) || forbidden.Code != "ERR_FORBIDDEN" {
		t.Fatalf("erro final: %v", err)
	}
	receipts := receiptsFor(t, store, "run-failure")
	if calls != 2 || len(receipts) != 2 {
		t.Fatalf("calls=%d receipts=%d", calls, len(receipts))
	}
	seen := map[int]bool{}
	for _, receipt := range receipts {
		seen[receipt.Status] = true
		if receipt.RecordCount != 0 || receipt.WireBytes != int64(len(`{"message":"topsecret"}`)) || receipt.CacheHit || time.Since(receipt.StartedAt) > time.Minute {
			t.Fatalf("recibo de falha: %+v", receipt)
		}
		content, err := os.ReadFile(filepath.Join(store.Root, requestReceiptKind, receipt.ID+".json"))
		if err != nil || strings.Contains(string(content), "topsecret") {
			t.Fatalf("recibo expôs corpo: %s err=%v", content, err)
		}
	}
	if !seen[503] || !seen[401] {
		t.Fatalf("status dos recibos: %+v", seen)
	}
}

func TestRequestReceiptWriteFailureStopsRetry(t *testing.T) {
	calls := 0
	client := secretClient(t, func(*http.Request) (*http.Response, error) {
		calls++
		res := response(503, `{}`)
		res.Header.Set("Retry-After", "0")
		return res, nil
	})
	client.SetCacheStore(failingCacheStore{})
	_, _, _, _, err := client.GetGitHubWithBytes(WithRunID(context.Background(), "run-disk-full"), "https://api.github.com/user/repos")
	var audit *contract.ErrorResponse
	if !errors.As(err, &audit) || audit.Code != "ERR_AUDIT_FAILED" || calls != 1 {
		t.Fatalf("falha de recibo permitiu retry: calls=%d err=%v", calls, err)
	}
}

func TestLinearReadQueryHasLogicalPOSTReceipt(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	client := secretClient(t, func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.URL.Path != "/graphql" {
			t.Fatalf("request Linear: %s %s", r.Method, r.URL.Path)
		}
		return response(200, `{"data":{"teams":{"nodes":[{"id":"a"},{"id":"b"}]}}}`), nil
	})
	client.SetCacheStore(store)
	client.LinearURL = "https://api.linear.app/graphql?session=topsecret"
	_, err = client.LinearQuery(WithAuditID(context.Background(), "plan-linear"), "query Teams { teams { nodes { id } } }", nil)
	if err != nil {
		t.Fatal(err)
	}
	receipts := receiptsFor(t, store, "plan-linear")
	if len(receipts) != 1 || receipts[0].Source != "linear" || receipts[0].Method != "POST" || receipts[0].Endpoint != "/graphql" || receipts[0].RecordCount != 2 || receipts[0].CacheHit {
		t.Fatalf("recibo Linear: %+v", receipts)
	}
	content, err := os.ReadFile(filepath.Join(store.Root, requestReceiptKind, receipts[0].ID+".json"))
	if err != nil || strings.Contains(string(content), "topsecret") || strings.Contains(string(content), "Teams") {
		t.Fatalf("recibo Linear expôs request: %s err=%v", content, err)
	}
}

func TestResponseRecordCountHandlesEmptyConnection(t *testing.T) {
	if got := responseRecordCount([]byte(`{"data":{"teams":{"nodes":[]}}}`)); got != 0 {
		t.Fatalf("contou coleção vazia como objeto: %d", got)
	}
	if got := responseRecordCount([]byte(`{"data":{"teams":{"nodes":[{},{}]}}}`)); got != 2 {
		t.Fatalf("contagem Linear: %d", got)
	}
}
