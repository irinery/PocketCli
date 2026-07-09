package memory

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"
)

func TestWriteSaveFromLastInteractionCreatesFullEntry(t *testing.T) {
	store := newTestStore(t)
	store.now = fixedClock("2026-04-06T15:04:05Z")
	store.newID = staticID("11111111-1111-4111-8111-111111111111")

	err := store.RememberInteraction(LastInteraction{
		SessionID: "22222222-2222-4222-8222-222222222222",
		Kind:      KindDecision,
		Scope:     "project:PocketCli",
		Title:     "Escolha do writer",
		Summary:   "Persistir memórias em jsonl por escopo.",
		Body:      "Persistir memórias em jsonl por escopo.",
		Tags:      []string{"memory", "writer"},
	})
	if err != nil {
		t.Fatalf("RememberInteraction returned error: %v", err)
	}

	entry, err := store.SaveFromLastInteraction()
	if err != nil {
		t.Fatalf("SaveFromLastInteraction returned error: %v", err)
	}

	if entry.ID != "11111111-1111-4111-8111-111111111111" {
		t.Fatalf("unexpected id: %q", entry.ID)
	}
	if entry.Confidence != 0.9 {
		t.Fatalf("expected confidence 0.9, got %.1f", entry.Confidence)
	}
	if entry.CreatedAt != "2026-04-06T15:04:05Z" {
		t.Fatalf("unexpected created_at: %q", entry.CreatedAt)
	}
	if entry.LastAccessed != entry.CreatedAt {
		t.Fatalf("expected last_accessed to match created_at, got %q", entry.LastAccessed)
	}
	if entry.AccessCount != 0 {
		t.Fatalf("expected access_count 0, got %d", entry.AccessCount)
	}

	entries := readScopeEntries(t, store, "project:pocketcli")
	if len(entries) != 1 {
		t.Fatalf("expected one persisted entry, got %d", len(entries))
	}
}

func TestWriteNoAutomaticSaveWithoutExplicitAction(t *testing.T) {
	store := newTestStore(t)

	err := store.RememberInteraction(LastInteraction{
		SessionID: "33333333-3333-4333-8333-333333333333",
		Kind:      KindPattern,
		Scope:     "global",
		Title:     "Interação recente",
		Summary:   "Pergunta aguardando save manual.",
		Body:      "Pergunta aguardando save manual.",
		Tags:      []string{"ask"},
	})
	if err != nil {
		t.Fatalf("RememberInteraction returned error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(store.memoryDir(), "global.jsonl")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected memory file to remain untouched, got err=%v", err)
	}
}

func TestUpdateConfidenceRevalidateExistingEntry(t *testing.T) {
	store := newTestStore(t)
	store.now = fixedClock("2026-04-06T16:00:00Z")

	entry, err := store.Write(Entry{
		ID:           "44444444-4444-4444-8444-444444444444",
		Kind:         KindIncident,
		Scope:        "global",
		Title:        "Falha no deploy",
		Summary:      "Detalhe da falha.",
		Body:         "Detalhe da falha.",
		Tags:         []string{"incident"},
		Confidence:   0.7,
		CreatedAt:    "2026-04-06T10:00:00Z",
		LastAccessed: "2026-04-06T10:00:00Z",
	})
	if err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	updated, err := store.UpdateConfidence(entry.ID, 0.1)
	if err != nil {
		t.Fatalf("UpdateConfidence returned error: %v", err)
	}

	if updated.Confidence != 0.8 {
		t.Fatalf("expected confidence 0.8, got %.1f", updated.Confidence)
	}
	if updated.CreatedAt != "2026-04-06T10:00:00Z" {
		t.Fatalf("created_at changed unexpectedly: %q", updated.CreatedAt)
	}
	if updated.LastAccessed != "2026-04-06T16:00:00Z" {
		t.Fatalf("expected updated last_accessed, got %q", updated.LastAccessed)
	}
}

