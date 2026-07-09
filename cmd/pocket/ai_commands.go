package main

import (
	"bytes"
	stdctx "context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"pocketcli/internal/backend"
	"pocketcli/internal/contextcollector"
	"pocketcli/internal/contextcompiler"
	"pocketcli/internal/ledger"
	"pocketcli/internal/memory"
	"pocketcli/internal/router"
	"pocketcli/internal/tools"
)

const defaultProfile = "default"

const maxBackendCommandOutputBytes = 64 * 1024

var (
	collectTaskContext = func(cwd string, session contextcollector.Session, options contextcollector.CollectOptions) (contextcollector.TaskContext, error) {
		return contextcollector.CollectWithOptions(cwd, session, options)
	}
	routeTask = func(mode string, request router.Request) (router.Decision, error) {
		return router.Route(mode, request)
	}
	callBackend       = backend.Call
	newCommandRuntime = defaultCommandRuntime
)

type taskRequest struct {
	Command     string
	UserInput   string
	CWD         string
	Profile     string
	Mode        string
	SessionID   string
	Host        string
	Attachments []string
	Flags       map[string]any
}

type askCommandOptions struct {
	Kind           string
	Scope          string
	Title          string
	TagsCSV        string
	Mode           string
	Host           string
	Profile        string
	SystemPrompt   string
	Attachments    []string
	Debug          bool
	ExplainContext bool
	Temperature    float64
	TemperatureSet bool
}

type contextCommandOptions struct {
	Host        string
	Profile     string
	Attachments []string
	Format      string
}

type commandRuntime struct {
	LocalClient  backend.Client
	RemoteClient backend.Client
	LocalProbe   router.Probe
	RemoteProbe  router.Probe
	LocalModel   string
	RemoteModel  string
}

type staticBackendClient struct {
	content string
	model   string
}

func (c staticBackendClient) Complete(_ stdctx.Context, request backend.CompletionRequest) (backend.CompletionResult, error) {
	model := strings.TrimSpace(c.model)
	if model == "" {
		model = request.Model
	}

	content := strings.TrimSpace(c.content)
	if content == "" {
		content = "(sem conteúdo retornado pelo backend)"
	}

	return backend.CompletionResult{
		Model:        model,
		Content:      content,
		TokenUsage:   approximateTokenCount(content),
		FinishReason: backend.FinishReasonStop,
	}, nil
}

type commandBackendClient struct {
	command string
}

func (c commandBackendClient) Complete(ctx stdctx.Context, request backend.CompletionRequest) (backend.CompletionResult, error) {
	cmd := exec.CommandContext(ctx, "sh", "-lc", c.command)
	cmd.Stdin = strings.NewReader(request.Prompt)
	cmd.Env = append(os.Environ(),
		"POCKETCLI_MODEL="+request.Model,
		"POCKETCLI_MAX_TOKENS="+strconv.Itoa(request.MaxTokens),
		"POCKETCLI_TEMPERATURE="+strconv.FormatFloat(request.Temperature, 'f', -1, 64),
		"POCKETCLI_TIMEOUT_MS="+strconv.FormatInt(request.Timeout.Milliseconds(), 10),
	)

	output := newCommandOutputBuffer(maxBackendCommandOutputBytes)
	cmd.Stdout = &output
	cmd.Stderr = &output
	err := cmd.Run()
	content := strings.TrimSpace(output.String())
	if output.Truncated() {
		content = strings.TrimSpace(commandOutputWithMarker(content, maxBackendCommandOutputBytes))
	}
	if err != nil {
		if content == "" {
			content = err.Error()
		}
		return backend.CompletionResult{}, errors.New(content)
	}

	if content == "" {
		content = "(sem conteúdo retornado pelo backend)"
	}

	return backend.CompletionResult{
		Model:        request.Model,
		Content:      content,
		TokenUsage:   approximateTokenCount(content),
		FinishReason: backend.FinishReasonStop,
	}, nil
}

type commandOutputBuffer struct {
	buffer    bytes.Buffer
	maxBytes  int
	truncated bool
}

