package main

import (
	"fmt"
	"io"
	"slices"

	"pocketcli/internal/investigation/contract"
)

// applyInvestigationFlags returns the effective input and the differences from
// an optional YAML plan. Scope is copied because callers may keep the original
// input to audit which fields the flags replaced.
func applyInvestigationFlags(input contract.PlanInput, f investigationFlags) (contract.PlanInput, []string, error) {
	before := input
	if input.Scope != nil {
		scope := *input.Scope
		scope.Repositories = slices.Clone(scope.Repositories)
		scope.Teams = slices.Clone(scope.Teams)
		input.Scope = &scope
	}
	if f.period != "" {
		input.Period.Preset = f.period
		if f.period != "custom" {
			if f.from != "" || f.to != "" {
				return input, nil, fmt.Errorf("ERR_VALIDATION: --from e --to exigem --period custom")
			}
			input.Period.FromDate = ""
			input.Period.ToDate = ""
		}
	}
	if f.from != "" {
		input.Period.FromDate = f.from
	}
	if f.to != "" {
		input.Period.ToDate = f.to
	}
	if f.timezone != "" {
		input.Period.Timezone = f.timezone
	}
	if f.investigation != "" {
		input.InvestigationID = f.investigation
	}
	if f.noGitHub {
		input.GitHubEnabled = false
	}
	if f.noLinear {
		input.LinearEnabled = false
	}
	if len(f.repos) > 0 {
		input.Scope.Mode = "selected"
		input.Scope.Repositories = slices.Clone(f.repos)
	}
	if len(f.teams) > 0 {
		input.Scope.Mode = "selected"
		input.Scope.Teams = slices.Clone(f.teams)
	}
	if input.Period.Preset != "" && input.Period.Preset != "custom" && (input.Period.FromDate != "" || input.Period.ToDate != "") {
		return input, nil, fmt.Errorf("ERR_VALIDATION: --from e --to exigem período custom")
	}
	if f.scope == "" {
		return input, nil, nil
	}
	var changes []string
	add := func(field string, old, current any) {
		if fmt.Sprint(old) != fmt.Sprint(current) {
			changes = append(changes, fmt.Sprintf("%s: %#v → %#v", field, old, current))
		}
	}
	add("period.preset", before.Period.Preset, input.Period.Preset)
	add("period.from", before.Period.FromDate, input.Period.FromDate)
	add("period.to", before.Period.ToDate, input.Period.ToDate)
	add("period.timezone", before.Period.Timezone, input.Period.Timezone)
	add("investigation_id", before.InvestigationID, input.InvestigationID)
	add("sources.github", before.GitHubEnabled, input.GitHubEnabled)
	add("sources.linear", before.LinearEnabled, input.LinearEnabled)
	if before.Scope != nil && input.Scope != nil {
		add("scope.mode", before.Scope.Mode, input.Scope.Mode)
		if !slices.Equal(before.Scope.Repositories, input.Scope.Repositories) {
			changes = append(changes, fmt.Sprintf("scope.repositories: %#v → %#v", before.Scope.Repositories, input.Scope.Repositories))
		}
		if !slices.Equal(before.Scope.Teams, input.Scope.Teams) {
			changes = append(changes, fmt.Sprintf("scope.teams: %#v → %#v", before.Scope.Teams, input.Scope.Teams))
		}
	}
	return input, changes, nil
}

func printInvestigationOverrides(out io.Writer, changes []string) error {
	if len(changes) == 0 {
		return nil
	}
	if _, err := io.WriteString(out, "Overrides do YAML por flags:\n"); err != nil {
		return err
	}
	for _, change := range changes {
		if _, err := fmt.Fprintf(out, "  %s\n", change); err != nil {
			return err
		}
	}
	return nil
}
