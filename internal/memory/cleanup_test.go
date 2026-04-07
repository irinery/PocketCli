package memory

import (
	"fmt"
	"testing"
	"time"
)

func TestCleanupCandidatesMC01IncludesLowConfidenceEntryOlderThanNinetyDays(t *testing.T) {
	store := newTestStore(t)
	store.now = fixedClock("2026-04-06T12:00:00Z")

	if _, err := store.Write(Entry{
		ID:           "mc01",
		Kind:         KindPattern,
		Scope:        "global",
		Title:        "Candidata",
		Summary:      "Baixa confiança e antiga.",
		Body:         "Baixa confiança e antiga.",
		Tags:         []string{"cleanup"},
		Confidence:   0.2,
		CreatedAt:    "2025-12-01T12:00:00Z",
		LastAccessed: "2026-01-05T12:00:00Z",
	}); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	candidates, err := store.CleanupCandidates("")
	if err != nil {
		t.Fatalf("CleanupCandidates returned error: %v", err)
	}

	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}
	if candidates[0].Entry.ID != "mc01" {
		t.Fatalf("unexpected candidate id: %q", candidates[0].Entry.ID)
	}
	if candidates[0].Reasons[0] != CleanupReasonLowConfidenceStale {
		t.Fatalf("unexpected reason: %#v", candidates[0].Reasons)
	}
}

func TestCleanupCandidatesMC02IgnoresEntryYoungerThanNinetyDays(t *testing.T) {
	store := newTestStore(t)
	store.now = fixedClock("2026-04-06T12:00:00Z")

	if _, err := store.Write(Entry{
		ID:           "mc02",
		Kind:         KindPattern,
		Scope:        "global",
		Title:        "Recente",
		Summary:      "Ainda recente.",
		Body:         "Ainda recente.",
		Tags:         []string{"cleanup"},
		Confidence:   0.2,
		CreatedAt:    "2026-01-01T12:00:00Z",
		LastAccessed: "2026-01-07T12:00:00Z",
	}); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	candidates, err := store.CleanupCandidates("")
	if err != nil {
		t.Fatalf("CleanupCandidates returned error: %v", err)
	}

	if len(candidates) != 0 {
		t.Fatalf("expected no candidates, got %d", len(candidates))
	}
}

func TestCleanupCandidatesMC03IgnoresHighConfidenceEntry(t *testing.T) {
	store := newTestStore(t)
	store.now = fixedClock("2026-04-06T12:00:00Z")

	if _, err := store.Write(Entry{
		ID:           "mc03",
		Kind:         KindPattern,
		Scope:        "global",
		Title:        "Alta confiança",
		Summary:      "Confiança acima do limiar.",
		Body:         "Confiança acima do limiar.",
		Tags:         []string{"cleanup"},
		Confidence:   0.5,
		CreatedAt:    "2025-01-01T12:00:00Z",
		LastAccessed: "2025-09-18T12:00:00Z",
	}); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	candidates, err := store.CleanupCandidates("")
	if err != nil {
		t.Fatalf("CleanupCandidates returned error: %v", err)
	}

	if len(candidates) != 0 {
		t.Fatalf("expected no candidates, got %d", len(candidates))
	}
}

func TestCleanupCandidatesMC04MarksOverflowEntriesBeyondFiveHundredPerScope(t *testing.T) {
	store := newTestStore(t)
	store.now = fixedClock("2026-04-06T12:00:00Z")

	base := mustTime("2026-01-01T00:00:00Z")
	for idx := 0; idx < 501; idx++ {
		timestamp := base.Add(time.Duration(idx) * time.Hour).Format(time.RFC3339)
		if _, err := store.Write(Entry{
			ID:           entryID(idx),
			Kind:         KindPattern,
			Scope:        "project:pocketcli",
			Title:        "Overflow",
			Summary:      "Overflow test.",
			Body:         "Overflow test.",
			Tags:         []string{"cleanup"},
			Confidence:   1.0,
			CreatedAt:    timestamp,
			LastAccessed: timestamp,
		}); err != nil {
			t.Fatalf("Write returned error at %d: %v", idx, err)
		}
	}

	candidates, err := store.CleanupCandidates("project:pocketcli")
	if err != nil {
		t.Fatalf("CleanupCandidates returned error: %v", err)
	}

	if len(candidates) != 1 {
		t.Fatalf("expected 1 overflow candidate, got %d", len(candidates))
	}
	if candidates[0].Entry.ID != entryID(0) {
		t.Fatalf("expected oldest entry to be candidate, got %q", candidates[0].Entry.ID)
	}
	if candidates[0].Reasons[0] != CleanupReasonScopeOverflow {
		t.Fatalf("unexpected reason: %#v", candidates[0].Reasons)
	}
}

func TestDeleteEntryRemovesPersistedCandidate(t *testing.T) {
	store := newTestStore(t)

	entry, err := store.Write(Entry{
		ID:         "mc-delete",
		Kind:       KindPattern,
		Scope:      "global",
		Title:      "Delete",
		Summary:    "Delete candidate.",
		Body:       "Delete candidate.",
		Tags:       []string{"cleanup"},
		Confidence: 0.1,
	})
	if err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	if err := store.DeleteEntry(entry.ID); err != nil {
		t.Fatalf("DeleteEntry returned error: %v", err)
	}

	entries := readScopeEntries(t, store, "global")
	if len(entries) != 0 {
		t.Fatalf("expected entry to be deleted, got %d entries", len(entries))
	}
}

func entryID(idx int) string {
	return fmt.Sprintf("overflow-%03d", idx)
}
