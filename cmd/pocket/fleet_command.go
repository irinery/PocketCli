package main

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/spf13/cobra"
	"pocketcli/internal/fleet"
	"pocketcli/internal/ledger"
	"pocketcli/internal/remoteaccess"
	"pocketcli/internal/safety"
)

const fleetOutputPreviewRunes = 1024

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
				if err := safety.ConsumeApproval(plan.EnvelopeID, approvalToken); err != nil {
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
	command := strings.Join(plan.Command, " ")
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
		result.Results[idx] = fleet.HostRunResult{HostID: target.HostID, HostAlias: fleetHostAlias(target), Status: "pending"}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			remoteResult := executeFleetTarget(context.Background(), target, command, plan.RequiresApproval)
			hostResult := fleetHostRunResult(target, remoteResult)
			result.Results[idx] = hostResult
			appendLedgerEvent(ledger.Event{
				Type:       ledger.EventSSHExec,
				HostID:     target.HostID,
				Status:     hostResult.Status,
				DurationMS: hostResult.DurationMS,
				Payload:    fleetLedgerPayload(command, remoteResult),
			})
		}()
	}
	wg.Wait()
	return result
}

func executeFleetTarget(ctx context.Context, target fleet.Target, command string, approved bool) remoteaccess.RemoteCommandResult {
	alias := fleetHostAlias(target)
	executor := newRemoteExecutor()
	executor.Resolver = fleetTargetResolver(target, alias)
	result, err := executor.Execute(ctx, remoteaccess.RemoteCommandRequest{
		HostAlias:      alias,
		Command:        command,
		TimeoutSeconds: remoteaccess.DefaultCommandTimeoutSeconds,
		RequestedBy:    remoteaccess.RequestedByHuman,
	}, remoteaccess.ExecuteOptions{Approved: approved})
	if err != nil {
		if result.Status == "" {
			result.Status = remoteaccess.StatusFailed
		}
		if strings.TrimSpace(result.Stderr) == "" {
			result.Stderr = err.Error()
		}
	}
	return result
}

func fleetTargetResolver(target fleet.Target, alias string) remoteaccess.HostResolver {
	return func(ctx context.Context, requested string) (remoteaccess.RemoteHost, error) {
		_ = ctx
		_ = requested
		host, ok, err := lookupConfiguredFleetHost(target, alias)
		if err != nil {
			return remoteaccess.RemoteHost{}, err
		}
		if ok {
			return host, nil
		}
		return remoteHostFromFleetTarget(target, alias), nil
	}
}

func lookupConfiguredFleetHost(target fleet.Target, alias string) (remoteaccess.RemoteHost, bool, error) {
	hosts, err := remoteaccess.DefaultHostStore().Load()
	if err != nil {
		return remoteaccess.RemoteHost{}, false, err
	}
	for _, host := range hosts {
		if fleetHostMatches(target, alias, host) {
			host.Alias = alias
			return host, true, nil
		}
	}
	return remoteaccess.RemoteHost{}, false, nil
}

func fleetHostMatches(target fleet.Target, alias string, host remoteaccess.RemoteHost) bool {
	candidates := []string{alias, target.HostID, target.Hostname, target.Address}
	hostValues := []string{host.Alias, host.Hostname}
	if host.TailscaleIP != nil {
		hostValues = append(hostValues, *host.TailscaleIP)
	}
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		for _, value := range hostValues {
			if strings.EqualFold(candidate, strings.TrimSpace(value)) {
				return true
			}
		}
	}
	return false
}

func remoteHostFromFleetTarget(target fleet.Target, alias string) remoteaccess.RemoteHost {
	hostname := strings.TrimSpace(target.Hostname)
	address := strings.TrimSpace(target.Address)
	if hostname == "" {
		hostname = alias
	}
	host := remoteaccess.RemoteHost{
		Alias:        alias,
		Hostname:     hostname,
		OSFamily:     remoteaccess.OSFamilyUnknown,
		AccessMethod: remoteaccess.AccessMethodSSH,
		SSHPort:      22,
		Enabled:      true,
	}
	if address != "" {
		host.TailscaleIP = &address
	}
	return host
}

func fleetHostAlias(target fleet.Target) string {
	for _, candidate := range []string{target.HostID, target.Hostname} {
		candidate = strings.TrimSpace(candidate)
		if remoteaccess.ValidateHostAlias(candidate) == nil {
			return candidate
		}
	}
	var builder strings.Builder
	for _, r := range target.HostID + "-" + target.Hostname {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '_' || r == '-':
			builder.WriteRune(r)
		}
		if builder.Len() >= 64 {
			break
		}
	}
	alias := strings.Trim(builder.String(), "-_")
	if alias == "" {
		return "fleet-host"
	}
	return alias
}

func fleetHostRunResult(target fleet.Target, remoteResult remoteaccess.RemoteCommandResult) fleet.HostRunResult {
	hostResult := fleet.HostRunResult{
		HostID:        target.HostID,
		HostAlias:     remoteResult.HostAlias,
		CommandID:     remoteResult.CommandID,
		Status:        fleetStatusFromRemote(remoteResult.Status),
		RemoteStatus:  string(remoteResult.Status),
		DurationMS:    remoteResult.DurationMS,
		OutputPreview: fleetOutputPreview(remoteResult.Stdout),
		StderrPreview: fleetOutputPreview(remoteResult.Stderr),
		Truncated:     remoteResult.Truncated,
	}
	if remoteResult.ExitCode != nil {
		hostResult.ExitCode = *remoteResult.ExitCode
	}
	if string(remoteResult.PolicyDecision.Decision) != "" {
		hostResult.PolicyDecision = remoteResult.PolicyDecision
	}
	return hostResult
}

func fleetStatusFromRemote(status remoteaccess.ResultStatus) string {
	switch status {
	case remoteaccess.StatusSuccess:
		return "ok"
	case "":
		return "failed"
	default:
		return string(status)
	}
}

func fleetOutputPreview(value string) string {
	if value == "" {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= fleetOutputPreviewRunes {
		return value
	}
	return string(runes[:fleetOutputPreviewRunes]) + "\n[preview truncated]"
}

func fleetLedgerPayload(command string, remoteResult remoteaccess.RemoteCommandResult) ledger.Payload {
	payload := ledger.Payload{Message: command}
	if remoteResult.Status != remoteaccess.StatusSuccess {
		payload.ErrorCode = string(remoteResult.Status)
	}
	return payload
}