func newCommandOutputBuffer(maxBytes int) commandOutputBuffer {
	return commandOutputBuffer{maxBytes: maxBytes}
}

func (b *commandOutputBuffer) Write(value []byte) (int, error) {
	remaining := b.maxBytes - b.buffer.Len()
	if remaining <= 0 {
		b.truncated = b.truncated || len(value) > 0
		return len(value), nil
	}
	if len(value) > remaining {
		_, _ = b.buffer.Write(value[:remaining])
		b.truncated = true
		return len(value), nil
	}
	_, err := b.buffer.Write(value)
	return len(value), err
}

func (b *commandOutputBuffer) String() string { return b.buffer.String() }

func (b *commandOutputBuffer) Truncated() bool { return b.truncated }

func commandOutputWithMarker(value string, maxBytes int) string {
	marker := "\n[output truncated]"
	if maxBytes <= len(marker) {
		return marker[:maxBytes]
	}
	if len(value)+len(marker) <= maxBytes {
		return value + marker
	}
	return value[:maxBytes-len(marker)] + marker
}

func newAskCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "ask [flags] <prompt...>",
		Short: "Executa o fluxo completo de contexto, memória, roteamento e backend",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAudited(cmd, "ask", args, func(cmd *cobra.Command, args []string, sessionID string) (commandAudit, error) {
				return runAskCommand(cmd, args, sessionID)
			})
		},
	}
}

func newRecallCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "recall [flags] <query...>",
		Short: "Busca memórias relevantes ordenadas por score",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAudited(cmd, "recall", args, func(cmd *cobra.Command, args []string, sessionID string) (commandAudit, error) {
				return runRecallCommand(cmd, args, sessionID)
			})
		},
	}
}

func newContextCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "context",
		Short: "Exibe o TaskContext coletado sem chamar backend",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runContextCommand(cmd, args)
		},
	}
}

func runAskCommand(cmd *cobra.Command, args []string, sessionID string) (commandAudit, error) {
	cwd, err := getWorkingDir()
	if err != nil {
		return commandAudit{}, err
	}

	request, askInput, opts, err := parseAskCommandArgs(args, cwd, sessionID)
	if err != nil {
		return commandAudit{}, err
	}

	store, err := newMemoryStore()
	if err != nil {
		return commandAudit{}, err
	}

	taskContext, memoryHits, err := buildTaskContextForRequest(request, store)
	if err != nil {
		return commandAudit{}, err
	}

	if opts.ExplainContext {
		compiled, err := contextcompiler.Compile(contextcompiler.Request{
			UserInput:   request.UserInput,
			Host:        request.Host,
			Attachments: request.Attachments,
			TaskContext: taskContext,
		})
		if err != nil {
			return commandAudit{}, err
		}
		if err := writeJSON(cmd.OutOrStdout(), compiled); err != nil {
			return commandAudit{}, err
		}
		if _, err := store.RecordAsk(askInput); err != nil {
			return commandAudit{}, err
		}
		return commandAudit{SessionID: request.SessionID, MemoryHit: len(memoryHits) > 0}, nil
	}

	runtime := newCommandRuntime()
	decision, err := routeTask(request.Mode, router.Request{
		Context:     taskContext,
		LocalProbe:  runtime.LocalProbe,
		RemoteProbe: runtime.RemoteProbe,
	})
	if err != nil {
		return commandAudit{}, err
	}

	result := commandAudit{
		SessionID: request.SessionID,
		Decision:  decision,
		MemoryHit: len(memoryHits) > 0,
	}

	if err := writeBackendNotification(cmd, decision); err != nil {
		return result, err
	}

	if decision.SelectedBackend == router.BackendNone {
		if request.Mode != router.ModeAuto {
			return result, errors.New(decision.Reason)
		}

		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "erro: %s\n", decision.Reason); err != nil {
			return result, err
		}
		if _, err := fmt.Fprintln(cmd.OutOrStdout(), formatTaskContext(taskContext)); err != nil {
			return result, err
		}

		if _, err := store.RecordAsk(askInput); err != nil {
			return result, err
		}
		return result, nil
	}

	response := callBackend(stdctx.Background(), backend.Request{
		Backend: decision.SelectedBackend,
		Context: taskContext,
		Mode: backend.Mode{
			Debug:        opts.Debug,
			UserInput:    request.UserInput,
			SystemPrompt: opts.SystemPrompt,
			Temperature:  optionalTemperature(opts),
		},
		LocalClient:  runtime.LocalClient,
		RemoteClient: runtime.RemoteClient,
		LocalModel:   runtime.LocalModel,
		RemoteModel:  runtime.RemoteModel,
	})

	result.Response = response
	result.LatencyMS = response.LatencyMS

	if strings.TrimSpace(response.Content) != "" {
		if _, err := fmt.Fprintln(cmd.OutOrStdout(), strings.TrimSpace(response.Content)); err != nil {
			return result, err
		}
	}

	if response.FinishReason == backend.FinishReasonError {
		return result, errors.New(strings.TrimSpace(response.Content))
	}

	if _, err := store.RecordAsk(askInput); err != nil {
		return result, err
	}

	return result, nil
}

