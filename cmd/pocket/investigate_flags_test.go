package main

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
	"time"

	"pocketcli/internal/investigation/contract"
)

func TestInvestigationShortFlagsListsAndAliases(t *testing.T) {
	f, err := parseInvestigationFlags([]string{"-t", "ENG,UNI", "-t=OPS", "-r", "a/b, c/d", "-p", "12h", "-c", "abcd1234", "--plan"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.teams, []string{"ENG", "UNI", "OPS"}) || !reflect.DeepEqual(f.repos, []string{"a/b", "c/d"}) || f.period != "12h" || f.confirm != "abcd1234" || !f.prepare {
		t.Fatalf("flags curtas incorretas: %+v", f)
	}
	f, err = parseInvestigationFlags([]string{"--since", "7d", "--until=2026-10-01"})
	if err != nil || f.period != "7d" || f.to != "2026-10-01" || f.since != "" {
		t.Fatalf("--since relativo: %+v %v", f, err)
	}
	if _, err := parseInvestigationFlags([]string{"--bogus"}); err == nil || !strings.Contains(err.Error(), "pocket investigate --help") {
		t.Fatalf("flag inválida sem dica de ajuda: %v", err)
	}
}

func TestInvestigationSinceDateRunsUntilToday(t *testing.T) {
	previous := investigationNow
	investigationNow = func() time.Time { return time.Date(2026, 10, 28, 1, 30, 0, 0, time.UTC) }
	defer func() { investigationNow = previous }()
	// 01:30Z is still 27 Oct in America/Sao_Paulo.
	f, err := parseInvestigationFlags([]string{"--since", "2026-09-01"})
	if err != nil || f.period != "custom" || f.from != "2026-09-01" || f.to != "2026-10-27" {
		t.Fatalf("--since com data: %+v %v", f, err)
	}
	f, err = parseInvestigationFlags([]string{"--since", "2026-09-01", "--timezone", "UTC", "--until", "2026-09-30"})
	if err != nil || f.to != "2026-09-30" {
		t.Fatalf("--until explícito: %+v %v", f, err)
	}
	for _, args := range [][]string{{"--since", "7d", "--period", "30d"}, {"--since", "2026-09-01", "--from", "2026-08-01"}} {
		if _, err := parseInvestigationFlags(args); err == nil || !strings.Contains(err.Error(), "--since") {
			t.Fatalf("aceitou %v: %v", args, err)
		}
	}
}

func TestSelectorImpliesItsSource(t *testing.T) {
	for _, test := range []struct {
		name             string
		f                investigationFlags
		noGitHub, noLinr bool
		note             string
	}{
		{"padrão: as duas fontes", investigationFlags{}, false, false, ""},
		{"--team só Linear", investigationFlags{teams: []string{"ENG"}}, true, false, "só Linear"},
		{"--repo só GitHub", investigationFlags{repos: []string{"a/b"}}, false, true, "só GitHub"},
		{"--repo e --team: as duas", investigationFlags{repos: []string{"a/b"}, teams: []string{"ENG"}}, false, false, ""},
		{"--github e --linear: as duas", investigationFlags{github: true, linear: true}, false, false, ""},
		{"--team mais --github", investigationFlags{teams: []string{"ENG"}, github: true}, false, false, ""},
		{"--linear só Linear", investigationFlags{linear: true}, true, false, "só Linear"},
	} {
		f := test.f
		note, err := selectInvestigationSources(&f)
		if err != nil || f.noGitHub != test.noGitHub || f.noLinear != test.noLinr || !strings.Contains(note, test.note) || (test.note == "" && note != "") {
			t.Fatalf("%s: noGitHub=%t noLinear=%t note=%q err=%v", test.name, f.noGitHub, f.noLinear, note, err)
		}
	}
	for _, f := range []investigationFlags{{teams: []string{"ENG"}, noLinear: true}, {repos: []string{"a/b"}, noGitHub: true}, {github: true, noGitHub: true}} {
		if _, err := selectInvestigationSources(&f); err == nil || !strings.Contains(err.Error(), "conflita") {
			t.Fatalf("aceitou conflito %+v: %v", f, err)
		}
	}
}

func TestPlanSummaryStatesWhichAPIsAreUsed(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, test := range []struct {
		input contract.PlanInput
		want  []string
		deny  []string
	}{
		{contract.PlanInput{GitHubEnabled: true, LinearEnabled: true}, []string{"APIs", "GitHub + Linear", "somente leitura", "POCKETCLI_INVESTIGATE_GITHUB_TOKEN", "POCKETCLI_INVESTIGATE_LINEAR_KEY"}, nil},
		{contract.PlanInput{LinearEnabled: true}, []string{"Linear", "POCKETCLI_INVESTIGATE_LINEAR_KEY"}, []string{"GitHub"}},
		{contract.PlanInput{}, []string{"somente importação local, sem chamadas de rede"}, []string{"GitHub", "Linear"}},
	} {
		var out bytes.Buffer
		if err := printInvestigationPlanSummary(&out, contract.Plan{Hash: "abcdef0123456789", Input: test.input}, "Plano", nil); err != nil {
			t.Fatal(err)
		}
		for _, want := range test.want {
			if !strings.Contains(out.String(), want) {
				t.Fatalf("resumo sem %q: %s", want, out.String())
			}
		}
		for _, deny := range test.deny {
			if strings.Contains(out.String(), deny) {
				t.Fatalf("resumo cita %q sem a fonte: %s", deny, out.String())
			}
		}
	}
}

func TestWizardSkippedWhenSourceFlagsAreGiven(t *testing.T) {
	for _, f := range []investigationFlags{{github: true}, {linear: true}, {teams: []string{"ENG"}}} {
		if shouldPromptInvestigationWizard(f, true) {
			t.Fatalf("wizard com flags de fonte: %+v", f)
		}
	}
}

func TestYesSourceAndConflicts(t *testing.T) {
	f, err := parseInvestigationFlags([]string{"-y", "-s", "git", "-p", "7d"})
	if err != nil || !f.yes || !f.github || f.linear || f.period != "7d" {
		t.Fatalf("--yes e --source git: %+v %v", f, err)
	}
	for source, want := range map[string][2]bool{"github": {true, false}, "linear": {false, true}, "both": {true, true}, "ambos": {true, true}} {
		f, err := parseInvestigationFlags([]string{"--source=" + source})
		if err != nil || f.github != want[0] || f.linear != want[1] {
			t.Fatalf("--source=%s: %+v %v", source, f, err)
		}
		if _, err := selectInvestigationSources(&f); err != nil {
			t.Fatal(err)
		}
		if f.noGitHub != !want[0] || f.noLinear != !want[1] {
			t.Fatalf("--source=%s não selecionou as fontes: %+v", source, f)
		}
	}
	for _, args := range [][]string{{"--source", "gitlab"}, {"--yes", "--plan"}, {"--yes", "--prepare"}, {"-y", "-c", "abcd1234"}} {
		if _, err := parseInvestigationFlags(args); err == nil {
			t.Fatalf("aceitou %v", args)
		}
	}
	if got := startConfirmation(investigationFlags{yes: true}, "hash-completo"); got != "hash-completo" {
		t.Fatalf("--yes não confirmou o plano: %q", got)
	}
	if got := startConfirmation(investigationFlags{confirm: "abcd1234"}, "abcd1234ffff"); got != "abcd1234ffff" {
		t.Fatalf("código curto não expandiu: %q", got)
	}
}

func TestShowFlagsAndTable(t *testing.T) {
	f, err := parseShowFlags([]string{"issues", "--limit", "5", "--snapshot", "snap-1", "--json"})
	if err != nil || f.limit != 5 || f.snapshot != "snap-1" || !f.json || len(f.rest) != 1 || f.rest[0] != "issues" {
		t.Fatalf("flags do show: %+v %v", f, err)
	}
	for _, args := range [][]string{{"--limit", "0"}, {"-n", "1001"}, {"--limit"}, {"--bogus"}} {
		if _, err := parseShowFlags(args); err == nil {
			t.Fatalf("aceitou %v", args)
		}
	}
	var out bytes.Buffer
	writeTable(&out, palette{}, []string{"identifier", "state", "title"}, [][]string{{"ENG-1", "started", strings.Repeat("a", 200)}, {"ENG-22", "completed", "curto"}}, 60)
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 3 || !strings.Contains(lines[1], "…") || strings.Contains(lines[2], "…") {
		t.Fatalf("tabela sem truncar só o título longo:\n%s", out.String())
	}
	for _, line := range lines {
		if len([]rune(line)) > 60 {
			t.Fatalf("linha acima da largura: %d %q", len([]rune(line)), line)
		}
	}
	if got := formatCell(contract.Cell{Kind: "text", Value: "2026-10-27T18:20:35Z"}); got != "2026-10-27 18:20" {
		t.Fatalf("horário: %q", got)
	}
	if got := formatCell(contract.Cell{Kind: "null"}); got != "—" {
		t.Fatalf("null: %q", got)
	}
	colored := palette{on: true}
	if got := statusColor(colored, "partial", "partial"); !strings.Contains(got, "\x1b[33m") {
		t.Fatalf("partial sem amarelo: %q", got)
	}
	if got := statusColor(colored, "unauthorized", "x"); !strings.Contains(got, "\x1b[31m") {
		t.Fatalf("unauthorized sem vermelho: %q", got)
	}
}
