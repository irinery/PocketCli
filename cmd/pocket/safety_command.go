package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"pocketcli/internal/ledger"
	"pocketcli/internal/safety"
)

func newApproveCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "approve [--duration-seconds N] <envelope_id>",
		Short: "Emite token temporário de aprovação para um envelope",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
				return cmd.Help()
			}
			envelopeID, duration, err := parseApproveArgs(args)
			if err != nil {
				return err
			}
			token, err := safety.Approve(envelopeID, duration, stdinInteractive())
			if err != nil {
				return err
			}
			return writeJSON(cmd.OutOrStdout(), token)
		},
	}
}

func parseApproveArgs(args []string) (string, int, error) {
	duration := safety.DefaultApprovalTTL
	envelopeID := ""
	for idx := 0; idx < len(args); idx++ {
		arg := args[idx]
		if arg == "--duration-seconds" {
			idx++
			if idx >= len(args) {
				return "", 0, fmt.Errorf("flag --duration-seconds requer valor")
			}
			if _, err := fmt.Sscanf(args[idx], "%d", &duration); err != nil {
				return "", 0, err
			}
			continue
		}
		if strings.HasPrefix(arg, "--duration-seconds=") {
			value := strings.TrimPrefix(arg, "--duration-seconds=")
			if _, err := fmt.Sscanf(value, "%d", &duration); err != nil {
				return "", 0, err
			}
			continue
		}
		if strings.HasPrefix(arg, "--") {
			return "", 0, fmt.Errorf("flag inválida: %s", arg)
		}
		if envelopeID != "" {
			return "", 0, fmt.Errorf("approve aceita apenas um envelope_id")
		}
		envelopeID = arg
	}
	if strings.TrimSpace(envelopeID) == "" {
		return "", 0, fmt.Errorf("requires at least 1 arg(s), received 0")
	}
	return envelopeID, duration, nil
}

func ensureCommandAllowed(action string, command []string, hostCount int, approvalToken string) (string, error) {
	decision, err := safety.Evaluate(safety.Request{
		Action:      action,
		Command:     command,
		HostCount:   hostCount,
		Interactive: stdinInteractive(),
	})
	if err == nil && !decision.ApprovalRequired {
		return "", nil
	}
	if decision.Classification == safety.ClassificationBlocked {
		appendLedgerEvent(deniedSafetyEvent(action, command, "blocked"))
		return "", safety.ErrApprovalBlocked
	}
	envelope, envErr := safety.CreateRunEnvelope(safety.Request{
		Action:      action,
		Command:     command,
		HostCount:   hostCount,
		Interactive: true,
	})
	if envErr != nil {
		appendLedgerEvent(deniedSafetyEvent(action, command, envErr.Error()))
		return "", envErr
	}
	if approvalToken == "" {
		appendLedgerEvent(deniedSafetyEvent(action, command, "approval_required envelope_id="+envelope.EnvelopeID))
		return envelope.EnvelopeID, fmt.Errorf("%w envelope_id=%s", safety.ErrApprovalRequired, envelope.EnvelopeID)
	}
	if err := safety.ValidateApproval(envelope.EnvelopeID, approvalToken); err != nil {
		appendLedgerEvent(deniedSafetyEvent(action, command, err.Error()))
		return envelope.EnvelopeID, err
	}
	return envelope.EnvelopeID, nil
}

func deniedSafetyEvent(action string, command []string, message string) ledger.Event {
	return ledger.Event{
		Type:    ledger.EventSafetyDenied,
		Command: action,
		Status:  "denied",
		Payload: ledger.Payload{Message: message + " command=" + strings.Join(command, " ")},
	}
}

func stdinInteractive() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func newRuntimeID() string {
	return fmt.Sprintf("%d", time.Now().UTC().UnixNano())
}
