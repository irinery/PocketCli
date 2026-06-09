package main

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
	"pocketcli/internal/ledger"
)

func newLedgerCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ledger",
		Short: "Busca e reconstrói o ledger operacional local",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newLedgerSearchCommand())
	cmd.AddCommand(newLedgerRebuildCommand())
	return cmd
}

func newLedgerSearchCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "search [flags]",
		Short: "Busca eventos no ledger",
		RunE: func(cmd *cobra.Command, args []string) error {
			filter, jsonOut, err := parseLedgerSearchArgs(args)
			if err != nil {
				return err
			}
			store, err := ledger.NewStore()
			if err != nil {
				return err
			}
			result, err := store.Search(filter)
			if err != nil {
				return err
			}
			if jsonOut {
				return writeJSON(cmd.OutOrStdout(), result)
			}
			for _, event := range result.Events {
				fmt.Fprintf(cmd.OutOrStdout(), "%s type=%s command=%s status=%s session_id=%s host_id=%s\n", event.Timestamp, event.Type, event.Command, event.Status, event.SessionID, event.HostID)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "truncated=%t index_status=%s\n", result.Truncated, result.IndexStatus)
			return nil
		},
	}
}

func newLedgerRebuildCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "rebuild-index",
		Short: "Reconstrói o índice de sessões do ledger",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := ledger.NewStore()
			if err != nil {
				return err
			}
			result, err := store.RebuildIndex()
			if err != nil {
				return err
			}
			if hasFlag(args, "--json") {
				return writeJSON(cmd.OutOrStdout(), result)
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "events_indexed=%d skipped_lines=%d\n", result.EventsIndexed, result.SkippedLines)
			return err
		},
	}
}

func parseLedgerSearchArgs(args []string) (ledger.SearchFilter, bool, error) {
	filter := ledger.SearchFilter{Limit: 50}
	jsonOut := false
	for idx := 0; idx < len(args); idx++ {
		arg := args[idx]
		if arg == "--json" {
			jsonOut = true
			continue
		}
		name, value, consumesNext, err := parseFlagValue(arg)
		if err != nil {
			return filter, jsonOut, err
		}
		if consumesNext {
			idx++
			if idx >= len(args) {
				return filter, jsonOut, fmt.Errorf("flag %s requer valor", name)
			}
			value = args[idx]
		}
		switch name {
		case "--session-id":
			filter.SessionID = value
		case "--host-id":
			filter.HostID = value
		case "--since":
			filter.Since = value
		case "--limit":
			limit, err := strconv.Atoi(value)
			if err != nil {
				return filter, jsonOut, err
			}
			filter.Limit = limit
		default:
			return filter, jsonOut, fmt.Errorf("flag inválida: %s", name)
		}
	}
	return filter, jsonOut, nil
}
