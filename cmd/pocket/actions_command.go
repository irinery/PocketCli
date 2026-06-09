package main

import (
	"fmt"

	"github.com/spf13/cobra"
	"pocketcli/internal/actions"
	"pocketcli/internal/capabilities"
)

func newActionsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "actions [--json]",
		Short: "Lista ações disponíveis por capacidade",
		RunE: func(cmd *cobra.Command, args []string) error {
			req := actions.ResolveRequest{
				Surface:         "menu",
				IncludeDisabled: hasFlag(args, "--include-disabled"),
			}
			if value, ok, err := flagValue(args, "--surface"); err != nil {
				return err
			} else if ok {
				req.Surface = value
			}
			if value, ok, err := flagValue(args, "--query"); err != nil {
				return err
			} else if ok {
				req.Query = value
			}
			manifest := capabilities.LoadOrDetect()
			result, err := actions.Resolve(req, manifest)
			if err != nil {
				return err
			}
			if hasFlag(args, "--json") {
				return writeJSON(cmd.OutOrStdout(), result)
			}
			for _, action := range result.Actions {
				status := "enabled"
				if !action.Enabled {
					status = "disabled:" + action.DisabledReason
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", action.ID, action.Title, status)
			}
			return nil
		},
	}
}
