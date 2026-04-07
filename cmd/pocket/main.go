package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
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
	rootCmd.AddCommand(newHostsCommand())
	rootCmd.AddCommand(newSSHCommand())
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
		Use:   "exec <host> <command...>",
		Short: "Execute remote command via SSH",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAudited(cmd, "exec", args, func(cmd *cobra.Command, args []string, sessionID string) (commandAudit, error) {
				host := args[0]
				remoteCmd := strings.Join(args[1:], " ")
				return commandAudit{SessionID: sessionID}, execSSH(host, remoteCmd)
			})
		},
	}
}
