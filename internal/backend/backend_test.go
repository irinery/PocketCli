package backend

import (
	stdctx "context"
	"errors"
	"strings"
	"testing"

	"pocketcli/internal/contextcollector"
	"pocketcli/internal/router"
)

type fakeClient struct {
	complete func(ctx stdctx.Context, request CompletionRequest) (CompletionResult, error)
}

func (f fakeClient) Complete(ctx stdctx.Context, request CompletionRequest) (CompletionResult, error) {
	return f.complete(ctx, request)
}

func TestBuildPrompt_B01PlacesLastErrorBeforeContextDataAndKeepsMemoryHitsWithinLimit(t *testing.T) {
	lastError := "panic: ssh timeout while opening tunnel"
	prompt := buildPrompt(contextcollector.TaskContext{
		Project: contextcollector.ProjectContext{
			Path: "/tmp/PocketCli",
			MainFiles: []contextcollector.MainFile{
				{Path: "cmd/pocket/main.go", Excerpt: "func main() {}"},
			},
		},
		Session: contextcollector.SessionContext{
			LastError: stringPtr(lastError),
		},
		System:     contextcollector.SystemContext{OS: "linux"},
		MemoryHits: []string{"reiniciar tailscaled antes do retry", "validar chave SSH antes do exec"},
	}, Mode{UserInput: "como destravar o acesso?"})

	if !strings.Contains(prompt, "last_error:\n"+lastError) {
		t.Fatalf("expected last_error in prompt, got %q", prompt)
	}
	if !strings.Contains(prompt, "memory_hits:\n- reiniciar tailscaled antes do retry\n- validar chave SSH antes do exec") {
		t.Fatalf("expected memory_hits in prompt, got %q", prompt)
	}
	if strings.Index(prompt, "last_error:\n"+lastError) > strings.Index(prompt, "project_path: /tmp/PocketCli") {
		t.Fatal("expected last_error before context data")
	}
	if approxTokens(prompt) > contextcollector.MaxContextTokens {
		t.Fatalf("expected prompt <= %d tokens, got %d", contextcollector.MaxContextTokens, approxTokens(prompt))
	}
}

func TestBuildPrompt_B02WarnsForMissingToolBeforeCollectedContextAndOutsideSystemPrompt(t *testing.T) {
	prompt := buildPrompt(contextcollector.TaskContext{
		Project: contextcollector.ProjectContext{
			Path: "/tmp/PocketCli",
		},
		ToolResults: []contextcollector.ToolResult{
			{ToolName: "git_status", OK: false},
		},
	}, Mode{UserInput: "resuma o estado"})

	warning := "[AVISO: tool git_status indisponível — dado ausente]"
	contextIdx := strings.Index(prompt, "[CONTEXTO]")
	warningIdx := strings.Index(prompt, warning)
	projectIdx := strings.Index(prompt, "project_path: /tmp/PocketCli")
	if contextIdx == -1 || warningIdx == -1 || projectIdx == -1 {
		t.Fatalf("expected context section, warning and project path, got %q", prompt)
	}
	if warningIdx < contextIdx {
		t.Fatal("expected warning inside context section")
	}
	if warningIdx > projectIdx {
		t.Fatal("expected warning before collected context")
	}

	systemSection := prompt[:contextIdx]
	if strings.Contains(systemSection, warning) {
		t.Fatal("expected warning to stay out of system prompt")
	}
}

func TestBuildPrompt_B03EmitsOneWarningPerMissingToolAtContextStart(t *testing.T) {
	prompt := buildPrompt(contextcollector.TaskContext{
		Project: contextcollector.ProjectContext{
			Path: "/tmp/PocketCli",
		},
		ToolResults: []contextcollector.ToolResult{
			{ToolName: "git_status", OK: false},
			{ToolName: "disk_usage", OK: false},
			{ToolName: "ssh_probe", OK: true, Summary: "reachable"},
		},
	}, Mode{UserInput: "o que aconteceu?"})

	warnings := []string{
		"[AVISO: tool git_status indisponível — dado ausente]",
		"[AVISO: tool disk_usage indisponível — dado ausente]",
	}
	projectIdx := strings.Index(prompt, "project_path: /tmp/PocketCli")
	for _, warning := range warnings {
		idx := strings.Index(prompt, warning)
		if idx == -1 {
			t.Fatalf("expected warning %q in prompt, got %q", warning, prompt)
		}
		if idx > projectIdx {
			t.Fatalf("expected warning %q before collected context", warning)
		}
	}
}

func TestCall_B04UsesLocalTimeoutAndReturnsTimeoutReason(t *testing.T) {
	var captured CompletionRequest
	response := Call(stdctx.Background(), Request{
		Backend: router.BackendLocal,
		Context: contextcollector.TaskContext{},
		Mode:    Mode{UserInput: "teste"},
		LocalClient: fakeClient{
			complete: func(ctx stdctx.Context, request CompletionRequest) (CompletionResult, error) {
				captured = request
				return CompletionResult{}, stdctx.DeadlineExceeded
			},
		},
	})

	if captured.Timeout != router.DefaultLocalTimeout {
		t.Fatalf("expected local timeout %s, got %s", router.DefaultLocalTimeout, captured.Timeout)
	}
	if response.Reason != "timeout" {
		t.Fatalf("expected timeout reason, got %q", response.Reason)
	}
	if response.FinishReason != FinishReasonError {
		t.Fatalf("expected finish_reason error, got %q", response.FinishReason)
	}
}

