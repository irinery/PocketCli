package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"pocketcli/internal/remoteaccess"
	"pocketcli/internal/ssh"
)

var (
	hostsViewer       = defaultHostsViewer
	openSSH           = ssh.Open
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
	rootCmd.AddCommand(newHostsCommand())
	rootCmd.AddCommand(newSSHCommand())
	rootCmd.AddCommand(newConnectCommand())
	rootCmd.AddCommand(newConnectPaneCommand())
	rootCmd.AddCommand(newExecCommand())

	return rootCmd
}

func newHostsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "hosts",
		Short: "List, filter and connect to Tailscale machines",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAudited(cmd, "hosts", args, func(cmd *cobra.Command, args []string, sessionID string) (commandAudit, error) {
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
		Use:   "exec [--json] [--timeout N] [--requested-by human|llm_plan] [--session-id ID] [--approve] <host> <command...>",
		Short: "Execute remote command via SSH",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAudited(cmd, "exec", args, func(cmd *cobra.Command, args []string, sessionID string) (commandAudit, error) {
				parsed, err := parseExecArgs(args)
				if err != nil {
					return commandAudit{SessionID: sessionID}, err
				}
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
				return commandAudit{SessionID: result.SessionID, LatencyMS: result.DurationMS}, err
			})
		},
	}
}

type execArgs struct {
	host           string
	command        string
	timeoutSeconds int
	requestedBy    remoteaccess.RequestedBy
	sessionID      string
	approved       bool
	jsonOutput     bool
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
		case arg == "--json":
			parsed.jsonOutput = true
			index++
		case arg == "--approve":
			parsed.approved = true
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
			parsed.sessionID = args[index+1]
			index += 2
		case strings.HasPrefix(arg, "--session-id="):
			parsed.sessionID = strings.TrimPrefix(arg, "--session-id=")
			index++
		default:
			return parsed, fmt.Errorf("pocket: opção inválida para exec: %s", arg)
		}
	}

	if index >= len(args) {
		return parsed, fmt.Errorf("Usage: pocket exec <host> <command...>")
	}
	parsed.host = args[index]
	index++
	if index >= len(args) {
		return parsed, fmt.Errorf("Usage: pocket exec <host> <command...>")
	}
	parsed.command = strings.Join(args[index:], " ")
	return parsed, nil
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

func printRemoteExecResult(cmd *cobra.Command, result remoteaccess.RemoteCommandResult, jsonOutput bool) error {
	if jsonOutput {
		encoder := json.NewEncoder(cmd.OutOrStdout())
		return encoder.Encode(result)
	}

	if result.Stdout != "" {
		fmt.Fprint(cmd.OutOrStdout(), result.Stdout)
	}
	if result.Stderr != "" && (result.Status == remoteaccess.StatusFailed || result.Status == remoteaccess.StatusTimeout) {
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
