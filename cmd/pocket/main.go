package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"pocketcli/internal/fleet"
	"pocketcli/internal/ledger"
	"pocketcli/internal/remoteaccess"
	"pocketcli/internal/safety"
)

var (
	hostsViewer       = defaultHostsViewer
	newRemoteExecutor = remoteaccess.NewDefaultExecutor
)

func main() {
	root := newRootCommand()
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCommand() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "pocket",
		Short: "PocketCli core CLI",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	rootCmd.AddCommand(newAskCommand())
	rootCmd.AddCommand(newRecallCommand())
	rootCmd.AddCommand(newContextCommand())
	rootCmd.AddCommand(newMemoryCommand())
	rootCmd.AddCommand(newCapabilitiesCommand())
	rootCmd.AddCommand(newLedgerCommand())
	rootCmd.AddCommand(newInsightsCommand())
	rootCmd.AddCommand(newActionsCommand())
	rootCmd.AddCommand(newFleetCommand())
	rootCmd.AddCommand(newApproveCommand())
	rootCmd.AddCommand(newDoctorCommand())
	rootCmd.AddCommand(newEvalCommand())
	rootCmd.AddCommand(newHostsCommand())
	rootCmd.AddCommand(newSSHCommand())
	rootCmd.AddCommand(newConnectCommand())
	rootCmd.AddCommand(newConnectPaneCommand())
	rootCmd.AddCommand(newExecCommand())

	return rootCmd
}

func newHostsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "hosts [--json]",
		Short: "List, filter and connect to Tailscale machines",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAudited(cmd, "hosts", args, func(cmd *cobra.Command, args []string, sessionID string) (commandAudit, error) {
				if hasFlag(args, "--json") {
					inventory, err := loadHostInventoryForCommand()
					return commandAudit{SessionID: sessionID}, writeJSON(cmd.OutOrStdout(), inventoryOrError(inventory, err))
				}
				return commandAudit{SessionID: sessionID}, hostsViewer(os.Stdin, cmd.OutOrStdout())
			})
		},
	}
}

func newSSHCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "ssh <host>",
		Short: "Abre sessão SSH interativa pelo fluxo seguro de conexão",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAudited(cmd, "ssh", args, func(cmd *cobra.Command, args []string, sessionID string) (commandAudit, error) {
				return commandAudit{SessionID: sessionID, HostID: args[0]}, connectInteractive(context.Background(), args[0], os.Stdin, cmd.OutOrStdout(), os.Stderr)
			})
		},
	}
}

func newExecCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "exec [--prepare] [--envelope-id ID] [--approval-token TOKEN] [--json] [--timeout N] [--requested-by human|llm_plan] [--session-id ID] [--approve] <host> <command...>",
		Short: "Execute remote command via SSH",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
				return cmd.Help()
			}
			return runAudited(cmd, "exec", args, func(cmd *cobra.Command, args []string, sessionID string) (commandAudit, error) {
				parsed, err := parseExecArgs(args)
				if err != nil {
					return commandAudit{SessionID: sessionID}, err
				}
				if strings.TrimSpace(parsed.sessionID) == "" {
					parsed.sessionID = sessionID
				}

				if parsed.prepare {
					envelope, err := createExecEnvelope(parsed.host, parsed.commandParts)
					if err != nil {
						return commandAudit{SessionID: sessionID, HostID: parsed.host}, err
					}
					appendExecEnvelopeEvent(sessionID, envelope)
					return commandAudit{SessionID: sessionID, HostID: parsed.host}, writeJSON(cmd.OutOrStdout(), newExecEnvelopeResult(envelope))
				}

				if parsed.envelopeID != "" {
					host, commandParts, envelopeApproved, err := resolvePreparedExec(parsed)
					if err != nil {
						return commandAudit{SessionID: sessionID, HostID: host}, err
					}
					parsed.host = host
					parsed.commandParts = commandParts
					parsed.command = strings.Join(commandParts, " ")
					parsed.approved = parsed.approved || envelopeApproved
					return runRemoteExec(cmd, sessionID, parsed)
				}

				if strings.TrimSpace(parsed.approvalToken) != "" {
					return commandAudit{SessionID: sessionID, HostID: parsed.host}, fmt.Errorf("flag --approval-token requer --envelope-id")
				}
				if err := ensureDirectExecAllowed(sessionID, parsed.host, parsed.commandParts); err != nil {
					return commandAudit{SessionID: sessionID, HostID: parsed.host}, err
				}
				return runRemoteExec(cmd, sessionID, parsed)
			})
		},
	}
}

type execArgs struct {
	prepare        bool
	envelopeID     string
	approvalToken  string
	host           string
	command        string
	commandParts   []string
	timeoutSeconds int
	requestedBy    remoteaccess.RequestedBy
	sessionID      string
	approved       bool
	jsonOutput     bool
}

