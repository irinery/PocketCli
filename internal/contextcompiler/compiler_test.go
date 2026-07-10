package contextcompiler

import (
	"os"
	"path/filepath"
	"testing"

	"pocketcli/internal/contextcollector"
	"pocketcli/internal/safety"
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

func TestCompileBlocksSensitiveAttachmentBehindSymlink(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	t.Setenv("HOME", home)
	sensitive := filepath.Join(home, ".ssh", "id_ed25519")
	if err := os.MkdirAll(filepath.Dir(sensitive), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(sensitive, []byte("private-key"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	link := filepath.Join(dir, "attachment.txt")
	if err := os.Symlink(sensitive, link); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}
	resolved, err := filepath.EvalSymlinks(link)
	if err != nil {
		t.Fatalf("EvalSymlinks() error = %v", err)
	}
	if !safety.SensitivePath(resolved) {
		t.Fatalf("expected sensitive path %q to be blocked", resolved)
	}
	if _, err := Compile(Request{Attachments: []string{link}}); err == nil {
		t.Fatal("expected sensitive symlink attachment to be blocked")
	}
}

func TestCompileBlocksAttachmentWithSecretContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(path, []byte("api_key=top-secret\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if _, err := Compile(Request{Attachments: []string{path}}); err == nil {
		t.Fatal("expected secret-bearing attachment to be blocked")
	}
}
