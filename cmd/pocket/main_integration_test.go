package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
