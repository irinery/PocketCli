package contextcollector

import (
	stdctx "context"
	"errors"
	"strings"
	"testing"

	"pocketcli/internal/tools"
)

func TestCollectT05IncludesFailedSkipToolAndAddsWarning(t *testing.T) {
	registry := tools.NewRegistry()
	if err := registry.Register(testCollectorTool("git_status", tools.FailureModeSkip, func(ctx stdctx.Context, input map[string]any) (tools.ExecutionOutput, error) {
		return tools.ExecutionOutput{}, errors.New("git ausente")
	})); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	ctx, err := CollectWithOptions(t.TempDir(), Session{}, CollectOptions{ToolRegistry: registry})
	if err != nil {
		t.Fatalf("CollectWithOptions returned error: %v", err)
	}
	if len(ctx.ToolResults) != 1 {
		t.Fatalf("expected 1 tool result, got %d", len(ctx.ToolResults))
	}
	if ctx.ToolResults[0].OK {
		t.Fatal("expected failed tool result to be preserved")
	}

	warning := ctx.ToolResults[0].Warning(0)
	if !containsNote(ctx.Notes, warning) {
		t.Fatalf("expected warning note %q, got %v", warning, ctx.Notes)
	}
}

func TestCollectT06AbortFailureStopsCollectionAndPropagatesToolName(t *testing.T) {
	registry := tools.NewRegistry()
	secondExecuted := false

	if err := registry.Register(testCollectorTool("a_abort", tools.FailureModeAbort, func(ctx stdctx.Context, input map[string]any) (tools.ExecutionOutput, error) {
		return tools.ExecutionOutput{}, errors.New("falha crítica")
	})); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	if err := registry.Register(testCollectorTool("z_never_runs", tools.FailureModeSkip, func(ctx stdctx.Context, input map[string]any) (tools.ExecutionOutput, error) {
		secondExecuted = true
		return tools.ExecutionOutput{}, nil
	})); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	ctx, err := CollectWithOptions(t.TempDir(), Session{}, CollectOptions{ToolRegistry: registry})
	if err == nil {
		t.Fatal("expected abort failure to stop collection")
	}
	if secondExecuted {
		t.Fatal("expected collection to stop before running the next tool")
	}
	if len(ctx.ToolResults) != 1 {
		t.Fatalf("expected partial context with first tool result, got %d", len(ctx.ToolResults))
	}

	var abortErr *ToolAbortError
	if !errors.As(err, &abortErr) {
		t.Fatalf("expected ToolAbortError, got %T", err)
	}
	if abortErr.ToolName != "a_abort" {
		t.Fatalf("expected abort tool name a_abort, got %q", abortErr.ToolName)
	}
	if !strings.Contains(err.Error(), "a_abort") {
		t.Fatalf("expected propagated error to identify the tool, got %v", err)
	}
}

func TestCollectT07WarnFailureAddsNoteAndContinues(t *testing.T) {
	registry := tools.NewRegistry()
	if err := registry.Register(testCollectorTool("disk_usage", tools.FailureModeWarn, func(ctx stdctx.Context, input map[string]any) (tools.ExecutionOutput, error) {
		return tools.ExecutionOutput{}, errors.New("sem permissão")
	})); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	ctx, err := CollectWithOptions(t.TempDir(), Session{}, CollectOptions{ToolRegistry: registry})
	if err != nil {
		t.Fatalf("CollectWithOptions returned error: %v", err)
	}
	if len(ctx.ToolResults) != 1 {
		t.Fatalf("expected 1 tool result, got %d", len(ctx.ToolResults))
	}

	warning := ctx.ToolResults[0].Warning(0)
	if !containsNote(ctx.Notes, warning) {
		t.Fatalf("expected warning note %q, got %v", warning, ctx.Notes)
	}
}

func testCollectorTool(name string, mode tools.FailureMode, run tools.Runner) tools.Tool {
	return tools.Tool{
		Definition: tools.Definition{
			Name:        name,
			Input:       tools.Schema{},
			Output:      tools.Schema{},
			TimeoutMS:   50,
			FailureMode: mode,
			Version:     "1.0.0",
		},
		Run: run,
	}
}
