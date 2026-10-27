package collect

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"pocketcli/internal/investigation/contract"
)

const rateHoldKind = "rateholds"

type rateHold struct {
	Key    string    `json:"key"`
	Source string    `json:"source"`
	Until  time.Time `json:"until"`
}

func rateHoldKey(source string, secret []byte) string {
	hash := sha256.New()
	hash.Write([]byte(source + ":"))
	hash.Write(secret)
	return hex.EncodeToString(hash.Sum(nil))
}

func (c *Client) lockPrincipal(ctx context.Context, source string, secret []byte) (func(), error) {
	if c.cacheStore == nil {
		return func() {}, nil
	}
	key, err := c.principalRateKey(source, secret)
	if err != nil {
		return nil, err
	}
	id := "http-" + key
	for {
		release, err := c.cacheStore.Lock(id)
		if err == nil {
			return release, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			return nil, &contract.ErrorResponse{Code: "ERR_AUDIT_FAILED", Message: "falha ao serializar requests da fonte"}
		}
		if err := waitUntil(ctx, time.Now().Add(25*time.Millisecond)); err != nil {
			return nil, err
		}
	}
}

func (c *Client) waitRateHold(ctx context.Context, source string, secret []byte) error {
	if c.cacheStore == nil {
		return nil
	}
	key, err := c.principalRateKey(source, secret)
	if err != nil {
		return err
	}
	var hold rateHold
	err = c.cacheStore.Get(rateHoldKind, key, &hold)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || hold.Key != key || hold.Source != source || hold.Until.IsZero() {
		return &contract.ErrorResponse{Code: "ERR_AUDIT_FAILED", Message: "espera da fonte corrompida"}
	}
	remaining := time.Until(hold.Until)
	if remaining <= 0 {
		return nil
	}
	if remaining > 30*time.Second {
		return &contract.ErrorResponse{Code: "ERR_RATE_LIMITED", Message: "fonte ainda exige espera antes da próxima request", Retryable: true}
	}
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < remaining {
		return &contract.ErrorResponse{Code: "ERR_RATE_LIMITED", Message: "espera da fonte excede sessão restante", Retryable: true}
	}
	return waitUntil(ctx, hold.Until)
}

func (c *Client) saveRateHold(ctx context.Context, source string, secret []byte, delay time.Duration) error {
	if c.cacheStore == nil || delay <= 0 {
		return nil
	}
	key, err := c.principalRateKey(source, secret)
	if err != nil {
		return err
	}
	until := time.Now().UTC().Add(delay)
	var previous rateHold
	err = c.cacheStore.Get(rateHoldKind, key, &previous)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return &contract.ErrorResponse{Code: "ERR_AUDIT_FAILED", Message: "espera da fonte corrompida"}
	}
	if err == nil {
		if previous.Key != key || previous.Source != source || previous.Until.IsZero() {
			return &contract.ErrorResponse{Code: "ERR_AUDIT_FAILED", Message: "espera da fonte corrompida"}
		}
		if previous.Until.After(until) {
			until = previous.Until
		}
	}
	if err := c.cacheStore.Put(rateHoldKind, key, rateHold{Key: key, Source: source, Until: until}); err != nil {
		return &contract.ErrorResponse{Code: "ERR_AUDIT_FAILED", Message: "falha ao auditar espera da fonte"}
	}
	if err := c.saveProviderRateHold(ctx, source, time.Until(until)); err != nil {
		return err
	}
	return nil
}

func (c *Client) finishPacing(ctx context.Context, source string, secret []byte, started time.Time) error {
	remaining := time.Until(started.Add(c.spacing[source]))
	if remaining <= 0 {
		return nil
	}
	return c.saveRateHold(ctx, source, secret, remaining)
}

func githubRateLimited(status int, header http.Header, body []byte) bool {
	if status == http.StatusTooManyRequests {
		return true
	}
	if status != http.StatusForbidden {
		return false
	}
	if header.Get("X-RateLimit-Remaining") == "0" {
		return true
	}
	var result struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &result) != nil {
		return false
	}
	message := strings.ToLower(result.Message)
	return strings.Contains(message, "rate limit") || strings.Contains(message, "abuse detection")
}

func linearRateLimited(body []byte) bool {
	var result struct {
		Errors []struct {
			Extensions struct {
				Code string `json:"code"`
			} `json:"extensions"`
		} `json:"errors"`
	}
	if json.Unmarshal(body, &result) != nil {
		return false
	}
	for _, item := range result.Errors {
		if item.Extensions.Code == "RATELIMITED" {
			return true
		}
	}
	return false
}

