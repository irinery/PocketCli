package main

import (
	"strings"
	"time"

	"github.com/spf13/cobra"
	"pocketcli/internal/audit"
	"pocketcli/internal/backend"
	"pocketcli/internal/ledger"
	"pocketcli/internal/router"
)

var (
	newAuditLogger    = audit.NewLogger
	newAuditSessionID = audit.NewSessionID
)

type commandAudit struct {
	SessionID string
	HostID    string
	Decision  router.Decision
	Response  backend.LLMResponse
	MemoryHit bool
	LatencyMS int
}

type auditedRun func(cmd *cobra.Command, args []string, sessionID string) (commandAudit, error)

func runAudited(cmd *cobra.Command, commandName string, args []string, run auditedRun) error {
	startedAt := time.Now().UTC()

	sessionID := ""
	if generatedSessionID, err := newAuditSessionID(); err == nil {
		sessionID = generatedSessionID
	}

	logger, err := newAuditLogger()
	if err == nil && logger != nil {
		_ = logger.Prepare()
	}

	appendLedgerEvent(ledger.Event{
		Type:      ledger.EventCommandStarted,
		Command:   commandName,
		SessionID: sessionID,
		Status:    "ok",
	})

	result, runErr := run(cmd, args, sessionID)
	if strings.TrimSpace(result.SessionID) == "" {
		result.SessionID = sessionID
	}
	if result.LatencyMS <= 0 {
		result.LatencyMS = int(time.Since(startedAt) / time.Millisecond)
	}

	if err == nil && logger != nil {
		_ = logger.Write(audit.Record{
			Timestamp:      startedAt,
			Command:        commandName,
			SessionID:      result.SessionID,
			RouterDecision: result.Decision,
			Response:       result.Response,
			MemoryHit:      result.MemoryHit,
			LatencyMS:      result.LatencyMS,
		})
	}

	status := "ok"
	eventType := ledger.EventCommandCompleted
	message := ""
	if runErr != nil {
		status = "error"
		eventType = ledger.EventCommandFailed
		message = runErr.Error()
	}
	appendLedgerEvent(ledger.Event{
		Type:       eventType,
		Command:    commandName,
		SessionID:  result.SessionID,
		HostID:     result.HostID,
		Status:     status,
		DurationMS: result.LatencyMS,
		Payload: ledger.Payload{
			Message:    message,
			Backend:    result.Response.Backend,
			Model:      result.Response.Model,
			TokenUsage: result.Response.TokenUsage,
		},
	})
	if strings.TrimSpace(result.Response.Backend) != "" || strings.TrimSpace(result.Decision.SelectedBackend) != "" {
		appendLedgerEvent(ledger.Event{
			Type:       ledger.EventBackendCall,
			Command:    commandName,
			SessionID:  result.SessionID,
			HostID:     result.HostID,
			Status:     status,
			DurationMS: result.LatencyMS,
			Payload: ledger.Payload{
				Message:    "fallback_occurred=" + boolString(result.Decision.FallbackOccurred) + " reason=" + result.Decision.Reason,
				Backend:    result.Response.Backend,
				Model:      result.Response.Model,
				TokenUsage: result.Response.TokenUsage,
			},
		})
	}

	return runErr
}

func appendLedgerEvent(event ledger.Event) {
	store, err := ledger.NewStore()
	if err != nil {
		return
	}
	_, _ = store.Append(event)
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
