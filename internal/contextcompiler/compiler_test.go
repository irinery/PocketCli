package contextcompiler

import (
	"os"
	"path/filepath"
	"testing"

	"pocketcli/internal/contextcollector"
)

func TestCompileBlocksSensitiveAttachment(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env.local")
	if err := os.WriteFile(path, []byte("TOKEN=secret\n"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	_, err := Compile(Request{
		Attachments: []string{path},
		TaskContext: contextcollector.TaskContext{},
	})
	if err == nil {
		t.Fatal("expected blocked attachment error")
	}
}

func TestCompileTruncatesLargeInput(t *testing.T) {
	long := make([]byte, 10000)
	for i := range long {
		long[i] = 'a'
	}
	compiled, err := Compile(Request{UserInput: string(long), TaskContext: contextcollector.TaskContext{}})
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if !compiled.Truncated {
		t.Fatal("expected truncated=true")
	}
	if compiled.TokenEstimate > MaxContextTokens {
		t.Fatalf("token estimate above max: %d", compiled.TokenEstimate)
	}
}
