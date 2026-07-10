package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"pocketcli/internal/connect"
	"pocketcli/internal/fleet"
	"pocketcli/internal/remoteaccess"
	"pocketcli/internal/safety"
)

func withArgs(t *testing.T, args []string) {
	t.Helper()
	orig := os.Args
	os.Args = append([]string(nil), args...)
	t.Cleanup(func() { os.Args = orig })
}

func testRemoteExecutor() *remoteaccess.Executor {
	executor := remoteaccess.NewExecutor()
	executor.Logger = nil
	executor.Resolver = func(ctx context.Context, alias string) (remoteaccess.RemoteHost, error) {
		return remoteaccess.RemoteHost{
			Alias:        alias,
			Hostname:     alias,
			AccessMethod: remoteaccess.AccessMethodSSH,
			OSFamily:     remoteaccess.OSFamilyLinux,
			SSHPort:      22,
			Enabled:      true,
		}, nil
	}
	executor.Probe = func(ctx context.Context, host remoteaccess.RemoteHost) error { return nil }
	return executor
}

func captureCommandOutput(t *testing.T, cmd *cobra.Command, args []string) (string, error) {
	t.Helper()
	origStdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe returned error: %v", err)
	}
	os.Stdout = writer
	defer func() { os.Stdout = origStdout }()

	runErr := cmd.RunE(cmd, args)
	if err := writer.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll returned error: %v", err)
	}
	return string(output), runErr
}

