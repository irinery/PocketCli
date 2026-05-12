package connect

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestT0201ConnectCreatesSessionAndAttaches(t *testing.T) {
	runner := newFakeRunner()
	runner.handle("tmux", []string{"has-session", "-t", "pocket_devcenter"}, "", fakeProcessExitError{code: 1})
	runner.handlePrefix("tmux", []string{"new-session", "-d", "-s", "pocket_devcenter"}, "", nil)
	runner.handle("tmux", []string{"attach-session", "-t", "pocket_devcenter"}, "", nil)

	orchestrator := New()
	orchestrator.In = strings.NewReader("y\n")
	orchestrator.Out = &bytes.Buffer{}
	orchestrator.Err = &bytes.Buffer{}
	orchestrator.ResolveHostFunc = func(ctx context.Context, host string) (HostInfo, error) {
		return HostInfo{
			Name:      host,
			IP:        "100.64.0.10",
			Online:    true,
			Reachable: true,
			Trust:     TrustObserved,
			Source:    SourceTailscale,
			Action:    ActionInteractive,
		}, nil
	}
	orchestrator.RunCommand = runner.run
	orchestrator.Executable = func() (string, error) { return "/tmp/pocket", nil }
	orchestrator.LookupPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }

	err := orchestrator.Connect(context.Background(), "devcenter")
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	calls := runner.calls()
	if len(calls) != 3 {
		t.Fatalf("len(calls) = %d, want 3", len(calls))
	}
	if calls[1].name != "tmux" || !strings.HasPrefix(strings.Join(calls[1].args, " "), "new-session -d -s pocket_devcenter") {
		t.Fatalf("new-session call = %#v", calls[1])
	}
	if !strings.Contains(strings.Join(calls[1].args, " "), "__connect-pane") {
		t.Fatalf("expected pane helper in new-session command, got %q", strings.Join(calls[1].args, " "))
	}
}

func TestT0202ConnectReusesExistingSession(t *testing.T) {
	runner := newFakeRunner()
	runner.handle("tmux", []string{"has-session", "-t", "pocket_devcenter"}, "", nil)
	runner.handle("tmux", []string{"attach-session", "-t", "pocket_devcenter"}, "", nil)

	orchestrator := New()
	orchestrator.In = strings.NewReader("y\n")
	orchestrator.Out = &bytes.Buffer{}
	orchestrator.Err = &bytes.Buffer{}
	orchestrator.ResolveHostFunc = func(ctx context.Context, host string) (HostInfo, error) {
		return HostInfo{Name: host, IP: "100.64.0.10", Online: true, Reachable: true, Trust: TrustObserved, Source: SourceTailscale, Action: ActionInteractive}, nil
	}
	orchestrator.RunCommand = runner.run
	orchestrator.LookupPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }

	err := orchestrator.Connect(context.Background(), "devcenter")
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	calls := runner.calls()
	if len(calls) != 2 {
		t.Fatalf("len(calls) = %d, want 2", len(calls))
	}
	for _, call := range calls {
		if call.name == "tmux" && len(call.args) > 0 && call.args[0] == "new-session" {
			t.Fatalf("did not expect new-session call, got %#v", call)
		}
	}
}

func TestT0203ResolveHostNotFoundReturnsExit3(t *testing.T) {
	runner := newFakeRunner()
	runner.handle("tailscale", []string{"status", "--json"}, `{"Peer":{"peer1":{"HostName":"outro","Online":true,"OS":"linux","TailscaleIPs":["100.64.0.10"]}}}`, nil)

	orchestrator := New()
	orchestrator.RunCommand = runner.run
	orchestrator.LookupPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }

	_, err := orchestrator.resolveHost(context.Background(), "fantasma")
	assertExitError(t, err, ExitCodeMissingRuntime, "pocket: host 'fantasma' não encontrado no Tailscale")
}