func TestUpdateConfidenceRespectsUpperBound(t *testing.T) {
	store := newTestStore(t)

	entry, err := store.Write(Entry{
		ID:         "55555555-5555-4555-8555-555555555555",
		Kind:       KindDecision,
		Scope:      "global",
		Title:      "Teto",
		Summary:    "Entrada no teto.",
		Body:       "Entrada no teto.",
		Tags:       []string{"confidence"},
		Confidence: 1.0,
	})
	if err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	updated, err := store.UpdateConfidence(entry.ID, 0.1)
	if err != nil {
		t.Fatalf("UpdateConfidence returned error: %v", err)
	}
	if updated.Confidence != 1.0 {
		t.Fatalf("expected confidence to remain 1.0, got %.1f", updated.Confidence)
	}
}

func TestUpdateConfidenceRespectsLowerBoundWithoutDeletingEntry(t *testing.T) {
	store := newTestStore(t)

	entry, err := store.Write(Entry{
		ID:         "66666666-6666-4666-8666-666666666666",
		Kind:       KindPattern,
		Scope:      "global",
		Title:      "Piso",
		Summary:    "Entrada no piso.",
		Body:       "Entrada no piso.",
		Tags:       []string{"confidence"},
		Confidence: 0.1,
	})
	if err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	updated, err := store.UpdateConfidence(entry.ID, -0.1)
	if err != nil {
		t.Fatalf("UpdateConfidence returned error: %v", err)
	}
	if updated.Confidence != 0.0 {
		t.Fatalf("expected confidence 0.0, got %.1f", updated.Confidence)
	}

	entries := readScopeEntries(t, store, "global")
	if len(entries) != 1 {
		t.Fatalf("expected entry to remain persisted, got %d entries", len(entries))
	}
	if entries[0].ID != entry.ID {
		t.Fatalf("unexpected remaining id: %q", entries[0].ID)
	}
}

func TestWriteKeepsZeroConfidenceEntryForCleanupPhase(t *testing.T) {
	store := newTestStore(t)

	entry, err := store.Write(Entry{
		ID:         "77777777-7777-4777-8777-777777777777",
		Kind:       KindPattern,
		Scope:      "host:MacMini",
		Title:      "Sem limpeza automática",
		Summary:    "Confidence zero continua salvo.",
		Body:       "Confidence zero continua salvo.",
		Tags:       []string{"cleanup"},
		Confidence: 0.0,
	})
	if err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	entries := readScopeEntries(t, store, "host:macmini")
	if len(entries) != 1 {
		t.Fatalf("expected one persisted entry, got %d", len(entries))
	}
	if entries[0].ID != entry.ID {
		t.Fatalf("unexpected id: %q", entries[0].ID)
	}
}