func retryAfterDelay(header http.Header) (time.Duration, bool) {
	value := strings.TrimSpace(header.Get("Retry-After"))
	if value == "" {
		return 0, false
	}
	if seconds, err := strconv.ParseInt(value, 10, 64); seconds >= 0 && (err == nil || errors.Is(err, strconv.ErrRange)) {
		if seconds > int64((1<<63-1)/time.Second) {
			return time.Duration(1<<63 - 1), true
		}
		return time.Duration(seconds) * time.Second, true
	}
	if when, err := http.ParseTime(value); err == nil {
		delay := time.Until(when)
		if delay < 0 {
			delay = 0
		}
		return delay, true
	}
	return 0, false
}

func resetDelay(value string, milliseconds bool) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	stamp, err := strconv.ParseInt(value, 10, 64)
	if err != nil || stamp <= 0 {
		return 0, false
	}
	reset := time.Unix(stamp, 0)
	if milliseconds {
		reset = time.UnixMilli(stamp)
	}
	delay := time.Until(reset)
	if delay < 0 {
		delay = 0
	}
	return delay, true
}

func rateLimitDelay(source string, header http.Header) time.Duration {
	delay := time.Minute // Conservative fallback for missing or invalid headers.
	valid := false
	if value, ok := retryAfterDelay(header); ok {
		delay, valid = value, true
	}
	if source == "github" {
		if header.Get("X-RateLimit-Remaining") == "0" {
			if value, ok := resetDelay(header.Get("X-RateLimit-Reset"), false); ok && (!valid || value > delay) {
				delay, valid = value, true
			}
		}
	} else if source == "linear" {
		for _, pair := range [][2]string{
			{"X-RateLimit-Requests-Remaining", "X-RateLimit-Requests-Reset"},
			{"X-RateLimit-Endpoint-Requests-Remaining", "X-RateLimit-Endpoint-Requests-Reset"},
			{"X-RateLimit-Complexity-Remaining", "X-RateLimit-Complexity-Reset"},
		} {
			if header.Get(pair[0]) != "0" {
				continue
			}
			if value, ok := resetDelay(header.Get(pair[1]), true); ok && (!valid || value > delay) {
				delay, valid = value, true
			}
		}
	}
	if delay < 0 {
		return 0
	}
	return delay
}

// quotaReserveDelay uses the provider's reported quota, not a locally guessed
// request count. GitHub resets are epoch seconds; Linear resets are millis.
func quotaReserveDelay(source string, header http.Header, percent int) (time.Duration, bool) {
	if percent <= 0 || percent > 100 {
		return 0, false
	}
	type quotaHeaders struct {
		limit, remaining, reset string
	}
	var buckets []quotaHeaders
	milliseconds := false
	switch source {
	case "github":
		buckets = []quotaHeaders{{"X-RateLimit-Limit", "X-RateLimit-Remaining", "X-RateLimit-Reset"}}
	case "linear":
		milliseconds = true
		buckets = []quotaHeaders{
			{"X-RateLimit-Requests-Limit", "X-RateLimit-Requests-Remaining", "X-RateLimit-Requests-Reset"},
			{"X-RateLimit-Endpoint-Requests-Limit", "X-RateLimit-Endpoint-Requests-Remaining", "X-RateLimit-Endpoint-Requests-Reset"},
			{"X-RateLimit-Complexity-Limit", "X-RateLimit-Complexity-Remaining", "X-RateLimit-Complexity-Reset"},
		}
	default:
		return 0, false
	}
	var longest time.Duration
	for _, bucket := range buckets {
		limit, limitErr := strconv.ParseInt(strings.TrimSpace(header.Get(bucket.limit)), 10, 64)
		remaining, remainingErr := strconv.ParseInt(strings.TrimSpace(header.Get(bucket.remaining)), 10, 64)
		if limitErr != nil || remainingErr != nil || limit <= 0 || remaining < 0 || remaining > limit {
			continue
		}
		threshold := limit/100*int64(percent) + (limit%100*int64(percent)+99)/100
		if remaining > threshold {
			continue
		}
		delay, valid := resetDelay(header.Get(bucket.reset), milliseconds)
		if !valid {
			delay = time.Minute
		} else if delay <= 0 {
			continue
		}
		if delay > longest {
			longest = delay
		}
	}
	return longest, longest > 0
}

func retryBackoff(attempt int) time.Duration {
	return time.Duration(1<<attempt)*time.Second + time.Duration(rand.Int63n(int64(250*time.Millisecond)))
}
