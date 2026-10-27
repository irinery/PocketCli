package collect

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"

	"pocketcli/internal/investigation/contract"
)

// CacheStore is the private, atomic store supplied by the investigation core.
// A fixed set of slots bounds persisted HTTP representations across refreshes.
type CacheStore interface {
	Get(kind, id string, value any) error
	Put(kind, id string, value any) error
	Lock(id string) (func(), error)
	LockShared(id string) (func(), error)
}

const (
	cacheKind          = "httpcache"
	runStatsKind       = "runstats"
	cacheSlots         = 64
	persistedBodyLimit = 256 * 1024
)

type RunStats struct {
	RunID        string `json:"run_id"`
	ReuseCounter int64  `json:"reuse_counter"`
}

type runIDContextKey struct{}
type auditIDContextKey struct{}

func WithRunID(ctx context.Context, runID string) context.Context {
	return context.WithValue(WithAuditID(ctx, runID), runIDContextKey{}, runID)
}

// WithAuditID groups HTTP receipts from plan discovery or a collection run.
func WithAuditID(ctx context.Context, auditID string) context.Context {
	return context.WithValue(ctx, auditIDContextKey{}, auditID)
}

// RecordReuse persists each confirmed cache hit before the run proceeds.
// It is shared by HTTP 304 and immutable Git object caches.
func RecordReuse(store CacheStore, runID string) error {
	if store == nil || runID == "" {
		return nil
	}
	stats := RunStats{RunID: runID}
	err := store.Get(runStatsKind, runID, &stats)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return &contract.ErrorResponse{Code: "ERR_AUDIT_FAILED", Message: "estatística de cache corrompida"}
	}
	if err == nil && (stats.RunID != runID || stats.ReuseCounter < 0) {
		return &contract.ErrorResponse{Code: "ERR_AUDIT_FAILED", Message: "estatística de cache corrompida"}
	}
	stats.RunID = runID
	stats.ReuseCounter++
	if err := store.Put(runStatsKind, runID, stats); err != nil {
		return &contract.ErrorResponse{Code: "ERR_AUDIT_FAILED", Message: "falha ao auditar reaproveitamento"}
	}
	return nil
}

func (c *Client) recordReuse(ctx context.Context) error {
	runID, _ := ctx.Value(runIDContextKey{}).(string)
	return RecordReuse(c.cacheStore, runID)
}

type persistedETagEntry struct {
	Key    string `json:"key"`
	ETag   string `json:"etag"`
	Link   string `json:"link,omitempty"`
	Body   []byte `json:"body"`
	SHA256 string `json:"sha256"`
}

func cacheSlot(key string) string {
	return fmt.Sprintf("%02x", firstHexByte(key)%cacheSlots)
}

func firstHexByte(key string) int {
	if len(key) < 2 {
		return 0
	}
	value, err := hex.DecodeString(key[:2])
	if err != nil {
		return 0
	}
	return int(value[0])
}

func (c *Client) SetCacheStore(store CacheStore) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cacheStore = store
}

func (c *Client) loadCache(key string) (etagEntry, error) {
	if cached, ok := c.cache[key]; ok {
		return cached, nil
	}
	if c.cacheStore == nil {
		return etagEntry{}, nil
	}
	var saved persistedETagEntry
	err := c.cacheStore.Get(cacheKind, cacheSlot(key), &saved)
	if errors.Is(err, os.ErrNotExist) {
		return etagEntry{}, nil
	}
	if err != nil {
		return etagEntry{}, &contract.ErrorResponse{Code: "ERR_AUDIT_FAILED", Message: "cache HTTP local corrompido"}
	}
	if saved.Key != key { // Another URL or credential owns this bounded slot.
		return etagEntry{}, nil
	}
	hash := sha256.Sum256(saved.Body)
	if saved.ETag == "" || len(saved.ETag) > 512 || len(saved.Link) > 4096 || len(saved.Body) > persistedBodyLimit || saved.SHA256 != hex.EncodeToString(hash[:]) {
		return etagEntry{}, &contract.ErrorResponse{Code: "ERR_AUDIT_FAILED", Message: "cache HTTP local corrompido"}
	}
	header := make(http.Header)
	header.Set("ETag", saved.ETag)
	if saved.Link != "" {
		header.Set("Link", saved.Link)
	}
	cached := etagEntry{ETag: saved.ETag, Body: append([]byte(nil), saved.Body...), Header: header}
	c.cache[key] = cached
	c.cacheSize += int64(len(cached.Body))
	return cached, nil
}

func (c *Client) saveCache(key string, entry etagEntry) error {
	if c.cacheStore == nil || len(entry.Body) > persistedBodyLimit || len(entry.ETag) > 512 || len(entry.Header.Get("Link")) > 4096 {
		return nil
	}
	hash := sha256.Sum256(entry.Body)
	saved := persistedETagEntry{Key: key, ETag: entry.ETag, Link: entry.Header.Get("Link"), Body: entry.Body, SHA256: hex.EncodeToString(hash[:])}
	if err := c.cacheStore.Put(cacheKind, cacheSlot(key), saved); err != nil {
		return &contract.ErrorResponse{Code: "ERR_AUDIT_FAILED", Message: "falha ao persistir cache HTTP"}
	}
	return nil
}
