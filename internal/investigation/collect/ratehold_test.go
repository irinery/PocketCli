package collect

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"testing"
	"time"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/secrets"
	"pocketcli/internal/investigation/storage"
)

func TestLongRetryAfterSurvivesNewClientAndScopesToCredential(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	requests := 0
	transport := transportFunc(func(*http.Request) (*http.Response, error) {
		requests++
		if requests == 1 {
			res := response(429, `{"message":"slow down"}`)
			res.Header.Set("Retry-After", "31")
			return res, nil
		}
		return response(200, `[]`), nil
	})
	first := secretClient(t, transport)
	first.SetCacheStore(store)
	const endpoint = "https://api.github.com/user/repos"
	_, _, _, _, err = first.GetGitHubWithBytes(WithRunID(context.Background(), "run-first"), endpoint)
	var limited *contract.ErrorResponse
	if !errors.As(err, &limited) || limited.Code != "ERR_RATE_LIMITED" || requests != 1 {
		t.Fatalf("primeiro rate limit: requests=%d err=%v", requests, err)
	}
	second := secretClient(t, transport)
	second.SetCacheStore(store)
	_, _, _, _, err = second.GetGitHubWithBytes(WithRunID(context.Background(), "run-second"), endpoint)
	if !errors.As(err, &limited) || limited.Code != "ERR_RATE_LIMITED" || requests != 1 {
		t.Fatalf("cliente novo ignorou hold: requests=%d err=%v", requests, err)
	}
	otherSecrets := secrets.NewSessionStore()
	if _, err := otherSecrets.Put(context.Background(), contract.CredentialInput{Source: "github", Secret: "different-secret", Persistence: "session"}); err != nil {
		t.Fatal(err)
	}
	other := NewClient(&http.Client{Transport: transport}, otherSecrets)
	other.SetCacheStore(store)
	if _, _, _, _, err := other.GetGitHubWithBytes(context.Background(), endpoint); err != nil || requests != 2 {
		t.Fatalf("credencial diferente bloqueada: requests=%d err=%v", requests, err)
	}
	key := rateHoldKey("github", []byte("topsecret"))
	var hold rateHold
	if err := store.Get(rateHoldKind, key, &hold); err != nil || hold.Key != key || time.Until(hold.Until) < 29*time.Second {
		t.Fatalf("hold não persistido: %+v err=%v", hold, err)
	}
	hold.Until = time.Now().Add(-time.Second)
	if err := store.Put(rateHoldKind, key, hold); err != nil {
		t.Fatal(err)
	}
	third := secretClient(t, transport)
	third.SetCacheStore(store)
	if _, _, _, _, err := third.GetGitHubWithBytes(context.Background(), endpoint); err != nil || requests != 3 {
		t.Fatalf("hold vencido bloqueou refresh: requests=%d err=%v", requests, err)
	}
}

func TestRetryAfterBeyondSessionPausesBeforeSecondRequest(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	requests := 0
	client := secretClient(t, func(*http.Request) (*http.Response, error) {
		requests++
		res := response(429, `{}`)
		res.Header.Set("Retry-After", "2")
		return res, nil
	})
	client.SetCacheStore(store)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, _, _, _, err = client.GetGitHubWithBytes(WithRunID(ctx, "run-short"), "https://api.github.com/user/repos")
	var limited *contract.ErrorResponse
	if !errors.As(err, &limited) || limited.Code != "ERR_RATE_LIMITED" || requests != 1 || time.Since(started) > time.Second {
		t.Fatalf("sessão esperou indevidamente: requests=%d elapsed=%s err=%v", requests, time.Since(started), err)
	}
	if len(receiptsFor(t, store, "run-short")) != 1 {
		t.Fatal("tentativa inicial sem recibo")
	}
}

