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
