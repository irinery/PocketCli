package remoteaccess

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRA001ExecutesAllowedCommandViaSSH(t *testing.T) {
	runner := &recordingRunner{output: RunOutput{Stdout: "up 10 days\n", ExitCode: 0}}
	executor := testExecutor(runner)

	result, err := executor.Execute(context.Background(), RemoteCommandRequest{
		SessionID:      validSessionID,
		HostAlias:      "dev",
		Command:        "uptime",
		TimeoutSeconds: 30,
		RequestedBy:    RequestedByHuman,
	}, ExecuteOptions{})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if result.Status != StatusSuccess {
		t.Fatalf("status = %q, want success", result.Status)
	}
	if result.Stdout != "up 10 days\n" || result.Stderr != "" {
		t.Fatalf("unexpected output stdout=%q stderr=%q", result.Stdout, result.Stderr)
	}
	if result.ExitCode == nil || *result.ExitCode != 0 {
		t.Fatalf("exit_code = %v, want 0", result.ExitCode)
	}
	if result.DurationMS <= 0 {
		t.Fatalf("duration_ms = %d, want > 0", result.DurationMS)
	}
	if len(runner.calls) != 1 || runner.calls[0].name != "ssh" || !strings.Contains(strings.Join(runner.calls[0].args, " "), "uptime") {
		t.Fatalf("unexpected runner calls: %#v", runner.calls)
	}
}

func TestRA002HostUnreachableRetriesThreeTimes(t *testing.T) {
	probes := 0
	executor := testExecutor(&recordingRunner{})
	executor.Probe = func(ctx context.Context, host RemoteHost) error {
		probes++
		return errors.New("unreachable")
	}

	result, err := executor.Execute(context.Background(), RemoteCommandRequest{
		HostAlias:   "dev",
		Command:     "uptime",
		RequestedBy: RequestedByHuman,
	}, ExecuteOptions{})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if result.Status != StatusHostUnreachable {
		t.Fatalf("status = %q, want host_unreachable", result.Status)
	}
	if probes != 3 {
		t.Fatalf("probe attempts = %d, want 3", probes)
	}
}

func TestRA003TimeoutReturnsTimeoutWithoutExitCode(t *testing.T) {
	executor := testExecutor(&recordingRunner{
		err: context.DeadlineExceeded,
	})
	executor.Now = fixedRemoteClock(
		time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC),
		time.Date(2026, 5, 5, 12, 0, 30, 0, time.UTC),
	)

	result, err := executor.Execute(context.Background(), RemoteCommandRequest{
		HostAlias:      "dev",
		Command:        "uptime",
		TimeoutSeconds: 30,
		RequestedBy:    RequestedByHuman,
	}, ExecuteOptions{})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if result.Status != StatusTimeout {
		t.Fatalf("status = %q, want timeout", result.Status)
	}
	if result.ExitCode != nil {
		t.Fatalf("exit_code = %v, want nil", *result.ExitCode)
	}
	if result.DurationMS != 30000 {
		t.Fatalf("duration_ms = %d, want 30000", result.DurationMS)
	}
}

func TestRA004BlockedCommandDoesNotOpenSSH(t *testing.T) {
	runner := &recordingRunner{}
	executor := testExecutor(runner)

	result, err := executor.Execute(context.Background(), RemoteCommandRequest{
		HostAlias:   "dev",
		Command:     "id",
		RequestedBy: RequestedByHuman,
	}, ExecuteOptions{})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if result.Status != StatusBlocked {
		t.Fatalf("status = %q, want blocked", result.Status)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("runner calls = %#v, want none", runner.calls)
	}
}

func TestRA005OutputIsTruncatedAt64KB(t *testing.T) {
	executor := testExecutor(&recordingRunner{
		output: RunOutput{Stdout: strings.Repeat("x", MaxCommandOutputBytes+128), ExitCode: 0},
	})

	result, err := executor.Execute(context.Background(), RemoteCommandRequest{
		HostAlias:   "dev",
		Command:     "uptime",
		RequestedBy: RequestedByHuman,
	}, ExecuteOptions{})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if !result.Truncated {
		t.Fatal("truncated = false, want true")
	}
	if len([]byte(result.Stdout)) > MaxCommandOutputBytes {
		t.Fatalf("stdout bytes = %d, want <= %d", len([]byte(result.Stdout)), MaxCommandOutputBytes)
	}
	if !strings.HasSuffix(result.Stdout, "[output truncated]") {
		t.Fatalf("stdout does not have truncation marker")
	}
}

func TestRA006LLMPlanRequiresValidSessionID(t *testing.T) {
	runner := &recordingRunner{}
	executor := testExecutor(runner)

	result, err := executor.Execute(context.Background(), RemoteCommandRequest{
		HostAlias:   "dev",
		Command:     "uptime",
		RequestedBy: RequestedByLLMPlan,
	}, ExecuteOptions{})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if result.Status != StatusInvalidSession {
		t.Fatalf("status = %q, want invalid_session", result.Status)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("runner calls = %#v, want none", runner.calls)
	}
}

func TestRA007RejectsSpecialCharactersInHostAliasBeforeSSH(t *testing.T) {
	runner := &recordingRunner{}
	executor := testExecutor(runner)

	result, err := executor.Execute(context.Background(), RemoteCommandRequest{
		HostAlias:   "host;rm",
		Command:     "uptime",
		RequestedBy: RequestedByHuman,
	}, ExecuteOptions{})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if result.Status != StatusInvalidHostname {
		t.Fatalf("status = %q, want invalid_hostname", result.Status)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("runner calls = %#v, want none", runner.calls)
	}
}