func TestCorruptRateHoldBlocksBeforeNetwork(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	key := rateHoldKey("github", []byte("topsecret"))
	if err := store.Put(rateHoldKind, key, rateHold{Key: "wrong", Source: "github", Until: time.Now().Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	requests := 0
	client := secretClient(t, func(*http.Request) (*http.Response, error) {
		requests++
		return response(200, `[]`), nil
	})
	client.SetCacheStore(store)
	_, _, _, _, err = client.GetGitHubWithBytes(context.Background(), "https://api.github.com/user/repos")
	var audit *contract.ErrorResponse
	if !errors.As(err, &audit) || audit.Code != "ERR_AUDIT_FAILED" || requests != 0 {
		t.Fatalf("hold corrompido permitiu rede: requests=%d err=%v", requests, err)
	}
}

func TestGitHub403RateLimitUsesEpochSecondsAndIsNotForbidden(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	requests := 0
	reset := time.Now().Add(45 * time.Second).Unix()
	client := secretClient(t, func(*http.Request) (*http.Response, error) {
		requests++
		res := response(403, `{"message":"API rate limit exceeded"}`)
		res.Header.Set("X-RateLimit-Remaining", "0")
		res.Header.Set("X-RateLimit-Reset", strconv.FormatInt(reset, 10))
		return res, nil
	})
	client.SetCacheStore(store)
	_, _, _, _, err = client.GetGitHubWithBytes(context.Background(), "https://api.github.com/user/repos")
	var limited *contract.ErrorResponse
	if !errors.As(err, &limited) || limited.Code != "ERR_RATE_LIMITED" || requests != 1 {
		t.Fatalf("403 classificado incorretamente: requests=%d err=%v", requests, err)
	}
	key := rateHoldKey("github", []byte("topsecret"))
	var hold rateHold
	if err := store.Get(rateHoldKind, key, &hold); err != nil || hold.Until.Sub(time.Unix(reset, 0)) < -time.Second || hold.Until.Sub(time.Unix(reset, 0)) > time.Second {
		t.Fatalf("reset GitHub em segundos: hold=%+v reset=%d err=%v", hold, reset, err)
	}
	newClient := secretClient(t, func(*http.Request) (*http.Response, error) {
		requests++
		return response(200, `[]`), nil
	})
	newClient.SetCacheStore(store)
	_, _, _, _, err = newClient.GetGitHubWithBytes(context.Background(), "https://api.github.com/user/repos")
	if !errors.As(err, &limited) || limited.Code != "ERR_RATE_LIMITED" || requests != 1 {
		t.Fatalf("novo cliente ignorou 403: requests=%d err=%v", requests, err)
	}
}

func TestGitHubSecondaryRateLimitWithoutHeaderWaitsOneMinute(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	client := secretClient(t, func(*http.Request) (*http.Response, error) {
		return response(403, `{"message":"You have exceeded a secondary rate limit"}`), nil
	})
	client.SetCacheStore(store)
	_, _, _, _, err = client.GetGitHubWithBytes(context.Background(), "https://api.github.com/user/repos")
	var limited *contract.ErrorResponse
	if !errors.As(err, &limited) || limited.Code != "ERR_RATE_LIMITED" {
		t.Fatalf("limite secundário virou permissão: %v", err)
	}
	var hold rateHold
	if err := store.Get(rateHoldKind, rateHoldKey("github", []byte("topsecret")), &hold); err != nil || time.Until(hold.Until) < 55*time.Second {
		t.Fatalf("espera secundária: hold=%+v err=%v", hold, err)
	}
}

func TestLinearRateLimitResetUsesEpochMilliseconds(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	reset := time.Now().Add(45 * time.Second).UnixMilli()
	client := secretClient(t, func(*http.Request) (*http.Response, error) {
		res := response(400, `{"errors":[{"extensions":{"code":"RATELIMITED"}}]}`)
		res.Header.Set("X-RateLimit-Requests-Remaining", "0")
		res.Header.Set("X-RateLimit-Requests-Reset", strconv.FormatInt(reset, 10))
		return res, nil
	})
	client.SetCacheStore(store)
	_, err = client.LinearQuery(context.Background(), "query Teams { teams { nodes { id } } }", nil)
	var limited *contract.ErrorResponse
	if !errors.As(err, &limited) || limited.Code != "ERR_RATE_LIMITED" {
		t.Fatalf("Linear 400: %v", err)
	}
	var hold rateHold
	if err := store.Get(rateHoldKind, rateHoldKey("linear", []byte("topsecret")), &hold); err != nil || hold.Until.Sub(time.UnixMilli(reset)) < -time.Second || hold.Until.Sub(time.UnixMilli(reset)) > time.Second {
		t.Fatalf("reset Linear em milissegundos: hold=%+v reset=%d err=%v", hold, reset, err)
	}
}

func TestLinearHTTP200RateLimitErrorAlsoPersistsHold(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	client := secretClient(t, func(*http.Request) (*http.Response, error) {
		return response(200, `{"data":null,"errors":[{"extensions":{"code":"RATELIMITED"}}]}`), nil
	})
	client.SetCacheStore(store)
	_, err = client.LinearQuery(context.Background(), "query Teams { teams { nodes { id } } }", nil)
	var limited *contract.ErrorResponse
	if !errors.As(err, &limited) || limited.Code != "ERR_RATE_LIMITED" {
		t.Fatalf("Linear 200 com erro GraphQL: %v", err)
	}
	var hold rateHold
	if err := store.Get(rateHoldKind, rateHoldKey("linear", []byte("topsecret")), &hold); err != nil || time.Until(hold.Until) < 55*time.Second {
		t.Fatalf("hold de GraphQL 200: %+v err=%v", hold, err)
	}
}

func TestHugeRetryAfterDoesNotOverflowToShortWait(t *testing.T) {
	header := make(http.Header)
	header.Set("Retry-After", "999999999999999999999999999")
	delay, ok := retryAfterDelay(header)
	if !ok || delay < 100*365*24*time.Hour {
		t.Fatalf("Retry-After transbordou: delay=%s ok=%v", delay, ok)
	}
}

func TestRetryBackoffHasBoundedJitter(t *testing.T) {
	for attempt, base := range []time.Duration{time.Second, 2 * time.Second, 4 * time.Second} {
		for sample := 0; sample < 20; sample++ {
			got := retryBackoff(attempt)
			if got < base || got >= base+250*time.Millisecond {
				t.Fatalf("attempt=%d delay=%s", attempt, got)
			}
		}
	}
}

func TestProviderHoldNeverShortensExistingPacing(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	client := secretClient(t, func(*http.Request) (*http.Response, error) { return response(200, `[]`), nil })
	client.SetCacheStore(store)
	secret := []byte("topsecret")
	if err := client.saveRateHold(context.Background(), "github", secret, 10*time.Second); err != nil {
		t.Fatal(err)
	}
	var first rateHold
	if err := store.Get(rateHoldKind, rateHoldKey("github", secret), &first); err != nil {
		t.Fatal(err)
	}
	if err := client.saveRateHold(context.Background(), "github", secret, time.Second); err != nil {
		t.Fatal(err)
	}
	var hold rateHold
	if err := store.Get(rateHoldKind, rateHoldKey("github", secret), &hold); err != nil || hold.Until.Before(first.Until) {
		t.Fatalf("hold encurtado: %+v err=%v", hold, err)
	}
}
