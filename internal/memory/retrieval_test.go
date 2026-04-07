package memory

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRetrieveR01ScoreCalculatesTagsAndTitleWeight(t *testing.T) {
	store := newTestStore(t)
	store.now = fixedClock("2026-04-06T12:00:00Z")

	entry := Entry{
		ID:         "r01",
		Kind:       KindPattern,
		Scope:      "global",
		Title:      "erro ssh no tailscale",
		Summary:    "conexao remota falhando sem dica adicional",
		Body:       "conexao remota falhando sem dica adicional",
		Tags:       []string{"ssh", "timeout"},
		Confidence: 1.0,
		CreatedAt:  "2026-04-06T08:00:00Z",
	}

	score, err := scoreEntry("ssh timeout", []string{"ssh", "timeout"}, entry, store.now())
	if err != nil {
		t.Fatalf("scoreEntry returned error: %v", err)
	}

	expectedWeight := 1 / math.Log(2)
	if score.base != 8 {
		t.Fatalf("expected base score 8, got %.2f", score.base)
	}
	assertClose(t, score.recencyWeight, expectedWeight)
	assertClose(t, score.final, 8*entry.Confidence*expectedWeight)
}

func TestRetrieveR02ScoreAppliesConfidenceAndRecency(t *testing.T) {
	store := newTestStore(t)
	store.now = fixedClock("2026-04-06T12:00:00Z")

	entry := Entry{
		ID:         "r02",
		Kind:       KindPattern,
		Scope:      "global",
		Title:      "ssh remoto",
		Summary:    "sem timeout registrado",
		Body:       "sem timeout registrado",
		Tags:       []string{"ssh"},
		Confidence: 0.5,
		CreatedAt:  "2026-04-05T12:00:00Z",
	}

	score, err := scoreEntry("ssh", []string{"ssh"}, entry, store.now())
	if err != nil {
		t.Fatalf("scoreEntry returned error: %v", err)
	}

	expectedWeight := 1 / math.Log(3)
	assertClose(t, score.recencyWeight, expectedWeight)
	assertClose(t, score.final, score.base*0.5*expectedWeight)
}

func TestRetrieveR03FiltersResultsBelowMinimumScore(t *testing.T) {
	store := newTestStore(t)
	store.now = fixedClock("2026-04-06T12:00:00Z")

	if _, err := store.Write(Entry{
		ID:         "r03",
		Kind:       KindPattern,
		Scope:      "global",
		Title:      "sem match forte",
		Summary:    "deploy falhou de leve",
		Body:       "deploy falhou de leve",
		Tags:       []string{"release"},
		Confidence: 0.9,
		CreatedAt:  "2026-04-06T08:00:00Z",
	}); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	results, err := store.Retrieve("deploy", RetrievalContext{})
	if err != nil {
		t.Fatalf("Retrieve returned error: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected no results, got %d", len(results))
	}
}

func TestRetrieveR04ReturnsTopFiveOrderedByScore(t *testing.T) {
	store := newTestStore(t)
	store.now = fixedClock("2026-04-06T12:00:00Z")

	entries := []Entry{
		{ID: "r04-1", Kind: KindPattern, Scope: "global", Title: "ssh timeout vpn", Summary: "erro remoto", Body: "erro remoto", Tags: []string{"ssh", "timeout"}, Confidence: 1.0, CreatedAt: "2026-04-06T08:00:00Z"},
		{ID: "r04-2", Kind: KindPattern, Scope: "global", Title: "ssh timeout", Summary: "remoto com ping", Body: "remoto com ping", Tags: []string{"ssh", "timeout"}, Confidence: 0.9, CreatedAt: "2026-04-06T08:00:00Z"},
		{ID: "r04-3", Kind: KindPattern, Scope: "global", Title: "ssh remoto", Summary: "timeout detectado", Body: "timeout detectado", Tags: []string{"ssh"}, Confidence: 1.0, CreatedAt: "2026-04-06T08:00:00Z"},
		{ID: "r04-4", Kind: KindPattern, Scope: "global", Title: "ssh remoto", Summary: "timeout detectado", Body: "timeout detectado", Tags: []string{"ssh"}, Confidence: 0.9, CreatedAt: "2026-04-06T08:00:00Z"},
		{ID: "r04-5", Kind: KindPattern, Scope: "global", Title: "timeout apenas", Summary: "erro de rede", Body: "erro de rede", Tags: []string{"timeout"}, Confidence: 1.0, CreatedAt: "2026-04-06T08:00:00Z"},
		{ID: "r04-6", Kind: KindPattern, Scope: "global", Title: "ssh apenas", Summary: "erro remoto", Body: "erro remoto", Tags: []string{"ssh"}, Confidence: 0.8, CreatedAt: "2026-04-06T08:00:00Z"},
	}

	for _, entry := range entries {
		if _, err := store.Write(entry); err != nil {
			t.Fatalf("Write returned error: %v", err)
		}
	}

	results, err := store.Retrieve("ssh timeout", RetrievalContext{})
	if err != nil {
		t.Fatalf("Retrieve returned error: %v", err)
	}

	if len(results) != 5 {
		t.Fatalf("expected 5 results, got %d", len(results))
	}

	expectedOrder := []string{"r04-1", "r04-2", "r04-3", "r04-4", "r04-5"}
	for idx, expectedID := range expectedOrder {
		if results[idx].ID != expectedID {
			t.Fatalf("expected result %d to be %q, got %q", idx, expectedID, results[idx].ID)
		}
	}
}