func runRecallCommand(cmd *cobra.Command, args []string, sessionID string) (commandAudit, error) {
	store, err := newMemoryStore()
	if err != nil {
		return commandAudit{}, err
	}

	cwd, err := getWorkingDir()
	if err != nil {
		cwd = ""
	}

	query, retrievalContext, err := parseRecallInput(args, cwd)
	if err != nil {
		return commandAudit{}, err
	}

	results, err := store.Retrieve(query, retrievalContext)
	if err != nil {
		return commandAudit{}, err
	}

	if len(results) == 0 {
		if _, err := fmt.Fprintln(cmd.OutOrStdout(), "nenhum resultado encontrado para a query"); err != nil {
			return commandAudit{}, err
		}
		return commandAudit{SessionID: sessionID}, nil
	}

	for _, entry := range results {
		if _, err := fmt.Fprintf(
			cmd.OutOrStdout(),
			"id=%s scope=%s confidence=%.1f access_count=%d last_accessed=%s title=%q summary=%q\n",
			entry.ID,
			entry.Scope,
			entry.Confidence,
			entry.AccessCount,
			entry.LastAccessed,
			entry.Title,
			entry.Summary,
		); err != nil {
			return commandAudit{}, err
		}
	}

	return commandAudit{
		SessionID: sessionID,
		MemoryHit: true,
	}, nil
}

func runContextCommand(cmd *cobra.Command, args []string) error {
	cwd, err := getWorkingDir()
	if err != nil {
		return err
	}

	opts, err := parseContextInput(args)
	if err != nil {
		return err
	}

	sessionID, err := newAuditSessionID()
	if err != nil {
		return err
	}

	request := taskRequest{
		Command:     "context",
		CWD:         cwd,
		Profile:     strings.TrimSpace(opts.Profile),
		Mode:        router.ModeAuto,
		SessionID:   sessionID,
		Host:        strings.TrimSpace(opts.Host),
		Attachments: normalizeAttachments(opts.Attachments),
		Flags: map[string]any{
			"profile": strings.TrimSpace(opts.Profile),
		},
	}

	taskContext, _, err := buildTaskContextForRequest(request, nil)
	if err != nil {
		return err
	}

	compiled, err := contextcompiler.Compile(contextcompiler.Request{
		Host:        request.Host,
		Attachments: request.Attachments,
		TaskContext: taskContext,
	})
	if err != nil {
		return err
	}
	if strings.TrimSpace(opts.Format) == "json" {
		if err := writeJSON(cmd.OutOrStdout(), compiled); err != nil {
			return err
		}
		appendLedgerEvent(ledgerContextEvent(request.SessionID, compiled))
		return nil
	}

	appendLedgerEvent(ledgerContextEvent(request.SessionID, compiled))
	_, err = fmt.Fprintln(cmd.OutOrStdout(), formatTaskContext(taskContext))
	return err
}

