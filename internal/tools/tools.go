package tools

import (
	stdctx "context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

var ErrInvalidTool = errors.New("tool inválida")

type FailureMode string

const (
	FailureModeSkip  FailureMode = "skip"
	FailureModeAbort FailureMode = "abort"
	FailureModeWarn  FailureMode = "warn"
)

type Schema map[string]string

type Definition struct {
	Name        string
	Input       Schema
	Output      Schema
	TimeoutMS   int
	FailureMode FailureMode
	Version     string
}

type ExecutionOutput struct {
	Summary   string
	Raw       string
	Artifacts map[string]any
	Metadata  map[string]any
}

type Runner func(ctx stdctx.Context, input map[string]any) (ExecutionOutput, error)

type Tool struct {
	Definition Definition
	Run        Runner
}

type ToolResult struct {
	ToolName   string
	OK         bool
	Summary    string
	Raw        string
	Artifacts  map[string]any
	DurationMS int
	Metadata   map[string]any
}

func (r ToolResult) FailureMode() FailureMode {
	if r.Metadata == nil {
		return FailureModeSkip
	}

	raw, _ := r.Metadata["failure_mode"].(string)
	mode := FailureMode(strings.TrimSpace(raw))
	if !isValidFailureMode(mode) {
		return FailureModeSkip
	}
	return mode
}

func (r ToolResult) Warning(index int) string {
	toolName := strings.TrimSpace(r.ToolName)
	if toolName == "" {
		toolName = strconv.Itoa(index + 1)
	}
	return fmt.Sprintf("[AVISO: tool %s indisponível — dado ausente]", toolName)
}

func (r ToolResult) WarnAtContextStart() bool {
	return !r.OK && r.FailureMode() == FailureModeSkip
}

type Registry struct {
	tools map[string]Tool
}

func NewRegistry() *Registry {
	return &Registry{tools: map[string]Tool{}}
}

func (r *Registry) Register(tool Tool) error {
	if r == nil {
		return errors.New("registry nil")
	}
	if err := validateTool(tool); err != nil {
		return err
	}

	name := strings.TrimSpace(tool.Definition.Name)
	if _, exists := r.tools[name]; exists {
		return fmt.Errorf("%w: tool %s já registrada", ErrInvalidTool, name)
	}

	r.tools[name] = cloneTool(tool)
	return nil
}

func (r *Registry) Get(name string) (Tool, bool) {
	if r == nil {
		return Tool{}, false
	}
	tool, ok := r.tools[strings.TrimSpace(name)]
	if !ok {
		return Tool{}, false
	}
	return cloneTool(tool), true
}

func (r *Registry) List() []Tool {
	if r == nil {
		return nil
	}

	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)

	list := make([]Tool, 0, len(names))
	for _, name := range names {
		list = append(list, cloneTool(r.tools[name]))
	}
	return list
}

func Execute(tool Tool, input map[string]any) ToolResult {
	start := time.Now()
	result := ToolResult{
		ToolName:  strings.TrimSpace(tool.Definition.Name),
		Artifacts: map[string]any{},
		Metadata:  map[string]any{},
	}

	if err := validateTool(tool); err != nil {
		result.Summary = err.Error()
		result.DurationMS = elapsedMS(start)
		return result
	}

	result.Metadata["failure_mode"] = string(tool.Definition.FailureMode)
	result.Metadata["version"] = strings.TrimSpace(tool.Definition.Version)

	clonedInput := cloneMap(input)
	if err := validateValues("input", tool.Definition.Input, clonedInput); err != nil {
		result.Summary = err.Error()
		result.DurationMS = elapsedMS(start)
		return result
	}

	ctx, cancel := stdctx.WithTimeout(stdctx.Background(), time.Duration(tool.Definition.TimeoutMS)*time.Millisecond)
	defer cancel()

	type executionOutcome struct {
		output ExecutionOutput
		err    error
		panic  any
	}

	done := make(chan executionOutcome, 1)
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				done <- executionOutcome{panic: recovered}
			}
		}()

		output, err := tool.Run(ctx, clonedInput)
		done <- executionOutcome{output: output, err: err}
	}()

	select {
	case <-ctx.Done():
		result.Summary = fmt.Sprintf("timeout após %dms", tool.Definition.TimeoutMS)
		result.Metadata["error"] = ctx.Err().Error()
		result.DurationMS = elapsedMS(start)
		return result
	case outcome := <-done:
		mergeMetadata(result.Metadata, outcome.output.Metadata)
		result.Raw = strings.TrimSpace(outcome.output.Raw)

		if outcome.panic != nil {
			result.Summary = fmt.Sprintf("panic: %v", outcome.panic)
			result.DurationMS = elapsedMS(start)
			return result
		}

		if outcome.err != nil {
			if summary := strings.TrimSpace(outcome.output.Summary); summary != "" {
				result.Summary = summary
			} else {
				result.Summary = strings.TrimSpace(outcome.err.Error())
			}
			result.DurationMS = elapsedMS(start)
			return result
		}

		artifacts, err := filterArtifacts(tool.Definition.Output, outcome.output.Artifacts)
		if err != nil {
			result.Summary = err.Error()
			result.DurationMS = elapsedMS(start)
			return result
		}

		result.OK = true
		if summary := strings.TrimSpace(outcome.output.Summary); summary != "" {
			result.Summary = summary
		} else {
			result.Summary = "ok"
		}
		result.Artifacts = artifacts
		result.DurationMS = elapsedMS(start)
		return result
	}
}

