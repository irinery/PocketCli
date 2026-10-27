package main

import (
	"fmt"
	"io"
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
	return interactive && f.scope == "" && f.period == "" && f.from == "" && f.to == "" && f.timezone == "" && f.confirm == "" && f.investigation == "" && len(f.repos) == 0 && len(f.teams) == 0 && !f.noGitHub && !f.noLinear
}

func promptInvestigationWizard(in io.Reader, out io.Writer, input contract.PlanInput) (contract.PlanInput, string, error) {
	if _, err := io.WriteString(out, "Escopo padrão: todos os recursos autorizados pelas fontes escolhidas.\n"); err != nil {
		return input, "", err
	}
	yamlPath, err := promptLineFrom(in, out, "YAML opcional (Enter para configurar aqui): ")
	if err != nil || yamlPath != "" {
		return input, yamlPath, err
	}
	name, err := promptLineFrom(in, out, "Nome da investigação [investigation]: ")
	if err != nil {
		return input, "", err
	}
	if name != "" {
		input.Name = name
	}
	sources, err := promptLineFrom(in, out, "Fontes [github,linear] (none para só importação local): ")
	if err != nil {
		return input, "", err
	}
	sourceNames, err := wizardNames(sources, []string{"github", "linear"}, []string{"github", "linear"})
	if err != nil {
		return input, "", err
	}
	input.GitHubEnabled, input.LinearEnabled = sourceNames["github"], sourceNames["linear"]
	input.Scope = &contract.Scope{Mode: "all_authorized", Repositories: []string{}, Teams: []string{}}
	if _, err := io.WriteString(out, "Deixe as listas vazias para descobrir todos os recursos autorizados; em duas fontes, preencha ambas para limitar o escopo.\n"); err != nil {
		return input, "", err
	}
	if input.GitHubEnabled {
		value, err := promptLineFrom(in, out, "Repositórios GitHub owner/repo separados por vírgula (Enter=padrão): ")
		if err != nil {
			return input, "", err
		}
		input.Scope.Repositories, err = wizardCSV(value)
		if err != nil {
			return input, "", err
		}
	}
	if input.LinearEnabled {
		value, err := promptLineFrom(in, out, "Teams Linear por chave ou ID, separados por vírgula (Enter=padrão): ")
		if err != nil {
			return input, "", err
		}
		input.Scope.Teams, err = wizardCSV(value)
		if err != nil {
			return input, "", err
		}
	}
	if input.GitHubEnabled && input.LinearEnabled && (len(input.Scope.Repositories) == 0) != (len(input.Scope.Teams) == 0) {
		return input, "", fmt.Errorf("ERR_VALIDATION: selecione repositórios e teams nas duas fontes ou deixe ambas as listas vazias para todos os recursos autorizados")
	}
	if len(input.Scope.Repositories)+len(input.Scope.Teams) > 0 {
		input.Scope.Mode = "selected"
	}
	defaults := []string{}
	if input.GitHubEnabled {
		defaults = []string{"github_issues", "code_history"}
	}
	modules, err := promptLineFrom(in, out, "Módulos [github_issues,code_history] (ci,deployments,runtime_import opcionais; none=nenhum): ")
	if err != nil {
		return input, "", err
	}
	moduleNames, err := wizardNames(modules, defaults, []string{"github_issues", "code_history", "ci", "deployments", "runtime_import"})
	if err != nil {
		return input, "", err
	}
	if !input.GitHubEnabled && (moduleNames["github_issues"] || moduleNames["code_history"] || moduleNames["ci"] || moduleNames["deployments"]) {
		return input, "", fmt.Errorf("ERR_VALIDATION: módulos GitHub exigem a fonte GitHub")
	}
	input.Modules = contract.Modules{GitHubIssues: moduleNames["github_issues"], CodeHistory: moduleNames["code_history"], CI: moduleNames["ci"], Deployments: moduleNames["deployments"], RuntimeImport: moduleNames["runtime_import"]}
	if !input.GitHubEnabled && !input.LinearEnabled && !input.Modules.RuntimeImport {
		return input, "", fmt.Errorf("ERR_VALIDATION: selecione uma fonte ou runtime_import")
	}
	period, err := promptLineFrom(in, out, "Período [30d,60d,90d,1y,all,custom]: ")
	if err != nil {
		return input, "", err
	}
	switch period {
	case "30d", "60d", "90d", "1y", "all", "custom":
		input.Period.Preset = period
	default:
		return input, "", fmt.Errorf("ERR_PERIOD_REQUIRED: escolha 30d, 60d, 90d, 1y, all ou custom")
	}
	if period == "custom" {
		input.Period.FromDate, err = promptLineFrom(in, out, "Data inicial AAAA-MM-DD: ")
		if err != nil {
			return input, "", err
		}
		input.Period.ToDate, err = promptLineFrom(in, out, "Data final AAAA-MM-DD: ")
		if err != nil {
			return input, "", err
		}
	}
	timezone, err := promptLineFrom(in, out, "Timezone [America/Sao_Paulo]: ")
	if err != nil {
		return input, "", err
	}
	if timezone != "" {
		input.Period.Timezone = timezone
	}
	return input, "", nil
}

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
