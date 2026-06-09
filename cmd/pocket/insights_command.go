package main

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
	"pocketcli/internal/insights"
)

func newInsightsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "insights",
		Short: "Lista insights operacionais locais",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newInsightsListCommand())
	cmd.AddCommand(newInsightsExplainCommand())
	return cmd
}

func newInsightsListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list [--json]",
		Short: "Lista insights ativos",
		RunE: func(cmd *cobra.Command, args []string) error {
			req, jsonOut, limit, err := parseInsightsArgs(args)
			if err != nil {
				return err
			}
			result, err := insights.Compute(req)
			if err != nil {
				return err
			}
			if limit > 0 && len(result.Insights) > limit {
				result.Insights = result.Insights[:limit]
				result.Summary.Total = len(result.Insights)
			}
			if jsonOut {
				return writeJSON(cmd.OutOrStdout(), result)
			}
			for _, insight := range result.Insights {
				fmt.Fprintf(cmd.OutOrStdout(), "id=%s kind=%s severity=%s confidence=%d title=%q action=%q\n", insight.ID, insight.Kind, insight.Severity, insight.Confidence, insight.Title, insight.RecommendedAction)
			}
			if len(result.Insights) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "nenhum insight ativo")
			}
			return nil
		},
	}
}

func newInsightsExplainCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "explain <id>",
		Short: "Mostra detalhes de um insight",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := insights.Compute(insights.Request{Scope: "all", TimeWindowMinutes: 1440})
			if err != nil {
				return err
			}
			for _, insight := range result.Insights {
				if insight.ID == args[0] {
					return writeJSON(cmd.OutOrStdout(), insight)
				}
			}
			return fmt.Errorf("insight não encontrado: %s", args[0])
		},
	}
}

func parseInsightsArgs(args []string) (insights.Request, bool, int, error) {
	req := insights.Request{Scope: "active", TimeWindowMinutes: 1440}
	jsonOut := false
	limit := 0
	for idx := 0; idx < len(args); idx++ {
		arg := args[idx]
		if arg == "--json" {
			jsonOut = true
			continue
		}
		name, value, consumesNext, err := parseFlagValue(arg)
		if err != nil {
			return req, jsonOut, limit, err
		}
		if consumesNext {
			idx++
			if idx >= len(args) {
				return req, jsonOut, limit, fmt.Errorf("flag %s requer valor", name)
			}
			value = args[idx]
		}
		switch name {
		case "--scope":
			req.Scope = value
		case "--host":
			req.HostID = value
		case "--project":
			req.ProjectPath = value
		case "--limit":
			parsed, err := strconv.Atoi(value)
			if err != nil {
				return req, jsonOut, limit, err
			}
			limit = parsed
		default:
			return req, jsonOut, limit, fmt.Errorf("flag inválida: %s", name)
		}
	}
	return req, jsonOut, limit, nil
}