func parseAskCommandArgs(args []string, cwd, sessionID string) (taskRequest, memory.AskInput, askCommandOptions, error) {
	opts := askCommandOptions{
		Kind:    memory.KindPattern,
		Mode:    router.ModeAuto,
		Profile: defaultProfile,
	}

	var promptParts []string
	for idx := 0; idx < len(args); idx++ {
		arg := args[idx]
		if !strings.HasPrefix(arg, "--") {
			promptParts = append(promptParts, arg)
			continue
		}

		if arg == "--debug" {
			opts.Debug = true
			continue
		}
		if arg == "--explain-context" {
			opts.ExplainContext = true
			continue
		}
		if strings.HasPrefix(arg, "--debug=") {
			debugValue := strings.TrimSpace(strings.TrimPrefix(arg, "--debug="))
			switch debugValue {
			case "1", "true", "yes", "on":
				opts.Debug = true
			case "0", "false", "no", "off":
				opts.Debug = false
			default:
				return taskRequest{}, memory.AskInput{}, askCommandOptions{}, fmt.Errorf("flag --debug inválida: %s", debugValue)
			}
			continue
		}

		name, value, consumesNext, err := parseFlagValue(arg)
		if err != nil {
			return taskRequest{}, memory.AskInput{}, askCommandOptions{}, err
		}
		if consumesNext {
			idx++
			if idx >= len(args) {
				return taskRequest{}, memory.AskInput{}, askCommandOptions{}, fmt.Errorf("flag %s requer valor", name)
			}
			value = args[idx]
		}

		switch name {
		case "--kind":
			opts.Kind = value
		case "--scope":
			opts.Scope = value
		case "--title":
			opts.Title = value
		case "--tags":
			opts.TagsCSV = value
		case "--mode":
			opts.Mode = value
		case "--host":
			opts.Host = value
		case "--profile":
			opts.Profile = value
		case "--attachment":
			opts.Attachments = append(opts.Attachments, value)
		case "--system-prompt":
			opts.SystemPrompt = value
		case "--explain-context":
			switch strings.ToLower(strings.TrimSpace(value)) {
			case "1", "true", "yes", "on":
				opts.ExplainContext = true
			case "0", "false", "no", "off":
				opts.ExplainContext = false
			default:
				return taskRequest{}, memory.AskInput{}, askCommandOptions{}, fmt.Errorf("flag --explain-context inválida: %s", value)
			}
		case "--temperature":
			temperature, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
			if err != nil {
				return taskRequest{}, memory.AskInput{}, askCommandOptions{}, fmt.Errorf("flag --temperature inválida: %w", err)
			}
			opts.Temperature = temperature
			opts.TemperatureSet = true
		default:
			return taskRequest{}, memory.AskInput{}, askCommandOptions{}, fmt.Errorf("flag inválida: %s", name)
		}
	}

	request, askInput, err := buildAskTaskRequest(promptParts, cwd, sessionID, opts)
	if err != nil {
		return taskRequest{}, memory.AskInput{}, askCommandOptions{}, err
	}
	return request, askInput, opts, nil
}

func buildAskTaskRequest(args []string, cwd, sessionID string, opts askCommandOptions) (taskRequest, memory.AskInput, error) {
	prompt := strings.TrimSpace(strings.Join(args, " "))
	if prompt == "" {
		return taskRequest{}, memory.AskInput{}, errors.New("nenhuma pergunta informada")
	}

	profile := strings.TrimSpace(opts.Profile)
	if profile == "" {
		profile = defaultProfile
	}

	scope := strings.TrimSpace(opts.Scope)
	if scope == "" {
		scope = memory.DefaultScopeFromCWD(cwd)
	}

	flags := map[string]any{
		"mode":         strings.TrimSpace(opts.Mode),
		"profile":      profile,
		"kind":         strings.TrimSpace(opts.Kind),
		"scope":        scope,
		"title":        strings.TrimSpace(opts.Title),
		"host":         strings.TrimSpace(opts.Host),
		"attachments":  normalizeAttachments(opts.Attachments),
		"debug":        opts.Debug,
		"systemPrompt": strings.TrimSpace(opts.SystemPrompt),
	}
	if tags := splitCSV(opts.TagsCSV); len(tags) > 0 {
		flags["tags"] = tags
	}
	if opts.TemperatureSet {
		flags["temperature"] = opts.Temperature
	}

	request := taskRequest{
		Command:     "ask",
		UserInput:   prompt,
		CWD:         cwd,
		Profile:     profile,
		Mode:        strings.TrimSpace(opts.Mode),
		SessionID:   sessionID,
		Host:        strings.TrimSpace(opts.Host),
		Attachments: normalizeAttachments(opts.Attachments),
		Flags:       flags,
	}

	return request, memory.AskInput{
		Prompt:    prompt,
		SessionID: sessionID,
		Kind:      strings.TrimSpace(opts.Kind),
		Scope:     scope,
		Title:     strings.TrimSpace(opts.Title),
		Tags:      splitCSV(opts.TagsCSV),
	}, nil
}

