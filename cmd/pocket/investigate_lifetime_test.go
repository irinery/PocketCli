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

// Each Linear team keeps its own lifetime; a retired team ends at its
// retirement instead of the window end.
func TestLifetimeRowsListRepositoriesAndEachTeam(t *testing.T) {
	end := "2026-10-29T12:00:00Z"
	resources := []contract.Resource{
		{Source: "github", Name: "acme/api", CreatedAt: "2021-03-01T10:00:00Z", LastChangeAt: "2026-09-30T08:30:00Z"},
		{Source: "linear", Name: "ENG", CreatedAt: "2026-07-21T14:03:48Z"},
		{Source: "linear", Name: "QA", CreatedAt: "2026-07-21T14:43:47Z", LastChangeAt: "2026-08-20T12:56:16Z"},
	}
	got := strings.Join(lifetimeRows(resources, end, time.UTC), "|")
	if want := "Linear ENG  21/07/2026 → 29/10/2026|Linear QA  21/07/2026 → 20/08/2026 (aposentado)|acme/api  01/03/2021 → 30/09/2026"; got != want {
		t.Fatalf("tempo de vida:\n%s\nesperado:\n%s", got, want)
	}
}
