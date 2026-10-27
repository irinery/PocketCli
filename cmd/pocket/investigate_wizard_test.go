package main

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/core"
)

func wizardBaseInput() contract.PlanInput {
	return contract.PlanInput{SchemaVersion: contract.Version, Name: "investigation", GitHubEnabled: true, LinearEnabled: true, Scope: &contract.Scope{Mode: "all_authorized"}, Modules: contract.Modules{GitHubIssues: true, CodeHistory: true}, Period: contract.Period{Timezone: "America/Sao_Paulo"}}
}

func TestInvestigationWizardBuildsManualSelectedPlan(t *testing.T) {
	answers := strings.NewReader("github\nirinery/socialMediaFactory\ngithub_issues,ci\ncustom\n2026-09-01\n2026-09-30\n")
	var output bytes.Buffer
	input, err := promptInvestigationWizard(answers, &output, wizardBaseInput())
	if err != nil {
		t.Fatal(err)
	}
	if input.Name != "investigation" || !input.GitHubEnabled || input.LinearEnabled {
		t.Fatalf("fontes incorretas: %#v", input)
	}
	if input.Scope.Mode != "selected" || len(input.Scope.Repositories) != 1 || input.Scope.Repositories[0] != "irinery/socialMediaFactory" {
		t.Fatalf("escopo incorreto: %#v", input.Scope)
	}
	if !input.Modules.GitHubIssues || !input.Modules.CI || input.Modules.CodeHistory || input.Modules.Deployments || input.Modules.RuntimeImport {
		t.Fatalf("módulos incorretos: %#v", input.Modules)
	}
	if input.Period.Preset != "custom" || input.Period.FromDate != "2026-09-01" || input.Period.ToDate != "2026-09-30" || input.Period.Timezone != "America/Sao_Paulo" {
		t.Fatalf("período incorreto: %#v", input.Period)
	}
	if !strings.Contains(output.String(), "Enter = todos autorizados") {
		t.Fatal("wizard não explica escopo padrão")
	}
}

func TestInvestigationWizardMatchesYAMLPlanInput(t *testing.T) {
	answers := strings.NewReader("github\nirinery/socialMediaFactory\ngithub_issues,ci\ncustom\n2026-09-01\n2026-09-30\n")
	var output bytes.Buffer
	manual, err := promptInvestigationWizard(answers, &output, wizardBaseInput())
	if err != nil {
		t.Fatal(err)
	}
	yaml := "schema_version: investigate.v1\nname: investigation\nsources:\n  github: true\n  linear: false\nscope:\n  mode: selected\n  repositories: [irinery/socialMediaFactory]\nmodules:\n  github_issues: true\n  code_history: false\n  ci: true\nperiod:\n  preset: custom\n  from: 2026-09-01\n  to: 2026-09-30\n  timezone: America/Sao_Paulo\n"
	parsed, err := core.NewSetup(nil).Parse(context.Background(), contract.SetupParseRequest{Filename: "scope.yaml", Content: yaml})
	if err != nil {
		t.Fatal(err)
	}
	manual.Limits = parsed.Input.Limits // Core fills the same defaults when preparing a manual plan.
	if !reflect.DeepEqual(manual, parsed.Input) {
		t.Fatalf("entrada manual e YAML diferem:\nmanual: %#v\nYAML: %#v", manual, parsed.Input)
	}
}

func TestInvestigationWizardLinearOnlyMatchesYAMLDefaults(t *testing.T) {
	answers := strings.NewReader("linear\nUNI\n30d\n")
	var output bytes.Buffer
	manual, err := promptInvestigationWizard(answers, &output, wizardBaseInput())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "Módulos") {
		t.Fatalf("wizard perguntou módulos GitHub sem GitHub: %q", output.String())
	}
	yaml := "schema_version: investigate.v1\nname: investigation\nsources:\n  github: false\n  linear: true\nscope:\n  mode: selected\n  teams: [UNI]\nperiod:\n  preset: 30d\n  timezone: America/Sao_Paulo\n"
	parsed, err := core.NewSetup(nil).Parse(context.Background(), contract.SetupParseRequest{Filename: "scope.yaml", Content: yaml})
	if err != nil {
		t.Fatal(err)
	}
	manual.Limits = parsed.Input.Limits
	if !reflect.DeepEqual(manual, parsed.Input) {
		t.Fatalf("YAML e wizard Linear divergem: manual=%#v YAML=%#v", manual, parsed.Input)
	}
}

