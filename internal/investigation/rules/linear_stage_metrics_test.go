package rules

import (
	"fmt"
	"testing"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/query"
)

func stageMetric(metrics []contract.Metric, prefix, key string) (contract.Metric, bool) {
	id := metricID(prefix, key)
	for _, metric := range metrics {
		if metric.ID == id {
			return metric, true
		}
	}
	return contract.Metric{}, false
}

func TestLinearStageMetricsNeedTenValidExitsAndCountOpenSeparately(t *testing.T) {
	d := query.Dataset{Snapshot: contract.Snapshot{WindowStart: "2026-01-01T00:00:00Z", WindowEnd: "2026-02-01T00:00:00Z"}}
	add := func(id, team, stage, ended string, seconds *int64, valid bool) {
		d.Issues = append(d.Issues, query.Issue{ID: id, Source: "linear", TeamID: team})
		d.Stages = append(d.Stages, contract.StageRecord{ID: "s-" + id + stage, SubjectID: id, Stage: "linear:" + stage, StartedAt: "2026-01-02T00:00:00Z", EndedAt: ended, ElapsedSeconds: seconds, DurationValid: valid})
	}
	for i := 1; i <= 10; i++ {
		seconds := int64(i * 3600)
		add(fmt.Sprintf("qa-%d", i), "eng", "QA", "2026-01-10T00:00:00Z", &seconds, true)
	}
	add("qa-gap", "eng", "QA", "2026-01-11T00:00:00Z", nil, false) // transição ausente
	add("qa-open", "eng", "QA", "", nil, false)
	two := int64(7200)
	add("rev-1", "eng", "Em revisão", "2026-01-10T00:00:00Z", &two, true)
	add("rev-other-team", "prd", "Em revisão", "2026-01-10T00:00:00Z", &two, true)
	d.Stages = append(d.Stages, contract.StageRecord{ID: "pr", SubjectID: "pr-1", Stage: "review_wait", EndedAt: "2026-01-10T00:00:00Z", ElapsedSeconds: &two, DurationValid: true})

	metrics := linearStageMetrics(d)
	median, ok := stageMetric(metrics, "metric-stage-median-team", "eng\x00QA")
	if !ok || median.Value.Kind != "real" || median.Value.Value != "19800" || median.SampleSize != 10 || median.EligibleSize == nil || *median.EligibleSize != 11 {
		t.Fatalf("mediana QA: %+v", median)
	}
	p85, _ := stageMetric(metrics, "metric-stage-p85-team", "eng\x00QA")
	if p85.Value.Value != "32400" {
		t.Fatalf("p85 QA nearest rank: %+v", p85)
	}
	open, _ := stageMetric(metrics, "metric-stage-open-team", "eng\x00QA")
	if open.Value.Value != "1" {
		t.Fatalf("abertos em QA: %+v", open)
	}
	review, _ := stageMetric(metrics, "metric-stage-median-team", "eng\x00Em revisão")
	if review.Value.Kind != "null" || review.SampleSize != 1 {
		t.Fatalf("amostra pequena deve ficar NULL: %+v", review)
	}
	if _, ok := stageMetric(metrics, "metric-stage-median-team", "prd\x00Em revisão"); !ok {
		t.Fatal("teams diferentes devem ter coortes separadas")
	}
	if len(metrics) != 9 {
		t.Fatalf("esperadas 3 métricas para cada um de 3 grupos, veio %d", len(metrics))
	}
	evaluated := Evaluate(d)
	if _, ok := stageMetric(evaluated.Metrics, "metric-stage-median-team", "eng\x00QA"); !ok {
		t.Fatal("Evaluate não publicou métricas de etapa")
	}
}
