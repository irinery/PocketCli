package remoteaccess

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"pocketcli/internal/commandpolicy"
)

type RunOptions struct{}

type RunOutput struct {
	Stdout          string
	Stderr          string
	ExitCode        int
	StdoutTruncated bool
	StderrTruncated bool
}

type CommandRunner func(ctx context.Context, name string, args []string, options RunOptions) (RunOutput, error)
type ProbeFunc func(ctx context.Context, host RemoteHost) error
type SleepFunc func(ctx context.Context, duration time.Duration) error

type Executor struct {
	Policy   *commandpolicy.Evaluator
	Resolver HostResolver
	Runner   CommandRunner
	Probe    ProbeFunc
	Logger   AuditLogger

	Now            func() time.Time
	NewCommandID   func() (string, error)
	Sleep          SleepFunc
	MaxOutputBytes int
	RetryAttempts  int
	RetryInterval  time.Duration
}

func NewExecutor() *Executor {
	return &Executor{
		Policy:         commandpolicy.New(),
		Resolver:       DefaultHostStore().Resolve,
		Runner:         defaultRunner,
		Now:            func() time.Time { return time.Now().UTC() },
		NewCommandID:   newCommandID,
		MaxOutputBytes: MaxCommandOutputBytes,
		RetryAttempts:  DefaultConnectionAttempts,
		RetryInterval:  time.Duration(DefaultConnectionIntervalSec) * time.Second,
	}
}

func NewDefaultExecutor() *Executor {
	executor := NewExecutor()
	executor.Logger = NewJSONLAuditLogger()
	return executor
}

func (e *Executor) Execute(ctx context.Context, request RemoteCommandRequest, options ExecuteOptions) (RemoteCommandResult, error) {
	request = normalizeRequest(request)
	startedAt := e.now()
	result := RemoteCommandResult{
		CommandID:   e.commandID(),
		SessionID:   strings.TrimSpace(request.SessionID),
		HostAlias:   strings.TrimSpace(request.HostAlias),
		Command:     commandpolicy.Normalize(request.Command),
		StartedAt:   startedAt.Format(time.RFC3339),
		Status:      StatusFailed,
		RequestedBy: request.RequestedBy,
	}

	if auditErr := e.prepareAudit(); auditErr != nil {
		result.Status = StatusBlocked
		result.Stderr = fmt.Sprintf("audit_unavailable: %v", auditErr)
		return e.finish(result, startedAt), nil
	}

	if request.RequestedBy == RequestedByLLMPlan && !ValidSessionID(request.SessionID) {
		result.Status = StatusInvalidSession
		result.Stderr = "invalid_session"
		return e.finishAndAudit(result, startedAt), nil
	}

	if err := ValidateHostAlias(request.HostAlias); err != nil {
		result.Status = StatusInvalidHostname
		result.Stderr = "invalid_hostname"
		return e.finishAndAudit(result, startedAt), nil
	}

	if len(result.Command) > 1024 {
		result.Status = StatusBlocked
		result.Stderr = "command_too_long"
		return e.finishAndAudit(result, startedAt), nil
	}

	policyDecision := e.policy().Evaluate(request.Command)
	result.PolicyDecision = policyDecision
	if policyDecision.Decision == commandpolicy.DecisionBlocked {
		result.Status = StatusBlocked
		if policyDecision.BlockReason != nil {
			result.Stderr = *policyDecision.BlockReason
		} else {
			result.Stderr = "blocked"
		}
		return e.finishAndAudit(result, startedAt), nil
	}
	if policyDecision.Decision == commandpolicy.DecisionPendingApproval && !options.Approved {
		result.Status = StatusBlocked
		result.Stderr = "approval_required"
		return e.finishAndAudit(result, startedAt), nil
	}

	host, err := e.resolver()(ctx, request.HostAlias)
	if err != nil {
		status := StatusHostUnreachable
		if errors.Is(err, ErrInvalidHostname) {
			status = StatusInvalidHostname
		}
		result.Status = status
		result.Stderr = statusMessage(status)
		return e.finishAndAudit(result, startedAt), nil
	}
	host = normalizeHost(request.HostAlias, host)
	if err := validateResolvedHost(host); err != nil {
		result.Status = StatusInvalidHostname
		result.Stderr = "invalid_hostname"
		return e.finishAndAudit(result, startedAt), nil
	}
	if !host.Enabled {
		result.Status = StatusHostUnreachable
		result.Stderr = "host_disabled"
		return e.finishAndAudit(result, startedAt), nil
	}

	if err := e.probeWithRetry(ctx, host); err != nil {
		result.Status = StatusHostUnreachable
		result.Stderr = "host_unreachable"
		return e.finishAndAudit(result, startedAt), nil
	}

	timeout := normalizeTimeout(request.TimeoutSeconds)
	execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	output, err := e.runner()(execCtx, commandName(host), commandArgs(host, result.Command, timeout), RunOptions{})
	if errors.Is(execCtx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		result.Status = StatusTimeout
		result.Stdout, result.Stderr, result.Truncated = finalizeOutputs(output, e.maxOutputBytes())
		return e.finishAndAudit(result, startedAt), nil
	}

	exitCode := output.ExitCode
	result.ExitCode = &exitCode
	result.Stdout, result.Stderr, result.Truncated = finalizeOutputs(output, e.maxOutputBytes())
	if err != nil || exitCode != 0 {
		result.Status = StatusFailed
		return e.finishAndAudit(result, startedAt), nil
	}

	result.Status = StatusSuccess
	return e.finishAndAudit(result, startedAt), nil
}

