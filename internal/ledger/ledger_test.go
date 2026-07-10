package ledger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAppendRedactsSecrets(t *testing.T) {
	store := NewStoreAt(t.TempDir())
	_, err := store.Append(Event{
		Type:      EventCommandCompleted,
		SessionID: "s1",
		Status:    "ok",
		Payload:   Payload{Message: "Authorization: Bearer abc123 password=secret"},
	})
	if err != nil {
		t.Fatalf("Append returned error: %v", err)
	}
	result, err := store.Search(SearchFilter{SessionID: "s1", Since: "2000-01-01", Limit: 10})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(result.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(result.Events))
	}
	msg := result.Events[0].Payload.Message
	if strings.Contains(msg, "abc123") || strings.Contains(msg, "secret") {
		t.Fatalf("secret leaked in %q", msg)
	}
	if result.Events[0].Payload.RedactionCount != 2 {
		t.Fatalf("expected redaction_count=2, got %d", result.Events[0].Payload.RedactionCount)
	}
}

func TestRebuildIndexSkipsTruncatedLine(t *testing.T) {
	base := t.TempDir()
	store := NewStoreAt(base)
	eventsDir := filepath.Join(base, "ledger")
	if err := os.MkdirAll(eventsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	valid := `{"schema_version":1,"event_id":"e1","timestamp":"` + time.Now().UTC().Format(time.RFC3339) + `","session_id":"s1","type":"command.completed","status":"ok","payload":{"redaction_count":0}}`
	if err := os.WriteFile(filepath.Join(eventsDir, time.Now().UTC().Format("2006-01-02")+".jsonl"), []byte(valid+"\n{\"bad\"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	result, err := store.RebuildIndex()
	if err != nil {
		t.Fatalf("RebuildIndex returned error: %v", err)
	}
	if result.EventsIndexed != 1 || result.SkippedLines != 1 {
		t.Fatalf("unexpected rebuild result: %#v", result)
	}
}

func TestAppendRepairsExistingLedgerPermissions(t *testing.T) {
	base := t.TempDir()
	store := NewStoreAt(base)
	result, err := store.Append(Event{Type: EventCommandCompleted, SessionID: "s1", Status: "ok"})
	if err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	if err := os.Chmod(filepath.Dir(result.Path), 0o755); err != nil {
		t.Fatalf("Chmod directory error = %v", err)
	}
	if err := os.Chmod(result.Path, 0o644); err != nil {
		t.Fatalf("Chmod file error = %v", err)
	}
	if _, err := store.Append(Event{Type: EventCommandCompleted, SessionID: "s1", Status: "ok"}); err != nil {
		t.Fatalf("second Append() error = %v", err)
	}
	assertLedgerMode(t, filepath.Dir(result.Path), 0o700)
	assertLedgerMode(t, result.Path, 0o600)
}

func assertLedgerMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(%s) error = %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("mode(%s) = %#o, want %#o", path, got, want)
	}
}