func buildTaskContextForRequest(request taskRequest, store *memory.Store) (contextcollector.TaskContext, []memory.Entry, error) {
	registry := newContextToolRegistry()
	taskContext, err := collectTaskContext(request.CWD, contextcollector.Session{
		LastCommand: formatLastCommand(request),
	}, contextcollector.CollectOptions{
		ToolRegistry: registry,
	})
	if err != nil {
		return contextcollector.TaskContext{}, nil, err
	}

	addRequestNotes(&taskContext, request)

	if store == nil || strings.TrimSpace(request.UserInput) == "" {
		return taskContext, nil, nil
	}

	memoryHits, err := store.Retrieve(request.UserInput, memory.RetrievalContext{
		WorkingDir: request.CWD,
		Host:       request.Host,
	})
	if err != nil {
		return contextcollector.TaskContext{}, nil, err
	}

	taskContext.MemoryHits = formatMemoryHits(memoryHits)
	return taskContext, memoryHits, nil
}

func writeBackendNotification(cmd *cobra.Command, decision router.Decision) error {
	if !decision.NotifyUser || decision.NotificationMessage == nil {
		return nil
	}

	_, err := fmt.Fprintln(cmd.OutOrStdout(), strings.TrimSpace(*decision.NotificationMessage))
	return err
}

func optionalTemperature(opts askCommandOptions) *float64 {
	if !opts.TemperatureSet {
		return nil
	}

	temperature := opts.Temperature
	return &temperature
}

func parseRecallInput(args []string, cwd string) (string, memory.RetrievalContext, error) {
	ctx := memory.RetrievalContext{WorkingDir: cwd}
	var queryParts []string

	for idx := 0; idx < len(args); idx++ {
		arg := args[idx]
		if !strings.HasPrefix(arg, "--") {
			queryParts = append(queryParts, arg)
			continue
		}

		name, value, consumesNext, err := parseFlagValue(arg)
		if err != nil {
			return "", memory.RetrievalContext{}, err
		}
		if consumesNext {
			idx++
			if idx >= len(args) {
				return "", memory.RetrievalContext{}, fmt.Errorf("flag %s requer valor", name)
			}
			value = args[idx]
		}

		switch name {
		case "--project":
			ctx.Project = value
		case "--host":
			ctx.Host = value
		default:
			return "", memory.RetrievalContext{}, fmt.Errorf("flag inválida: %s", name)
		}
	}

	if len(queryParts) == 0 {
		return "", memory.RetrievalContext{}, errors.New("nenhuma query informada")
	}

	return strings.Join(queryParts, " "), ctx, nil
}

func parseContextInput(args []string) (contextCommandOptions, error) {
	opts := contextCommandOptions{Profile: defaultProfile}

	for idx := 0; idx < len(args); idx++ {
		arg := args[idx]
		if arg == "--json" {
			opts.Format = "json"
			continue
		}
		if !strings.HasPrefix(arg, "--") {
			return contextCommandOptions{}, fmt.Errorf("argumento inválido: %s", arg)
		}

		name, value, consumesNext, err := parseFlagValue(arg)
		if err != nil {
			return contextCommandOptions{}, err
		}
		if consumesNext {
			idx++
			if idx >= len(args) {
				return contextCommandOptions{}, fmt.Errorf("flag %s requer valor", name)
			}
			value = args[idx]
		}

		switch name {
		case "--host":
			opts.Host = value
		case "--profile":
			opts.Profile = value
		case "--attachment":
			opts.Attachments = append(opts.Attachments, value)
		case "--format":
			switch strings.TrimSpace(value) {
			case "json", "text":
				opts.Format = strings.TrimSpace(value)
			default:
				return contextCommandOptions{}, fmt.Errorf("flag --format inválida: %s", value)
			}
		default:
			return contextCommandOptions{}, fmt.Errorf("flag inválida: %s", name)
		}
	}

	return opts, nil
}