func TestT0204ResolveHostOfflineReturnsExit1(t *testing.T) {
	runner := newFakeRunner()
	runner.handle("tailscale", []string{"status", "--json"}, `{"Peer":{"peer1":{"HostName":"devcenter","Online":false,"OS":"linux","TailscaleIPs":["100.64.0.10"]}}}`, nil)

	orchestrator := New()
	orchestrator.RunCommand = runner.run
	orchestrator.LookupPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }

	_, err := orchestrator.resolveHost(context.Background(), "devcenter")
	assertExitError(t, err, ExitCodeFailure, "pocket: host 'devcenter' está offline (Tailscale)")
}

func TestT0205ConnectApprovalDeniedPrintsCancellation(t *testing.T) {
	out := &bytes.Buffer{}
	runner := newFakeRunner()

	orchestrator := New()
	orchestrator.In = strings.NewReader("n\n")
	orchestrator.Out = out
	orchestrator.Err = &bytes.Buffer{}
	orchestrator.ResolveHostFunc = func(ctx context.Context, host string) (HostInfo, error) {
		return HostInfo{Name: host, IP: "100.64.0.10", Online: true, Reachable: true, Trust: TrustObserved, Source: SourceTailscale, Action: ActionInteractive}, nil
	}
	orchestrator.RunCommand = runner.run
	orchestrator.LookupPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }

	err := orchestrator.Connect(context.Background(), "devcenter")
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if !strings.Contains(out.String(), "Sessão cancelada.") {
		t.Fatalf("stdout = %q, want cancellation message", out.String())
	}
	if got := len(runner.calls()); got != 0 {
		t.Fatalf("len(calls) = %d, want 0", got)
	}
}

func TestT0206ConnectWithoutTmuxReturnsExit3(t *testing.T) {
	orchestrator := New()
	orchestrator.LookupPath = func(name string) (string, error) {
		if name == "tmux" {
			return "", errors.New("missing")
		}
		return "/usr/bin/" + name, nil
	}

	err := orchestrator.Connect(context.Background(), "devcenter")
	assertExitError(t, err, ExitCodeMissingRuntime, "pocket: tmux não encontrado. Instale tmux para continuar.")
}

func TestT0209ConnectRejectsHostWithSpecialCharacters(t *testing.T) {
	orchestrator := New()
	err := orchestrator.Connect(context.Background(), "host; rm -rf ~")
	assertExitError(t, err, ExitCodeInvalidInput, "pocket: nome de host inválido")
}

func TestT0210ConnectRejectsHostWithTraversal(t *testing.T) {
	orchestrator := New()
	err := orchestrator.Connect(context.Background(), "../../etc/passwd")
	assertExitError(t, err, ExitCodeInvalidInput, "pocket: nome de host inválido")
}

func TestT0211ApprovalCtrlCStopsWithExit1(t *testing.T) {
	errOut := &bytes.Buffer{}
	signalCh := make(chan os.Signal, 1)

	orchestrator := New()
	reader, _, pipeErr := os.Pipe()
	if pipeErr != nil {
		t.Fatalf("Pipe() error = %v", pipeErr)
	}
	defer reader.Close()

	orchestrator.In = reader
	orchestrator.Out = &bytes.Buffer{}
	orchestrator.Err = errOut
	orchestrator.Signals = signalCh
	orchestrator.ResolveHostFunc = func(ctx context.Context, host string) (HostInfo, error) {
		return HostInfo{Name: host, IP: "100.64.0.10", Online: true, Reachable: true, Trust: TrustObserved, Source: SourceTailscale, Action: ActionInteractive}, nil
	}
	orchestrator.LookupPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }

	done := make(chan error, 1)
	go func() {
		done <- orchestrator.Connect(context.Background(), "devcenter")
	}()

	signalCh <- syscall.SIGINT

	err := waitConnectResult(t, done)
	assertExitError(t, err, ExitCodeFailure, "Sessão cancelada.")
	if !strings.Contains(errOut.String(), "Sessão cancelada.") {
		t.Fatalf("stderr = %q, want cancellation message", errOut.String())
	}
}

