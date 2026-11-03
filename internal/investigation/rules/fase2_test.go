package rules

import (
	"fmt"
	"strings"
	"testing"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/query"
)

// A team with 300 issues of which only 100 had their history read: every
// stage metric states the base and is labelled low coverage.
func TestStageMetricsOnPartialHistoryAreLowCoverage(t *testing.T) {
	d := query.Dataset{Snapshot: contract.Snapshot{WindowStart: "2026-07-01T00:00:00Z", WindowEnd: "2026-11-01T00:00:00Z"}}
	for i := 0; i < 300; i++ {
		id := fmt.Sprintf("linear-eng-%03d", i)
		d.Issues = append(d.Issues, query.Issue{ID: id, Source: "linear", TeamID: "eng", CreatedAt: "2026-07-22T00:00:00Z"})
		if i < 100 {
			seconds := int64(3600 * (i + 1))
			d.Stages = append(d.Stages, contract.StageRecord{ID: "st-" + id, SubjectID: id, Stage: "linear:QA", StartedAt: "2026-07-23T00:00:00Z", EndedAt: "2026-08-01T00:00:00Z", ElapsedSeconds: &seconds, DurationValid: true})
		}
	}
	for i := 0; i < 20; i++ {
		id := fmt.Sprintf("linear-cad-%02d", i)
		seconds := int64(60)
		d.Issues = append(d.Issues, query.Issue{ID: id, Source: "linear", TeamID: "cad"})
		d.Stages = append(d.Stages, contract.StageRecord{ID: "st-" + id, SubjectID: id, Stage: "linear:QA", StartedAt: "2026-09-23T00:00:00Z", EndedAt: "2026-09-24T00:00:00Z", ElapsedSeconds: &seconds, DurationValid: true})
	}
	metrics := linearStageMetrics(d)
	for _, prefix := range []string{"metric-stage-median-team", "metric-stage-p85-team", "metric-stage-open-team"} {
		eng, ok := stageMetric(metrics, prefix, "eng\x00QA")
		if !ok || eng.Base == nil || eng.Base.Observed != 100 || eng.Base.Total != 300 || !eng.Base.Low {
			t.Fatalf("%s do ENG sem base de baixa cobertura: %+v", prefix, eng)
		}
		if !strings.Contains(strings.Join(eng.Limitations, " "), "Base: 100 de 300 issues do team com histórico lido (33%).") || !strings.Contains(strings.Join(eng.Limitations, " "), "Baixa cobertura") {
			t.Fatalf("%s sem base nas limitações: %v", prefix, eng.Limitations)
		}
		cad, ok := stageMetric(metrics, prefix, "cad\x00QA")
		if !ok || cad.Base == nil || cad.Base.Low || cad.Base.Observed != 20 {
			t.Fatalf("%s do CAD com histórico completo: %+v", prefix, cad.Base)
		}
	}
}

// Actors are labelled by stable pseudonyms, never their source IDs, and the
// label never turns into a productivity score.
func TestActorMetricsUsePseudonymsInsteadOfIDs(t *testing.T) {
	d := query.Dataset{Snapshot: contract.Snapshot{WindowStart: "2026-07-01T00:00:00Z", WindowEnd: "2026-11-01T00:00:00Z", Actors: []contract.Actor{{ID: "uuid-ana", Source: "linear", Name: "Ana"}}}}
	d.Issues = []query.Issue{
		{ID: "linear-1", Source: "linear", TeamID: "eng", AssigneeID: "uuid-bruno", Outcome: "completed", CreatedAt: "2026-07-25T00:00:00Z", CompletedAt: "2026-08-02T00:00:00Z"},
		{ID: "linear-2", Source: "linear", TeamID: "eng", Outcome: "completed", CreatedAt: "2026-07-25T00:00:00Z", CompletedAt: "2026-08-02T00:00:00Z"},
	}
	d.Events = []query.Event{
		{ID: "e1", EntityID: "linear-1", Type: "state_changed", EventAt: "2026-07-22T00:00:00Z", ActorID: "uuid-ana", OldValue: "started", NewValue: "completed"},
		{ID: "e2", EntityID: "linear-2", Type: "state_changed", EventAt: "2026-08-02T00:00:00Z", ActorID: "uuid-bruno", OldValue: "started", NewValue: "completed"},
	}
	d.Stages = []contract.StageRecord{{ID: "s", SubjectID: "linear-1", Stage: "linear:Done"}}
	out := Evaluate(d)
	labels := map[string]string{}
	for _, a := range out.Snapshot.Actors {
		labels[a.ID] = a.Pseudonym
	}
	if labels["uuid-ana"] != "Pessoa A" || labels["uuid-bruno"] != "Pessoa B" {
		t.Fatalf("pseudônimos pela primeira atividade: %+v", out.Snapshot.Actors)
	}
	for _, a := range out.Snapshot.Actors {
		if a.ID == "uuid-ana" && a.Name != "Ana" {
			t.Fatalf("nome reportado pela fonte perdido: %+v", a)
		}
	}
	found := 0
	for _, m := range out.Metrics {
		if strings.Contains(m.Name, "uuid-") || strings.Contains(m.Definition, "uuid-") {
			t.Fatalf("ID cru no rótulo: %s", m.Name)
		}
		if strings.Contains(m.Name, "Pessoa ") {
			found++
			text := strings.ToLower(m.Name + " " + m.Definition)
			if strings.Contains(text, "score") || strings.Contains(text, "ranking") || strings.Contains(text, "produtividade") && !strings.Contains(text, "não") {
				t.Fatalf("rótulo de ator virou score: %s", m.Name)
			}
			if !strings.Contains(strings.ToLower(strings.Join(m.Limitations, " ")), "produtividade") {
				t.Fatalf("métrica de ator sem a limitação de produtividade: %+v", m)
			}
		}
		if m.ID == metricID("metric-closures-actor", "uuid-ana") && (m.Base == nil || m.Base.Observed != 1 || m.Base.Total != 2 || m.Base.Low) {
			t.Fatalf("conclusões por ator sem base do histórico: %+v", m.Base)
		}
	}
	if found < 3 {
		t.Fatalf("métricas de ator esperadas com pseudônimo: %d", found)
	}
	if pseudonymLetters(25) != "Z" || pseudonymLetters(26) != "AA" || pseudonymLetters(27) != "AB" {
		t.Fatal("sequência de pseudônimos")
	}
}