func ledgerContextEvent(sessionID string, compiled contextcompiler.CompiledContext) ledger.Event {
	status := "ok"
	if compiled.Partial || compiled.Truncated {
		status = "partial"
	}
	data, _ := json.Marshal(map[string]any{
		"sections":       len(compiled.Sections),
		"token_estimate": compiled.TokenEstimate,
		"truncated":      compiled.Truncated,
		"partial":        compiled.Partial,
	})
	return ledger.Event{
		Type:      ledger.EventContextCollected,
		SessionID: sessionID,
		Command:   "context",
		Status:    status,
		Payload:   ledger.Payload{Message: string(data)},
	}
}

func newContextToolRegistry() *tools.Registry {
	registry := tools.NewRegistry()
	_ = registry.Register(tools.GitStatusTool())
	return registry
}

func addRequestNotes(taskContext *contextcollector.TaskContext, request taskRequest) {
	if taskContext == nil {
		return
	}

	if request.Host != "" {
		taskContext.Notes = append(taskContext.Notes, "[host solicitado] "+request.Host)
	}
	if request.Profile != "" {
		taskContext.Notes = append(taskContext.Notes, "[profile] "+request.Profile)
	}
	for _, attachment := range request.Attachments {
		taskContext.Notes = append(taskContext.Notes, "[attachment] "+attachment)
	}
}

func formatLastCommand(request taskRequest) string {
	parts := []string{"pocket", request.Command}
	if strings.TrimSpace(request.UserInput) != "" {
		parts = append(parts, request.UserInput)
	}
	return strings.Join(parts, " ")
}

func formatMemoryHits(entries []memory.Entry) []string {
	hits := make([]string, 0, len(entries))
	for _, entry := range entries {
		summary := strings.TrimSpace(entry.Summary)
		if summary == "" {
			summary = strings.TrimSpace(entry.Title)
		}

		hits = append(hits, fmt.Sprintf(
			"scope=%s confidence=%.1f title=%s summary=%s",
			entry.Scope,
			entry.Confidence,
			compactInline(entry.Title),
			compactInline(summary),
		))
	}
	return hits
}

