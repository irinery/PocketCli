package rules

import (
	"fmt"
	"testing"
	"time"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/query"
)

func findingsByRule(d query.Dataset) map[string][]contract.Finding {
	out := map[string][]contract.Finding{}
	for _, f := range Evaluate(d).Findings {
		out[f.RuleID] = append(out[f.RuleID], f)
	}
	return out
}

func TestBlockSeverityFollowsDependentStateAndGroupsBlockers(t *testing.T) {
	d := query.Dataset{
		Issues: []query.Issue{
			{ID: "planned", State: "backlog", Outcome: "active", EvidenceID: "ev-planned"},
			{ID: "working", State: "started", Outcome: "active", EvidenceID: "ev-working"},
			{ID: "a", State: "backlog", Outcome: "active", EvidenceID: "ev-a"},
			{ID: "b", State: "unstarted", Outcome: "active", EvidenceID: "ev-b"},
		},
		Links: []contract.LinkRecord{
			{ID: "l1", SourceID: "planned", Relation: "blocked_by", TargetID: "a", Status: "direct", EvidenceID: "ev-l1"},
			{ID: "l2", SourceID: "planned", Relation: "blocked_by", TargetID: "b", Status: "direct", EvidenceID: "ev-l2"},
			{ID: "l3", SourceID: "working", Relation: "blocked_by", TargetID: "a", Status: "direct", EvidenceID: "ev-l3"},
		},
	}
	got := findingsByRule(d)
	if len(got["R-BLOCK-001"]) != 1 || got["R-BLOCK-001"][0].SubjectID != "working" || got["R-BLOCK-001"][0].Severity != "medium" {
		t.Fatalf("item em andamento bloqueado: %+v", got["R-BLOCK-001"])
	}
	planned := got["R-BLOCK-002"]
	if len(planned) != 1 || planned[0].SubjectID != "planned" || planned[0].Severity != "info" || len(planned[0].EvidenceIDs) != 5 {
		t.Fatalf("dependência planejada deveria virar um achado info com os dois bloqueadores: %+v", planned)
	}
}

func TestBatchCreationFindsProjectWithManyIssuesInADay(t *testing.T) {
	base := time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)
	d := query.Dataset{Snapshot: contract.Snapshot{WindowStart: "2026-10-01T00:00:00Z", WindowEnd: "2026-11-01T00:00:00Z"}, Components: []contract.ComponentRecord{{ID: "proj-docs", Name: "Manuais"}, {ID: "proj-slow", Name: "Lento"}}}
	add := func(project string, i int, at time.Time) {
		id := fmt.Sprintf("%s-%d", project, i)
		d.Issues = append(d.Issues, query.Issue{ID: id, Source: "linear", CreatedAt: at.Format(time.RFC3339), Outcome: "active", State: "backlog", EvidenceID: "ev-" + id})
		d.Links = append(d.Links, contract.LinkRecord{ID: "link-" + id, SourceID: id, Relation: "belongs_to_project", TargetID: project, Status: "direct", EvidenceID: "ev-" + id})
	}
	for i := 0; i < BatchCreationThreshold; i++ {
		add("proj-docs", i, base.Add(time.Duration(i)*time.Hour))
		add("proj-slow", i, base.Add(time.Duration(i)*48*time.Hour))
	}
	got := findingsByRule(d)["R-BATCH-001"]
	if len(got) != 1 || got[0].SubjectID != "proj-docs" || len(got[0].EvidenceIDs) != findingEvidenceSample {
		t.Fatalf("lote esperado só no projeto rápido: %+v", got)
	}
}

func TestIssuesOlderThanTheirTeamAreReportedOncePerTeam(t *testing.T) {
	d := query.Dataset{
		Snapshot: contract.Snapshot{Resources: []contract.Resource{{Source: "linear", ID: "team-b", Name: "B", CreatedAt: "2026-09-20T00:00:00Z"}}},
		Issues: []query.Issue{
			{ID: "old-1", Source: "linear", TeamID: "team-b", CreatedAt: "2026-08-28T10:00:00Z", EvidenceID: "ev-1"},
			{ID: "old-2", Source: "linear", TeamID: "team-b", CreatedAt: "2026-09-01T10:00:00Z", EvidenceID: "ev-2"},
			{ID: "new", Source: "linear", TeamID: "team-b", CreatedAt: "2026-10-01T10:00:00Z", EvidenceID: "ev-3"},
		},
	}
	got := findingsByRule(d)["R-MOVE-001"]
	if len(got) != 1 || len(got[0].EvidenceIDs) != 2 {
		t.Fatalf("movimentação entre teams: %+v", got)
	}
}

func TestWeightMetricsPointsDuplicatesGapsAndRejections(t *testing.T) {
	two, three := 2.0, 3.0
	d := query.Dataset{
		Snapshot: contract.Snapshot{WindowEnd: "2026-11-01T00:00:00Z"},
		Issues: []query.Issue{
			{ID: "i1", Identifier: "ENG-1", Source: "linear", TeamID: "eng", Outcome: "completed", State: "completed", CompletedAt: "2026-10-01T00:00:00Z", EstimateCurrent: &two, EvidenceID: "e1"},
			{ID: "i2", Identifier: "ENG-2", Source: "linear", TeamID: "eng", Outcome: "completed", State: "completed", CompletedAt: "2026-10-02T00:00:00Z", EstimateCurrent: &three, EvidenceID: "e2"},
			{ID: "i3", Identifier: "ENG-3", Source: "linear", TeamID: "eng", Outcome: "completed", State: "completed", CompletedAt: "2026-10-03T00:00:00Z", EvidenceID: "e3"},
			{ID: "i4", Identifier: "ENG-7", Source: "linear", TeamID: "eng", Outcome: "duplicate", State: "duplicate", EvidenceID: "e4"},
		},
		Stages: []contract.StageRecord{{ID: "s1", SubjectID: "i2", Stage: "linear:Reprovado", StartedAt: "2026-10-01T12:00:00Z", EvidenceID: "e-s1"}, {ID: "s2", SubjectID: "i2", Stage: "linear:QA", StartedAt: "2026-10-01T10:00:00Z", EvidenceID: "e-s2"}},
	}
	out := Evaluate(d)
	values := map[string]contract.Metric{}
	for _, m := range out.Metrics {
		values[m.ID] = m
	}
	points := values[metricID("metric-completed-points-team", "eng")]
	if points.Value.Value != "5" || points.SampleSize != 2 || points.EligibleSize == nil || *points.EligibleSize != 3 {
		t.Fatalf("pontos entregues: %+v", points)
	}
	if dup := values[metricID("metric-duplicates-team", "eng")]; dup.Value.Value != "1" || *dup.EligibleSize != 4 {
		t.Fatalf("duplicadas: %+v", dup)
	}
	if gaps := values[metricID("metric-identifier-gaps-team", "eng")]; gaps.Value.Value != "3" || *gaps.EligibleSize != 7 {
		t.Fatalf("lacunas de identificador: %+v", gaps)
	}
	rejections := 0
	for _, r := range out.Rework {
		if r.Kind == "stage_rejection" {
			rejections++
		}
	}
	if rejections != 1 || values[metricID("metric-rework", "stage_rejection")].Value.Value != "1" {
		t.Fatalf("reprovação como retrabalho: rework=%+v", out.Rework)
	}
}
