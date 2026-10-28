package main

import (
	"fmt"
	"io"
	"regexp"
	"strings"

	"pocketcli/internal/investigation/contract"
)

// Read only one byte at a time so a pasted next answer is not buffered away
// from the masked credential prompt, which reads directly from the TTY.
func promptLineFrom(in io.Reader, out io.Writer, label string) (string, error) {
	if _, err := io.WriteString(out, label); err != nil {
		return "", err
	}
	var data []byte
	var one [1]byte
	for {
		n, err := in.Read(one[:])
		if n > 0 {
			if one[0] == '\n' {
				return strings.TrimSpace(string(data)), nil
			}
			data = append(data, one[0])
			if len(data) > 4096 {
				return "", fmt.Errorf("ERR_VALIDATION: resposta interativa acima de 4096 bytes")
			}
		}
		if err != nil {
			return "", err
		}
	}
}

func shouldPromptInvestigationWizard(f investigationFlags, interactive bool) bool {
	return interactive && f.scope == "" && f.period == "" && f.from == "" && f.to == "" && f.timezone == "" && f.confirm == "" && f.investigation == "" && len(f.repos) == 0 && len(f.teams) == 0 && !f.noGitHub && !f.noLinear && !f.github && !f.linear
}

// promptInvestigationWizard asks only what changes the plan: sources, their
// scope, GitHub modules and the period. Enter keeps each default; the name,
// timezone and YAML scope remain available as flags (--timezone, --scope).
func promptInvestigationWizard(in io.Reader, out io.Writer, input contract.PlanInput) (contract.PlanInput, error) {
	p := newPalette(out)
	if _, err := io.WriteString(out, p.dim("Coleta pelas APIs do GitHub e do Linear, somente leitura. Enter aceita o padrão de cada pergunta.")+"\n"); err != nil {
		return input, err
	}
	ask := func(question, hint string) (string, error) {
		return promptLineFrom(in, out, p.bold(question)+" "+p.dim(hint)+": ")
	}
	sources, err := ask("Fontes", "[github,linear]")
	if err != nil {
		return input, err
	}
	sourceNames, err := wizardNames(sources, []string{"github", "linear"}, []string{"github", "linear"})
	if err != nil {
		return input, err
	}
	input.GitHubEnabled, input.LinearEnabled = sourceNames["github"], sourceNames["linear"]
	input.Scope = &contract.Scope{Mode: "all_authorized", Repositories: []string{}, Teams: []string{}}
	if input.GitHubEnabled {
		value, err := ask("Repositórios GitHub", "(owner/repo, vírgula; Enter = todos autorizados)")
		if err != nil {
			return input, err
		}
		if input.Scope.Repositories, err = wizardCSV(value); err != nil {
			return input, err
		}
	}
	if input.LinearEnabled {
		value, err := ask("Teams Linear", "(chave ou ID, vírgula; Enter = todos autorizados)")
		if err != nil {
			return input, err
		}
		if input.Scope.Teams, err = wizardCSV(value); err != nil {
			return input, err
		}
	}
	if len(input.Scope.Repositories)+len(input.Scope.Teams) > 0 {
		input.Scope.Mode = "selected"
	}
	input.Modules = contract.Modules{}
	switch {
	case input.GitHubEnabled:
		modules, err := ask("Módulos", "[github_issues,code_history] (+ci, deployments, runtime_import)")
		if err != nil {
			return input, err
		}
		moduleNames, err := wizardNames(modules, []string{"github_issues", "code_history"}, []string{"github_issues", "code_history", "ci", "deployments", "runtime_import"})
		if err != nil {
			return input, err
		}
		input.Modules = contract.Modules{GitHubIssues: moduleNames["github_issues"], CodeHistory: moduleNames["code_history"], CI: moduleNames["ci"], Deployments: moduleNames["deployments"], RuntimeImport: moduleNames["runtime_import"]}
	case !input.LinearEnabled:
		input.Modules.RuntimeImport = true // "none": only a local runtime import
	}
	period, err := ask("Período", "[30d] (12h, 7d, 90d, 1y, all, custom)")
	if err != nil {
		return input, err
	}
	if period == "" {
		period = "30d"
	}
	if period != "all" && period != "custom" && period != "1y" && !wizardRelativePeriod.MatchString(period) {
		return input, fmt.Errorf("ERR_PERIOD_REQUIRED: use 12h, 7d, 30d, 90d, 1y, all ou custom")
	}
	input.Period.Preset = period
	if period == "custom" {
		if input.Period.FromDate, err = ask("Data inicial", "AAAA-MM-DD"); err != nil {
			return input, err
		}
		if input.Period.ToDate, err = ask("Data final", "AAAA-MM-DD"); err != nil {
			return input, err
		}
	}
	return input, nil
}

var wizardRelativePeriod = regexp.MustCompile(`^[1-9][0-9]{0,3}[hd]$`)

func wizardCSV(value string) ([]string, error) {
	if value == "" {
		return []string{}, nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("ERR_VALIDATION: lista contém item vazio")
		}
		out = append(out, part)
	}
	return out, nil
}

func wizardNames(value string, defaults, allowed []string) (map[string]bool, error) {
	if value == "" {
		value = strings.Join(defaults, ",")
	}
	if strings.EqualFold(value, "none") {
		return map[string]bool{}, nil
	}
	items, err := wizardCSV(value)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool)
	for _, item := range items {
		item = strings.ToLower(item)
		found := false
		for _, name := range allowed {
			found = found || item == name
		}
		if !found {
			return nil, fmt.Errorf("ERR_VALIDATION: opção interativa inválida: %s", item)
		}
		out[item] = true
	}
	return out, nil
}
