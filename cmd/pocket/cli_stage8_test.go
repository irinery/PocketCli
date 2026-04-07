package main

import (
	stdctx "context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"pocketcli/internal/backend"
	"pocketcli/internal/contextcollector"
	"pocketcli/internal/memory"
	"pocketcli/internal/router"
)

type fakeCLIBackendClient struct {
	complete func(ctx stdctx.Context, request backend.CompletionRequest) (backend.CompletionResult, error)
}

func (f fakeCLIBackendClient) Complete(ctx stdctx.Context, request backend.CompletionRequest) (backend.CompletionResult, error) {
	return f.complete(ctx, request)
}

func stubCommandRuntime(t *testing.T, runtime commandRuntime) {
	t.Helper()

	orig := newCommandRuntime
	newCommandRuntime = func() commandRuntime { return runtime }
	t.Cleanup(func() { newCommandRuntime = orig })
}

func withWorkingDir(t *testing.T, dir string) {
	t.Helper()

	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd returned error: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir returned error: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(origWD)
	})
}

func initGitRepository(t *testing.T, dir string) {
	t.Helper()

	cmd := exec.Command("git", "init", "-q", dir)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init returned error: %v output=%s", err, output)
	}
}

func TestIntegration_AskSanitizesContextAndAppendsAuditLog(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	projectDir := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, ".env"), []byte("API_TOKEN=super-secret-token\n"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "README.md"), []byte("Projeto de teste\n"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "main.sh"), []byte("#!/bin/sh\necho ok\n"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	withWorkingDir(t, projectDir)

	capturedPrompt := ""
	stubCommandRuntime(t, commandRuntime{
		LocalClient: fakeCLIBackendClient{
			complete: func(ctx stdctx.Context, request backend.CompletionRequest) (backend.CompletionResult, error) {
				capturedPrompt = request.Prompt
				return backend.CompletionResult{
					Model:        "local-test",
					Content:      "resposta segura",
					TokenUsage:   25,
					FinishReason: backend.FinishReasonStop,
				}, nil
			},
		},
		LocalProbe: func(ctx stdctx.Context, collectedContext contextcollector.TaskContext) error { return nil },
	})

	output, err := executeCommand(t, []string{"pocket", "ask", "explique", "esse", "erro"}, "")
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !strings.Contains(output, "resposta segura") {
		t.Fatalf("expected backend response in output, got %q", output)
	}
	if strings.Contains(output, "super-secret-token") {
		t.Fatalf("did not expect secret in output, got %q", output)
	}
	if strings.Contains(capturedPrompt, "super-secret-token") {
		t.Fatalf("did not expect secret in backend prompt, got %q", capturedPrompt)
	}

	data, err := os.ReadFile(filepath.Join(homeDir, ".pocket", "audit.log"))
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if !strings.Contains(string(data), " | ask | local | ") {
		t.Fatalf("expected local backend in audit log, got %q", string(data))
	}
}

func TestIntegration_AskAutoFallsBackToRemoteWhenLocalUnavailable(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	projectDir := t.TempDir()
	withWorkingDir(t, projectDir)

	stubCommandRuntime(t, commandRuntime{
		LocalProbe: func(ctx stdctx.Context, collectedContext contextcollector.TaskContext) error {
			return router.ErrConnection
		},
		RemoteProbe: func(ctx stdctx.Context, collectedContext contextcollector.TaskContext) error {
			return nil
		},
		RemoteClient: fakeCLIBackendClient{
			complete: func(ctx stdctx.Context, request backend.CompletionRequest) (backend.CompletionResult, error) {
				return backend.CompletionResult{
					Model:        "remote-test",
					Content:      "fallback remoto",
					TokenUsage:   14,
					FinishReason: backend.FinishReasonStop,
				}, nil
			},
		},
	})

	output, err := executeCommand(t, []string{"pocket", "ask", "explique", "esse", "erro"}, "")
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	trimmed := strings.TrimSpace(output)
	if !strings.HasPrefix(trimmed, "[backend: remote — local indisponível]") {
		t.Fatalf("expected fallback notification on first line, got %q", output)
	}
	if !strings.Contains(output, "fallback remoto") {
		t.Fatalf("expected remote response, got %q", output)
	}

	data, err := os.ReadFile(filepath.Join(homeDir, ".pocket", "audit.log"))
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if !strings.Contains(string(data), " | ask | remote | ") {
		t.Fatalf("expected remote backend in audit log, got %q", string(data))
	}
}

