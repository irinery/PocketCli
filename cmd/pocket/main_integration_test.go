package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pocketcli/internal/memory"
)

func TestIntegration_RootDispatchesHosts(t *testing.T) {
	orig := hostsViewer
	t.Cleanup(func() { hostsViewer = orig })

	called := false
	hostsViewer = func(in io.Reader, out io.Writer) error {
		called = true
		return nil
	}

	root := newRootCommand()
	origArgs := os.Args
	os.Args = []string{"pocket", "hosts"}
	t.Cleanup(func() { os.Args = origArgs })

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !called {
		t.Fatal("expected hostsViewer to be called")
	}
}

func TestIntegration_RootDispatchesExecAndPropagatesError(t *testing.T) {
	orig := execSSH
	t.Cleanup(func() { execSSH = orig })

	execSSH = func(host, remoteCmd string) error {
		if host != "node-1" || remoteCmd != "echo hi" {
			t.Fatalf("unexpected dispatch args host=%q cmd=%q", host, remoteCmd)
		}
		return errors.New("remote failed")
	}

	root := newRootCommand()
	origArgs := os.Args
	os.Args = []string{"pocket", "exec", "node-1", "echo", "hi"}
	t.Cleanup(func() { os.Args = origArgs })

	err := root.Execute()
	if err == nil || err.Error() != "remote failed" {
		t.Fatalf("expected dispatched error, got %v", err)
	}
}

func TestIntegration_MemorySaveWithoutRecentInteractionReturnsInformativeError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	root := newRootCommand()
	origArgs := os.Args
	os.Args = []string{"pocket", "memory", "save"}
	t.Cleanup(func() { os.Args = origArgs })

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error when no recent interaction exists")
	}
	if err.Error() != "nenhuma interação recente para salvar" {
		t.Fatalf("unexpected error message: %q", err.Error())
	}
}

func TestIntegration_AskThenMemorySavePersistsProjectScopedEntry(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	projectDir := filepath.Join(t.TempDir(), "PocketCli")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}

	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd returned error: %v", err)
	}
	if err := os.Chdir(projectDir); err != nil {
		t.Fatalf("Chdir returned error: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(origWD)
	})

	origArgs := os.Args
	t.Cleanup(func() { os.Args = origArgs })

	os.Args = []string{"pocket", "ask", "persistir", "memória", "válida"}
	if err := newRootCommand().Execute(); err != nil {
		t.Fatalf("ask execution returned error: %v", err)
	}

	os.Args = []string{"pocket", "memory", "save"}
	if err := newRootCommand().Execute(); err != nil {
		t.Fatalf("memory save execution returned error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(homeDir, ".pocket", "memory", "project_pocketcli.jsonl"))
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if !strings.Contains(string(data), `"scope":"project:pocketcli"`) {
		t.Fatalf("expected project scope in persisted entry, got %q", string(data))
	}
	if !strings.Contains(string(data), `"confidence":0.9`) {
		t.Fatalf("expected confidence 0.9 in persisted entry, got %q", string(data))
	}
}

func TestIntegration_MemorySearchReturnsProjectAndGlobalResults(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	projectDir := filepath.Join(t.TempDir(), "PocketCli")
	if err := os.MkdirAll(filepath.Join(projectDir, ".git"), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}

	store, err := memory.NewStore()
	if err != nil {
		t.Fatalf("NewStore returned error: %v", err)
	}

	for _, entry := range []memory.Entry{
		{
			ID:         "global-entry",
			Kind:       memory.KindPattern,
			Scope:      "global",
			Title:      "ssh timeout global",
			Summary:    "erro remoto",
			Body:       "erro remoto",
			Tags:       []string{"ssh"},
			Confidence: 1.0,
			CreatedAt:  "2026-04-05T18:00:00Z",
		},
		{
			ID:         "project-entry",
			Kind:       memory.KindPattern,
			Scope:      "project:pocketcli",
			Title:      "timeout no projeto",
			Summary:    "ssh travando",
			Body:       "ssh travando",
			Tags:       []string{"timeout"},
			Confidence: 1.0,
			CreatedAt:  "2026-04-05T18:00:00Z",
		},
		{
			ID:         "other-host",
			Kind:       memory.KindPattern,
			Scope:      "host:outro",
			Title:      "ssh timeout host",
			Summary:    "erro remoto",
			Body:       "erro remoto",
			Tags:       []string{"ssh", "timeout"},
			Confidence: 1.0,
			CreatedAt:  "2026-04-05T18:00:00Z",
		},
	} {
		if _, err := store.Write(entry); err != nil {
			t.Fatalf("Write returned error: %v", err)
		}
	}

	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd returned error: %v", err)
	}
	if err := os.Chdir(projectDir); err != nil {
		t.Fatalf("Chdir returned error: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(origWD)
	})

	root := newRootCommand()
	withArgs(t, []string{"pocket", "memory", "search", "ssh", "timeout"})

	output, err := captureStdout(t, root.Execute)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !strings.Contains(output, "id=global-entry") {
		t.Fatalf("expected global entry in output, got %q", output)
	}
	if !strings.Contains(output, "id=project-entry") {
		t.Fatalf("expected project entry in output, got %q", output)
	}
	if strings.Contains(output, "id=other-host") {
		t.Fatalf("did not expect host-scoped entry in project search, got %q", output)
	}
}

func TestIntegration_MemorySearchReportsWhenNoResultsAreFound(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	root := newRootCommand()
	withArgs(t, []string{"pocket", "memory", "search", "sem", "match"})

	output, err := captureStdout(t, root.Execute)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if strings.TrimSpace(output) != "nenhum resultado encontrado" {
		t.Fatalf("unexpected output: %q", output)
	}
}

func captureStdout(t *testing.T, run func() error) (string, error) {
	t.Helper()

	origStdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe returned error: %v", err)
	}

	os.Stdout = writer
	defer func() {
		os.Stdout = origStdout
	}()

	runErr := run()

	if err := writer.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll returned error: %v", err)
	}

	return string(output), runErr
}