func TestCall_B05UsesRemoteTimeoutAndReturnsTimeoutReason(t *testing.T) {
	var captured CompletionRequest
	response := Call(stdctx.Background(), Request{
		Backend: router.BackendRemote,
		Context: contextcollector.TaskContext{},
		Mode:    Mode{UserInput: "teste"},
		RemoteClient: fakeClient{
			complete: func(ctx stdctx.Context, request CompletionRequest) (CompletionResult, error) {
				captured = request
				return CompletionResult{}, stdctx.DeadlineExceeded
			},
		},
	})

	if captured.Timeout != router.DefaultRemoteTimeout {
		t.Fatalf("expected remote timeout %s, got %s", router.DefaultRemoteTimeout, captured.Timeout)
	}
	if response.Reason != "timeout" {
		t.Fatalf("expected timeout reason, got %q", response.Reason)
	}
	if response.FinishReason != FinishReasonError {
		t.Fatalf("expected finish_reason error, got %q", response.FinishReason)
	}
}

func TestCall_B06BuildsStructuredResponseOnSuccess(t *testing.T) {
	response := Call(stdctx.Background(), Request{
		Backend: router.BackendLocal,
		Context: contextcollector.TaskContext{},
		Mode:    Mode{UserInput: "responda"},
		LocalClient: fakeClient{
			complete: func(ctx stdctx.Context, request CompletionRequest) (CompletionResult, error) {
				return CompletionResult{
					Model:        "llama3.1",
					Content:      "ação aplicada",
					TokenUsage:   321,
					FinishReason: FinishReasonStop,
				}, nil
			},
		},
		LocalModel: "llama3.1",
	})

	if response.Backend != router.BackendLocal {
		t.Fatalf("expected backend local, got %q", response.Backend)
	}
	if response.Model != "llama3.1" {
		t.Fatalf("expected model llama3.1, got %q", response.Model)
	}
	if response.Content != "ação aplicada" {
		t.Fatalf("expected content, got %q", response.Content)
	}
	if response.TokenUsage != 321 {
		t.Fatalf("expected token usage 321, got %d", response.TokenUsage)
	}
	if response.LatencyMS < 0 {
		t.Fatalf("expected non-negative latency, got %d", response.LatencyMS)
	}
	if response.FinishReason != FinishReasonStop {
		t.Fatalf("expected finish_reason stop, got %q", response.FinishReason)
	}
}

func TestCall_B07ReturnsReadableErrorWithoutPanicking(t *testing.T) {
	response := Call(stdctx.Background(), Request{
		Backend: router.BackendRemote,
		Context: contextcollector.TaskContext{},
		Mode:    Mode{UserInput: "responda"},
		RemoteClient: fakeClient{
			complete: func(ctx stdctx.Context, request CompletionRequest) (CompletionResult, error) {
				return CompletionResult{}, errors.New("HTTP 503 upstream unavailable")
			},
		},
	})

	if response.FinishReason != FinishReasonError {
		t.Fatalf("expected finish_reason error, got %q", response.FinishReason)
	}
	if !strings.Contains(response.Content, "HTTP 503 upstream unavailable") {
		t.Fatalf("expected readable error message, got %q", response.Content)
	}
}

func TestCall_B08UsesDefaultTemperatureWithoutOverride(t *testing.T) {
	var captured CompletionRequest
	response := Call(stdctx.Background(), Request{
		Backend: router.BackendLocal,
		Context: contextcollector.TaskContext{},
		Mode:    Mode{UserInput: "responda"},
		LocalClient: fakeClient{
			complete: func(ctx stdctx.Context, request CompletionRequest) (CompletionResult, error) {
				captured = request
				return CompletionResult{Content: "ok", FinishReason: FinishReasonStop}, nil
			},
		},
	})

	if response.FinishReason != FinishReasonStop {
		t.Fatalf("expected successful response, got %q", response.FinishReason)
	}
	if captured.Temperature != DefaultTemperature {
		t.Fatalf("expected default temperature %.1f, got %.1f", DefaultTemperature, captured.Temperature)
	}
}

func TestCall_B09IgnoresTemperatureOverrideOutsideDebug(t *testing.T) {
	temperature := 0.8
	var captured CompletionRequest
	response := Call(stdctx.Background(), Request{
		Backend: router.BackendLocal,
		Context: contextcollector.TaskContext{},
		Mode: Mode{
			UserInput:   "responda",
			Temperature: &temperature,
		},
		LocalClient: fakeClient{
			complete: func(ctx stdctx.Context, request CompletionRequest) (CompletionResult, error) {
				captured = request
				return CompletionResult{Content: "ok", FinishReason: FinishReasonStop}, nil
			},
		},
	})

	if response.FinishReason != FinishReasonStop {
		t.Fatalf("expected successful response, got %q", response.FinishReason)
	}
	if captured.Temperature != DefaultTemperature {
		t.Fatalf("expected override to be ignored and temperature %.1f, got %.1f", DefaultTemperature, captured.Temperature)
	}
}

func TestCall_B10AcceptsTemperatureOverrideInDebugMode(t *testing.T) {
	temperature := 0.8
	var captured CompletionRequest
	response := Call(stdctx.Background(), Request{
		Backend: router.BackendRemote,
		Context: contextcollector.TaskContext{},
		Mode: Mode{
			Debug:       true,
			UserInput:   "responda",
			Temperature: &temperature,
		},
		RemoteClient: fakeClient{
			complete: func(ctx stdctx.Context, request CompletionRequest) (CompletionResult, error) {
				captured = request
				return CompletionResult{Content: "ok", FinishReason: FinishReasonStop}, nil
			},
		},
	})

	if response.FinishReason != FinishReasonStop {
		t.Fatalf("expected successful response, got %q", response.FinishReason)
	}
	if captured.Temperature != 0.8 {
		t.Fatalf("expected debug override 0.8, got %.1f", captured.Temperature)
	}
}

func stringPtr(value string) *string {
	return &value
}
