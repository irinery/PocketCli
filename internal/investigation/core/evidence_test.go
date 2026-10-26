package core

import (
	"encoding/json"
	"testing"

	"pocketcli/internal/investigation/storage"
)

func TestEvidenceHashMatchesPersistedJSONWithHTMLCharacters(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCore(CoreConfig{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	raw := json.RawMessage(`{"title":"<b>review</b>","body":"a & b"}`)
	first, err := c.evidence("github", "repo#1", "2026-10-25T00:00:00Z", raw)
	if err != nil {
		t.Fatal(err)
	}
	second, err := c.evidence("github", "repo#1", "2026-10-25T00:00:00Z", raw)
	if err != nil {
		t.Fatalf("releitura da evidência idêntica: %v", err)
	}
	if first.ID != second.ID || first.SHA256 != second.SHA256 {
		t.Fatalf("identidade mudou: %+v %+v", first, second)
	}
	var persisted json.RawMessage
	if err := store.Get("objects", first.ID, &persisted); err != nil {
		t.Fatal(err)
	}
	if storage.Hash(persisted) != first.SHA256 {
		t.Fatal("hash da evidência não corresponde aos bytes persistidos")
	}
}