// Enter on every question: both sources, everything authorized, default
// modules and 30 days. Reads stop at the last answer.
func TestInvestigationWizardDefaultsOnEnterAndLeavesNextAnswer(t *testing.T) {
	answers := strings.NewReader("\n\n\n\n\nnext-answer\n")
	var output bytes.Buffer
	input, err := promptInvestigationWizard(answers, &output, wizardBaseInput())
	if err != nil {
		t.Fatal(err)
	}
	if !input.GitHubEnabled || !input.LinearEnabled || input.Scope.Mode != "all_authorized" || !input.Modules.GitHubIssues || !input.Modules.CodeHistory || input.Period.Preset != "30d" {
		t.Fatalf("padrões incorretos: %#v", input)
	}
	next, err := promptLineFrom(answers, &output, "next: ")
	if err != nil || next != "next-answer" {
		t.Fatalf("prompt seguinte: %q, %v", next, err)
	}
}

func TestInvestigationWizardRuntimeOnlyAndValidation(t *testing.T) {
	var output bytes.Buffer
	input, err := promptInvestigationWizard(strings.NewReader("none\n12h\n"), &output, wizardBaseInput())
	if err != nil {
		t.Fatal(err)
	}
	if input.GitHubEnabled || input.LinearEnabled || !input.Modules.RuntimeImport || input.Period.Preset != "12h" {
		t.Fatalf("runtime local incorreto: %#v", input)
	}
	for answers, want := range map[string]string{
		"other\n":           "opção interativa inválida",
		"github\n\nbogus\n": "opção interativa inválida",
		"linear\n\n2w\n":    "ERR_PERIOD_REQUIRED",
		"linear\n\n0h\n":    "ERR_PERIOD_REQUIRED",
	} {
		if _, err := promptInvestigationWizard(strings.NewReader(answers), &output, wizardBaseInput()); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("respostas %q: erro %v, esperado %q", answers, err, want)
		}
	}
	mixed, err := promptInvestigationWizard(strings.NewReader("\nowner/repo\n\n\n30d\n"), &output, wizardBaseInput())
	if err != nil || mixed.Scope.Mode != "selected" || len(mixed.Scope.Repositories) != 1 || len(mixed.Scope.Teams) != 0 {
		t.Fatalf("escopo misto por fonte incorreto: %#v, %v", mixed.Scope, err)
	}
}

func TestPlanConfirmationAcceptsShortCode(t *testing.T) {
	hash := "a1b2c3d4e5f6a7b8c9d0"
	for confirm, want := range map[string]string{"a1b2c3d4": hash, hash: hash, "a1b2c3d": "a1b2c3d", "ffffffff": "ffffffff", "": ""} {
		if got := expandPlanConfirmation(confirm, hash); got != want {
			t.Fatalf("expandPlanConfirmation(%q) = %q, esperado %q", confirm, got, want)
		}
	}
}

func TestInvestigationWizardOnlyForBareInteractiveCommand(t *testing.T) {
	if !shouldPromptInvestigationWizard(investigationFlags{}, true) {
		t.Fatal("não abriu wizard no TTY sem flags")
	}
	for _, f := range []investigationFlags{{period: "30d"}, {scope: "x.yaml"}, {repos: []string{"a/b"}}, {json: true}, {noLinear: true}} {
		if shouldPromptInvestigationWizard(f, !f.json) {
			t.Fatalf("wizard inesperado com flags: %#v", f)
		}
	}
	if shouldPromptInvestigationWizard(investigationFlags{}, false) {
		t.Fatal("wizard em entrada sem TTY")
	}
}