type execEnvelopeResult struct {
	EnvelopeID       string          `json:"envelope_id"`
	Host             string          `json:"host"`
	Command          []string        `json:"command"`
	Decision         safety.Decision `json:"decision"`
	ApprovalRequired bool            `json:"approval_required"`
	ApproveCommand   string          `json:"approve_command,omitempty"`
	ExecuteCommand   string          `json:"execute_command"`
}

func parseExecArgs(args []string) (execArgs, error) {
	parsed := execArgs{
		timeoutSeconds: remoteaccess.DefaultCommandTimeoutSeconds,
		requestedBy:    remoteaccess.RequestedByHuman,
	}

	index := 0
	for index < len(args) {
		arg := args[index]
		if arg == "--" {
			index++
			break
		}
		if !strings.HasPrefix(arg, "--") {
			break
		}

		switch {
		case arg == "--prepare":
			parsed.prepare = true
			index++
		case arg == "--json":
			parsed.jsonOutput = true
			index++
		case arg == "--approve":
			parsed.approved = true
			index++
		case arg == "--envelope-id":
			if index+1 >= len(args) {
				return parsed, fmt.Errorf("flag --envelope-id requer valor")
			}
			parsed.envelopeID = strings.TrimSpace(args[index+1])
			if parsed.envelopeID == "" {
				return parsed, fmt.Errorf("flag --envelope-id requer valor")
			}
			index += 2
		case strings.HasPrefix(arg, "--envelope-id="):
			parsed.envelopeID = strings.TrimSpace(strings.TrimPrefix(arg, "--envelope-id="))
			if parsed.envelopeID == "" {
				return parsed, fmt.Errorf("flag --envelope-id requer valor")
			}
			index++
		case arg == "--approval-token":
			if index+1 >= len(args) {
				return parsed, fmt.Errorf("flag --approval-token requer valor")
			}
			parsed.approvalToken = strings.TrimSpace(args[index+1])
			if parsed.approvalToken == "" {
				return parsed, fmt.Errorf("flag --approval-token requer valor")
			}
			index += 2
		case strings.HasPrefix(arg, "--approval-token="):
			parsed.approvalToken = strings.TrimSpace(strings.TrimPrefix(arg, "--approval-token="))
			if parsed.approvalToken == "" {
				return parsed, fmt.Errorf("flag --approval-token requer valor")
			}
			index++
		case arg == "--timeout":
			if index+1 >= len(args) {
				return parsed, fmt.Errorf("pocket: --timeout exige valor")
			}
			timeout, err := strconv.Atoi(args[index+1])
			if err != nil {
				return parsed, fmt.Errorf("pocket: timeout inválido")
			}
			parsed.timeoutSeconds = timeout
			index += 2
		case strings.HasPrefix(arg, "--timeout="):
			timeout, err := strconv.Atoi(strings.TrimPrefix(arg, "--timeout="))
			if err != nil {
				return parsed, fmt.Errorf("pocket: timeout inválido")
			}
			parsed.timeoutSeconds = timeout
			index++
		case arg == "--requested-by":
			if index+1 >= len(args) {
				return parsed, fmt.Errorf("pocket: --requested-by exige valor")
			}
			requestedBy, err := parseRequestedBy(args[index+1])
			if err != nil {
				return parsed, err
			}
			parsed.requestedBy = requestedBy
			index += 2
		case strings.HasPrefix(arg, "--requested-by="):
			requestedBy, err := parseRequestedBy(strings.TrimPrefix(arg, "--requested-by="))
			if err != nil {
				return parsed, err
			}
			parsed.requestedBy = requestedBy
			index++
		case arg == "--session-id":
			if index+1 >= len(args) {
				return parsed, fmt.Errorf("pocket: --session-id exige valor")
			}
			parsed.sessionID = strings.TrimSpace(args[index+1])
			index += 2
		case strings.HasPrefix(arg, "--session-id="):
			parsed.sessionID = strings.TrimSpace(strings.TrimPrefix(arg, "--session-id="))
			index++
		default:
			return parsed, fmt.Errorf("pocket: opção inválida para exec: %s", arg)
		}
	}

	remaining := append([]string(nil), args[index:]...)
	if parsed.prepare && strings.TrimSpace(parsed.envelopeID) != "" {
		return parsed, fmt.Errorf("--prepare não pode ser usado com --envelope-id")
	}
	if strings.TrimSpace(parsed.envelopeID) != "" {
		if len(remaining) > 0 {
			return parsed, fmt.Errorf("--envelope-id executa o comando salvo; não passe host/comando junto")
		}
		return parsed, nil
	}
	if strings.TrimSpace(parsed.approvalToken) != "" {
		return parsed, fmt.Errorf("flag --approval-token requer --envelope-id")
	}
	if len(remaining) < 2 {
		return parsed, fmt.Errorf("Usage: pocket exec <host> <command...>")
	}
	parsed.host = remaining[0]
	parsed.commandParts = append([]string(nil), remaining[1:]...)
	parsed.command = strings.Join(parsed.commandParts, " ")
	if err := validateExecHost(parsed.host); err != nil {
		return parsed, err
	}
	return parsed, nil
}

