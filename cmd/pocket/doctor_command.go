package main

import (
	"fmt"

	"github.com/spf13/cobra"
	"pocketcli/internal/doctor"
)

func newDoctorCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor [--json]",
		Short: "Roda checks locais do PocketCli",
		RunE: func(cmd *cobra.Command, args []string) error {
			strict := hasFlag(args, "--strict")
			report := doctor.Run(strict)
			if hasFlag(args, "--json") {
				if err := writeJSON(cmd.OutOrStdout(), report); err != nil {
					return err
				}
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "status=%s\n", report.Status)
				for _, check := range report.Checks {
					fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", check.ID, check.Status, check.Message)
				}
			}
			if report.Status == "error" {
				return fmt.Errorf("doctor status=error")
			}
			return nil
		},
	}
}

func newEvalCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "eval",
		Short: "Roda avaliações locais",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "insights --fixtures <dir>",
		Short: "Valida fixtures de insights",
		RunE: func(cmd *cobra.Command, args []string) error {
			fixtures, ok, err := flagValue(args, "--fixtures")
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("ERR_EVAL_FIXTURES_REQUIRED")
			}
			report, err := doctor.EvalInsights(fixtures)
			if err != nil {
				_ = writeJSON(cmd.OutOrStdout(), report)
				return err
			}
			return writeJSON(cmd.OutOrStdout(), report)
		},
	})
	return cmd
}
