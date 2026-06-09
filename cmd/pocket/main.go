package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"pocketcli/internal/fleet"
	"pocketcli/internal/ledger"
	"pocketcli/internal/safety"
	"pocketcli/internal/ssh"
)

var (
	hostsViewer = defaultHostsViewer
	openSSH     = ssh.Open
	execSSH     = ssh.Exec
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
		Short: "Open SSH session to host",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAudited(cmd, "ssh", args, func(cmd *cobra.Command, args []string, sessionID string) (commandAudit, error) {
				return commandAudit{SessionID: sessionID}, openSSH(args[0])
			})
		},
	}
}

func newExecCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "exec [--prepare] [--envelope-id ID] [--approval-token TOKEN] <host> <command...>",
		Short: "Execute remote command via SSH",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
				return cmd.Help()
			}
			return runAudited(cmd, "exec", args, func(cmd *cobra.Command, args []string, sessionID string) (commandAudit, error) {
				options, err := parseExecArgs(args)
				if err != nil {
					return commandAudit{SessionID: sessionID}, err
				}
				if options.Prepare {
					envelope, err := createExecEnvelope(options.Host, options.Command)
					if err != nil {
						return commandAudit{SessionID: sessionID, HostID: options.Host}, err
					}
					appendExecEnvelopeEvent(sessionID, envelope)
					return commandAudit{SessionID: sessionID, HostID: options.Host}, writeJSON(cmd.OutOrStdout(), newExecEnvelopeResult(envelope))
				}
				if options.EnvelopeID != "" {
					host, command, err := resolvePreparedExec(options)
					if err != nil {
						return commandAudit{SessionID: sessionID, HostID: host}, err
					}
					return runExecSSH(sessionID, host, command)
				}
				if strings.TrimSpace(options.ApprovalToken) != "" {
					return commandAudit{SessionID: sessionID, HostID: options.Host}, fmt.Errorf("flag --approval-token requer --envelope-id")
				}
				if err := ensureDirectExecAllowed(sessionID, options.Host, options.Command); err != nil {
					return commandAudit{SessionID: sessionID, HostID: options.Host}, err
				}
				return runExecSSH(sessionID, options.Host, options.Command)
			})
		},
	}
}

type execOptions struct {
	Prepare       bool
	EnvelopeID    string
	ApprovalToken string
	Host          string
	Command       []string
	Positional    []string
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

func parseExecArgs(args []string) (execOptions, error) {
	options := execOptions{}
	remaining := make([]string, 0, len(args))
	for idx := 0; idx < len(args); idx++ {
		arg := args[idx]
		if arg == "--" {
			remaining = append(remaining, args[idx+1:]...)
			break
		}
		if arg == "--prepare" {
			options.Prepare = true
			continue
		}
		if arg == "--envelope-id" {
			idx++
			if idx >= len(args) {
				return execOptions{}, fmt.Errorf("flag --envelope-id requer valor")
			}
			options.EnvelopeID = args[idx]
			continue
		}
		if strings.HasPrefix(arg, "--envelope-id=") {
			options.EnvelopeID = strings.TrimPrefix(arg, "--envelope-id=")
			if strings.TrimSpace(options.EnvelopeID) == "" {
				return execOptions{}, fmt.Errorf("flag --envelope-id requer valor")
			}
			continue
		}
		if arg == "--approval-token" {
			idx++
			if idx >= len(args) {
				return execOptions{}, fmt.Errorf("flag --approval-token requer valor")
			}
			options.ApprovalToken = args[idx]
			continue
		}
		if strings.HasPrefix(arg, "--approval-token=") {
			options.ApprovalToken = strings.TrimPrefix(arg, "--approval-token=")
			if strings.TrimSpace(options.ApprovalToken) == "" {
				return execOptions{}, fmt.Errorf("flag --approval-token requer valor")
			}
			continue
		}
		remaining = append(remaining, arg)
	}
	options.Positional = append([]string(nil), remaining...)
	if options.Prepare && strings.TrimSpace(options.EnvelopeID) != "" {
		return execOptions{}, fmt.Errorf("--prepare não pode ser usado com --envelope-id")
	}
	if strings.TrimSpace(options.EnvelopeID) != "" {
		if len(remaining) > 0 {
			return execOptions{}, fmt.Errorf("--envelope-id executa o comando salvo; não passe host/comando junto")
		}
		return options, nil
	}
	if len(remaining) < 2 {
		return execOptions{}, fmt.Errorf("requires at least 2 arg(s), received %d", len(remaining))
	}
	options.Host = remaining[0]
	options.Command = append([]string(nil), remaining[1:]...)
	if err := validateExecHost(options.Host); err != nil {
		return execOptions{}, err
	}
	return options, nil
}

func validateExecHost(host string) error {
	host = strings.TrimSpace(host)
	if host == "" {
		return fmt.Errorf("ERR_EXEC_HOST_INVALID")
	}
	if len([]rune(host)) > 256 || strings.ContainsAny(host, " \t\n\r") {
		return fmt.Errorf("ERR_EXEC_HOST_INVALID")
	}
	return nil
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

func resolvePreparedExec(options execOptions) (string, []string, error) {
	envelope, err := safety.LoadEnvelope(options.EnvelopeID)
	if err != nil {
		return "", nil, safety.ErrApprovalNotFound
	}
	if strings.TrimSpace(envelope.Request.Action) != "exec" {
		return envelope.Request.Host, nil, fmt.Errorf("ERR_APPROVAL_ENVELOPE_ACTION_MISMATCH")
	}
	host := strings.TrimSpace(envelope.Request.Host)
	if err := validateExecHost(host); err != nil {
		return host, nil, err
	}
	command := append([]string(nil), envelope.Request.Command...)
	if len(command) == 0 {
		return host, nil, safety.ErrCommandInvalid
	}
	if envelope.Decision.Classification == safety.ClassificationBlocked {
		return host, nil, safety.ErrApprovalBlocked
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
		return host, nil, safety.ErrApprovalBlocked
	}
	if evalErr != nil && evalErr != safety.ErrApprovalRequired {
		appendLedgerEvent(deniedSafetyEvent("exec", command, evalErr.Error()))
		return host, nil, evalErr
	}
	if envelope.Decision.ApprovalRequired || currentDecision.ApprovalRequired || evalErr == safety.ErrApprovalRequired {
		if err := safety.ValidateApproval(envelope.EnvelopeID, options.ApprovalToken); err != nil {
			appendLedgerEvent(deniedSafetyEvent("exec", command, err.Error()))
			return host, nil, err
		}
	}
	return host, command, nil
}

func runExecSSH(sessionID, host string, command []string) (commandAudit, error) {
	remoteCmd := strings.Join(command, " ")
	runErr := execSSH(host, remoteCmd)
	status := "ok"
	if runErr != nil {
		status = "error"
	}
	appendLedgerEvent(ledger.Event{
		Type:      ledger.EventSSHExec,
		Command:   "exec",
		SessionID: sessionID,
		HostID:    host,
		Status:    status,
		Payload:   ledger.Payload{Message: remoteCmd},
	})
	return commandAudit{SessionID: sessionID, HostID: host}, runErr
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
