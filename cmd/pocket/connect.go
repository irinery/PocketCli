package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"pocketcli/internal/connect"
)

var newConnectOrchestrator = connect.New
var errHostsTUIInterrupted = errors.New("hosts TUI interrupted")

func newConnectCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "connect <host>",
		Short: "Abre uma sessão tmux nomeada com SSH para o host resolvido no Tailscale",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAudited(cmd, "connect", args, func(cmd *cobra.Command, args []string, sessionID string) (commandAudit, error) {
				orchestrator := newConnectOrchestrator()
				orchestrator.In = os.Stdin
				orchestrator.Out = cmd.OutOrStdout()
				orchestrator.Err = os.Stderr
				return commandAudit{SessionID: sessionID}, orchestrator.Connect(context.Background(), args[0])
			})
		},
	}
}

func newConnectPaneCommand() *cobra.Command {
	return &cobra.Command{
		Use:    "__connect-pane --session <name> --host <host> --ip <ip> --started-at <timestamp>",
		Hidden: true,
		Args:   cobra.ExactArgs(8),
		RunE: func(cmd *cobra.Command, args []string) error {
			values, err := parseConnectPaneArgs(args)
			if err != nil {
				return &connect.ExitError{
					Code:    connect.ExitCodeInvalidInput,
					Message: err.Error(),
				}
			}

			parsedStartedAt, err := time.Parse(time.RFC3339, values["--started-at"])
			if err != nil {
				return &connect.ExitError{
					Code:    connect.ExitCodeInvalidInput,
					Message: "pocket: parâmetro started_at inválido",
				}
			}

			orchestrator := newConnectOrchestrator()
			orchestrator.In = os.Stdin
			orchestrator.Out = os.Stdout
			orchestrator.Err = os.Stderr

			return orchestrator.RunPane(context.Background(), connect.PaneRequest{
				SessionName: values["--session"],
				Host:        values["--host"],
				IP:          values["--ip"],
				StartedAt:   parsedStartedAt,
			})
		},
	}
}

func exitCodeForError(err error) (int, bool) {
	var exitErr *connect.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), true
	}
	if errors.Is(err, errHostsTUIInterrupted) {
		return 1, true
	}
	return 0, false
}

func shouldPrintError(err error) bool {
	var exitErr *connect.ExitError
	if errors.As(err, &exitErr) {
		return !exitErr.Printed && exitErr.Error() != ""
	}
	return !errors.Is(err, errHostsTUIInterrupted)
}

func parseConnectPaneArgs(args []string) (map[string]string, error) {
	expected := map[string]struct{}{
		"--session":    {},
		"--host":       {},
		"--ip":         {},
		"--started-at": {},
	}

	values := make(map[string]string, len(expected))
	if len(args)%2 != 0 {
		return nil, fmt.Errorf("pocket: parâmetros internos inválidos")
	}

	for index := 0; index < len(args); index += 2 {
		key := strings.TrimSpace(args[index])
		value := strings.TrimSpace(args[index+1])
		if _, ok := expected[key]; !ok {
			return nil, fmt.Errorf("pocket: parâmetro interno inválido: %s", key)
		}
		if value == "" {
			return nil, fmt.Errorf("pocket: parâmetro interno ausente: %s", key)
		}
		values[key] = value
	}

	for key := range expected {
		if strings.TrimSpace(values[key]) == "" {
			return nil, fmt.Errorf("pocket: parâmetro interno ausente: %s", key)
		}
	}

	return values, nil
}