func TestT0212ApprovalTimeoutStopsWithExit1(t *testing.T) {
	errOut := &bytes.Buffer{}

	orchestrator := New()
	reader, _, pipeErr := os.Pipe()
	if pipeErr != nil {
		t.Fatalf("Pipe() error = %v", pipeErr)
	}
	defer reader.Close()

	orchestrator.In = reader
	orchestrator.Out = &bytes.Buffer{}
	orchestrator.Err = errOut
	orchestrator.ApprovalTimeout = 10 * time.Millisecond
	orchestrator.ResolveHostFunc = func(ctx context.Context, host string) (HostInfo, error) {
		return HostInfo{Name: host, IP: "100.64.0.10", Online: true, Reachable: true, Trust: TrustObserved, Source: SourceTailscale, Action: ActionInteractive}, nil
	}
	orchestrator.LookupPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }

	err := orchestrator.Connect(context.Background(), "devcenter")
	assertExitError(t, err, ExitCodeFailure, "Aprovação expirada. Sessão cancelada.")
	if !strings.Contains(errOut.String(), "Aprovação expirada. Sessão cancelada.") {
		t.Fatalf("stderr = %q, want timeout message", errOut.String())
	}
}

func TestRunPaneLogsAndCleansSessionOnNormalExit(t *testing.T) {
	homeDir := t.TempDir()
	runner := newFakeRunner()
	runner.handlePrefix("ssh", []string{"-o", "ConnectTimeout=10", "-o", "StrictHostKeyChecking=accept-new", "100.64.0.10"}, "", nil)
	runner.handle("tmux", []string{"list-panes", "-t", "pocket_devcenter"}, "%1\n", nil)
	runner.handle("tmux", []string{"kill-session", "-t", "pocket_devcenter"}, "", nil)

	orchestrator := New()
	orchestrator.RunCommand = runner.run
	orchestrator.HomeDir = func() (string, error) { return homeDir, nil }
	orchestrator.Out = io.Discard
	orchestrator.Err = &bytes.Buffer{}
	orchestrator.Now = fixedClock(time.Date(2026, 4, 10, 19, 43, 0, 0, time.UTC), time.Date(2026, 4, 10, 20, 1, 15, 0, time.UTC))

	err := orchestrator.RunPane(context.Background(), PaneRequest{
		SessionName: "pocket_devcenter",
		Host:        "devcenter",
		IP:          "100.64.0.10",
		StartedAt:   time.Date(2026, 4, 10, 19, 43, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("RunPane() error = %v", err)
	}

	data, readErr := os.ReadFile(filepath.Join(homeDir, ".pocketcli", "logs", "sessions.log"))
	if readErr != nil {
		t.Fatalf("ReadFile() error = %v", readErr)
	}
	text := string(data)
	if !strings.Contains(text, `"event":"connect"`) {
		t.Fatalf("log = %q, want connect event", text)
	}
	if !strings.Contains(text, `"event":"disconnect"`) || !strings.Contains(text, `"exit_cause":"user_exit"`) {
		t.Fatalf("log = %q, want disconnect user_exit", text)
	}
}

func TestRunPaneKeepsSessionOpenAfterSSHError(t *testing.T) {
	homeDir := t.TempDir()
	runner := newFakeRunner()
	runner.handlePrefix("ssh", []string{"-o", "ConnectTimeout=10", "-o", "StrictHostKeyChecking=accept-new", "100.64.0.10"}, "", errors.New("ssh failed"))
	runner.handle("/bin/sh", nil, "", nil)

	errOut := &bytes.Buffer{}

	orchestrator := New()
	orchestrator.RunCommand = runner.run
	orchestrator.HomeDir = func() (string, error) { return homeDir, nil }
	orchestrator.Out = io.Discard
	orchestrator.Err = errOut
	orchestrator.Now = fixedClock(time.Date(2026, 4, 10, 19, 43, 0, 0, time.UTC), time.Date(2026, 4, 10, 19, 43, 5, 0, time.UTC))

	origShell := os.Getenv("SHELL")
	t.Setenv("SHELL", "/bin/sh")
	t.Cleanup(func() { _ = os.Setenv("SHELL", origShell) })

	err := orchestrator.RunPane(context.Background(), PaneRequest{
		SessionName: "pocket_devcenter",
		Host:        "devcenter",
		IP:          "100.64.0.10",
		StartedAt:   time.Date(2026, 4, 10, 19, 43, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("RunPane() error = %v", err)
	}

	if !strings.Contains(errOut.String(), "SSH falhou; sessão tmux mantida para inspeção.") {
		t.Fatalf("stderr = %q, want inspection message", errOut.String())
	}

	data, readErr := os.ReadFile(filepath.Join(homeDir, ".pocketcli", "logs", "sessions.log"))
	if readErr != nil {
		t.Fatalf("ReadFile() error = %v", readErr)
	}
	if !strings.Contains(string(data), `"exit_cause":"ssh_error"`) {
		t.Fatalf("log = %q, want ssh_error", string(data))
	}

	for _, call := range runner.calls() {
		if call.name == "tmux" && len(call.args) > 0 && call.args[0] == "kill-session" {
			t.Fatalf("did not expect kill-session after ssh error, got %#v", call)
		}
	}
}

type fakeProcessExitError struct {
	code int
}

func (e fakeProcessExitError) Error() string {
	return "process exited"
}

func (e fakeProcessExitError) ExitCode() int {
	return e.code
}

type fakeRunner struct {
	mu       sync.Mutex
	handlers []fakeHandler
	history  []commandCall
}

type fakeHandler struct {
	name      string
	args      []string
	matchType string
	output    string
	err       error
}

type commandCall struct {
	name string
	args []string
}

func newFakeRunner() *fakeRunner {
	return &fakeRunner{}
}

func (r *fakeRunner) handle(name string, args []string, output string, err error) {
	r.handlers = append(r.handlers, fakeHandler{name: name, args: append([]string(nil), args...), matchType: "exact", output: output, err: err})
}

func (r *fakeRunner) handlePrefix(name string, args []string, output string, err error) {
	r.handlers = append(r.handlers, fakeHandler{name: name, args: append([]string(nil), args...), matchType: "prefix", output: output, err: err})
}

func (r *fakeRunner) run(ctx context.Context, name string, args []string, options CommandOptions) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.history = append(r.history, commandCall{name: name, args: append([]string(nil), args...)})
	for _, handler := range r.handlers {
		if handler.name != name {
			continue
		}
		if handler.matchType == "exact" && slicesEqual(handler.args, args) {
			return handler.output, handler.err
		}
		if handler.matchType == "prefix" && slicesHavePrefix(args, handler.args) {
			return handler.output, handler.err
		}
	}

	return "", nil
}

func (r *fakeRunner) calls() []commandCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]commandCall(nil), r.history...)
}

func slicesEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func slicesHavePrefix(values, prefix []string) bool {
	if len(prefix) > len(values) {
		return false
	}
	for i := range prefix {
		if values[i] != prefix[i] {
			return false
		}
	}
	return true
}

func assertExitError(t *testing.T, err error, code int, message string) {
	t.Helper()

	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("error = %v, want *ExitError", err)
	}
	if exitErr.ExitCode() != code {
		t.Fatalf("ExitCode() = %d, want %d", exitErr.ExitCode(), code)
	}
	if exitErr.Error() != message {
		t.Fatalf("Error() = %q, want %q", exitErr.Error(), message)
	}
}

func waitConnectResult(t *testing.T, done <-chan error) error {
	t.Helper()

	select {
	case err := <-done:
		return err
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Connect() did not finish within 200ms")
		return nil
	}
}

func fixedClock(times ...time.Time) func() time.Time {
	index := 0
	return func() time.Time {
		if len(times) == 0 {
			return time.Time{}
		}
		if index >= len(times) {
			return times[len(times)-1]
		}
		current := times[index]
		index++
		return current
	}
}
