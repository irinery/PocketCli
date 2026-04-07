package contextcollector

import (
	"fmt"
	"strings"

	"pocketcli/internal/tools"
)

type CollectOptions struct {
	ToolRegistry *tools.Registry
	ToolInputs   map[string]map[string]any
}

type ToolAbortError struct {
	ToolName string
	Result   ToolResult
}

func (e *ToolAbortError) Error() string {
	toolName := strings.TrimSpace(e.ToolName)
	if toolName == "" {
		toolName = "desconhecida"
	}
	summary := strings.TrimSpace(e.Result.Summary)
	if summary == "" {
		summary = "falha sem detalhe"
	}
	return fmt.Sprintf("tool %s falhou em modo abort: %s", toolName, summary)
}

func executeRegisteredTools(ctx *TaskContext, options CollectOptions) error {
	if ctx == nil || options.ToolRegistry == nil {
		return nil
	}

	for _, tool := range options.ToolRegistry.List() {
		input := resolveToolInput(ctx.Project.Path, tool, options.ToolInputs)
		result := tools.Execute(tool, input)
		ctx.ToolResults = append(ctx.ToolResults, result)

		index := len(ctx.ToolResults) - 1
		warning := result.Warning(index)

		switch result.FailureMode() {
		case tools.FailureModeAbort:
			if !result.OK {
				return &ToolAbortError{
					ToolName: result.ToolName,
					Result:   result,
				}
			}
		case tools.FailureModeWarn, tools.FailureModeSkip:
			if !result.OK {
				addNote(&ctx.Notes, warning)
			}
		}
	}

	return nil
}

func resolveToolInput(projectPath string, tool tools.Tool, inputs map[string]map[string]any) map[string]any {
	input := map[string]any{}
	if inputs != nil {
		input = cloneAnyMap(inputs[tool.Definition.Name])
	}

	if _, expectsPath := tool.Definition.Input["path"]; expectsPath {
		if _, exists := input["path"]; !exists {
			input["path"] = projectPath
		}
	}

	return input
}

func cloneAnyMap(input map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
	}

	cloned := make(map[string]any, len(input))
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}