func formatTaskContext(taskContext contextcollector.TaskContext) string {
	var lines []string

	lines = append(lines, "[TASK_CONTEXT]")
	if taskContext.Project.Path != "" {
		lines = append(lines, "project.path="+taskContext.Project.Path)
	}
	lines = append(lines, fmt.Sprintf("project.is_git=%t", taskContext.Project.IsGit))
	if taskContext.Project.Branch != nil && strings.TrimSpace(*taskContext.Project.Branch) != "" {
		lines = append(lines, "project.branch="+strings.TrimSpace(*taskContext.Project.Branch))
	}
	if taskContext.System.OS != "" {
		lines = append(lines, "system.os="+taskContext.System.OS)
	}
	if taskContext.Session.LastCommand != nil && strings.TrimSpace(*taskContext.Session.LastCommand) != "" {
		lines = append(lines, "session.last_command="+compactInline(*taskContext.Session.LastCommand))
	}
	if taskContext.Session.LastError != nil && strings.TrimSpace(*taskContext.Session.LastError) != "" {
		lines = append(lines, "session.last_error="+compactInline(*taskContext.Session.LastError))
	}

	lines = appendBlock(lines, "main_files", renderMainFiles(taskContext.Project.MainFiles))
	if taskContext.Project.ReadmeExcerpt != nil {
		lines = appendBlock(lines, "readme_excerpt", strings.TrimSpace(*taskContext.Project.ReadmeExcerpt))
	}
	if taskContext.Project.GitDiffHead != nil {
		lines = appendBlock(lines, "git_diff_head", strings.TrimSpace(*taskContext.Project.GitDiffHead))
	}
	if taskContext.Project.GitLog != nil {
		lines = appendBlock(lines, "git_log", strings.TrimSpace(*taskContext.Project.GitLog))
	}
	lines = appendBlock(lines, "memory_hits", strings.Join(taskContext.MemoryHits, "\n"))
	lines = appendBlock(lines, "tool_results", renderToolResults(taskContext.ToolResults))
	lines = appendBlock(lines, "notes", strings.Join(taskContext.Notes, "\n"))

	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func renderMainFiles(files []contextcollector.MainFile) string {
	if len(files) == 0 {
		return ""
	}

	lines := make([]string, 0, len(files)*2)
	for _, file := range files {
		lines = append(lines, "- "+file.Path)
		if excerpt := strings.TrimSpace(file.Excerpt); excerpt != "" {
			for _, line := range strings.Split(excerpt, "\n") {
				lines = append(lines, "  "+line)
			}
		}
	}
	return strings.Join(lines, "\n")
}

func renderToolResults(results []contextcollector.ToolResult) string {
	if len(results) == 0 {
		return ""
	}

	lines := make([]string, 0, len(results))
	for _, result := range results {
		status := "error"
		if result.OK {
			status = "ok"
		}

		line := fmt.Sprintf("- %s status=%s duration_ms=%d", result.ToolName, status, result.DurationMS)
		if summary := strings.TrimSpace(result.Summary); summary != "" {
			line += " summary=" + compactInline(summary)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func appendBlock(lines []string, title, body string) []string {
	body = strings.TrimSpace(body)
	if body == "" {
		return lines
	}

	lines = append(lines, title+":")
	for _, line := range strings.Split(body, "\n") {
		lines = append(lines, "  "+line)
	}
	return lines
}

func compactInline(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func normalizeAttachments(attachments []string) []string {
	if len(attachments) == 0 {
		return nil
	}

	normalized := make([]string, 0, len(attachments))
	seen := make(map[string]struct{}, len(attachments))
	for _, attachment := range attachments {
		trimmed := strings.TrimSpace(attachment)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		normalized = append(normalized, trimmed)
	}
	return normalized
}

func approximateTokenCount(value string) int {
	return len(strings.Fields(strings.TrimSpace(value)))
}

func defaultCommandRuntime() commandRuntime {
	return commandRuntime{
		LocalClient:  backendClientFromEnv("POCKETCLI_LOCAL_BACKEND"),
		RemoteClient: backendClientFromEnv("POCKETCLI_REMOTE_BACKEND"),
		LocalProbe:   probeForBackendEnv("POCKETCLI_LOCAL_BACKEND"),
		RemoteProbe:  probeForBackendEnv("POCKETCLI_REMOTE_BACKEND"),
		LocalModel:   strings.TrimSpace(os.Getenv("POCKETCLI_LOCAL_BACKEND_MODEL")),
		RemoteModel:  strings.TrimSpace(os.Getenv("POCKETCLI_REMOTE_BACKEND_MODEL")),
	}
}

func backendClientFromEnv(prefix string) backend.Client {
	response := strings.TrimSpace(os.Getenv(prefix + "_RESPONSE"))
	if response != "" {
		return staticBackendClient{
			content: response,
			model:   strings.TrimSpace(os.Getenv(prefix + "_MODEL")),
		}
	}

	command := strings.TrimSpace(os.Getenv(prefix + "_CMD"))
	if command != "" {
		return commandBackendClient{command: command}
	}

	return nil
}

func probeForBackendEnv(prefix string) router.Probe {
	if strings.TrimSpace(os.Getenv(prefix+"_RESPONSE")) != "" {
		return func(_ stdctx.Context, _ contextcollector.TaskContext) error { return nil }
	}
	if strings.TrimSpace(os.Getenv(prefix+"_CMD")) != "" {
		return func(_ stdctx.Context, _ contextcollector.TaskContext) error { return nil }
	}
	return nil
}