func TestRootCommand_NoSubcommandShowsHelpWithoutError(t *testing.T) {
	root := newRootCommand()
	withArgs(t, []string{"pocket"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
}

func TestSSHCommand_UsesConnectOrchestrator(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	orig := newConnectOrchestrator
	t.Cleanup(func() { newConnectOrchestrator = orig })

	expected := errors.New("boom")
	var gotHost string
	newConnectOrchestrator = func() *connect.Orchestrator {
		orchestrator := connect.New()
		orchestrator.LookupPath = func(name string) (string, error) { return "/bin/" + name, nil }
		orchestrator.ResolveHostFunc = func(ctx context.Context, host string) (connect.HostInfo, error) {
			gotHost = host
			return connect.HostInfo{}, expected
		}
		return orchestrator
	}

	cmd := newSSHCommand()
	err := cmd.RunE(cmd, []string{"x"})
	if !errors.Is(err, expected) {
		t.Fatalf("expected propagated error, got %v", err)
	}
	if gotHost != "x" {
		t.Fatalf("expected connect orchestrator to receive host x, got %q", gotHost)
	}
}

func TestExecCommand_JoinsCommandAndCallsRemoteExecutor(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	orig := newRemoteExecutor
	t.Cleanup(func() { newRemoteExecutor = orig })

	var gotHost, gotCmd string
	newRemoteExecutor = func() *remoteaccess.Executor {
		executor := testRemoteExecutor()
		executor.Runner = func(ctx context.Context, name string, args []string, options remoteaccess.RunOptions) (remoteaccess.RunOutput, error) {
			gotHost = args[len(args)-2]
			gotCmd = args[len(args)-1]
			return remoteaccess.RunOutput{ExitCode: 0}, nil
		}
		return executor
	}

	cmd := newExecCommand()
	if err := cmd.RunE(cmd, []string{"prod-api", "uname", "-a"}); err != nil {
		t.Fatalf("RunE returned error: %v", err)
	}
	if gotHost != "prod-api" || gotCmd != "uname -a" {
		t.Fatalf("unexpected call: host=%q cmd=%q", gotHost, gotCmd)
	}
}

func TestExecCommandPrepareCreatesEnvelopeWithoutCallingRemoteExecutor(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	orig := newRemoteExecutor
	t.Cleanup(func() { newRemoteExecutor = orig })
	newRemoteExecutor = func() *remoteaccess.Executor {
		t.Fatal("newRemoteExecutor should not be called during prepare")
		return nil
	}

	output, err := captureCommandOutput(t, newExecCommand(), []string{"--prepare", "prod-api", "sudo", "systemctl", "restart", "nginx"})
	if err != nil {
		t.Fatalf("RunE returned error: %v", err)
	}

	var result execEnvelopeResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if result.EnvelopeID == "" || result.Host != "prod-api" || !result.ApprovalRequired {
		t.Fatalf("unexpected prepare result: %#v", result)
	}
	envelope, err := safety.LoadEnvelope(result.EnvelopeID)
	if err != nil {
		t.Fatalf("LoadEnvelope returned error: %v", err)
	}
	if envelope.Request.Host != "prod-api" || strings.Join(envelope.Request.Command, " ") != "sudo systemctl restart nginx" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestExecCommandRunsPreparedEnvelopeWithApproval(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	envelope, err := createExecEnvelope("prod-api", []string{"sudo", "systemctl", "restart", "nginx"})
	if err != nil {
		t.Fatalf("createExecEnvelope returned error: %v", err)
	}
	token, err := safety.Approve(envelope.EnvelopeID, safety.DefaultApprovalTTL, true)
	if err != nil {
		t.Fatalf("Approve returned error: %v", err)
	}

	orig := newRemoteExecutor
	t.Cleanup(func() { newRemoteExecutor = orig })

	var gotHost, gotCmd string
	newRemoteExecutor = func() *remoteaccess.Executor {
		executor := testRemoteExecutor()
		executor.Runner = func(ctx context.Context, name string, args []string, options remoteaccess.RunOptions) (remoteaccess.RunOutput, error) {
			gotHost = args[len(args)-2]
			gotCmd = args[len(args)-1]
			return remoteaccess.RunOutput{ExitCode: 0}, nil
		}
		return executor
	}

	cmd := newExecCommand()
	if err := cmd.RunE(cmd, []string{"--envelope-id", envelope.EnvelopeID, "--approval-token", token.ApprovalToken}); err != nil {
		t.Fatalf("RunE returned error: %v", err)
	}
	if gotHost != "prod-api" || gotCmd != "sudo systemctl restart nginx" {
		t.Fatalf("unexpected call: host=%q cmd=%q", gotHost, gotCmd)
	}
}

func TestExecCommandRejectsExtraArgsWithEnvelope(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cmd := newExecCommand()
	err := cmd.RunE(cmd, []string{"--envelope-id", "abc123", "prod-api", "uptime"})
	if err == nil || !strings.Contains(err.Error(), "não passe host/comando junto") {
		t.Fatalf("expected envelope mutation error, got %v", err)
	}
}

func TestParseExecArgsParsesSupportedFlags(t *testing.T) {
	parsed, err := parseExecArgs([]string{
		"--json",
		"--timeout", "45",
		"--requested-by=llm_plan",
		"--session-id", "550e8400-e29b-41d4-a716-446655440000",
		"--approve",
		"prod-api",
		"systemctl", "restart", "nginx",
	})
	if err != nil {
		t.Fatalf("parseExecArgs returned error: %v", err)
	}

	if !parsed.jsonOutput || !parsed.approved {
		t.Fatalf("expected jsonOutput and approved flags")
	}
	if parsed.timeoutSeconds != 45 {
		t.Fatalf("timeoutSeconds = %d, want 45", parsed.timeoutSeconds)
	}
	if parsed.requestedBy != remoteaccess.RequestedByLLMPlan {
		t.Fatalf("requestedBy = %q, want llm_plan", parsed.requestedBy)
	}
	if parsed.host != "prod-api" || parsed.command != "systemctl restart nginx" {
		t.Fatalf("unexpected host/command: %q %q", parsed.host, parsed.command)
	}
}

func TestParseApproveArgsAcceptsDurationBeforeEnvelope(t *testing.T) {
	envelopeID, duration, err := parseApproveArgs([]string{"--duration-seconds", "60", "env-123"})
	if err != nil {
		t.Fatalf("parseApproveArgs returned error: %v", err)
	}
	if envelopeID != "env-123" || duration != 60 {
		t.Fatalf("unexpected parsed args: envelope=%q duration=%d", envelopeID, duration)
	}
}

func TestExecCommand_BlockedCommandReturnsErrorBeforeSSH(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	orig := newRemoteExecutor
	t.Cleanup(func() { newRemoteExecutor = orig })

	called := false
	newRemoteExecutor = func() *remoteaccess.Executor {
		executor := testRemoteExecutor()
		executor.Runner = func(ctx context.Context, name string, args []string, options remoteaccess.RunOptions) (remoteaccess.RunOutput, error) {
			called = true
			return remoteaccess.RunOutput{}, nil
		}
		return executor
	}

	cmd := newExecCommand()
	err := cmd.RunE(cmd, []string{"prod-api", "id"})
	if err == nil || !strings.Contains(err.Error(), "not_in_allowlist") {
		t.Fatalf("expected not_in_allowlist error, got %v", err)
	}
	if called {
		t.Fatal("runner was called for blocked command")
	}
}

func TestExecCommand_JsonOutputIncludesStatus(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	orig := newRemoteExecutor
	t.Cleanup(func() { newRemoteExecutor = orig })

	newRemoteExecutor = func() *remoteaccess.Executor {
		executor := testRemoteExecutor()
		executor.Runner = func(ctx context.Context, name string, args []string, options remoteaccess.RunOptions) (remoteaccess.RunOutput, error) {
			return remoteaccess.RunOutput{Stdout: "ok\n", ExitCode: 0}, nil
		}
		return executor
	}

	output, err := captureCommandOutput(t, newExecCommand(), []string{"--json", "prod-api", "uptime"})
	if err != nil {
		t.Fatalf("RunE returned error: %v", err)
	}
	if !strings.Contains(output, `"status":"success"`) || !strings.Contains(output, `"stdout":"ok\n"`) {
		t.Fatalf("unexpected json output: %s", output)
	}
}

type recordingRemoteAuditLogger struct {
	prepareCalls int
	writeCalls   int
}

func (l *recordingRemoteAuditLogger) Prepare() error {
	l.prepareCalls++
	return nil
}

func (l *recordingRemoteAuditLogger) Write(remoteaccess.RemoteCommandResult) error {
	l.writeCalls++
	return nil
}

func TestFleetExecUsesRemoteExecutorAndAudit(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	origExecutor := newRemoteExecutor
	t.Cleanup(func() {
		newRemoteExecutor = origExecutor
	})

	audit := &recordingRemoteAuditLogger{}
	var gotName string
	var gotArgs []string
	newRemoteExecutor = func() *remoteaccess.Executor {
		executor := remoteaccess.NewExecutor()
		executor.Logger = audit
		executor.Probe = func(ctx context.Context, host remoteaccess.RemoteHost) error { return nil }
		executor.Runner = func(ctx context.Context, name string, args []string, options remoteaccess.RunOptions) (remoteaccess.RunOutput, error) {
			gotName = name
			gotArgs = append([]string(nil), args...)
			return remoteaccess.RunOutput{Stdout: "up\n", ExitCode: 0}, nil
		}
		return executor
	}

	result := runFleetPlan(fleet.Plan{
		PlanID:      "plan-1",
		Command:     []string{"uptime"},
		MaxParallel: 1,
		Targets: []fleet.Target{{
			HostID:   "host-a",
			Hostname: "srv-a",
			Address:  "100.64.0.10",
		}},
	})

	if gotName != "ssh" || len(gotArgs) == 0 || gotArgs[len(gotArgs)-2] != "100.64.0.10" || gotArgs[len(gotArgs)-1] != "uptime" {
		t.Fatalf("unexpected remote runner call: name=%q args=%v", gotName, gotArgs)
	}
	if audit.prepareCalls != 1 || audit.writeCalls != 1 {
		t.Fatalf("expected remote audit logger to be used, got prepare=%d writes=%d", audit.prepareCalls, audit.writeCalls)
	}
	if len(result.Results) != 1 || result.Results[0].Status != "ok" || result.Results[0].OutputPreview != "up\n" {
		t.Fatalf("unexpected fleet result: %#v", result.Results)
	}
	if result.Results[0].CommandID == "" || result.Results[0].PolicyDecision == nil {
		t.Fatalf("expected command_id and policy_decision in fleet result: %#v", result.Results[0])
	}
}

func TestFleetExecBlocksUnsafeCommandBeforeRunner(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	origExecutor := newRemoteExecutor
	t.Cleanup(func() { newRemoteExecutor = origExecutor })

	runnerCalled := false
	newRemoteExecutor = func() *remoteaccess.Executor {
		executor := remoteaccess.NewExecutor()
		executor.Logger = nil
		executor.Probe = func(ctx context.Context, host remoteaccess.RemoteHost) error { return nil }
		executor.Runner = func(ctx context.Context, name string, args []string, options remoteaccess.RunOptions) (remoteaccess.RunOutput, error) {
			runnerCalled = true
			return remoteaccess.RunOutput{ExitCode: 0}, nil
		}
		return executor
	}

	result := runFleetPlan(fleet.Plan{
		PlanID:      "plan-1",
		Command:     []string{"id"},
		MaxParallel: 1,
		Targets:     []fleet.Target{{HostID: "host-a", Hostname: "srv-a"}},
	})

	if runnerCalled {
		t.Fatal("runner was called for command blocked by remote policy")
	}
	if len(result.Results) != 1 || result.Results[0].Status != "blocked" || !strings.Contains(result.Results[0].StderrPreview, "not_in_allowlist") {
		t.Fatalf("unexpected blocked fleet result: %#v", result.Results)
	}
}

func TestFleetExecUsesConfiguredRemoteTransport(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configDir := filepath.Join(home, ".config", "pocketcli")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	hostsJSON := `[{"alias":"host-a","hostname":"tail-host","access_method":"tailscale_ssh","enabled":true}]`
	if err := os.WriteFile(filepath.Join(configDir, "remote-hosts.json"), []byte(hostsJSON), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	origExecutor := newRemoteExecutor
	t.Cleanup(func() { newRemoteExecutor = origExecutor })

	var gotName string
	var gotArgs []string
	newRemoteExecutor = func() *remoteaccess.Executor {
		executor := remoteaccess.NewExecutor()
		executor.Logger = nil
		executor.Probe = func(ctx context.Context, host remoteaccess.RemoteHost) error { return nil }
		executor.Runner = func(ctx context.Context, name string, args []string, options remoteaccess.RunOptions) (remoteaccess.RunOutput, error) {
			gotName = name
			gotArgs = append([]string(nil), args...)
			return remoteaccess.RunOutput{ExitCode: 0}, nil
		}
		return executor
	}

	result := runFleetPlan(fleet.Plan{
		PlanID:      "plan-1",
		Command:     []string{"uptime"},
		MaxParallel: 1,
		Targets:     []fleet.Target{{HostID: "host-a", Hostname: "srv-a"}},
	})

	if gotName != "tailscale" || len(gotArgs) != 3 || gotArgs[0] != "ssh" || gotArgs[1] != "tail-host" || gotArgs[2] != "uptime" {
		t.Fatalf("expected tailscale ssh transport, got name=%q args=%v", gotName, gotArgs)
	}
	if len(result.Results) != 1 || result.Results[0].Status != "ok" {
		t.Fatalf("unexpected fleet result: %#v", result.Results)
	}
}

func TestFleetExecFailsClosedWhenConfiguredHostsFileIsInvalid(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configDir := filepath.Join(home, ".config", "pocketcli")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "remote-hosts.json"), []byte(`{"hosts":`), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	origExecutor := newRemoteExecutor
	t.Cleanup(func() { newRemoteExecutor = origExecutor })

	runnerCalled := false
	newRemoteExecutor = func() *remoteaccess.Executor {
		executor := remoteaccess.NewExecutor()
		executor.Logger = nil
		executor.Probe = func(ctx context.Context, host remoteaccess.RemoteHost) error { return nil }
		executor.Runner = func(ctx context.Context, name string, args []string, options remoteaccess.RunOptions) (remoteaccess.RunOutput, error) {
			runnerCalled = true
			return remoteaccess.RunOutput{ExitCode: 0}, nil
		}
		return executor
	}

	result := runFleetPlan(fleet.Plan{
		PlanID:      "plan-1",
		Command:     []string{"uptime"},
		MaxParallel: 1,
		Targets: []fleet.Target{{
			HostID:   "host-a",
			Hostname: "srv-a",
			Address:  "100.64.0.10",
		}},
	})

	if runnerCalled {
		t.Fatal("runner was called after invalid remote-hosts.json")
	}
	if len(result.Results) != 1 || result.Results[0].Status != "host_unreachable" || result.Results[0].RemoteStatus != "host_unreachable" {
		t.Fatalf("expected fail-closed host_unreachable result, got %#v", result.Results)
	}
	if result.Results[0].StderrPreview != "host_unreachable" {
		t.Fatalf("expected sanitized host_unreachable stderr, got %#v", result.Results[0])
	}
}

func TestParseAskInputDefaultsScopeFromWorkingDir(t *testing.T) {
	input, err := parseAskInput([]string{"como", "persistir", "jsonl"}, "/tmp/PocketCli")
	if err != nil {
		t.Fatalf("parseAskInput returned error: %v", err)
	}

	if input.Scope != "project:pocketcli" {
		t.Fatalf("expected default project scope, got %q", input.Scope)
	}
	if input.Kind != "pattern" {
		t.Fatalf("expected default kind pattern, got %q", input.Kind)
	}
	if input.Prompt != "como persistir jsonl" {
		t.Fatalf("unexpected prompt: %q", input.Prompt)
	}
}

func TestParseAskInputParsesInlineFlags(t *testing.T) {
	input, err := parseAskInput([]string{"--kind=decision", "--scope=host:Mac Mini", "--title=Writer", "--tags=memory,writer", "salvar", "memória"}, filepath.Clean("/"))
	if err != nil {
		t.Fatalf("parseAskInput returned error: %v", err)
	}

	if input.Kind != "decision" {
		t.Fatalf("unexpected kind: %q", input.Kind)
	}
	if input.Scope != "host:Mac Mini" {
		t.Fatalf("unexpected scope: %q", input.Scope)
	}
	if input.Title != "Writer" {
		t.Fatalf("unexpected title: %q", input.Title)
	}
	if len(input.Tags) != 2 || input.Tags[0] != "memory" || input.Tags[1] != "writer" {
		t.Fatalf("unexpected tags: %#v", input.Tags)
	}
}
