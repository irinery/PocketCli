package connect

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"pocketcli/internal/tailscale"
	"pocketcli/internal/tui/terminal"
)

type exitCoder interface {
	ExitCode() int
}

const (
	DefaultResolveTimeout  = 5 * time.Second
	DefaultApprovalTimeout = 30 * time.Second
	DefaultSSHTimeout      = 10 * time.Second
	DefaultLogMaxSizeBytes = 10 * 1024 * 1024

	ActionInteractive = "interactive"

	SourceTailscale = "tailscale"
	SourceDirect    = "direct"

	TrustObserved  = "observed"
	TrustUntrusted = "untrusted"

	ExitCodeFailure        = 1
	ExitCodeInvalidInput   = 2
	ExitCodeMissingRuntime = 3

	maxCapturedCommandBytes = 1024 * 1024
)

var validHostPattern = regexp.MustCompile(`^[a-zA-Z0-9._-]{1,63}$`)

type ExitError struct {
	Code    int
	Message string
	Printed bool
}

func (e *ExitError) Error() string {
	return strings.TrimSpace(e.Message)
}

func (e *ExitError) ExitCode() int {
	if e == nil || e.Code == 0 {
		return ExitCodeFailure
	}
	return e.Code
}

type HostInfo struct {
	Name      string
	IP        string
	Online    bool
	Reachable bool
	Trust     string
	Source    string
	Action    string
}

type SessionRecord struct {
	Host      string `json:"host"`
	IP        string `json:"ip,omitempty"`
	StartedAt string `json:"started_at,omitempty"`
	EndedAt   string `json:"ended_at,omitempty"`
	DurationS int    `json:"duration_s,omitempty"`
	ExitCause string `json:"exit_cause,omitempty"`
	Event     string `json:"event"`
}

type CommandOptions struct {
	Stdin         io.Reader
	Stdout        io.Writer
	Stderr        io.Writer
	CaptureOutput bool
	Env           []string
}

type CommandRunner func(ctx context.Context, name string, args []string, options CommandOptions) (string, error)

type Orchestrator struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer

	Now             func() time.Time
	LookupPath      func(string) (string, error)
	RunCommand      CommandRunner
	ResolveHostFunc func(context.Context, string) (HostInfo, error)
	ApprovalFunc    func(context.Context, HostInfo) (bool, error)
	HomeDir         func() (string, error)
	Executable      func() (string, error)
	Signals         <-chan os.Signal
	Logger          *SessionLogger

	ResolveTimeout  time.Duration
	ApprovalTimeout time.Duration
	SSHTimeout      time.Duration
	LogMaxSizeBytes int64
}

type PaneRequest struct {
	SessionName string
	Host        string
	IP          string
	StartedAt   time.Time
}

func New() *Orchestrator {
	return &Orchestrator{
		In:              os.Stdin,
		Out:             os.Stdout,
		Err:             os.Stderr,
		Now:             func() time.Time { return time.Now().UTC() },
		LookupPath:      exec.LookPath,
		RunCommand:      defaultCommandRunner,
		HomeDir:         os.UserHomeDir,
		Executable:      os.Executable,
		ResolveTimeout:  DefaultResolveTimeout,
		ApprovalTimeout: DefaultApprovalTimeout,
		SSHTimeout:      DefaultSSHTimeout,
		LogMaxSizeBytes: DefaultLogMaxSizeBytes,
	}
}

func (o *Orchestrator) Connect(ctx context.Context, host string) error {
	host = strings.TrimSpace(host)
	if err := validateHost(host); err != nil {
		return err
	}

	if err := o.checkDependencies(); err != nil {
		return err
	}

	info, err := o.resolveHost(ctx, host)
	if err != nil {
		return err
	}

	approved, err := o.approve(ctx, info)
	if err != nil {
		return err
	}
	if !approved {
		fmt.Fprintln(o.stdout(), "Sessão cancelada.")
		return nil
	}

	sessionName := SessionName(host)
	exists, err := o.sessionExists(ctx, sessionName)
	if err != nil {
		return err
	}
	if exists {
		return o.attachSession(ctx, sessionName)
	}

	startedAt := o.now()
	executablePath, err := o.executable()
	if err != nil {
		return &ExitError{Code: ExitCodeFailure, Message: fmt.Sprintf("pocket: falha ao localizar o binário atual: %v", err)}
	}

	cols, rows := o.terminalSize()
	command := buildPaneCommand(executablePath, PaneRequest{
		SessionName: sessionName,
		Host:        info.Name,
		IP:          info.IP,
		StartedAt:   startedAt,
	})

	_, err = o.runCommand(ctx, "tmux", []string{
		"new-session",
		"-d",
		"-s", sessionName,
		"-x", strconv.Itoa(cols),
		"-y", strconv.Itoa(rows),
		command,
	}, CommandOptions{
		Stderr: o.stderr(),
	})
	if err != nil {
		return &ExitError{
			Code:    ExitCodeFailure,
			Message: fmt.Sprintf("pocket: falha ao criar sessão tmux: %v", err),
		}
	}

	return o.attachSession(ctx, sessionName)
}

