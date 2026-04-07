package backend

import (
	stdctx "context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"pocketcli/internal/contextcollector"
	"pocketcli/internal/router"
)

const (
	DefaultMaxTokens   = 1200
	DefaultTemperature = 0.2

	FinishReasonStop   = "stop"
	FinishReasonLength = "length"
	FinishReasonError  = "error"

	defaultSystemPrompt = "Você é o assistente técnico do PocketCli. Responda com foco operacional, objetividade e passos concretos."
	defaultLocalModel   = "local-default"
	defaultRemoteModel  = "remote-default"
)

type Mode struct {
	Debug        bool
	UserInput    string
	SystemPrompt string
	Temperature  *float64
}

type Request struct {
	Backend      string
	Context      contextcollector.TaskContext
	Mode         Mode
	LocalClient  Client
	RemoteClient Client
	LocalModel   string
	RemoteModel  string
	MaxTokens    int
}

type CompletionRequest struct {
	Prompt      string
	Model       string
	MaxTokens   int
	Temperature float64
	Timeout     time.Duration
}

type CompletionResult struct {
	Model        string
	Content      string
	TokenUsage   int
	FinishReason string
}

type Client interface {
	Complete(ctx stdctx.Context, request CompletionRequest) (CompletionResult, error)
}

type LLMResponse struct {
	Backend      string `json:"backend"`
	Model        string `json:"model"`
	Content      string `json:"content"`
	TokenUsage   int    `json:"token_usage"`
	LatencyMS    int    `json:"latency_ms"`
	FinishReason string `json:"finish_reason"`
	Reason       string `json:"reason,omitempty"`
}

func Call(parent stdctx.Context, request Request) LLMResponse {
	if parent == nil {
		parent = stdctx.Background()
	}

	start := time.Now()
	backendName := normalizeBackend(request.Backend)
	model := resolveModel(backendName, request)
	response := LLMResponse{
		Backend:      backendName,
		Model:        model,
		Content:      "",
		TokenUsage:   0,
		LatencyMS:    0,
		FinishReason: FinishReasonError,
	}

	client, timeout, reason, err := resolveRuntime(backendName, request)
	if err != nil {
		return errorResponse(start, response, reason, err)
	}

	callRequest := CompletionRequest{
		Prompt:      buildPrompt(request.Context, request.Mode),
		Model:       model,
		MaxTokens:   resolveMaxTokens(request.MaxTokens),
		Temperature: resolveTemperature(request.Mode),
		Timeout:     timeout,
	}

	ctx, cancel := stdctx.WithTimeout(parent, timeout)
	defer cancel()

	result, err := client.Complete(ctx, callRequest)
	if err != nil {
		return errorResponse(start, response, classifyCallError(err), err)
	}

	response.Content = strings.TrimSpace(result.Content)
	response.TokenUsage = result.TokenUsage
	response.LatencyMS = elapsedMS(start)
	if strings.TrimSpace(result.Model) != "" {
		response.Model = strings.TrimSpace(result.Model)
	}
	if strings.TrimSpace(result.FinishReason) == "" {
		response.FinishReason = FinishReasonStop
	} else {
		response.FinishReason = strings.TrimSpace(result.FinishReason)
	}

	return response
}

func resolveRuntime(backendName string, request Request) (Client, time.Duration, string, error) {
	switch backendName {
	case router.BackendLocal:
		if request.LocalClient == nil {
			return nil, 0, "unavailable", errors.New("backend local não configurado")
		}
		return request.LocalClient, router.DefaultLocalTimeout, "", nil
	case router.BackendRemote:
		if request.RemoteClient == nil {
			return nil, 0, "unavailable", errors.New("backend remoto não configurado")
		}
		return request.RemoteClient, router.DefaultRemoteTimeout, "", nil
	default:
		return nil, 0, "invalid_backend", fmt.Errorf("backend inválido: %s", strings.TrimSpace(request.Backend))
	}
}

func resolveModel(backendName string, request Request) string {
	switch backendName {
	case router.BackendLocal:
		if strings.TrimSpace(request.LocalModel) != "" {
			return strings.TrimSpace(request.LocalModel)
		}
		return defaultLocalModel
	case router.BackendRemote:
		if strings.TrimSpace(request.RemoteModel) != "" {
			return strings.TrimSpace(request.RemoteModel)
		}
		return defaultRemoteModel
	default:
		return "unknown"
	}
}

func resolveMaxTokens(maxTokens int) int {
	if maxTokens <= 0 {
		return DefaultMaxTokens
	}
	return maxTokens
}

func resolveTemperature(mode Mode) float64 {
	if mode.Temperature == nil {
		return DefaultTemperature
	}
	if !mode.Debug {
		return DefaultTemperature
	}
	return *mode.Temperature
}