func normalizeRequest(request RemoteCommandRequest) RemoteCommandRequest {
	request.HostAlias = strings.TrimSpace(request.HostAlias)
	request.Command = commandpolicy.Normalize(request.Command)
	request.SessionID = strings.TrimSpace(request.SessionID)
	if request.RequestedBy == "" {
		request.RequestedBy = RequestedByHuman
	}
	return request
}

func (e *Executor) prepareAudit() error {
	if e == nil || e.Logger == nil {
		return nil
	}
	return e.Logger.Prepare()
}

func (e *Executor) finishAndAudit(result RemoteCommandResult, startedAt time.Time) RemoteCommandResult {
	result = e.finish(result, startedAt)
	if e != nil && e.Logger != nil {
		if err := e.Logger.Write(result); err != nil {
			result.Status = StatusAuditFailed
			result.Stderr = "audit_unavailable"
		}
	}
	return result
}

func (e *Executor) finish(result RemoteCommandResult, startedAt time.Time) RemoteCommandResult {
	finishedAt := e.now()
	result.FinishedAt = finishedAt.Format(time.RFC3339)
	result.DurationMS = int(finishedAt.Sub(startedAt) / time.Millisecond)
	if result.DurationMS < 0 {
		result.DurationMS = 0
	}
	return result
}

func (e *Executor) probeWithRetry(ctx context.Context, host RemoteHost) error {
	attempts := e.retryAttempts()
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		if err := e.probe()(ctx, host); err != nil {
			lastErr = err
			if attempt < attempts {
				if sleepErr := e.sleep()(ctx, e.retryInterval()); sleepErr != nil {
					return sleepErr
				}
			}
			continue
		}
		return nil
	}
	return lastErr
}

func validateResolvedHost(host RemoteHost) error {
	if err := ValidateHostAlias(host.Alias); err != nil {
		return err
	}
	if err := ValidateHostname(host.Hostname); err != nil {
		return err
	}
	if host.TailscaleIP != nil {
		if err := ValidateTailscaleIP(*host.TailscaleIP); err != nil {
			return err
		}
	}
	if err := ValidateDefaultUser(host.DefaultUser); err != nil {
		return err
	}
	if host.SSHPort < 1 || host.SSHPort > 65535 {
		return ErrInvalidHostname
	}
	switch host.AccessMethod {
	case AccessMethodSSH, AccessMethodTailscaleSSH:
	default:
		return ErrInvalidHostname
	}
	return nil
}

func normalizeTimeout(timeout int) int {
	if timeout <= 0 {
		return DefaultCommandTimeoutSeconds
	}
	if timeout > MaxCommandTimeoutSeconds {
		return MaxCommandTimeoutSeconds
	}
	return timeout
}

func commandName(host RemoteHost) string {
	if host.AccessMethod == AccessMethodTailscaleSSH {
		return "tailscale"
	}
	return "ssh"
}

func commandArgs(host RemoteHost, command string, timeout int) []string {
	if host.AccessMethod == AccessMethodTailscaleSSH {
		return []string{"ssh", targetHost(host, false), command}
	}

	args := []string{
		"-n",
		"-o", "BatchMode=yes",
		"-o", fmt.Sprintf("ConnectTimeout=%d", timeout),
		"-o", "StrictHostKeyChecking=accept-new",
	}
	if host.SSHPort > 0 && host.SSHPort != 22 {
		args = append(args, "-p", strconv.Itoa(host.SSHPort))
	}
	args = append(args, targetHost(host, true), command)
	return args
}

func probeArgs(host RemoteHost) (string, []string) {
	if host.AccessMethod == AccessMethodTailscaleSSH {
		return "tailscale", []string{"ssh", targetHost(host, false), "true"}
	}

	args := []string{
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=20",
		"-o", "StrictHostKeyChecking=accept-new",
	}
	if host.SSHPort > 0 && host.SSHPort != 22 {
		args = append(args, "-p", strconv.Itoa(host.SSHPort))
	}
	args = append(args, targetHost(host, true), "true")
	return "ssh", args
}

func targetHost(host RemoteHost, preferTailscaleIP bool) string {
	target := strings.TrimSpace(host.Hostname)
	if preferTailscaleIP && host.TailscaleIP != nil && strings.TrimSpace(*host.TailscaleIP) != "" {
		target = strings.TrimSpace(*host.TailscaleIP)
	}
	if user := strings.TrimSpace(host.DefaultUser); user != "" {
		return user + "@" + target
	}
	return target
}