func TestWritePersistsRequiredFields(t *testing.T) {
	store := newTestStore(t)
	store.newID = staticID("88888888-8888-4888-8888-888888888888")

	err := store.RememberInteraction(LastInteraction{
		SessionID: "99999999-9999-4999-8999-999999999999",
		Kind:      KindPattern,
		Scope:     "global",
		Title:     "Campos obrigatórios",
		Summary:   "Todos os campos obrigatórios precisam existir.",
		Body:      "Todos os campos obrigatórios precisam existir.",
		Tags:      []string{"schema"},
	})
	if err != nil {
		t.Fatalf("RememberInteraction returned error: %v", err)
	}

	if _, err := store.SaveFromLastInteraction(); err != nil {
		t.Fatalf("SaveFromLastInteraction returned error: %v", err)
	}

	path, err := store.scopeFilePath("global")
	if err != nil {
		t.Fatalf("scopeFilePath returned error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}

	requiredStrings := []string{"id", "kind", "scope", "title", "summary", "created_at", "last_accessed"}
	for _, key := range requiredStrings {
		value, ok := raw[key].(string)
		if !ok || value == "" {
			t.Fatalf("expected non-empty string field %q, got %#v", key, raw[key])
		}
	}

	tags, ok := raw["tags"].([]any)
	if !ok || len(tags) == 0 {
		t.Fatalf("expected non-empty tags, got %#v", raw["tags"])
	}
	if raw["confidence"] == nil {
		t.Fatalf("expected confidence to be present")
	}
	if raw["access_count"] == nil {
		t.Fatalf("expected access_count to be present")
	}
}

func TestMemoryStateAndEntriesArePrivate(t *testing.T) {
	store := newTestStore(t)
	err := store.RememberInteraction(LastInteraction{
		SessionID: "99999999-9999-4999-8999-999999999999",
		Kind:      KindPattern,
		Scope:     "global",
		Title:     "Privacidade",
		Summary:   "Estado local privado.",
		Body:      "Estado local privado.",
		Tags:      []string{"privacy"},
	})
	if err != nil {
		t.Fatalf("RememberInteraction() error = %v", err)
	}
	if _, err := store.SaveFromLastInteraction(); err != nil {
		t.Fatalf("SaveFromLastInteraction() error = %v", err)
	}
	assertMemoryMode(t, store.stateDir(), 0o700)
	assertMemoryMode(t, store.lastInteractionPath(), 0o600)
	assertMemoryMode(t, store.memoryDir(), 0o700)
	path, err := store.scopeFilePath("global")
	if err != nil {
		t.Fatalf("scopeFilePath() error = %v", err)
	}
	assertMemoryMode(t, path, 0o600)
}

func TestWriteRejectsInvalidKind(t *testing.T) {
	store := newTestStore(t)

	_, err := store.Write(Entry{
		Kind:       "rascunho",
		Scope:      "global",
		Title:      "Inválido",
		Summary:    "Não deve persistir.",
		Body:       "Não deve persistir.",
		Tags:       []string{"invalid"},
		Confidence: 0.9,
	})
	if err == nil {
		t.Fatal("expected invalid kind error")
	}
	if !errors.Is(err, ErrInvalidKind) {
		t.Fatalf("expected ErrInvalidKind, got %v", err)
	}

	if _, err := os.Stat(filepath.Join(store.memoryDir(), "global.jsonl")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no file to be created, got err=%v", err)
	}
}

func TestSaveFromLastInteractionGeneratesUUIDv4(t *testing.T) {
	store := newTestStore(t)

	err := store.RememberInteraction(LastInteraction{
		SessionID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
		Kind:      KindPattern,
		Scope:     "global",
		Title:     "UUID",
		Summary:   "Precisa gerar UUIDv4 automaticamente.",
		Body:      "Precisa gerar UUIDv4 automaticamente.",
		Tags:      []string{"uuid"},
	})
	if err != nil {
		t.Fatalf("RememberInteraction returned error: %v", err)
	}

	entry, err := store.SaveFromLastInteraction()
	if err != nil {
		t.Fatalf("SaveFromLastInteraction returned error: %v", err)
	}

	pattern := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	if !pattern.MatchString(entry.ID) {
		t.Fatalf("expected UUIDv4, got %q", entry.ID)
	}
	if entry.Confidence != 0.9 {
		t.Fatalf("expected confidence 0.9, got %.1f", entry.Confidence)
	}
}

func TestSaveFromLastInteractionWithoutRecentAskReturnsInformativeError(t *testing.T) {
	store := newTestStore(t)

	_, err := store.SaveFromLastInteraction()
	if err == nil {
		t.Fatal("expected error when no recent interaction exists")
	}
	if !errors.Is(err, ErrNoRecentInteraction) {
		t.Fatalf("expected ErrNoRecentInteraction, got %v", err)
	}
	if err.Error() != "nenhuma interação recente para salvar" {
		t.Fatalf("unexpected error message: %q", err.Error())
	}
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	return NewStoreAt(t.TempDir())
}

func assertMemoryMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(%s) error = %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("mode(%s) = %#o, want %#o", path, got, want)
	}
}

func fixedClock(value string) func() time.Time {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return func() time.Time { return parsed }
}

func staticID(id string) func() (string, error) {
	return func() (string, error) { return id, nil }
}

func readScopeEntries(t *testing.T, store *Store, scope string) []Entry {
	t.Helper()

	path, err := store.scopeFilePath(scope)
	if err != nil {
		t.Fatalf("scopeFilePath returned error: %v", err)
	}

	entries, err := store.loadEntries(path)
	if err != nil {
		t.Fatalf("loadEntries returned error: %v", err)
	}

	return entries
}