func (o *Orchestrator) RunPane(ctx context.Context, request PaneRequest) error {
	if strings.TrimSpace(request.SessionName) == "" || strings.TrimSpace(request.Host) == "" || strings.TrimSpace(request.IP) == "" {
		return &ExitError{Code: ExitCodeInvalidInput, Message: "pocket: parâmetros internos inválidos"}
	}

	logger := o.sessionLogger()
	logger.LogConnect(request.Host, request.IP, request.StartedAt)

	exitCause := "user_exit"
	err := o.runSSH(ctx, request.IP)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			exitCause = "signal"
		} else {
			exitCause = "ssh_error"
		}
	}

	finishedAt := o.now()
	logger.LogDisconnect(request.Host, request.IP, finishedAt, finishedAt.Sub(request.StartedAt), exitCause)

	switch exitCause {
	case "user_exit":
		o.cleanupSession(ctx, request.SessionName)
		return nil
	case "signal":
		return &ExitError{Code: ExitCodeFailure, Message: "pocket: sessão interrompida", Printed: true}
	default:
		fmt.Fprintln(o.stderr(), "SSH falhou; sessão tmux mantida para inspeção.")
		return o.openInspectionShell(ctx)
	}
}

func SessionName(host string) string {
	return "pocket_" + host
}

func validateHost(host string) error {
	if !validHostPattern.MatchString(strings.TrimSpace(host)) {
		return &ExitError{Code: ExitCodeInvalidInput, Message: "pocket: nome de host inválido"}
	}
	return nil
}

func (o *Orchestrator) checkDependencies() error {
	if _, err := o.lookupPath("tmux"); err != nil {
		return &ExitError{Code: ExitCodeMissingRuntime, Message: "pocket: tmux não encontrado. Instale tmux para continuar."}
	}
	if _, err := o.lookupPath("ssh"); err != nil {
		return &ExitError{Code: ExitCodeMissingRuntime, Message: "pocket: ssh não encontrado. Instale ssh para continuar."}
	}
	return nil
}

func (o *Orchestrator) resolveHost(ctx context.Context, host string) (HostInfo, error) {
	if o.ResolveHostFunc != nil {
		return o.ResolveHostFunc(ctx, host)
	}

	tailscaleCommand, err := tailscale.CLIPath(o.lookupPath)
	if err != nil {
		fmt.Fprintln(o.stderr(), "pocket: tailscale não encontrado. Tentando resolução DNS.")
		return o.resolveHostDNS(ctx, host)
	}

	resolveCtx, cancel := context.WithTimeout(ctx, o.resolveTimeout())
	defer cancel()

	output, err := o.runCommand(resolveCtx, tailscaleCommand, []string{"status", "--json"}, CommandOptions{
		CaptureOutput: true,
		Stderr:        io.Discard,
		Env:           []string{"TAILSCALE_BE_CLI=1"},
	})
	if err != nil {
		fmt.Fprintln(o.stderr(), "pocket: tailscale status indisponível. Tentando resolução DNS.")
		return o.resolveHostDNS(ctx, host)
	}

	var status tailscale.Status
	if err := json.Unmarshal([]byte(output), &status); err != nil {
		fmt.Fprintln(o.stderr(), "pocket: resposta inválida do Tailscale. Tentando resolução DNS.")
		return o.resolveHostDNS(ctx, host)
	}

	peer, ok := findPeer(status, host)
	if !ok {
		return HostInfo{}, &ExitError{Code: ExitCodeMissingRuntime, Message: fmt.Sprintf("pocket: host '%s' não encontrado no Tailscale", host)}
	}

	ip := ""
	if len(peer.TailscaleIPs) > 0 {
		ip = strings.TrimSpace(peer.TailscaleIPs[0])
	}

	info := HostInfo{
		Name:      host,
		IP:        ip,
		Online:    peer.Online,
		Reachable: peer.Online && ip != "",
		Trust:     TrustObserved,
		Source:    SourceTailscale,
		Action:    ActionInteractive,
	}

	if !info.Online {
		return HostInfo{}, &ExitError{Code: ExitCodeFailure, Message: fmt.Sprintf("pocket: host '%s' está offline (Tailscale)", host)}
	}
	if !info.Reachable {
		return HostInfo{}, &ExitError{Code: ExitCodeFailure, Message: fmt.Sprintf("pocket: host '%s' está inacessível via Tailscale", host)}
	}

	return info, nil
}