func buildPrompt(taskContext contextcollector.TaskContext, mode Mode) string {
	systemPrompt := strings.TrimSpace(mode.SystemPrompt)
	if systemPrompt == "" {
		systemPrompt = defaultSystemPrompt
	}

	contextSection := buildContextSection(taskContext)
	userInput := strings.TrimSpace(mode.UserInput)
	if userInput == "" {
		userInput = "(sem user_input)"
	}

	systemSection := renderSection("SYSTEM", systemPrompt)
	userSection := renderSection("USER_INPUT", userInput)

	allowedContextTokens := contextcollector.MaxContextTokens - approxTokens(systemSection) - approxTokens(userSection)
	if allowedContextTokens < 1 {
		allowedContextTokens = 1
	}
	contextSection = truncateToTokens(renderSection("CONTEXTO", contextSection), allowedContextTokens)

	prompt := strings.Join([]string{systemSection, contextSection, userSection}, "\n\n")
	if approxTokens(prompt) <= contextcollector.MaxContextTokens {
		return prompt
	}

	allowedUserTokens := contextcollector.MaxContextTokens - approxTokens(systemSection) - approxTokens(contextSection)
	if allowedUserTokens < 1 {
		allowedUserTokens = 1
	}
	userSection = truncateToTokens(userSection, allowedUserTokens)

	prompt = strings.Join([]string{systemSection, contextSection, userSection}, "\n\n")
	if approxTokens(prompt) <= contextcollector.MaxContextTokens {
		return prompt
	}

	allowedSystemTokens := contextcollector.MaxContextTokens - approxTokens(contextSection) - approxTokens(userSection)
	if allowedSystemTokens < 1 {
		allowedSystemTokens = 1
	}
	systemSection = truncateToTokens(systemSection, allowedSystemTokens)

	return strings.Join([]string{systemSection, contextSection, userSection}, "\n\n")
}

func buildContextSection(taskContext contextcollector.TaskContext) string {
	var blocks []string

	if warnings := formatMissingToolWarnings(taskContext.ToolResults); len(warnings) > 0 {
		blocks = append(blocks, strings.Join(warnings, "\n"))
	}

	if taskContext.Session.LastError != nil && strings.TrimSpace(*taskContext.Session.LastError) != "" {
		blocks = append(blocks, "last_error:\n"+strings.TrimSpace(*taskContext.Session.LastError))
	}

	var contextData []string
	if taskContext.Session.LastCommand != nil && strings.TrimSpace(*taskContext.Session.LastCommand) != "" {
		contextData = append(contextData, "last_command: "+strings.TrimSpace(*taskContext.Session.LastCommand))
	}
	if strings.TrimSpace(taskContext.Project.Path) != "" {
		contextData = append(contextData, "project_path: "+strings.TrimSpace(taskContext.Project.Path))
	}
	if taskContext.Project.Branch != nil && strings.TrimSpace(*taskContext.Project.Branch) != "" {
		contextData = append(contextData, "project_branch: "+strings.TrimSpace(*taskContext.Project.Branch))
	}
	if strings.TrimSpace(taskContext.System.OS) != "" {
		contextData = append(contextData, "system_os: "+strings.TrimSpace(taskContext.System.OS))
	}
	if len(taskContext.Project.MainFiles) > 0 {
		var lines []string
		for _, file := range taskContext.Project.MainFiles {
			entry := strings.TrimSpace(file.Path)
			if strings.TrimSpace(file.Excerpt) != "" {
				entry += "\n" + strings.TrimSpace(file.Excerpt)
			}
			lines = append(lines, "- "+entry)
		}
		contextData = append(contextData, "main_files:\n"+strings.Join(lines, "\n"))
	}
	if taskContext.Project.ReadmeExcerpt != nil && strings.TrimSpace(*taskContext.Project.ReadmeExcerpt) != "" {
		contextData = append(contextData, "readme_excerpt:\n"+strings.TrimSpace(*taskContext.Project.ReadmeExcerpt))
	}
	if taskContext.Project.GitDiffHead != nil && strings.TrimSpace(*taskContext.Project.GitDiffHead) != "" {
		contextData = append(contextData, "git_diff_head:\n"+strings.TrimSpace(*taskContext.Project.GitDiffHead))
	}
	if taskContext.Project.GitLog != nil && strings.TrimSpace(*taskContext.Project.GitLog) != "" {
		contextData = append(contextData, "git_log:\n"+strings.TrimSpace(*taskContext.Project.GitLog))
	}
	if successful := formatSuccessfulToolResults(taskContext.ToolResults); len(successful) > 0 {
		contextData = append(contextData, "tool_results:\n"+strings.Join(successful, "\n"))
	}
	if notes := visibleNotes(taskContext.Notes, taskContext.ToolResults); len(notes) > 0 {
		contextData = append(contextData, "notes:\n- "+strings.Join(notes, "\n- "))
	}
	if len(contextData) > 0 {
		blocks = append(blocks, strings.Join(contextData, "\n\n"))
	}

	if len(taskContext.MemoryHits) > 0 {
		var lines []string
		for _, hit := range taskContext.MemoryHits {
			if strings.TrimSpace(hit) == "" {
				continue
			}
			lines = append(lines, "- "+strings.TrimSpace(hit))
		}
		if len(lines) > 0 {
			blocks = append(blocks, "memory_hits:\n"+strings.Join(lines, "\n"))
		}
	}

	if len(blocks) == 0 {
		return "sem contexto adicional"
	}

	return strings.Join(blocks, "\n\n")
}