func TestRetrieveR05PrefersMoreRecentEntriesWithSameBaseScore(t *testing.T) {
	store := newTestStore(t)
	store.now = fixedClock("2026-04-06T12:00:00Z")

	for _, entry := range []Entry{
		{ID: "recent", Kind: KindPattern, Scope: "global", Title: "ssh timeout remoto", Summary: "timeout no ssh", Body: "timeout no ssh", Tags: []string{"ssh", "timeout"}, Confidence: 1.0, CreatedAt: "2026-04-05T12:00:00Z"},
		{ID: "old", Kind: KindPattern, Scope: "global", Title: "ssh timeout remoto", Summary: "timeout no ssh", Body: "timeout no ssh", Tags: []string{"ssh", "timeout"}, Confidence: 1.0, CreatedAt: "2026-02-05T12:00:00Z"},
	} {
		if _, err := store.Write(entry); err != nil {
			t.Fatalf("Write returned error: %v", err)
		}
	}

	results, err := store.Retrieve("ssh timeout", RetrievalContext{})
	if err != nil {
		t.Fatalf("Retrieve returned error: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].ID != "recent" || results[1].ID != "old" {
		t.Fatalf("unexpected order: %#v", []string{results[0].ID, results[1].ID})
	}
}

func TestRetrieveR06RecencyWeightUsesMaximumValueForToday(t *testing.T) {
	weight, err := recencyWeight("2026-04-06T08:00:00Z", mustTime("2026-04-06T12:00:00Z"))
	if err != nil {
		t.Fatalf("recencyWeight returned error: %v", err)
	}

	assertClose(t, weight, 1/math.Log(2))
}

func TestRetrieveR07UpdatesAccessMetadataForReturnedEntries(t *testing.T) {
	store := newTestStore(t)
	store.now = fixedClock("2026-04-06T18:00:00Z")

	if _, err := store.Write(Entry{
		ID:           "r07",
		Kind:         KindPattern,
		Scope:        "global",
		Title:        "ssh timeout",
		Summary:      "erro remoto",
		Body:         "erro remoto",
		Tags:         []string{"ssh", "timeout"},
		Confidence:   1.0,
		CreatedAt:    "2026-04-05T18:00:00Z",
		LastAccessed: "2026-04-05T18:00:00Z",
		AccessCount:  3,
	}); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	results, err := store.Retrieve("ssh timeout", RetrievalContext{})
	if err != nil {
		t.Fatalf("Retrieve returned error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].AccessCount != 4 {
		t.Fatalf("expected access_count 4, got %d", results[0].AccessCount)
	}
	if results[0].LastAccessed != "2026-04-06T18:00:00Z" {
		t.Fatalf("unexpected last_accessed: %q", results[0].LastAccessed)
	}

	entries := readScopeEntries(t, store, "global")
	if entries[0].AccessCount != 4 {
		t.Fatalf("expected persisted access_count 4, got %d", entries[0].AccessCount)
	}
	if entries[0].LastAccessed != "2026-04-06T18:00:00Z" {
		t.Fatalf("unexpected persisted last_accessed: %q", entries[0].LastAccessed)
	}
}

func TestRetrieveR08DoesNotTouchEntriesOutsideResults(t *testing.T) {
	store := newTestStore(t)
	store.now = fixedClock("2026-04-06T18:00:00Z")

	for _, entry := range []Entry{
		{ID: "kept", Kind: KindPattern, Scope: "global", Title: "ssh timeout", Summary: "erro remoto", Body: "erro remoto", Tags: []string{"ssh", "timeout"}, Confidence: 1.0, CreatedAt: "2026-04-05T18:00:00Z", LastAccessed: "2026-04-05T18:00:00Z", AccessCount: 3},
		{ID: "ignored", Kind: KindPattern, Scope: "global", Title: "sem match forte", Summary: "deploy falhou de leve", Body: "deploy falhou de leve", Tags: []string{"release"}, Confidence: 0.9, CreatedAt: "2026-04-05T18:00:00Z", LastAccessed: "2026-04-05T18:00:00Z", AccessCount: 7},
	} {
		if _, err := store.Write(entry); err != nil {
			t.Fatalf("Write returned error: %v", err)
		}
	}

	if _, err := store.Retrieve("ssh timeout", RetrievalContext{}); err != nil {
		t.Fatalf("Retrieve returned error: %v", err)
	}

	entries := readScopeEntries(t, store, "global")
	byID := make(map[string]Entry, len(entries))
	for _, entry := range entries {
		byID[entry.ID] = entry
	}

	if byID["ignored"].AccessCount != 7 {
		t.Fatalf("expected ignored access_count 7, got %d", byID["ignored"].AccessCount)
	}
	if byID["ignored"].LastAccessed != "2026-04-05T18:00:00Z" {
		t.Fatalf("unexpected ignored last_accessed: %q", byID["ignored"].LastAccessed)
	}
}

func TestRetrieveR09ReturnsEmptyListWithoutErrorWhenNothingMatches(t *testing.T) {
	store := newTestStore(t)
	store.now = fixedClock("2026-04-06T12:00:00Z")

	if _, err := store.Write(Entry{
		ID:         "r09",
		Kind:       KindPattern,
		Scope:      "global",
		Title:      "deploy",
		Summary:    "release",
		Body:       "release",
		Tags:       []string{"release"},
		Confidence: 1.0,
		CreatedAt:  "2026-04-06T08:00:00Z",
	}); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	results, err := store.Retrieve("tailscale", RetrievalContext{})
	if err != nil {
		t.Fatalf("Retrieve returned error: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected empty results, got %d", len(results))
	}
}

func TestRetrieveR10UsesProjectAndGlobalScopesForProjectContext(t *testing.T) {
	store := newTestStore(t)
	store.now = fixedClock("2026-04-06T12:00:00Z")

	projectDir := filepath.Join(t.TempDir(), "PocketCli")
	if err := os.MkdirAll(filepath.Join(projectDir, ".git"), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}

	for _, entry := range []Entry{
		{ID: "global", Kind: KindPattern, Scope: "global", Title: "ssh timeout global", Summary: "erro remoto", Body: "erro remoto", Tags: []string{"ssh"}, Confidence: 1.0, CreatedAt: "2026-04-06T08:00:00Z"},
		{ID: "project", Kind: KindPattern, Scope: "project:pocketcli", Title: "ssh timeout projeto", Summary: "erro remoto", Body: "erro remoto", Tags: []string{"timeout"}, Confidence: 1.0, CreatedAt: "2026-04-06T08:00:00Z"},
		{ID: "host", Kind: KindPattern, Scope: "host:outro", Title: "ssh timeout host", Summary: "erro remoto", Body: "erro remoto", Tags: []string{"ssh", "timeout"}, Confidence: 1.0, CreatedAt: "2026-04-06T08:00:00Z"},
	} {
		if _, err := store.Write(entry); err != nil {
			t.Fatalf("Write returned error: %v", err)
		}
	}

	results, err := store.Retrieve("ssh timeout", RetrievalContext{WorkingDir: filepath.Join(projectDir, "subdir")})
	if err != nil {
		t.Fatalf("Retrieve returned error: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	gotIDs := []string{results[0].ID, results[1].ID}
	if containsString(gotIDs, "host") {
		t.Fatalf("host scope should not be included in project context: %#v", gotIDs)
	}
	if !containsString(gotIDs, "global") || !containsString(gotIDs, "project") {
		t.Fatalf("expected global and project scopes, got %#v", gotIDs)
	}
}

func TestResolveCandidateScopesIncludesExplicitHostAndProject(t *testing.T) {
	scopes, err := ResolveCandidateScopes(RetrievalContext{
		Project: "PocketCli",
		Host:    "Mac Mini",
	})
	if err != nil {
		t.Fatalf("ResolveCandidateScopes returned error: %v", err)
	}

	expected := []string{"global", "project:pocketcli", "host:mac_mini"}
	if len(scopes) != len(expected) {
		t.Fatalf("expected %d scopes, got %d: %#v", len(expected), len(scopes), scopes)
	}
	for idx, scope := range expected {
		if scopes[idx] != scope {
			t.Fatalf("expected scope %d to be %q, got %q", idx, scope, scopes[idx])
		}
	}
}

func assertClose(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 0.0001 {
		t.Fatalf("expected %.4f, got %.4f", want, got)
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func mustTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return parsed
}
