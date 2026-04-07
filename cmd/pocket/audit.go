package main

import (
	"strings"
	"time"

	"github.com/spf13/cobra"
	"pocketcli/internal/audit"
	"pocketcli/internal/backend"
	"pocketcli/internal/router"
)

var (
	newAuditLogger    = audit.NewLogger
	newAuditSessionID = audit.NewSessionID
)

type commandAudit struct {
	SessionID string
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

	return runErr
}