func validateExecHost(host string) error {
	host = strings.TrimSpace(host)
	if err := remoteaccess.ValidateHostAlias(host); err != nil {
		return fmt.Errorf("ERR_EXEC_HOST_INVALID")
	}
	return nil
}

func parseRequestedBy(value string) (remoteaccess.RequestedBy, error) {
	switch remoteaccess.RequestedBy(strings.TrimSpace(value)) {
	case remoteaccess.RequestedByHuman:
		return remoteaccess.RequestedByHuman, nil
	case remoteaccess.RequestedByLLMPlan:
		return remoteaccess.RequestedByLLMPlan, nil
	default:
		return "", fmt.Errorf("pocket: requested-by inválido")
	}
}

func createExecEnvelope(host string, command []string) (safety.RunEnvelope, error) {
	if err := validateExecHost(host); err != nil {
		return safety.RunEnvelope{}, err
	}
	return safety.CreateRunEnvelope(safety.Request{
		Action:      "exec",
		Command:     append([]string(nil), command...),
		Host:        host,
		HostCount:   1,
		Interactive: true,
	})
}

func ensureDirectExecAllowed(sessionID, host string, command []string) error {
	decision, err := safety.Evaluate(safety.Request{
		Action:      "exec",
		Command:     command,
		Host:        host,
		HostCount:   1,
		Interactive: stdinInteractive(),
	})
	if err == nil && !decision.ApprovalRequired {
		return nil
	}
	if decision.Classification == safety.ClassificationBlocked {
		appendLedgerEvent(deniedSafetyEvent("exec", command, "blocked"))
		return safety.ErrApprovalBlocked
	}
	if err != nil && err != safety.ErrApprovalRequired {
		appendLedgerEvent(deniedSafetyEvent("exec", command, err.Error()))
		return err
	}
	envelope, envErr := createExecEnvelope(host, command)
	if envErr != nil {
		appendLedgerEvent(deniedSafetyEvent("exec", command, envErr.Error()))
		return envErr
	}
	appendExecEnvelopeEvent(sessionID, envelope)
	appendLedgerEvent(deniedSafetyEvent("exec", command, "approval_required envelope_id="+envelope.EnvelopeID))
	return fmt.Errorf("%w envelope_id=%s approve=\"pocket approve %s\" execute=\"pocket exec --envelope-id %s --approval-token TOKEN\"", safety.ErrApprovalRequired, envelope.EnvelopeID, envelope.EnvelopeID, envelope.EnvelopeID)
}

func resolvePreparedExec(parsed execArgs) (string, []string, bool, error) {
	envelope, err := safety.LoadEnvelope(parsed.envelopeID)
	if err != nil {
		return "", nil, false, safety.ErrApprovalNotFound
	}
	if strings.TrimSpace(envelope.Request.Action) != "exec" {
		return envelope.Request.Host, nil, false, fmt.Errorf("ERR_APPROVAL_ENVELOPE_ACTION_MISMATCH")
	}
	host := strings.TrimSpace(envelope.Request.Host)
	if err := validateExecHost(host); err != nil {
		return host, nil, false, err
	}
	command := append([]string(nil), envelope.Request.Command...)
	if len(command) == 0 {
		return host, nil, false, safety.ErrCommandInvalid
	}
	if envelope.Decision.Classification == safety.ClassificationBlocked {
		return host, nil, false, safety.ErrApprovalBlocked
	}
	currentDecision, evalErr := safety.Evaluate(safety.Request{
		Action:      "exec",
		Command:     command,
		Host:        host,
		HostCount:   1,
		Interactive: true,
	})
	if currentDecision.Classification == safety.ClassificationBlocked {
		appendLedgerEvent(deniedSafetyEvent("exec", command, "blocked"))
		return host, nil, false, safety.ErrApprovalBlocked
	}
	if evalErr != nil && evalErr != safety.ErrApprovalRequired {
		appendLedgerEvent(deniedSafetyEvent("exec", command, evalErr.Error()))
		return host, nil, false, evalErr
	}
	if envelope.Decision.ApprovalRequired || currentDecision.ApprovalRequired || evalErr == safety.ErrApprovalRequired {
		if err := safety.ConsumeApproval(envelope.EnvelopeID, parsed.approvalToken); err != nil {
			appendLedgerEvent(deniedSafetyEvent("exec", command, err.Error()))
			return host, nil, false, err
		}
		return host, command, true, nil
	}
	return host, command, false, nil
}