func truncateOutputs(stdout, stderr string, maxBytes int) (string, string, bool) {
	var truncated bool
	stdout, truncated = truncateString(stdout, maxBytes)
	stderr, stderrTruncated := truncateString(stderr, maxBytes)
	return stdout, stderr, truncated || stderrTruncated
}

func finalizeOutputs(output RunOutput, maxBytes int) (string, string, bool) {
	stdout, stderr, truncated := truncateOutputs(output.Stdout, output.Stderr, maxBytes)
	if output.StdoutTruncated {
		stdout = addTruncationMarker(stdout, maxBytes)
		truncated = true
	}
	if output.StderrTruncated {
		stderr = addTruncationMarker(stderr, maxBytes)
		truncated = true
	}
	return stdout, stderr, truncated
}

func addTruncationMarker(value string, maxBytes int) string {
	marker := "\n[output truncated]"
	if maxBytes <= len(marker) {
		return marker[:maxBytes]
	}
	if len(value)+len(marker) <= maxBytes {
		return value + marker
	}
	return value[:maxBytes-len(marker)] + marker
}

func truncateString(value string, maxBytes int) (string, bool) {
	if maxBytes <= 0 || len([]byte(value)) <= maxBytes {
		return value, false
	}
	marker := "\n[output truncated]"
	limit := maxBytes - len([]byte(marker))
	if limit < 0 {
		limit = maxBytes
		marker = ""
	}
	data := []byte(value)
	return string(data[:limit]) + marker, true
}

func defaultRunner(ctx context.Context, name string, args []string, _ RunOptions) (RunOutput, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	stdout := newCappedBuffer(MaxCommandOutputBytes)
	stderr := newCappedBuffer(MaxCommandOutputBytes)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		exitCode = 1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
	}
	return RunOutput{
		Stdout:          stdout.String(),
		Stderr:          stderr.String(),
		ExitCode:        exitCode,
		StdoutTruncated: stdout.Truncated(),
		StderrTruncated: stderr.Truncated(),
	}, err
}

type cappedBuffer struct {
	buffer    bytes.Buffer
	maxBytes  int
	truncated bool
}

func newCappedBuffer(maxBytes int) cappedBuffer {
	return cappedBuffer{maxBytes: maxBytes}
}

func (b *cappedBuffer) Write(value []byte) (int, error) {
	if b.maxBytes <= 0 {
		b.truncated = b.truncated || len(value) > 0
		return len(value), nil
	}
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

func (b *cappedBuffer) String() string {
	return b.buffer.String()
}

func (b *cappedBuffer) Truncated() bool {
	return b.truncated
}

func (e *Executor) defaultProbe(ctx context.Context, host RemoteHost) error {
	name, args := probeArgs(host)
	_, err := e.runner()(ctx, name, args, RunOptions{})
	return err
}

func defaultSleep(ctx context.Context, duration time.Duration) error {
	if duration <= 0 {
		return nil
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (e *Executor) policy() *commandpolicy.Evaluator {
	if e != nil && e.Policy != nil {
		return e.Policy
	}
	return commandpolicy.New()
}

func (e *Executor) resolver() HostResolver {
	if e != nil && e.Resolver != nil {
		return e.Resolver
	}
	return DefaultHostStore().Resolve
}

func (e *Executor) runner() CommandRunner {
	if e != nil && e.Runner != nil {
		return e.Runner
	}
	return defaultRunner
}

func (e *Executor) probe() ProbeFunc {
	if e != nil && e.Probe != nil {
		return e.Probe
	}
	return e.defaultProbe
}

func (e *Executor) sleep() SleepFunc {
	if e != nil && e.Sleep != nil {
		return e.Sleep
	}
	return defaultSleep
}

func (e *Executor) now() time.Time {
	if e != nil && e.Now != nil {
		return e.Now().UTC()
	}
	return time.Now().UTC()
}

func (e *Executor) retryAttempts() int {
	if e != nil && e.RetryAttempts > 0 {
		return e.RetryAttempts
	}
	return DefaultConnectionAttempts
}

func (e *Executor) retryInterval() time.Duration {
	if e != nil && e.RetryInterval > 0 {
		return e.RetryInterval
	}
	return time.Duration(DefaultConnectionIntervalSec) * time.Second
}

func (e *Executor) maxOutputBytes() int {
	if e != nil && e.MaxOutputBytes > 0 {
		return e.MaxOutputBytes
	}
	return MaxCommandOutputBytes
}

func (e *Executor) commandID() string {
	if e != nil && e.NewCommandID != nil {
		if id, err := e.NewCommandID(); err == nil && strings.TrimSpace(id) != "" {
			return id
		}
	}
	id, err := newCommandID()
	if err != nil {
		return "remote-command"
	}
	return id
}

func newCommandID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	return fmt.Sprintf(
		"%08x-%04x-%04x-%04x-%012x",
		raw[0:4],
		raw[4:6],
		raw[6:8],
		raw[8:10],
		raw[10:16],
	), nil
}

func statusMessage(status ResultStatus) string {
	return string(status)
}
