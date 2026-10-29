package core

import (
	"strings"
	"testing"

	"pocketcli/internal/investigation/storage"
)

func TestPublishedDatasetHashPreservesOldWireShapeAndDetectsTamper(t *testing.T) {
	canonical := `{"Snapshot":{"resources":[{"id":"repo-1"}],"id":"","published_at":"","dataset_hash":""},"PullRequests":[{"ID":"pr-1","OldField":"preserved"}]}`
	hash := storage.Hash([]byte(canonical))
	published := strings.Replace(canonical, `"id":"","published_at":"","dataset_hash":""`, `"id":"snap-`+hash[:24]+`","published_at":"2026-10-28T00:00:00Z","dataset_hash":"`+hash+`"`, 1)
	got, err := publishedDatasetHash([]byte(published + "\n"))
	if err != nil || got != hash {
		t.Fatalf("hash publicado: got=%q err=%v want=%q", got, err, hash)
	}
	tampered := strings.Replace(published, `"OldField":"preserved"`, `"OldField":"changed"`, 1)
	changed, err := publishedDatasetHash([]byte(tampered))
	if err != nil || changed == hash {
		t.Fatalf("tamper não detectado: got=%q err=%v", changed, err)
	}
}
