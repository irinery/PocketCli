package main

import (
	stdctx "context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pocketcli/internal/backend"
	"pocketcli/internal/contextcollector"
	"pocketcli/internal/memory"
	"pocketcli/internal/remoteaccess"
)

func TestIntegration_RootDispatchesHosts(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

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
	t.Setenv("HOME", t.TempDir())

	orig := newRemoteExecutor
	t.Cleanup(func() { newRemoteExecutor = orig })

	newRemoteExecutor = func() *remoteaccess.Executor {
		executor := remoteaccess.NewExecutor()
		executor.Logger = nil
		executor.Resolver = func(ctx stdctx.Context, alias string) (remoteaccess.RemoteHost, error) {
			return remoteaccess.RemoteHost{
				Alias:        alias,
				Hostname:     alias,
				AccessMethod: remoteaccess.AccessMethodSSH,
				OSFamily:     remoteaccess.OSFamilyLinux,
				SSHPort:      22,
				Enabled:      true,
			}, nil
		}
		executor.Probe = func(ctx stdctx.Context, host remoteaccess.RemoteHost) error { return nil }
		executor.Runner = func(ctx stdctx.Context, name string, args []string, options remoteaccess.RunOptions) (remoteaccess.RunOutput, error) {
			if gotHost, gotCmd := args[len(args)-2], args[len(args)-1]; gotHost != "node-1" || gotCmd != "uptime" {
				t.Fatalf("unexpected dispatch args host=%q cmd=%q", gotHost, gotCmd)
			}
			return remoteaccess.RunOutput{ExitCode: 1, Stderr: "remote failed"}, errors.New("remote failed")
		}
		return executor
	}

	root := newRootCommand()
	origArgs := os.Args
	os.Args = []string{"pocket", "exec", "node-1", "uptime"}
	t.Cleanup(func() { os.Args = origArgs })

	err := root.Execute()
	if err == nil || err.Error() != "remote command failed: exit_code=1" {
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
	stubCommandRuntime(t, commandRuntime{
		LocalClient: fakeCLIBackendClient{
			complete: func(ctx stdctx.Context, request backend.CompletionRequest) (backend.CompletionResult, error) {
				return backend.CompletionResult{
					Model:        "local-test",
					Content:      "resposta local",
					TokenUsage:   12,
					FinishReason: backend.FinishReasonStop,
				}, nil
			},
		},
		LocalProbe: func(ctx stdctx.Context, collectedContext contextcollector.TaskContext) error { return nil },
	})

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

func TestIntegration_RecallReturnsProjectAndGlobalResultsOrderedByScore(t *testing.T) {
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
	createdAt := time.Now().UTC().Format(time.RFC3339)

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
			CreatedAt:  createdAt,
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
			CreatedAt:  createdAt,
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
			CreatedAt:  createdAt,
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

	output, err := executeCommand(t, []string{"pocket", "recall", "ssh", "timeout"}, "")
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if strings.Index(output, "id=project-entry") > strings.Index(output, "id=global-entry") {
		t.Fatalf("expected project entry before global entry, got %q", output)
	}
	if !strings.Contains(output, "id=global-entry") {
		t.Fatalf("expected global entry in output, got %q", output)
	}
	if !strings.Contains(output, "id=project-entry") {
		t.Fatalf("expected project entry in output, got %q", output)
	}
	if !strings.Contains(output, `title="timeout no projeto"`) {
		t.Fatalf("expected title in output, got %q", output)
	}
	if !strings.Contains(output, `summary="ssh travando"`) {
		t.Fatalf("expected summary in output, got %q", output)
	}
	if strings.Contains(output, "id=other-host") {
		t.Fatalf("did not expect host-scoped entry in project search, got %q", output)
	}

	globalEntries := readEntriesAtPath(t, filepath.Join(homeDir, ".pocket", "memory", "global.jsonl"))
	if globalEntries[0].AccessCount != 1 || strings.TrimSpace(globalEntries[0].LastAccessed) == "" {
		t.Fatalf("expected recall to update access metadata for global entry, got %#v", globalEntries[0])
	}

	projectEntries := readEntriesAtPath(t, filepath.Join(homeDir, ".pocket", "memory", "project_pocketcli.jsonl"))
	if projectEntries[0].AccessCount != 1 || strings.TrimSpace(projectEntries[0].LastAccessed) == "" {
		t.Fatalf("expected recall to update access metadata for project entry, got %#v", projectEntries[0])
	}
}

func TestIntegration_RecallReportsWhenNoResultsAreFound(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	output, err := executeCommand(t, []string{"pocket", "recall", "sem", "match"}, "")
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if strings.TrimSpace(output) != "nenhum resultado encontrado para a query" {
		t.Fatalf("unexpected output: %q", output)
	}
}

func TestIntegration_AskWritesAuditLogWithSameSessionID(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	stubCommandRuntime(t, commandRuntime{
		LocalClient: fakeCLIBackendClient{
			complete: func(ctx stdctx.Context, request backend.CompletionRequest) (backend.CompletionResult, error) {
				return backend.CompletionResult{
					Model:        "local-test",
					Content:      "resposta local",
					TokenUsage:   10,
					FinishReason: backend.FinishReasonStop,
				}, nil
			},
		},
		LocalProbe: func(ctx stdctx.Context, collectedContext contextcollector.TaskContext) error { return nil },
	})

	if _, err := executeCommand(t, []string{"pocket", "ask", "registrar", "auditoria"}, ""); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	store, err := newMemoryStore()
	if err != nil {
		t.Fatalf("newMemoryStore returned error: %v", err)
	}
	lastInteraction, err := store.LoadLastInteraction()
	if err != nil {
		t.Fatalf("LoadLastInteraction returned error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(homeDir, ".pocket", "audit.log"))
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}

	line := strings.TrimSpace(string(data))
	if !strings.Contains(line, " | ask | local | tokens=10 | ") {
		t.Fatalf("expected ask audit line, got %q", line)
	}
	if !strings.Contains(line, "session_id="+lastInteraction.SessionID) {
		t.Fatalf("expected same session_id in audit line, got %q", line)
	}
}

func TestIntegration_MemoryCleanDryRunListsCandidatesWithoutDeleting(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	store, err := memory.NewStore()
	if err != nil {
		t.Fatalf("NewStore returned error: %v", err)
	}

	if _, err := store.Write(memory.Entry{
		ID:           "dry-run",
		Kind:         memory.KindPattern,
		Scope:        "global",
		Title:        "Dry run",
		Summary:      "Candidate for cleanup.",
		Body:         "Candidate for cleanup.",
		Tags:         []string{"cleanup"},
		Confidence:   0.2,
		CreatedAt:    "2000-01-01T00:00:00Z",
		LastAccessed: "2000-01-01T00:00:00Z",
	}); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	output, err := executeCommand(t, []string{"pocket", "memory", "clean", "--dry-run"}, "")
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !strings.Contains(output, "id=dry-run") || !strings.Contains(output, "reasons=low_confidence_stale") {
		t.Fatalf("unexpected dry-run output: %q", output)
	}

	entries := readEntriesAtPath(t, filepath.Join(homeDir, ".pocket", "memory", "global.jsonl"))
	if len(entries) != 1 {
		t.Fatalf("expected candidate to remain persisted, got %d entries", len(entries))
	}
}

func TestIntegration_MemoryCleanPromptsPerEntryAndAllowsRefusal(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	store := memory.NewStoreAt(filepath.Join(homeDir, ".pocket"))
	if _, err := store.Write(memory.Entry{
		ID:           "interactive",
		Kind:         memory.KindPattern,
		Scope:        "global",
		Title:        "Interactive",
		Summary:      "Candidate for cleanup.",
		Body:         "Candidate for cleanup.",
		Tags:         []string{"cleanup"},
		Confidence:   0.2,
		CreatedAt:    "2000-01-01T00:00:00Z",
		LastAccessed: "2000-01-01T00:00:00Z",
	}); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	origNewMemoryStore := newMemoryStore
	newMemoryStore = func() (*memory.Store, error) { return store, nil }
	t.Cleanup(func() { newMemoryStore = origNewMemoryStore })

	output, err := executeCommand(t, []string{"pocket", "memory", "clean"}, "n\n")
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !strings.Contains(output, "mantida id=interactive") {
		t.Fatalf("expected refusal output, got %q", output)
	}
	if !strings.Contains(output, "total_deleted=0") {
		t.Fatalf("expected zero deletions, got %q", output)
	}

	entries := readEntriesAtPath(t, filepath.Join(homeDir, ".pocket", "memory", "global.jsonl"))
	if len(entries) != 1 {
		t.Fatalf("expected entry to remain persisted, got %d entries", len(entries))
	}
}

func TestIntegration_MemoryCleanForceDeletesCandidatesAndShowsTotal(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	store := memory.NewStoreAt(filepath.Join(homeDir, ".pocket"))
	for _, entry := range []memory.Entry{
		{
			ID:           "force-1",
			Kind:         memory.KindPattern,
			Scope:        "global",
			Title:        "Force 1",
			Summary:      "Candidate one.",
			Body:         "Candidate one.",
			Tags:         []string{"cleanup"},
			Confidence:   0.2,
			CreatedAt:    "2000-01-01T00:00:00Z",
			LastAccessed: "2000-01-01T00:00:00Z",
		},
		{
			ID:           "force-2",
			Kind:         memory.KindPattern,
			Scope:        "global",
			Title:        "Force 2",
			Summary:      "Candidate two.",
			Body:         "Candidate two.",
			Tags:         []string{"cleanup"},
			Confidence:   0.2,
			CreatedAt:    "2000-01-02T00:00:00Z",
			LastAccessed: "2000-01-02T00:00:00Z",
		},
	} {
		if _, err := store.Write(entry); err != nil {
			t.Fatalf("Write returned error: %v", err)
		}
	}

	origNewMemoryStore := newMemoryStore
	newMemoryStore = func() (*memory.Store, error) { return store, nil }
	t.Cleanup(func() { newMemoryStore = origNewMemoryStore })

	output, err := executeCommand(t, []string{"pocket", "memory", "clean", "--force"}, "")
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !strings.Contains(output, "total_deleted=2") {
		t.Fatalf("expected total_deleted=2, got %q", output)
	}

	entries := readEntriesAtPath(t, filepath.Join(homeDir, ".pocket", "memory", "global.jsonl"))
	if len(entries) != 0 {
		t.Fatalf("expected all candidates deleted, got %d entries", len(entries))
	}
}

func TestIntegration_MemoryCleanReportsWhenNoCandidatesExist(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	output, err := executeCommand(t, []string{"pocket", "memory", "clean", "--dry-run"}, "")
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if strings.TrimSpace(output) != "nenhuma entrada candidata à remoção" {
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

func executeCommand(t *testing.T, args []string, input string) (string, error) {
	t.Helper()

	root := newRootCommand()
	withArgs(t, args)

	origStdout := os.Stdout
	origStdin := os.Stdin

	outputReader, outputWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe stdout returned error: %v", err)
	}
	inputReader, inputWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe stdin returned error: %v", err)
	}

	if _, err := inputWriter.WriteString(input); err != nil {
		t.Fatalf("WriteString returned error: %v", err)
	}
	if err := inputWriter.Close(); err != nil {
		t.Fatalf("Close stdin writer returned error: %v", err)
	}

	os.Stdout = outputWriter
	os.Stdin = inputReader
	defer func() {
		os.Stdout = origStdout
		os.Stdin = origStdin
	}()

	runErr := root.Execute()

	if err := outputWriter.Close(); err != nil {
		t.Fatalf("Close stdout writer returned error: %v", err)
	}

	output, err := io.ReadAll(outputReader)
	if err != nil {
		t.Fatalf("ReadAll returned error: %v", err)
	}

	return string(output), runErr
}

func readEntriesAtPath(t *testing.T, path string) []memory.Entry {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) == 1 && strings.TrimSpace(lines[0]) == "" {
		return nil
	}

	entries := make([]memory.Entry, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var entry memory.Entry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("Unmarshal returned error: %v", err)
		}
		entries = append(entries, entry)
	}

	return entries
}
