package audit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pocketcli/internal/backend"
	"pocketcli/internal/router"
)

func TestWriteAL01AddsFormattedLineWithRequiredFields(t *testing.T) {
	logger := newTestLogger(t)
	logger.now = fixedClock("2026-04-06T14:32:11Z")
	logger.newSessionID = staticSessionID("550e8400-e29b-41d4-a716-446655440000")

	if err := logger.Write(Record{
		Command:   "ask",
		LatencyMS: 1200,
		Response: backend.LLMResponse{
			Backend:    router.BackendLocal,
			TokenUsage: 420,
		},
	}); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	data, err := os.ReadFile(logger.activePath())
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}

	got := strings.TrimSpace(string(data))
	want := "2026-04-06T14:32:11Z | ask | local | tokens=420 | latency=1200ms | memory_hit=false | session_id=550e8400-e29b-41d4-a716-446655440000"
	if got != want {
		t.Fatalf("unexpected audit line:\nwant: %q\ngot:  %q", want, got)
	}
}

func TestWriteAL02UsesRemoteBackendAndKeepsExplicitSessionID(t *testing.T) {
	logger := newTestLogger(t)
	logger.now = fixedClock("2026-04-06T15:00:00Z")

	if err := logger.Write(Record{
		Command:   "context",
		SessionID: "11111111-1111-4111-8111-111111111111",
		LatencyMS: 900,
		RouterDecision: router.Decision{
			SelectedBackend: router.BackendRemote,
		},
		Response: backend.LLMResponse{
			Backend:    router.BackendRemote,
			TokenUsage: 321,
		},
	}); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	data, err := os.ReadFile(logger.activePath())
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}

	line := strings.TrimSpace(string(data))
	if !strings.Contains(line, " | remote | ") {
		t.Fatalf("expected remote backend, got %q", line)
	}
	if !strings.Contains(line, "session_id=11111111-1111-4111-8111-111111111111") {
		t.Fatalf("expected explicit session_id, got %q", line)
	}
}

func TestWriteAL03PersistsMemoryHitFlag(t *testing.T) {
	logger := newTestLogger(t)
	logger.now = fixedClock("2026-04-06T15:10:00Z")
	logger.newSessionID = staticSessionID("22222222-2222-4222-8222-222222222222")

	if err := logger.Write(Record{
		Command:   "recall",
		MemoryHit: true,
		LatencyMS: 100,
	}); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	data, err := os.ReadFile(logger.activePath())
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}

	if !strings.Contains(string(data), "memory_hit=true") {
		t.Fatalf("expected memory_hit=true, got %q", string(data))
	}
}

func TestWriteAL04PersistsFailedBackendAttempt(t *testing.T) {
	logger := newTestLogger(t)
	logger.now = fixedClock("2026-04-06T15:20:00Z")
	logger.newSessionID = staticSessionID("33333333-3333-4333-8333-333333333333")

	if err := logger.Write(Record{
		Command: "ask",
		Response: backend.LLMResponse{
			Backend:      router.BackendLocal,
			TokenUsage:   0,
			LatencyMS:    2450,
			FinishReason: backend.FinishReasonError,
			Reason:       "timeout",
		},
	}); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	data, err := os.ReadFile(logger.activePath())
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}

	line := strings.TrimSpace(string(data))
	if !strings.Contains(line, " | local | ") {
		t.Fatalf("expected failed backend to be logged, got %q", line)
	}
	if !strings.Contains(line, "latency=2450ms") {
		t.Fatalf("expected backend latency, got %q", line)
	}
}

func TestPrepareAL05RotatesBySizeBeforeNextWrite(t *testing.T) {
	logger := newTestLogger(t)
	logger.now = fixedClock("2026-04-06T16:00:00Z")
	logger.newSessionID = staticSessionID("44444444-4444-4444-8444-444444444444")
	logger.maxSizeBytes = 16

	if err := os.WriteFile(logger.activePath(), []byte(strings.Repeat("x", 16)), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	if err := logger.Prepare(); err != nil {
		t.Fatalf("Prepare returned error: %v", err)
	}
	if err := logger.Write(Record{Command: "ask", LatencyMS: 10}); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	rotatedData, err := os.ReadFile(logger.rotatedPath(logger.now()))
	if err != nil {
		t.Fatalf("ReadFile rotated returned error: %v", err)
	}
	if string(rotatedData) != strings.Repeat("x", 16) {
		t.Fatalf("unexpected rotated content: %q", string(rotatedData))
	}

	currentData, err := os.ReadFile(logger.activePath())
	if err != nil {
		t.Fatalf("ReadFile active returned error: %v", err)
	}
	if !strings.Contains(string(currentData), " | ask | ") {
		t.Fatalf("expected current command in new active log, got %q", string(currentData))
	}
}

func TestPrepareAL06KeepsOnlyThreeHistoricalFiles(t *testing.T) {
	logger := newTestLogger(t)
	logger.now = fixedClock("2026-04-06T16:30:00Z")

	for _, name := range []string{
		"audit.log.2026-04-05",
		"audit.log.2026-04-04",
		"audit.log.2026-04-03",
	} {
		path := filepath.Join(logger.baseDir, name)
		if err := os.WriteFile(path, []byte(name), 0o644); err != nil {
			t.Fatalf("WriteFile returned error: %v", err)
		}
	}
	if err := os.WriteFile(logger.activePath(), []byte(strings.Repeat("x", 32)), 0o644); err != nil {
		t.Fatalf("WriteFile active returned error: %v", err)
	}
	logger.maxSizeBytes = 16

	if err := logger.Prepare(); err != nil {
		t.Fatalf("Prepare returned error: %v", err)
	}

	entries, err := os.ReadDir(logger.baseDir)
	if err != nil {
		t.Fatalf("ReadDir returned error: %v", err)
	}

	var historical []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "audit.log.") {
			historical = append(historical, entry.Name())
		}
	}

	if len(historical) != 3 {
		t.Fatalf("expected 3 historical files, got %d: %#v", len(historical), historical)
	}
	if containsString(historical, "audit.log.2026-04-03") {
		t.Fatalf("expected oldest historical file to be pruned, got %#v", historical)
	}
}

func TestPrepareAL07RotatesByAge(t *testing.T) {
	logger := newTestLogger(t)
	now := mustTime("2026-04-06T17:00:00Z")
	logger.now = func() time.Time { return now }
	logger.newSessionID = staticSessionID("77777777-7777-4777-8777-777777777777")

	if err := os.WriteFile(logger.activePath(), []byte("old"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	oldTime := now.Add(-(31 * 24 * time.Hour))
	if err := os.Chtimes(logger.activePath(), oldTime, oldTime); err != nil {
		t.Fatalf("Chtimes returned error: %v", err)
	}

	if err := logger.Prepare(); err != nil {
		t.Fatalf("Prepare returned error: %v", err)
	}

	if _, err := os.Stat(logger.rotatedPath(now)); err != nil {
		t.Fatalf("expected rotated file by age, got err=%v", err)
	}
}

func newTestLogger(t *testing.T) *Logger {
	t.Helper()
	return NewLoggerAt(t.TempDir())
}

func fixedClock(value string) func() time.Time {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return func() time.Time { return parsed }
}

func staticSessionID(value string) func() (string, error) {
	return func() (string, error) { return value, nil }
}

func mustTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return parsed
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
