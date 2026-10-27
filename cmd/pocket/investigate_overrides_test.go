package main

import (
	"bytes"
	"strings"
	"testing"

	"pocketcli/internal/investigation/contract"
)

func TestInvestigationYAMLOverridesAreVisibleAndClearStaleCustomDates(t *testing.T) {
	input := contract.PlanInput{
		GitHubEnabled: true, LinearEnabled: true,
		Scope:  &contract.Scope{Mode: "all_authorized", Repositories: []string{}, Teams: []string{}},
		Period: contract.Period{Preset: "custom", FromDate: "2025-01-01", ToDate: "2025-02-01", Timezone: "UTC"},
	}
	f := investigationFlags{scope: "plan.yaml", period: "90d", noLinear: true, repos: []string{"irinery/PocketCli"}, timezone: "America/Sao_Paulo"}
	effective, changes, err := applyInvestigationFlags(input, f)
	if err != nil {
		t.Fatal(err)
	}
	if effective.Period.Preset != "90d" || effective.Period.FromDate != "" || effective.Period.ToDate != "" || effective.GitHubEnabled != true || effective.LinearEnabled != false || effective.Scope.Mode != "selected" || len(effective.Scope.Repositories) != 1 {
		t.Fatalf("plano efetivo incorreto: %#v", effective)
	}
	if input.Period.Preset != "custom" || input.Scope.Mode != "all_authorized" || len(input.Scope.Repositories) != 0 {
		t.Fatalf("YAML original alterado: %#v", input)
	}
	var output bytes.Buffer
	if err := printInvestigationOverrides(&output, changes); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"period.preset", "period.from", "period.to", "period.timezone", "sources.linear", "scope.mode", "scope.repositories"} {
		if !strings.Contains(output.String(), field+":") {
			t.Errorf("override %s não mostrado: %s", field, output.String())
		}
	}
	if strings.Contains(output.String(), "%!q") {
		t.Fatalf("formatação inválida: %s", output.String())
	}
}

func TestInvestigationFlagsRejectDatesForNonCustomPeriod(t *testing.T) {
	input := contract.PlanInput{Scope: &contract.Scope{Mode: "all_authorized"}}
	_, _, err := applyInvestigationFlags(input, investigationFlags{period: "90d", from: "2025-01-01"})
	if err == nil || !strings.Contains(err.Error(), "--period custom") {
		t.Fatalf("aceitou datas ignoradas: %v", err)
	}
	_, changes, err := applyInvestigationFlags(input, investigationFlags{period: "90d"})
	if err != nil || len(changes) != 0 {
		t.Fatalf("flag sem YAML: %v, %#v", err, changes)
	}
	input.Period.Preset = "90d"
	_, _, err = applyInvestigationFlags(input, investigationFlags{scope: "plan.yaml", from: "2025-01-01"})
	if err == nil || !strings.Contains(err.Error(), "período custom") {
		t.Fatalf("aceitou data sobre preset do YAML: %v", err)
	}
}

func TestDisableSourceFlagClearsDependentYAMLFields(t *testing.T) {
	input := contract.PlanInput{
		GitHubEnabled: true, LinearEnabled: true,
		Scope:   &contract.Scope{Mode: "selected", Repositories: []string{"owner/repo"}, Teams: []string{"UNI"}},
		Modules: contract.Modules{GitHubIssues: true, CodeHistory: true, CI: true, Deployments: true},
	}
	effective, changes, err := applyInvestigationFlags(input, investigationFlags{scope: "plan.yaml", noGitHub: true})
	if err != nil {
		t.Fatal(err)
	}
	if effective.GitHubEnabled || len(effective.Scope.Repositories) != 0 || len(effective.Scope.Teams) != 1 || effective.Scope.Mode != "selected" || effective.Modules.GitHubIssues || effective.Modules.CodeHistory || effective.Modules.CI || effective.Modules.Deployments {
		t.Fatalf("dependências GitHub não foram desligadas: %#v", effective)
	}
	if !input.GitHubEnabled || len(input.Scope.Repositories) != 1 || !input.Modules.GitHubIssues {
		t.Fatal("YAML original foi alterado")
	}
	var output bytes.Buffer
	if err := printInvestigationOverrides(&output, changes); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"sources.github", "scope.repositories", "modules.github_issues", "modules.code_history", "modules.ci", "modules.deployments"} {
		if !strings.Contains(output.String(), field+":") {
			t.Errorf("override de %s ausente: %s", field, output.String())
		}
	}
	if _, _, err := applyInvestigationFlags(input, investigationFlags{noGitHub: true, repos: []string{"owner/repo"}}); err == nil {
		t.Fatal("aceitou --no-github junto de --repo")
	}
	if _, _, err := applyInvestigationFlags(input, investigationFlags{noLinear: true, teams: []string{"UNI"}}); err == nil {
		t.Fatal("aceitou --no-linear junto de --team")
	}
	input.GitHubEnabled = false
	input.Scope.Repositories = []string{}
	input.Modules = contract.Modules{}
	if _, _, err := applyInvestigationFlags(input, investigationFlags{scope: "plan.yaml", repos: []string{"owner/repo"}}); err == nil {
		t.Fatal("aceitou --repo sobre YAML com GitHub desligado")
	}
}