func (o *Orchestrator) resolveHostDNS(ctx context.Context, host string) (HostInfo, error) {
	resolveCtx, cancel := context.WithTimeout(ctx, o.resolveTimeout())
	defer cancel()

	addresses, err := net.DefaultResolver.LookupHost(resolveCtx, host)
	if err != nil || len(addresses) == 0 {
		return HostInfo{}, &ExitError{Code: ExitCodeMissingRuntime, Message: fmt.Sprintf("pocket: host '%s' não encontrado no Tailscale", host)}
	}

	return HostInfo{
		Name:      host,
		IP:        strings.TrimSpace(addresses[0]),
		Online:    true,
		Reachable: true,
		Trust:     TrustUntrusted,
		Source:    SourceDirect,
		Action:    ActionInteractive,
	}, nil
}

func (o *Orchestrator) approve(ctx context.Context, info HostInfo) (bool, error) {
	if o.ApprovalFunc != nil {
		return o.ApprovalFunc(ctx, info)
	}

	fmt.Fprintf(o.stdout(), "Session approval required\n  Hostname:      %s\n  IP Tailscale:  %s\n  Source:        %s\n  Online:        %t\n  Reachable:     %t\n  Trust level:   %s\n  Layout:        agent-default\n  Action:        interactive\n\nApprove this SSH session? [y/N]\n", info.Name, info.IP, info.Source, info.Online, info.Reachable, info.Trust)

	type approvalResult struct {
		line string
		err  error
	}

	resultCh := make(chan approvalResult, 1)
	go func() {
		reader := bufio.NewReader(o.stdin())
		line, err := reader.ReadString('\n')
		if errors.Is(err, io.EOF) && line != "" {
			err = nil
		}
		resultCh <- approvalResult{line: line, err: err}
	}()

	signalCh, stop := o.signalChannel()
	defer stop()

	timer := time.NewTimer(o.approvalTimeout())
	defer timer.Stop()

	select {
	case <-ctx.Done():
		fmt.Fprintln(o.stderr(), "Sessão cancelada.")
		return false, &ExitError{Code: ExitCodeFailure, Message: "Sessão cancelada.", Printed: true}
	case <-timer.C:
		fmt.Fprintln(o.stderr(), "Aprovação expirada. Sessão cancelada.")
		return false, &ExitError{Code: ExitCodeFailure, Message: "Aprovação expirada. Sessão cancelada.", Printed: true}
	case <-signalCh:
		fmt.Fprintln(o.stderr(), "Sessão cancelada.")
		return false, &ExitError{Code: ExitCodeFailure, Message: "Sessão cancelada.", Printed: true}
	case result := <-resultCh:
		if result.err != nil {
			fmt.Fprintln(o.stderr(), "Sessão cancelada.")
			return false, &ExitError{Code: ExitCodeFailure, Message: "Sessão cancelada.", Printed: true}
		}
		answer := strings.ToLower(strings.TrimSpace(result.line))
		return answer == "y" || answer == "yes", nil
	}
}

func (o *Orchestrator) sessionExists(ctx context.Context, sessionName string) (bool, error) {
	_, err := o.runCommand(ctx, "tmux", []string{"has-session", "-t", sessionName}, CommandOptions{
		Stderr: io.Discard,
	})
	if err == nil {
		return true, nil
	}
	if isExitCode(err, 1) {
		return false, nil
	}
	return false, &ExitError{Code: ExitCodeFailure, Message: fmt.Sprintf("pocket: falha ao consultar sessão tmux: %v", err)}
}

