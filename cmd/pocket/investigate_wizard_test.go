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
	answers := strings.NewReader("\nSMF\ngithub\nirinery/socialMediaFactory\ngithub_issues,ci\ncustom\n2026-09-01\n2026-09-30\nUTC\n")
	var output bytes.Buffer
	input, path, err := promptInvestigationWizard(answers, &output, wizardBaseInput())
	if err != nil {
		t.Fatal(err)
	}
	if path != "" || input.Name != "SMF" || !input.GitHubEnabled || input.LinearEnabled {
		t.Fatalf("fontes/caminho incorretos: %#v, %q", input, path)
	}
	if input.Scope.Mode != "selected" || len(input.Scope.Repositories) != 1 || input.Scope.Repositories[0] != "irinery/socialMediaFactory" {
		t.Fatalf("escopo incorreto: %#v", input.Scope)
	}
	if !input.Modules.GitHubIssues || !input.Modules.CI || input.Modules.CodeHistory || input.Modules.Deployments || input.Modules.RuntimeImport {
		t.Fatalf("módulos incorretos: %#v", input.Modules)
	}
	if input.Period.Preset != "custom" || input.Period.FromDate != "2026-09-01" || input.Period.ToDate != "2026-09-30" || input.Period.Timezone != "UTC" {
		t.Fatalf("período incorreto: %#v", input.Period)
	}
	if !strings.Contains(output.String(), "todos os recursos autorizados") {
		t.Fatal("wizard não explica escopo padrão")
	}
}

func TestInvestigationWizardMatchesYAMLPlanInput(t *testing.T) {
	answers := strings.NewReader("\nSMF\ngithub\nirinery/socialMediaFactory\ngithub_issues,ci\ncustom\n2026-09-01\n2026-09-30\nUTC\n")
	var output bytes.Buffer
	manual, _, err := promptInvestigationWizard(answers, &output, wizardBaseInput())
	if err != nil {
		t.Fatal(err)
	}
	yaml := "schema_version: investigate.v1\nname: SMF\nsources:\n  github: true\n  linear: false\nscope:\n  mode: selected\n  repositories: [irinery/socialMediaFactory]\nmodules:\n  github_issues: true\n  code_history: false\n  ci: true\nperiod:\n  preset: custom\n  from: 2026-09-01\n  to: 2026-09-30\n  timezone: UTC\n"
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
	answers := strings.NewReader("\nlinear-only\nlinear\nUNI\n\n30d\nUTC\n")
	var output bytes.Buffer
	manual, _, err := promptInvestigationWizard(answers, &output, wizardBaseInput())
	if err != nil {
		t.Fatal(err)
	}
	yaml := "schema_version: investigate.v1\nname: linear-only\nsources:\n  github: false\n  linear: true\nscope:\n  mode: selected\n  teams: [UNI]\nperiod:\n  preset: 30d\n  timezone: UTC\n"
	parsed, err := core.NewSetup(nil).Parse(context.Background(), contract.SetupParseRequest{Filename: "scope.yaml", Content: yaml})
	if err != nil {
		t.Fatal(err)
	}
	manual.Limits = parsed.Input.Limits
	if !reflect.DeepEqual(manual, parsed.Input) {
		t.Fatalf("YAML e wizard Linear divergem: manual=%#v YAML=%#v", manual, parsed.Input)
	}
}

func TestInvestigationWizardAcceptsYAMLWithoutConsumingNextAnswer(t *testing.T) {
	answers := strings.NewReader("./auditoria.yaml\nnext-answer\n")
	var output bytes.Buffer
	_, path, err := promptInvestigationWizard(answers, &output, wizardBaseInput())
	if err != nil || path != "./auditoria.yaml" {
		t.Fatalf("YAML: %q, %v", path, err)
	}
	next, err := promptLineFrom(answers, &output, "next: ")
	if err != nil || next != "next-answer" {
		t.Fatalf("prompt seguinte: %q, %v", next, err)
	}
}

func TestInvestigationWizardRuntimeOnlyAndValidation(t *testing.T) {
	var output bytes.Buffer
	input, _, err := promptInvestigationWizard(strings.NewReader("\n\nnone\nruntime_import\n30d\n\n"), &output, wizardBaseInput())
	if err != nil {
		t.Fatal(err)
	}
	if input.GitHubEnabled || input.LinearEnabled || !input.Modules.RuntimeImport || input.Period.Preset != "30d" {
		t.Fatalf("runtime local incorreto: %#v", input)
	}
	_, _, err = promptInvestigationWizard(strings.NewReader("\n\nnone\ngithub_issues\n"), &output, wizardBaseInput())
	if err == nil || !strings.Contains(err.Error(), "exigem a fonte GitHub") {
		t.Fatalf("aceitou módulo sem fonte: %v", err)
	}
	_, _, err = promptInvestigationWizard(strings.NewReader("\n\nother\n"), &output, wizardBaseInput())
	if err == nil || !strings.Contains(err.Error(), "opção interativa inválida") {
		t.Fatalf("aceitou fonte inválida: %v", err)
	}
	mixed, _, err := promptInvestigationWizard(strings.NewReader("\n\n\nowner/repo\n\n\n30d\n\n"), &output, wizardBaseInput())
	if err != nil || mixed.Scope.Mode != "selected" || len(mixed.Scope.Repositories) != 1 || len(mixed.Scope.Teams) != 0 {
		t.Fatalf("escopo misto por fonte incorreto: %#v, %v", mixed.Scope, err)
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