func TestIntegration_AskShowsCollectedContextWhenAllBackendsUnavailable(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	projectDir := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "README.md"), []byte("README\n"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	withWorkingDir(t, projectDir)

	stubCommandRuntime(t, commandRuntime{
		LocalProbe: func(ctx stdctx.Context, collectedContext contextcollector.TaskContext) error {
			return router.ErrConnection
		},
		RemoteProbe: func(ctx stdctx.Context, collectedContext contextcollector.TaskContext) error {
			return router.ErrConnection
		},
	})

	output, err := executeCommand(t, []string{"pocket", "ask", "explique", "esse", "erro"}, "")
	if err != nil {
		t.Fatalf("expected degraded success, got error: %v", err)
	}
	if !strings.Contains(output, "[backend: none — nenhum backend disponível — exibindo contexto coletado]") {
		t.Fatalf("expected none-backend notification, got %q", output)
	}
	if !strings.Contains(output, "erro: nenhum backend disponível — exibindo contexto coletado") {
		t.Fatalf("expected readable error message, got %q", output)
	}
	if !strings.Contains(output, "[TASK_CONTEXT]") {
		t.Fatalf("expected collected context in output, got %q", output)
	}

	data, err := os.ReadFile(filepath.Join(homeDir, ".pocket", "audit.log"))
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if !strings.Contains(string(data), " | ask | none | ") {
		t.Fatalf("expected none backend in audit log, got %q", string(data))
	}
}

func TestIntegration_AskLocalModeDoesNotFallbackToRemote(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	withWorkingDir(t, t.TempDir())

	remoteCalls := 0
	stubCommandRuntime(t, commandRuntime{
		LocalProbe: func(ctx stdctx.Context, collectedContext contextcollector.TaskContext) error {
			return router.ErrConnection
		},
		RemoteProbe: func(ctx stdctx.Context, collectedContext contextcollector.TaskContext) error {
			remoteCalls++
			return nil
		},
	})

	_, err := executeCommand(t, []string{"pocket", "ask", "--mode", "local", "teste"}, "")
	if err == nil {
		t.Fatal("expected local-mode error")
	}
	if err.Error() != "backend local indisponível — modo local não permite fallback" {
		t.Fatalf("unexpected error message: %q", err.Error())
	}
	if remoteCalls != 0 {
		t.Fatalf("expected remote probe not to run, got %d calls", remoteCalls)
	}
}