func formatMissingToolWarnings(results []contextcollector.ToolResult) []string {
	var warnings []string
	for idx, result := range results {
		if !result.WarnAtContextStart() {
			continue
		}
		warnings = append(warnings, result.Warning(idx))
	}
	return warnings
}

func formatSuccessfulToolResults(results []contextcollector.ToolResult) []string {
	var lines []string
	for _, result := range results {
		if !result.OK {
			continue
		}

		toolName := strings.TrimSpace(result.ToolName)
		if toolName == "" {
			toolName = "tool"
		}

		summary := strings.TrimSpace(result.Summary)
		raw := strings.TrimSpace(result.Raw)

		switch {
		case summary != "":
			lines = append(lines, fmt.Sprintf("- %s: %s", toolName, summary))
		case raw != "":
			lines = append(lines, fmt.Sprintf("- %s: %s", toolName, raw))
		default:
			lines = append(lines, fmt.Sprintf("- %s: ok", toolName))
		}
	}
	return lines
}

func visibleNotes(notes []string, results []contextcollector.ToolResult) []string {
	hiddenWarnings := map[string]struct{}{}
	for idx, result := range results {
		if result.WarnAtContextStart() {
			hiddenWarnings[result.Warning(idx)] = struct{}{}
		}
	}

	filtered := make([]string, 0, len(notes))
	for _, note := range notes {
		if _, hidden := hiddenWarnings[note]; hidden {
			continue
		}
		filtered = append(filtered, note)
	}
	return filtered
}

func renderSection(title, body string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		body = "(vazio)"
	}
	return fmt.Sprintf("[%s]\n%s", title, body)
}

func normalizeBackend(backend string) string {
	return strings.ToLower(strings.TrimSpace(backend))
}

func classifyCallError(err error) string {
	switch {
	case errors.Is(err, stdctx.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, stdctx.Canceled):
		return "canceled"
	default:
		return "backend_error"
	}
}

func errorResponse(start time.Time, response LLMResponse, reason string, err error) LLMResponse {
	response.Reason = reason
	response.Content = formatErrorMessage(response.Backend, reason, err)
	response.LatencyMS = elapsedMS(start)
	response.FinishReason = FinishReasonError
	return response
}

func formatErrorMessage(backendName, reason string, err error) string {
	switch reason {
	case "timeout":
		return fmt.Sprintf("backend %s excedeu o timeout configurado", backendName)
	case "canceled":
		return fmt.Sprintf("backend %s foi cancelado: %v", backendName, err)
	case "unavailable":
		return err.Error()
	case "invalid_backend":
		return err.Error()
	default:
		return fmt.Sprintf("backend %s falhou: %v", backendName, err)
	}
}

func elapsedMS(start time.Time) int {
	return int(time.Since(start) / time.Millisecond)
}

func approxTokens(input string) int {
	count := 0
	inToken := false
	for _, r := range input {
		if unicode.IsSpace(r) {
			inToken = false
			continue
		}
		if !inToken {
			count++
			inToken = true
		}
	}
	return count
}

func truncateToTokens(input string, allowedTokens int) string {
	if allowedTokens < 1 {
		allowedTokens = 1
	}
	if approxTokens(input) <= allowedTokens {
		return input
	}

	count := 0
	inToken := false
	keep := 0

	for idx, r := range input {
		if unicode.IsSpace(r) {
			if count <= allowedTokens {
				keep = idx + len(string(r))
			}
			inToken = false
			continue
		}
		if !inToken {
			count++
			inToken = true
			if count > allowedTokens {
				return strings.TrimRightFunc(input[:keep], unicode.IsSpace)
			}
		}
		if count <= allowedTokens {
			keep = idx + len(string(r))
		}
	}

	return input
}