func validateTool(tool Tool) error {
	def := tool.Definition
	name := strings.TrimSpace(def.Name)
	if name == "" {
		return fmt.Errorf("%w: tool sem name declarado", ErrInvalidTool)
	}
	if tool.Run == nil {
		return fmt.Errorf("%w: tool %s sem executor declarado", ErrInvalidTool, name)
	}
	if def.Input == nil {
		return fmt.Errorf("%w: tool %s sem input declarado", ErrInvalidTool, name)
	}
	if def.Output == nil {
		return fmt.Errorf("%w: tool %s sem output declarado", ErrInvalidTool, name)
	}
	if def.TimeoutMS <= 0 {
		return fmt.Errorf("%w: tool %s sem timeout_ms válido", ErrInvalidTool, name)
	}
	if !isValidFailureMode(def.FailureMode) {
		return fmt.Errorf("%w: tool %s com failure_mode inválido", ErrInvalidTool, name)
	}
	if strings.TrimSpace(def.Version) == "" {
		return fmt.Errorf("%w: tool %s sem version declarada", ErrInvalidTool, name)
	}
	if err := validateSchema("input", def.Input); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidTool, err)
	}
	if err := validateSchema("output", def.Output); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidTool, err)
	}
	return nil
}

func validateSchema(label string, schema Schema) error {
	for field, kind := range schema {
		if strings.TrimSpace(field) == "" {
			return fmt.Errorf("%s schema contém campo vazio", label)
		}
		if !isSupportedType(kind) {
			return fmt.Errorf("%s schema contém tipo inválido para %s", label, field)
		}
	}
	return nil
}

func validateValues(label string, schema Schema, values map[string]any) error {
	for field, kind := range schema {
		value, ok := values[field]
		if !ok {
			return fmt.Errorf("%s inválido: campo obrigatório %s ausente", label, field)
		}
		if !matchesType(kind, value) {
			return fmt.Errorf("%s inválido: campo %s deve ser %s", label, field, normalizeType(kind))
		}
	}
	return nil
}

func filterArtifacts(schema Schema, artifacts map[string]any) (map[string]any, error) {
	filtered := make(map[string]any, len(schema))
	if artifacts == nil {
		artifacts = map[string]any{}
	}

	for field, kind := range schema {
		value, ok := artifacts[field]
		if !ok {
			return nil, fmt.Errorf("output inválido: campo obrigatório %s ausente", field)
		}
		if !matchesType(kind, value) {
			return nil, fmt.Errorf("output inválido: campo %s deve ser %s", field, normalizeType(kind))
		}
		filtered[field] = cloneValue(value)
	}

	return filtered, nil
}

func isValidFailureMode(mode FailureMode) bool {
	switch mode {
	case FailureModeSkip, FailureModeAbort, FailureModeWarn:
		return true
	default:
		return false
	}
}

func isSupportedType(kind string) bool {
	switch normalizeType(kind) {
	case "string", "boolean", "int", "map", "[string]":
		return true
	default:
		return false
	}
}

func matchesType(kind string, value any) bool {
	switch normalizeType(kind) {
	case "string":
		_, ok := value.(string)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "int":
		_, ok := value.(int)
		return ok
	case "map":
		_, ok := value.(map[string]any)
		return ok
	case "[string]":
		_, ok := value.([]string)
		return ok
	default:
		return false
	}
}

func normalizeType(kind string) string {
	normalized := strings.ToLower(strings.TrimSpace(kind))
	switch normalized {
	case "bool":
		return "boolean"
	default:
		return normalized
	}
}

func cloneTool(tool Tool) Tool {
	cloned := tool
	cloned.Definition.Input = cloneSchema(tool.Definition.Input)
	cloned.Definition.Output = cloneSchema(tool.Definition.Output)
	return cloned
}

func cloneSchema(schema Schema) Schema {
	if schema == nil {
		return nil
	}
	cloned := make(Schema, len(schema))
	for key, value := range schema {
		cloned[key] = value
	}
	return cloned
}

func cloneMap(input map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
	}
	cloned := make(map[string]any, len(input))
	for key, value := range input {
		cloned[key] = cloneValue(value)
	}
	return cloned
}

func cloneValue(value any) any {
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...)
	case map[string]any:
		return cloneMap(typed)
	default:
		return typed
	}
}

func mergeMetadata(base map[string]any, extra map[string]any) {
	for key, value := range extra {
		base[key] = cloneValue(value)
	}
}

func elapsedMS(start time.Time) int {
	elapsed := int(time.Since(start) / time.Millisecond)
	if elapsed < 1 {
		return 1
	}
	return elapsed
}
