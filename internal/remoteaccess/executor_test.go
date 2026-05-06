package remoteaccess

import (
	"context"
	"errors"
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
