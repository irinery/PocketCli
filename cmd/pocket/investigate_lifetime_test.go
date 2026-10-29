package main

import (
	"strings"
	"testing"
	"time"

	"pocketcli/internal/investigation/contract"
)

func TestFormatWindowShowsLifetimeForAllPeriod(t *testing.T) {
	period := contract.Period{Preset: "all", Timezone: "UTC"}
	end := "2026-10-29T12:00:00Z"
	resources := []contract.Resource{
		{Source: "github", Name: "acme/api", CreatedAt: "2021-03-01T10:00:00Z", LastChangeAt: "2026-09-30T08:30:00Z"},
		{Source: "linear", Name: "ENG", CreatedAt: "2019-05-02T00:00:00Z"},
	}
	got := formatWindow("", end, period, resources...)
	if want := "02/05/2019 00:00 → 29/10/2026 12:00 (all, tempo de vida, UTC)"; got != want {
		t.Fatalf("janela = %q, esperado %q", got, want)
	}
	if got := formatWindow("", end, period); got != "all (todo o histórico)" {
		t.Fatalf("sem tempo de vida conhecido: %q", got)
	}
	if strings.Contains(formatWindow("", end, period, resources...), " —") {
		t.Fatal("o início não pode ficar em branco")
	}
}

func TestLifetimeRowsListRepositoriesAndLinearScope(t *testing.T) {
	end := "2026-10-29T12:00:00Z"
	resources := []contract.Resource{
		{Source: "github", Name: "acme/api", CreatedAt: "2021-03-01T10:00:00Z", LastChangeAt: "2026-09-30T08:30:00Z"},
		{Source: "linear", Name: "ENG", CreatedAt: "2019-05-02T00:00:00Z"},
		{Source: "linear", Name: "AIF", CreatedAt: "2022-07-10T12:00:00Z"},
	}
	organization := strings.Join(lifetimeRows(resources, end, false, time.UTC), "|")
	if want := "Linear (organização)  02/05/2019 → 29/10/2026|acme/api  01/03/2021 → 30/09/2026"; organization != want {
		t.Fatalf("organização:\n%s\nesperado:\n%s", organization, want)
	}
	targeted := strings.Join(lifetimeRows(resources, end, true, time.UTC), "|")
	if !strings.Contains(targeted, "Linear AIF  10/07/2022 → 29/10/2026") || strings.Contains(targeted, "organização") {
		t.Fatalf("team direcionado usa o próprio início: %s", targeted)
	}
}
