package tools

import (
	stdctx "context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRegistryT01RegisteredToolDeclaresRequiredSchema(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(GitStatusTool()); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	registered := registry.List()
	if len(registered) != 1 {
		t.Fatalf("expected 1 registered tool, got %d", len(registered))
	}

	def := registered[0].Definition
	if def.Name == "" {
		t.Fatal("expected name to be declared")
	}
	if def.Input == nil {
		t.Fatal("expected input schema to be declared")
	}
	if def.Output == nil {
		t.Fatal("expected output schema to be declared")
	}
	if def.TimeoutMS <= 0 {
		t.Fatal("expected timeout_ms to be declared")
	}
	if def.FailureMode == "" {
		t.Fatal("expected failure_mode to be declared")
	}
	if def.Version == "" {
		t.Fatal("expected version to be declared")
	}
}

func TestRegistryT02RejectsToolWithoutVersion(t *testing.T) {
	registry := NewRegistry()

	err := registry.Register(newTestTool("sem_version", FailureModeSkip, 50, Definition{
		Input:       Schema{},
		Output:      Schema{},
		Version:     "",
		TimeoutMS:   50,
		FailureMode: FailureModeSkip,
	}, func(ctx stdctx.Context, input map[string]any) (ExecutionOutput, error) {
		return ExecutionOutput{}, nil
	}))
	if err == nil {
		t.Fatal("expected missing version to be rejected")
	}
	if !strings.Contains(err.Error(), "version") {
		t.Fatalf("expected explicit version error, got %v", err)
	}
}

func TestExecuteT03GitStatusReturnsStructuredArtifactsOnValidRepo(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	writeFile(t, dir, "staged.txt", "alpha\n")
	writeFile(t, dir, "unstaged.txt", "beta\n")
	runGitCLI(t, dir, "add", "staged.txt")

	result := Execute(GitStatusTool(), map[string]any{"path": dir})
	if !result.OK {
		t.Fatalf("expected ok result, got summary=%q raw=%q", result.Summary, result.Raw)
	}
	if result.DurationMS <= 0 {
		t.Fatalf("expected duration_ms > 0, got %d", result.DurationMS)
	}

	branch, _ := result.Artifacts["branch"].(string)
	dirty, _ := result.Artifacts["dirty"].(bool)
	stagedFiles, _ := result.Artifacts["staged_files"].([]string)
	unstagedFiles, _ := result.Artifacts["unstaged_files"].([]string)

	if branch != "main" {
		t.Fatalf("expected branch main, got %q", branch)
	}
	if !dirty {
		t.Fatal("expected dirty=true")
	}
	if !containsString(stagedFiles, "staged.txt") {
		t.Fatalf("expected staged_files to contain staged.txt, got %v", stagedFiles)
	}
	if !containsString(unstagedFiles, "unstaged.txt") {
		t.Fatalf("expected unstaged_files to contain unstaged.txt, got %v", unstagedFiles)
	}
}

func TestExecuteT04GitStatusReturnsControlledFailureOutsideGitRepo(t *testing.T) {
	result := Execute(GitStatusTool(), map[string]any{"path": t.TempDir()})
	if result.OK {
		t.Fatal("expected ok=false outside git repo")
	}
	if !strings.Contains(result.Summary, "repositório git") {
		t.Fatalf("expected readable failure summary, got %q", result.Summary)
	}
}

func TestExecuteT08ReturnsTimeoutResultWhenToolExceedsDeclaredTimeout(t *testing.T) {
	tool := newTestTool("slow_tool", FailureModeSkip, 20, Definition{
		Input:       Schema{},
		Output:      Schema{},
		TimeoutMS:   20,
		FailureMode: FailureModeSkip,
		Version:     "1.0.0",
	}, func(ctx stdctx.Context, input map[string]any) (ExecutionOutput, error) {
		<-ctx.Done()
		return ExecutionOutput{}, ctx.Err()
	})

	result := Execute(tool, map[string]any{})
	if result.OK {
		t.Fatal("expected ok=false after timeout")
	}
	if result.Summary != "timeout após 20ms" {
		t.Fatalf("expected timeout summary, got %q", result.Summary)
	}
	if result.FailureMode() != FailureModeSkip {
		t.Fatalf("expected failure_mode skip in metadata, got %q", result.FailureMode())
	}
}

func TestExecuteT09IgnoresArtifactsOutsideDeclaredOutputSchema(t *testing.T) {
	tool := newTestTool("git_status", FailureModeSkip, 50, Definition{
		Input: Schema{
			"path": "string",
		},
		Output: Schema{
			"branch":         "string",
			"dirty":          "boolean",
			"staged_files":   "[string]",
			"unstaged_files": "[string]",
		},
		TimeoutMS:   50,
		FailureMode: FailureModeSkip,
		Version:     "1.0.0",
	}, func(ctx stdctx.Context, input map[string]any) (ExecutionOutput, error) {
		return ExecutionOutput{
			Artifacts: map[string]any{
				"branch":         "main",
				"dirty":          true,
				"staged_files":   []string{"tracked.txt"},
				"unstaged_files": []string{},
				"ssh_config":     "Host *",
			},
		}, nil
	})

	result := Execute(tool, map[string]any{"path": t.TempDir()})
	if !result.OK {
		t.Fatalf("expected ok=true, got summary=%q", result.Summary)
	}
	if _, leaked := result.Artifacts["ssh_config"]; leaked {
		t.Fatalf("expected undeclared artifact to be ignored, got %v", result.Artifacts)
	}
}

func newTestTool(
	name string,
	mode FailureMode,
	timeoutMS int,
	override Definition,
	run Runner,
) Tool {
	definition := Definition{
		Name:        name,
		Input:       Schema{},
		Output:      Schema{},
		TimeoutMS:   timeoutMS,
		FailureMode: mode,
		Version:     "1.0.0",
	}

	if override.Name != "" {
		definition.Name = override.Name
	}
	if override.Input != nil {
		definition.Input = override.Input
	}
	if override.Output != nil {
		definition.Output = override.Output
	}
	if override.TimeoutMS != 0 {
		definition.TimeoutMS = override.TimeoutMS
	}
	if override.FailureMode != "" {
		definition.FailureMode = override.FailureMode
	}
	definition.Version = override.Version

	return Tool{
		Definition: definition,
		Run:        run,
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()

	fullPath := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
}

func initGitRepo(t *testing.T, dir string) {
	t.Helper()

	runGitCLI(t, dir, "init")
	runGitCLI(t, dir, "config", "user.email", "test@example.com")
	runGitCLI(t, dir, "config", "user.name", "PocketCli Tests")
	writeFile(t, dir, "tracked.txt", "base\n")
	runGitCLI(t, dir, "add", "tracked.txt")
	runGitCLI(t, dir, "commit", "-m", "init")
	runGitCLI(t, dir, "branch", "-M", "main")
}

func runGitCLI(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, output)
	}
}

func TestExecuteDoesNotPanicOnRunnerError(t *testing.T) {
	tool := newTestTool("error_tool", FailureModeSkip, 50, Definition{
		Input:       Schema{},
		Output:      Schema{},
		TimeoutMS:   50,
		FailureMode: FailureModeSkip,
		Version:     "1.0.0",
	}, func(ctx stdctx.Context, input map[string]any) (ExecutionOutput, error) {
		return ExecutionOutput{}, errors.New("falha")
	})

	result := Execute(tool, map[string]any{})
	if result.OK {
		t.Fatal("expected failure result")
	}
}