// Two months by two teams: one table cell per team and month, with the
// created, completed and canceled counts of that month.
func TestEvolutionMetricsCountPerMonthAndTeam(t *testing.T) {
	d := query.Dataset{Snapshot: contract.Snapshot{WindowEnd: "2026-11-01T00:00:00Z"}}
	issue := func(id, team, created, outcome, closed string) {
		i := query.Issue{ID: id, Source: "linear", TeamID: team, CreatedAt: created, Outcome: outcome}
		switch outcome {
		case "completed":
			i.CompletedAt = closed
		case "cancelled":
			i.CanceledAt = closed
		}
		d.Issues = append(d.Issues, i)
	}
	issue("e1", "eng", "2026-07-22T00:00:00Z", "completed", "2026-08-03T00:00:00Z")
	issue("e2", "eng", "2026-07-23T00:00:00Z", "", "")
	issue("e3", "eng", "2026-08-01T00:00:00Z", "cancelled", "2026-08-02T00:00:00Z")
	issue("p1", "prd", "2026-07-30T00:00:00Z", "completed", "2026-07-31T00:00:00Z")
	issue("p2", "prd", "2026-08-10T00:00:00Z", "duplicate", "")
	d.Issues[0].CycleID, d.Issues[2].CycleID = "cyc-1", "cyc-1"
	d.Cycles = []query.Cycle{{ID: "cyc-1", TeamID: "eng", Number: 3, StartsAt: "2026-07-27T00:00:00Z", EndsAt: "2026-08-10T00:00:00Z"}}
	d.Components = []contract.ComponentRecord{{ID: "proj-abc", Name: "Assinatura digital", Kind: "linear_project"}}
	d.Links = []contract.LinkRecord{{ID: "l1", SourceID: "e1", Relation: "belongs_to_project", TargetID: "proj-abc"}, {ID: "l2", SourceID: "e2", Relation: "belongs_to_project", TargetID: "proj-abc"}}
	cells := map[string][3]string{}
	other := map[string]contract.Metric{}
	for _, m := range evolutionMetrics(d) {
		dim := m.Dimensions
		switch dim["series"] {
		case SeriesCreated, SeriesCompleted, SeriesCanceled:
			key := dim["month"] + " " + dim["team"]
			c := cells[key]
			c[map[string]int{SeriesCreated: 0, SeriesCompleted: 1, SeriesCanceled: 2}[dim["series"]]] = m.Value.Value
			cells[key] = c
		default:
			other[dim["series"]] = m
		}
	}
	want := map[string][3]string{
		"2026-07 eng": {"2", "0", "0"},
		"2026-08 eng": {"1", "1", "1"},
		"2026-07 prd": {"1", "1", "0"},
		"2026-08 prd": {"1", "0", "0"},
	}
	if len(cells) != 4 {
		t.Fatalf("esperadas 4 células: %v", cells)
	}
	for key, value := range want {
		if cells[key] != value {
			t.Fatalf("%s = %v, esperado %v (todas: %v)", key, cells[key], value, cells)
		}
	}
	if m := other[SeriesCycleScope]; m.Value.Value != "2" || m.Dimensions["cycle"] != "3" || m.Dimensions["starts_at"] != "2026-07-27T00:00:00Z" {
		t.Fatalf("escopo final do ciclo: %+v", m)
	}
	if m := other[SeriesCycleCompleted]; m.Value.Value != "1" {
		t.Fatalf("concluídas do ciclo: %+v", m)
	}
	if m := other[SeriesProjectIssues]; m.Value.Value != "2" || m.Dimensions["name"] != "Assinatura digital" || !strings.Contains(m.Name, "Assinatura digital") {
		t.Fatalf("projeto pelo nome: %+v", m)
	}
}
