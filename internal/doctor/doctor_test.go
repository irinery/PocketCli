package doctor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEvalInsightsRejectsSecretFixture(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("TOKEN=secret\n"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	_, err := EvalInsights(dir)
	if err == nil {
		t.Fatal("expected secret fixture error")
	}
}