func runRemoteExec(cmd *cobra.Command, sessionID string, parsed execArgs) (commandAudit, error) {
	if strings.TrimSpace(parsed.sessionID) == "" {
		parsed.sessionID = sessionID
	}

	executor := newRemoteExecutor()
	result, err := executor.Execute(context.Background(), remoteaccess.RemoteCommandRequest{
		SessionID:      parsed.sessionID,
		HostAlias:      parsed.host,
		Command:        parsed.command,
		TimeoutSeconds: parsed.timeoutSeconds,
		RequestedBy:    parsed.requestedBy,
	}, remoteaccess.ExecuteOptions{
		Approved: parsed.approved,
	})
	if printErr := printRemoteExecResult(cmd, result, parsed.jsonOutput); printErr != nil && err == nil {
		err = printErr
	}
	if err == nil {
		err = errorForRemoteResult(result)
	}

	status := string(result.Status)
	if status == "" {
		status = "error"
	}
	if result.Status == remoteaccess.StatusSuccess {
		status = "ok"
	}
	appendLedgerEvent(ledger.Event{
		Type:       ledger.EventSSHExec,
		Command:    "exec",
		SessionID:  result.SessionID,
		HostID:     parsed.host,
		Status:     status,
		DurationMS: result.DurationMS,
		Payload:    ledger.Payload{Message: parsed.command},
	})

	return commandAudit{SessionID: result.SessionID, HostID: parsed.host, LatencyMS: result.DurationMS}, err
}

func printRemoteExecResult(cmd *cobra.Command, result remoteaccess.RemoteCommandResult, jsonOutput bool) error {
	if jsonOutput {
		encoder := json.NewEncoder(cmd.OutOrStdout())
		return encoder.Encode(result)
	}

	if result.Stdout != "" {
		fmt.Fprint(cmd.OutOrStdout(), result.Stdout)
	}
	if result.Stderr != "" && (result.Status == remoteaccess.StatusFailed || result.Status == remoteaccess.StatusTimeout || result.Status == remoteaccess.StatusAuditFailed) {
		fmt.Fprint(os.Stderr, result.Stderr)
	}
	return nil
}

func errorForRemoteResult(result remoteaccess.RemoteCommandResult) error {
	switch result.Status {
	case remoteaccess.StatusSuccess:
		return nil
	case remoteaccess.StatusFailed:
		if result.ExitCode != nil {
			return fmt.Errorf("remote command failed: exit_code=%d", *result.ExitCode)
		}
		return fmt.Errorf("remote command failed")
	case remoteaccess.StatusBlocked:
		reason := strings.TrimSpace(result.Stderr)
		if reason == "" {
			reason = "blocked"
		}
		return fmt.Errorf("remote command blocked: %s", reason)
	case remoteaccess.StatusTimeout:
		return fmt.Errorf("remote command timed out")
	default:
		return fmt.Errorf("remote command %s", result.Status)
	}
}

func appendExecEnvelopeEvent(sessionID string, envelope safety.RunEnvelope) {
	appendLedgerEvent(ledger.Event{
		Type:      ledger.EventSafetyEnvelope,
		Command:   "exec",
		SessionID: sessionID,
		HostID:    envelope.Request.Host,
		Status:    "ok",
		Payload: ledger.Payload{
			Message: "envelope_id=" + envelope.EnvelopeID + " command=" + strings.Join(envelope.Request.Command, " "),
		},
	})
}

func newExecEnvelopeResult(envelope safety.RunEnvelope) execEnvelopeResult {
	result := execEnvelopeResult{
		EnvelopeID:       envelope.EnvelopeID,
		Host:             envelope.Request.Host,
		Command:          append([]string(nil), envelope.Request.Command...),
		Decision:         envelope.Decision,
		ApprovalRequired: envelope.Decision.ApprovalRequired,
		ExecuteCommand:   "pocket exec --envelope-id " + envelope.EnvelopeID,
	}
	if envelope.Decision.ApprovalRequired {
		result.ApproveCommand = "pocket approve " + envelope.EnvelopeID
		result.ExecuteCommand = "pocket exec --envelope-id " + envelope.EnvelopeID + " --approval-token TOKEN"
	}
	return result
}

func loadHostInventoryForCommand() (fleet.InventoryResult, error) {
	return fleet.LoadInventory()
}

func inventoryOrError(inventory fleet.InventoryResult, err error) any {
	if err != nil {
		return map[string]any{
			"hosts":        []any{},
			"online_count": 0,
			"sources":      []string{},
			"error":        err.Error(),
		}
	}
	return inventory
}