func TestIntegration_ContextDisplaysCollectedTaskContextWithoutAuditLog(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	projectDir := filepath.Join(t.TempDir(), "context-project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	initGitRepository(t, projectDir)

	var readmeLines []string
	for i := 0; i < 80; i++ {
		readmeLines = append(readmeLines, "linha readme")
	}
	if err := os.WriteFile(filepath.Join(projectDir, "README.md"), []byte(strings.Join(readmeLines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	var mainLines []string
	for i := 0; i < 140; i++ {
		mainLines = append(mainLines, "echo linha")
	}
	if err := os.WriteFile(filepath.Join(projectDir, "main.sh"), []byte(strings.Join(mainLines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, ".env"), []byte("APP_SECRET=nao-pode-vazar\n"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	withWorkingDir(t, projectDir)

	output, err := executeCommand(t, []string{"pocket", "context"}, "")
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !strings.Contains(output, "[TASK_CONTEXT]") {
		t.Fatalf("expected task context header, got %q", output)
	}
	if !strings.Contains(output, "project.is_git=true") {
		t.Fatalf("expected git context, got %q", output)
	}
	if !strings.Contains(output, "[contexto parcial — itens omitidos]") {
		t.Fatalf("expected truncation note, got %q", output)
	}
	if strings.Contains(output, "nao-pode-vazar") {
		t.Fatalf("did not expect secret in context output, got %q", output)
	}
	if _, err := os.Stat(filepath.Join(homeDir, ".pocket", "audit.log")); !os.IsNotExist(err) {
		t.Fatalf("expected no audit log for context command, got err=%v", err)
	}
}

func TestIntegration_MemorySaveWithExistingIDIncreasesConfidence(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	store := memory.NewStoreAt(filepath.Join(homeDir, ".pocket"))
	if _, err := store.Write(memory.Entry{
		ID:         "known-entry",
		Kind:       memory.KindPattern,
		Scope:      "global",
		Title:      "Known",
		Summary:    "Known",
		Body:       "Known",
		Tags:       []string{"known"},
		Confidence: 0.9,
		CreatedAt:  "2026-04-05T18:00:00Z",
	}); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	origNewMemoryStore := newMemoryStore
	newMemoryStore = func() (*memory.Store, error) { return store, nil }
	t.Cleanup(func() { newMemoryStore = origNewMemoryStore })

	output, err := executeCommand(t, []string{"pocket", "memory", "save", "known-entry"}, "")
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !strings.Contains(output, "id=known-entry confidence=1.0") {
		t.Fatalf("unexpected output: %q", output)
	}
}

func TestIntegration_MemoryDiscardDecreasesConfidence(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	store := memory.NewStoreAt(filepath.Join(homeDir, ".pocket"))
	if _, err := store.Write(memory.Entry{
		ID:         "known-entry",
		Kind:       memory.KindPattern,
		Scope:      "global",
		Title:      "Known",
		Summary:    "Known",
		Body:       "Known",
		Tags:       []string{"known"},
		Confidence: 0.4,
		CreatedAt:  "2026-04-05T18:00:00Z",
	}); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	origNewMemoryStore := newMemoryStore
	newMemoryStore = func() (*memory.Store, error) { return store, nil }
	t.Cleanup(func() { newMemoryStore = origNewMemoryStore })

	output, err := executeCommand(t, []string{"pocket", "memory", "discard", "known-entry"}, "")
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !strings.Contains(output, "id=known-entry confidence=0.3") {
		t.Fatalf("unexpected output: %q", output)
	}
}

func TestIntegration_AuditLogUsesDifferentSessionIDsPerInvocation(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	withWorkingDir(t, t.TempDir())

	stubCommandRuntime(t, commandRuntime{
		LocalClient: fakeCLIBackendClient{
			complete: func(ctx stdctx.Context, request backend.CompletionRequest) (backend.CompletionResult, error) {
				return backend.CompletionResult{
					Model:        "local-test",
					Content:      "ok",
					TokenUsage:   2,
					FinishReason: backend.FinishReasonStop,
				}, nil
			},
		},
		LocalProbe: func(ctx stdctx.Context, collectedContext contextcollector.TaskContext) error { return nil },
	})

	if _, err := executeCommand(t, []string{"pocket", "ask", "primeiro"}, ""); err != nil {
		t.Fatalf("ask returned error: %v", err)
	}
	if _, err := executeCommand(t, []string{"pocket", "recall", "sem", "match"}, ""); err != nil {
		t.Fatalf("recall returned error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(homeDir, ".pocket", "audit.log"))
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 audit lines, got %d", len(lines))
	}

	uuidv4Pattern := regexp.MustCompile(`session_id=([0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12})`)
	m1 := uuidv4Pattern.FindStringSubmatch(lines[0])
	m2 := uuidv4Pattern.FindStringSubmatch(lines[1])
	if len(m1) != 2 || len(m2) != 2 {
		t.Fatalf("expected UUIDv4 session ids, got %q", string(data))
	}
	if m1[1] == m2[1] {
		t.Fatalf("expected distinct session ids, got %q", string(data))
	}
}