func (o *Orchestrator) attachSession(ctx context.Context, sessionName string) error {
	_, err := o.runCommand(ctx, "tmux", []string{"attach-session", "-t", sessionName}, CommandOptions{
		Stdin:  o.stdin(),
		Stdout: o.stdout(),
		Stderr: o.stderr(),
	})
	if err != nil {
		return &ExitError{Code: ExitCodeFailure, Message: fmt.Sprintf("pocket: falha ao anexar sessão tmux: %v", err)}
	}
	return nil
}

func (o *Orchestrator) runSSH(ctx context.Context, ip string) error {
	sshCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	signalCh, stop := o.signalChannel()
	defer stop()

	done := make(chan error, 1)
	go func() {
		_, err := o.runCommand(sshCtx, "ssh", []string{
			"-o", fmt.Sprintf("ConnectTimeout=%d", int(o.sshTimeout().Seconds())),
			"-o", "StrictHostKeyChecking=accept-new",
			ip,
		}, CommandOptions{
			Stdin:  o.stdin(),
			Stdout: o.stdout(),
			Stderr: o.stderr(),
		})
		done <- err
	}()

	select {
	case <-ctx.Done():
		cancel()
		return ctx.Err()
	case <-signalCh:
		cancel()
		return context.Canceled
	case err := <-done:
		return err
	}
}

func (o *Orchestrator) cleanupSession(ctx context.Context, sessionName string) {
	output, err := o.runCommand(ctx, "tmux", []string{"list-panes", "-t", sessionName}, CommandOptions{
		CaptureOutput: true,
		Stderr:        io.Discard,
	})
	if err != nil {
		return
	}

	paneCount := 0
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) != "" {
			paneCount++
		}
	}

	if paneCount <= 1 {
		_, _ = o.runCommand(ctx, "tmux", []string{"kill-session", "-t", sessionName}, CommandOptions{
			Stderr: io.Discard,
		})
	}
}

func (o *Orchestrator) openInspectionShell(ctx context.Context) error {
	shellPath := strings.TrimSpace(os.Getenv("SHELL"))
	if shellPath == "" {
		shellPath = "/bin/sh"
	}

	_, err := o.runCommand(ctx, shellPath, nil, CommandOptions{
		Stdin:  o.stdin(),
		Stdout: o.stdout(),
		Stderr: o.stderr(),
	})
	return err
}

func (o *Orchestrator) sessionLogger() *SessionLogger {
	if o.Logger != nil {
		return o.Logger
	}

	return &SessionLogger{
		HomeDir:         o.homeDir,
		Now:             o.now,
		Err:             o.stderr(),
		MaxSizeBytes:    o.logMaxSizeBytes(),
		SessionsLogPath: filepath.Join(".pocketcli", "logs", "sessions.log"),
	}
}

func buildPaneCommand(executablePath string, request PaneRequest) string {
	args := []string{
		shellQuote(executablePath),
		"__connect-pane",
		"--session", shellQuote(request.SessionName),
		"--host", shellQuote(request.Host),
		"--ip", shellQuote(request.IP),
		"--started-at", shellQuote(request.StartedAt.UTC().Format(time.RFC3339)),
	}
	return strings.Join(args, " ")
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func findPeer(status tailscale.Status, host string) (tailscale.Peer, bool) {
	for _, peer := range status.Peer {
		if hostMatches(peer.HostName, host) {
			return peer, true
		}
	}
	return tailscale.Peer{}, false
}

func hostMatches(candidate, requested string) bool {
	candidate = strings.TrimSpace(candidate)
	requested = strings.TrimSpace(requested)
	if strings.EqualFold(candidate, requested) {
		return true
	}
	if idx := strings.Index(candidate, "."); idx > 0 && strings.EqualFold(candidate[:idx], requested) {
		return true
	}
	return false
}

func defaultCommandRunner(ctx context.Context, name string, args []string, options CommandOptions) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if len(options.Env) > 0 {
		cmd.Env = append(os.Environ(), options.Env...)
	}

	if options.CaptureOutput {
		output := newConnectOutputBuffer(maxCapturedCommandBytes)
		cmd.Stdout = &output
		if options.Stderr != nil {
			cmd.Stderr = options.Stderr
		}
		err := cmd.Run()
		if output.Truncated() && err == nil {
			err = errors.New("command output too large")
		}
		return output.String(), err
	}

	cmd.Stdin = options.Stdin
	cmd.Stdout = options.Stdout
	cmd.Stderr = options.Stderr
	return "", cmd.Run()
}

