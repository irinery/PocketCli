package core

import (
	"context"
	"strings"
	"testing"
	"time"

	"pocketcli/internal/investigation/contract"
)

func TestParseStrictAndDefaults(t *testing.T) {
	s := NewSetup(func() time.Time { return time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC) })
	for _, suffix := range []string{"yml", "yaml"} {
		p, err := s.Parse(context.Background(), contract.SetupParseRequest{Filename: "plan." + suffix, Content: "schema_version: investigate.v1\nname: example\nperiod:\n  preset: 90d\n  timezone: UTC\n"})
		if err != nil {
			t.Fatal(err)
		}
		if p.Input.Scope == nil || p.Input.Scope.Mode != "all_authorized" || !p.Input.GitHubEnabled || !p.Input.LinearEnabled {
			t.Fatalf("defaults: %+v", p.Input)
		}
	}
	for _, content := range []string{
		"schema_version: investigate.v1\nname: a\nname: b\n",
		"schema_version: investigate.v1\nname: a\ntoken: secret\n",
		"schema_version: investigate.v1\nname: a\nunknown: x\n",
		"schema_version: investigate.v1\nname: !custom a\n",
		"schema_version: investigate.v1\nname: a\nscope:\n  mode: all_authorized\n  repositories: [owner/repo]\n",
	} {
		if _, err := s.Parse(context.Background(), contract.SetupParseRequest{Filename: "plan.yaml", Content: content}); err == nil {
			t.Fatalf("accepted %q", content)
		}
	}
}

func TestPreparePlanRejectsAllAuthorizedWithSelectorsBeforeDiscovery(t *testing.T) {
	c, err := NewCore(CoreConfig{Root: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.PreparePlan(context.Background(), contract.PlanInput{
		SchemaVersion: contract.Version,
		Scope:         &contract.Scope{Mode: "all_authorized", Repositories: []string{"owner/repo"}},
		Modules:       contract.Modules{RuntimeImport: true},
		Period:        contract.Period{Preset: "30d", Timezone: "UTC"},
	})
	if err == nil || !strings.Contains(err.Error(), "ERR_VALIDATION") {
		t.Fatalf("aceitou seleção oculta em all_authorized: %v", err)
	}
}

func TestDisabledSourceDefaultsAndRejectsContradictoryYAML(t *testing.T) {
	s := NewSetup(nil)
	base := "schema_version: investigate.v1\nname: linear-only\nsources:\n  github: false\n  linear: true\nperiod:\n  preset: 30d\n"
	parsed, err := s.Parse(context.Background(), contract.SetupParseRequest{Filename: "scope.yaml", Content: base})
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Input.GitHubEnabled || !parsed.Input.LinearEnabled || parsed.Input.Modules.GitHubIssues || parsed.Input.Modules.CodeHistory || parsed.Input.Modules.CI || parsed.Input.Modules.Deployments {
		t.Fatalf("módulos GitHub ativos sem fonte: %#v", parsed.Input)
	}
	for _, addition := range []string{
		"modules:\n  github_issues: true\n",
		"modules:\n  code_history: true\n",
		"modules:\n  ci: true\n",
		"modules:\n  deployments: true\n",
		"scope:\n  mode: selected\n  repositories: [owner/repo]\n",
	} {
		if _, err := s.Parse(context.Background(), contract.SetupParseRequest{Filename: "scope.yaml", Content: base + addition}); err == nil || !strings.Contains(err.Error(), "ERR_VALIDATION") {
			t.Fatalf("aceitou configuração contraditória %q: %v", addition, err)
		}
	}
}

func TestPreparePlanRejectsDisabledSourceSelectorsAndModules(t *testing.T) {
	c, err := NewCore(CoreConfig{Root: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	base := contract.PlanInput{SchemaVersion: contract.Version, LinearEnabled: true, Scope: &contract.Scope{Mode: "all_authorized"}, Period: contract.Period{Preset: "30d", Timezone: "UTC"}}
	for _, mutate := range []func(*contract.PlanInput){
		func(input *contract.PlanInput) { input.Modules.GitHubIssues = true },
		func(input *contract.PlanInput) { input.Modules.CodeHistory = true },
		func(input *contract.PlanInput) {
			input.Scope.Mode = "selected"
			input.Scope.Repositories = []string{"owner/repo"}
		},
		func(input *contract.PlanInput) {
			input.LinearEnabled = false
			input.Modules.RuntimeImport = true
			input.Scope.Mode = "selected"
			input.Scope.Teams = []string{"UNI"}
		},
	} {
		input := base
		input.Scope = &contract.Scope{Mode: base.Scope.Mode}
		mutate(&input)
		if _, err := c.PreparePlan(context.Background(), input); err == nil || !strings.Contains(err.Error(), "ERR_VALIDATION") {
			t.Fatalf("aceitou seletor/módulo sem fonte: %#v, %v", input, err)
		}
	}
}

func TestResolveLeapYearAndMissing(t *testing.T) {
	s := NewSetup(func() time.Time { return time.Date(2024, 2, 29, 10, 0, 0, 0, time.UTC) })
	p, err := s.ResolvePeriod(context.Background(), contract.Period{Preset: "1y", Timezone: "UTC"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(p.WindowStart, "2023-02-28T10:00:00Z") {
		t.Fatal(p.WindowStart)
	}
	if _, err := s.ResolvePeriod(context.Background(), contract.Period{}); err == nil {
		t.Fatal("period required")
	}
}

func TestResolvePeriodRejectsDatesOutsideCustom(t *testing.T) {
	s := NewSetup(nil)
	for _, period := range []contract.Period{
		{Preset: "90d", FromDate: "2025-01-01", Timezone: "UTC"},
		{Preset: "all", ToDate: "2025-02-01", Timezone: "UTC"},
	} {
		if _, err := s.ResolvePeriod(context.Background(), period); err == nil || !strings.Contains(err.Error(), "ERR_VALIDATION") {
			t.Fatalf("aceitou datas ignoradas em %q: %v", period.Preset, err)
		}
	}
}

func TestHistoricalWindowDoesNotProjectCurrentOpenStateBackwards(t *testing.T) {
	start, end := "2023-01-01T00:00:00Z", "2024-01-01T00:00:00Z"
	if activeInWindow(start, end, "2022-01-01T00:00:00Z", "2026-01-01T00:00:00Z", "open", false) {
		t.Fatal("current open state projected into historical window")
	}
	if !activeInWindow(start, end, "2022-01-01T00:00:00Z", "2023-06-01T00:00:00Z", "closed", false) {
		t.Fatal("activity inside historical window excluded")
	}
	if !activeInWindow(start, end, "2022-01-01T00:00:00Z", "2023-06-01T00:00:00Z", "open", true) {
		t.Fatal("open item active in current window excluded")
	}
}
