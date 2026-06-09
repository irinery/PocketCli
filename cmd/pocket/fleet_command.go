package main

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"pocketcli/internal/fleet"
	"pocketcli/internal/ledger"
	"pocketcli/internal/safety"
)

func newFleetCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fleet",
		Short: "Planeja e executa comandos em múltiplos hosts",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newFleetPlanCommand())
	cmd.AddCommand(newFleetExecCommand())
	return cmd
}

func newFleetPlanCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "plan --selector <selector> -- <command...>",
		Short: "Cria um plano de execução em hosts",
		RunE: func(cmd *cobra.Command, args []string) error {
			selector, command, maxParallel, err := parseFleetPlanArgs(args)
			if err != nil {
				return err
			}
			plan, err := fleet.CreatePlan(selector, command, maxParallel)
			if err != nil {
				return err
			}
			return writeJSON(cmd.OutOrStdout(), plan)
		},
	}
}

func newFleetExecCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "exec [--plan-id ID] [--selector S -- command...]",
		Short: "Executa um plano fleet",
		RunE: func(cmd *cobra.Command, args []string) error {
			plan, approvalToken, err := resolveFleetExecPlan(args)
			if err != nil {
				return err
			}
			if plan.RequiresApproval {
				if strings.TrimSpace(plan.EnvelopeID) == "" {
					return safety.ErrApprovalRequired
				}
				if err := safety.ValidateApproval(plan.EnvelopeID, approvalToken); err != nil {
					return err
				}
			}
			result := runFleetPlan(plan)
			return writeJSON(cmd.OutOrStdout(), result)
		},
	}
}

func parseFleetPlanArgs(args []string) (string, []string, int, error) {
	selector := ""
	maxParallel := 4
	var command []string
	for idx := 0; idx < len(args); idx++ {
		arg := args[idx]
		if arg == "--" {
			command = append([]string(nil), args[idx+1:]...)
			break
		}
		name, value, consumesNext, err := parseFlagValue(arg)
		if err != nil {
			return "", nil, 0, err
		}
		if consumesNext {
			idx++
			if idx >= len(args) {
				return "", nil, 0, fmt.Errorf("flag %s requer valor", name)
			}
			value = args[idx]
		}
		switch name {
		case "--selector":
			selector = value
		case "--max-parallel":
			if _, err := fmt.Sscanf(value, "%d", &maxParallel); err != nil {
				return "", nil, 0, err
			}
		default:
			return "", nil, 0, fmt.Errorf("flag inválida: %s", name)
		}
	}
	if selector == "" {
		return "", nil, 0, fmt.Errorf("ERR_FLEET_BAD_SELECTOR")
	}
	if len(command) == 0 {
		return "", nil, 0, fmt.Errorf("ERR_FLEET_COMMAND_UNSAFE")
	}
	return selector, command, maxParallel, nil
}

func resolveFleetExecPlan(args []string) (fleet.Plan, string, error) {
	approvalToken := ""
	if value, ok, err := flagValue(args, "--approval-token"); err != nil {
		return fleet.Plan{}, "", err
	} else if ok {
		approvalToken = value
	}
	if value, ok, err := flagValue(args, "--plan-id"); err != nil {
		return fleet.Plan{}, "", err
	} else if ok {
		plan, err := fleet.LoadPlan(value)
		return plan, approvalToken, err
	}
	selector, command, maxParallel, err := parseFleetPlanArgs(args)
	if err != nil {
		return fleet.Plan{}, "", err
	}
	plan, err := fleet.CreatePlan(selector, command, maxParallel)
	return plan, approvalToken, err
}

func runFleetPlan(plan fleet.Plan) fleet.RunResult {
	result := fleet.RunResult{
		RunID:   newRuntimeID(),
		PlanID:  plan.PlanID,
		Results: make([]fleet.HostRunResult, len(plan.Targets)),
	}
	parallel := plan.MaxParallel
	if parallel <= 0 {
		parallel = 4
	}
	if parallel > 16 {
		parallel = 16
	}
	sem := make(chan struct{}, parallel)
	var wg sync.WaitGroup
	for idx, target := range plan.Targets {
		idx, target := idx, target
		result.Results[idx] = fleet.HostRunResult{HostID: target.HostID, Status: "pending"}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			start := time.Now()
			host := target.Hostname
			if target.Address != "" {
				host = target.Address
			}
			err := execSSH(host, strings.Join(plan.Command, " "))
			status := "ok"
			exitCode := 0
			if err != nil {
				status = "failed"
				exitCode = 1
			}
			result.Results[idx] = fleet.HostRunResult{HostID: target.HostID, Status: status, ExitCode: exitCode, DurationMS: int(time.Since(start) / time.Millisecond)}
			appendLedgerEvent(ledger.Event{
				Type:       ledger.EventSSHExec,
				HostID:     target.HostID,
				Status:     status,
				DurationMS: int(time.Since(start) / time.Millisecond),
				Payload:    ledger.Payload{Message: strings.Join(plan.Command, " ")},
			})
		}()
	}
	wg.Wait()
	return result
}
