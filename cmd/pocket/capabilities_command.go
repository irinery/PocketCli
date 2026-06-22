package main

import (
	"fmt"

	"github.com/spf13/cobra"
	"pocketcli/internal/capabilities"
)

func newCapabilitiesCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "capabilities [--json]",
		Short: "Mostra capacidades locais do PocketCli",
		RunE: func(cmd *cobra.Command, args []string) error {
			manifest, err := capabilities.Detect("")
			if err != nil {
				return err
			}
			manifest, _ = capabilities.Save(manifest)
			if hasFlag(args, "--json") || hasFlag(args, "--format=json") {
				return writeJSON(cmd.OutOrStdout(), manifest)
			}
			_, err = fmt.Fprintf(
				cmd.OutOrStdout(),
				"mode=%s layout=%s ssh=%t tailscale=%t tmux=%t degraded=%v\n",
				manifest.ModeEffective,
				manifest.Terminal.TUILayout,
				manifest.Capabilities.HasSSH,
				manifest.Capabilities.HasTailscale,
				manifest.Capabilities.HasTMUX,
				manifest.DegradationReasons,
			)
			return err
		},
	}
}