type connectOutputBuffer struct {
	buffer    bytes.Buffer
	maxBytes  int
	truncated bool
}

func newConnectOutputBuffer(maxBytes int) connectOutputBuffer {
	return connectOutputBuffer{maxBytes: maxBytes}
}

func (b *connectOutputBuffer) Write(value []byte) (int, error) {
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

func (b *connectOutputBuffer) String() string { return b.buffer.String() }

func (b *connectOutputBuffer) Truncated() bool { return b.truncated }

func isExitCode(err error, code int) bool {
	var codedErr exitCoder
	if errors.As(err, &codedErr) {
		return codedErr.ExitCode() == code
	}

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return false
	}
	return exitErr.ExitCode() == code
}

func (o *Orchestrator) signalChannel() (<-chan os.Signal, func()) {
	if o.Signals != nil {
		return o.Signals, func() {}
	}

	signalCh := make(chan os.Signal, 2)
	signal.Notify(signalCh, syscall.SIGINT, syscall.SIGTERM)
	return signalCh, func() { signal.Stop(signalCh) }
}

func (o *Orchestrator) terminalSize() (int, int) {
	if cols, rows, ok := envTerminalSize(); ok {
		return cols, rows
	}

	term, err := terminal.New(int(os.Stdout.Fd()))
	if err == nil {
		cols, rows, sizeErr := term.Size()
		term = nil
		if sizeErr == nil && cols > 0 && rows > 0 {
			return int(cols), int(rows)
		}
	}

	return 80, 24
}

func envTerminalSize() (int, int, bool) {
	cols, errCols := strconv.Atoi(strings.TrimSpace(os.Getenv("COLUMNS")))
	rows, errRows := strconv.Atoi(strings.TrimSpace(os.Getenv("LINES")))
	if errCols != nil || errRows != nil || cols <= 0 || rows <= 0 {
		return 0, 0, false
	}
	return cols, rows, true
}

func (o *Orchestrator) stdin() io.Reader {
	if o.In == nil {
		return os.Stdin
	}
	return o.In
}

func (o *Orchestrator) stdout() io.Writer {
	if o.Out == nil {
		return io.Discard
	}
	return o.Out
}

func (o *Orchestrator) stderr() io.Writer {
	if o.Err == nil {
		return io.Discard
	}
	return o.Err
}

func (o *Orchestrator) executable() (string, error) {
	if o.Executable == nil {
		return os.Executable()
	}
	return o.Executable()
}

func (o *Orchestrator) lookupPath(name string) (string, error) {
	if o.LookupPath == nil {
		return exec.LookPath(name)
	}
	return o.LookupPath(name)
}

func (o *Orchestrator) runCommand(ctx context.Context, name string, args []string, options CommandOptions) (string, error) {
	runner := o.RunCommand
	if runner == nil {
		runner = defaultCommandRunner
	}
	return runner(ctx, name, args, options)
}

func (o *Orchestrator) now() time.Time {
	if o.Now == nil {
		return time.Now().UTC()
	}
	return o.Now().UTC()
}

func (o *Orchestrator) homeDir() (string, error) {
	if o.HomeDir == nil {
		return os.UserHomeDir()
	}
	return o.HomeDir()
}

func (o *Orchestrator) resolveTimeout() time.Duration {
	if o.ResolveTimeout <= 0 {
		return DefaultResolveTimeout
	}
	return o.ResolveTimeout
}

func (o *Orchestrator) approvalTimeout() time.Duration {
	if o.ApprovalTimeout <= 0 {
		return DefaultApprovalTimeout
	}
	return o.ApprovalTimeout
}

func (o *Orchestrator) sshTimeout() time.Duration {
	if o.SSHTimeout <= 0 {
		return DefaultSSHTimeout
	}
	return o.SSHTimeout
}

func (o *Orchestrator) logMaxSizeBytes() int64 {
	if o.LogMaxSizeBytes <= 0 {
		return DefaultLogMaxSizeBytes
	}
	return o.LogMaxSizeBytes
}