func TestAuditUnavailableBlocksBeforeSSH(t *testing.T) {
	runner := &recordingRunner{}
	executor := testExecutor(runner)
	executor.Logger = failingAuditLogger{}

	result, err := executor.Execute(context.Background(), RemoteCommandRequest{
		HostAlias:   "dev",
		Command:     "uptime",
		RequestedBy: RequestedByHuman,
	}, ExecuteOptions{})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if result.Status != StatusBlocked || !strings.Contains(result.Stderr, "audit_unavailable") {
		t.Fatalf("unexpected audit block result: %#v", result)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("runner calls = %#v, want none", runner.calls)
	}
}

func TestAuditWriteFailureIsReportedAfterExecution(t *testing.T) {
	runner := &recordingRunner{output: RunOutput{ExitCode: 0}}
	executor := testExecutor(runner)
	executor.Logger = writeFailingAuditLogger{}

	result, err := executor.Execute(context.Background(), RemoteCommandRequest{
		HostAlias:   "dev",
		Command:     "uptime",
		RequestedBy: RequestedByHuman,
	}, ExecuteOptions{})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.Status != StatusAuditFailed || result.Stderr != "audit_unavailable" {
		t.Fatalf("unexpected audit failure result: %#v", result)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("runner calls = %#v, want one execution", runner.calls)
	}
}

func TestResolvedHostRejectsSSHOptionInjection(t *testing.T) {
	runner := &recordingRunner{}
	executor := testExecutor(runner)
	executor.Resolver = func(context.Context, string) (RemoteHost, error) {
		return RemoteHost{
			Alias:        "dev",
			Hostname:     "-oProxyCommand=malicious",
			AccessMethod: AccessMethodSSH,
			SSHPort:      22,
			Enabled:      true,
		}, nil
	}

	result, err := executor.Execute(context.Background(), RemoteCommandRequest{
		HostAlias:   "dev",
		Command:     "uptime",
		RequestedBy: RequestedByHuman,
	}, ExecuteOptions{})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.Status != StatusInvalidHostname || len(runner.calls) != 0 {
		t.Fatalf("unexpected invalid host handling result=%#v calls=%#v", result, runner.calls)
	}
}

func TestHostStoreRejectsGroupWritableConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "remote-hosts.json")
	if err := os.WriteFile(path, []byte(`[]`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.Chmod(path, 0o660); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}
	store := &JSONHostStore{Path: path}
	if _, err := store.Load(); !errors.Is(err, ErrUnsafeHostStore) {
		t.Fatalf("Load() error = %v, want ErrUnsafeHostStore", err)
	}
}

func TestCappedBufferLimitsCapturedOutput(t *testing.T) {
	buffer := newCappedBuffer(4)
	if _, err := buffer.Write([]byte("abcdef")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if buffer.String() != "abcd" || !buffer.Truncated() {
		t.Fatalf("unexpected capped output value=%q truncated=%t", buffer.String(), buffer.Truncated())
	}
}

func TestJSONLAuditLoggerCreatesPrivateLog(t *testing.T) {
	home := t.TempDir()
	logger := NewJSONLAuditLogger()
	logger.HomeDir = func() (string, error) { return home, nil }
	logger.User = func() string { return "tester" }

	result := RemoteCommandResult{
		CommandID:   "command-1",
		SessionID:   validSessionID,
		HostAlias:   "dev",
		Command:     "uptime",
		StartedAt:   "2026-05-05T12:00:00Z",
		DurationMS:  12,
		Status:      StatusSuccess,
		RequestedBy: RequestedByHuman,
	}
	if err := logger.Prepare(); err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if err := logger.Write(result); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	assertRemoteMode(t, filepath.Join(home, ".pocketcli", "logs"), 0o700)
	assertRemoteMode(t, filepath.Join(home, ".pocketcli", "logs", "remote-commands.jsonl"), 0o600)
}

const validSessionID = "550e8400-e29b-41d4-a716-446655440000"

type recordedCall struct {
	name string
	args []string
}

type recordingRunner struct {
	output RunOutput
	err    error
	calls  []recordedCall
}

type failingAuditLogger struct{}

func (failingAuditLogger) Prepare() error                  { return errors.New("readonly") }
func (failingAuditLogger) Write(RemoteCommandResult) error { return nil }

type writeFailingAuditLogger struct{}

func (writeFailingAuditLogger) Prepare() error                  { return nil }
func (writeFailingAuditLogger) Write(RemoteCommandResult) error { return errors.New("disk full") }

func (r *recordingRunner) run(ctx context.Context, name string, args []string, options RunOptions) (RunOutput, error) {
	r.calls = append(r.calls, recordedCall{name: name, args: append([]string(nil), args...)})
	return r.output, r.err
}

func testExecutor(runner *recordingRunner) *Executor {
	executor := NewExecutor()
	executor.Runner = runner.run
	executor.Logger = nil
	executor.RetryInterval = time.Nanosecond
	executor.Now = fixedRemoteClock(
		time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC),
		time.Date(2026, 5, 5, 12, 0, 0, 50*int(time.Millisecond), time.UTC),
	)
	executor.NewCommandID = func() (string, error) { return "command-1", nil }
	executor.Resolver = func(ctx context.Context, alias string) (RemoteHost, error) {
		return RemoteHost{
			Alias:        alias,
			Hostname:     "100.64.0.10",
			OSFamily:     OSFamilyLinux,
			AccessMethod: AccessMethodSSH,
			SSHPort:      22,
			Enabled:      true,
		}, nil
	}
	executor.Probe = func(ctx context.Context, host RemoteHost) error { return nil }
	executor.Sleep = func(ctx context.Context, duration time.Duration) error { return nil }
	return executor
}

func fixedRemoteClock(times ...time.Time) func() time.Time {
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

func assertRemoteMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(%s) error = %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("mode(%s) = %#o, want %#o", path, got, want)
	}
}
