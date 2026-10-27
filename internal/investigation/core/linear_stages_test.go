package core

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/query"
	"pocketcli/internal/investigation/storage"
)

func linearHistoryItem(id, at, from, to string) json.RawMessage {
	state := func(name string) string {
		if name == "" {
			return "null"
		}
		return fmt.Sprintf(`{"name":%q,"type":"started"}`, name)
	}
	return json.RawMessage(fmt.Sprintf(`{"id":%q,"createdAt":%q,"fromState":%s,"toState":%s}`, id, at, state(from), state(to)))
}

func stagesBySubject(d query.Dataset, subject string) map[string]contract.StageRecord {
	out := map[string]contract.StageRecord{}
	for _, stage := range d.Stages {
		if stage.SubjectID == subject {
			out[stage.Stage] = stage
		}
	}
	return out
}

// Estados do workflow real do team ENG (Somos UNI). Uma transição ausente,
// janela parcial ou saída posterior à janela nunca vira duração.
func TestLinearStagesUseOnlyConsistentTransitionsInsideWindow(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCore(CoreConfig{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	plan := contract.Plan{WindowStart: "2026-01-03T00:00:00Z", WindowEnd: "2026-01-15T00:00:00Z"}
	issue := query.Issue{ID: "linear-1", Identifier: "ENG-1", Source: "linear", TeamID: "team-eng", ComponentID: "cmp-api", CreatedAt: "2026-01-01T00:00:00Z", EvidenceID: "ev-issue"}
	items := []json.RawMessage{
		linearHistoryItem("h4", "2026-01-20T00:00:00Z", "Pronto p/ Merge", "Finalizado/Merged"),
		linearHistoryItem("h1", "2026-01-02T00:00:00Z", "Backlog", "Em Desenvolvimento"),
		linearHistoryItem("h2", "2026-01-05T00:00:00Z", "Em Desenvolvimento", "Em revisão"),
		json.RawMessage(`{"id":"h-est","createdAt":"2026-01-05T12:00:00Z","fromEstimate":2,"toEstimate":3}`),
		linearHistoryItem("h3", "2026-01-06T00:00:00Z", "QA", "Pronto p/ Merge"),
	}
	d := query.Dataset{Stages: []contract.StageRecord{{ID: "pr-stage", SubjectID: "pr-1", Stage: "review_wait"}}}
	if err := c.projectLinearStages(plan, issue, items, &d); err != nil {
		t.Fatal(err)
	}
	stages := stagesBySubject(d, issue.ID)
	if _, ok := stages["linear:Backlog"]; ok {
		t.Fatal("intervalo encerrado antes da janela não deveria entrar")
	}
	if _, ok := stages["linear:Finalizado/Merged"]; ok {
		t.Fatal("intervalo iniciado após a janela não deveria entrar")
	}
	dev := stages["linear:Em Desenvolvimento"]
	if !dev.DurationValid || dev.ElapsedSeconds == nil || *dev.ElapsedSeconds != 3*86400 || dev.ComponentID != "cmp-api" || dev.StartedAt != "2026-01-02T00:00:00Z" {
		t.Fatalf("Em Desenvolvimento: %+v", dev)
	}
	review := stages["linear:Em revisão"]
	if review.EndedAt != "2026-01-06T00:00:00Z" || review.ElapsedSeconds != nil || review.DurationValid {
		t.Fatalf("saída por outro estado indica transição ausente e não deveria ter duração: %+v", review)
	}
	merge := stages["linear:Pronto p/ Merge"]
	if merge.EndedAt != "" || merge.ElapsedSeconds != nil || merge.DurationValid {
		t.Fatalf("saída após a janela deve ficar aberta no snapshot: %+v", merge)
	}
	if len(stages) != 3 {
		t.Fatalf("estágios inesperados: %+v", stages)
	}
	evidence := map[string]bool{}
	for _, e := range d.Evidence {
		evidence[e.ID] = true
	}
	for name, stage := range stages {
		if !evidence[stage.EvidenceID] || stage.ID == "" || !strings.HasPrefix(stage.ID, "stage-") {
			t.Fatalf("%s sem evidência ou ID estável: %+v", name, stage)
		}
	}
	firstIDs := map[string]string{}
	for name, stage := range stages {
		firstIDs[name] = stage.ID
	}
	if err := c.projectLinearStages(plan, issue, items, &d); err != nil {
		t.Fatal(err)
	}
	again := stagesBySubject(d, issue.ID)
	if len(again) != 3 || len(d.Stages) != 4 {
		t.Fatalf("reprocessar duplicou intervalos: %d estágios no total", len(d.Stages))
	}
	for name, stage := range again {
		if firstIDs[name] != stage.ID {
			t.Fatalf("ID de %s não é determinístico", name)
		}
	}
}

func TestLinearStageStartsAtCreationAndRetentionFollowsNewWindow(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCore(CoreConfig{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	plan := contract.Plan{WindowStart: "2026-01-01T00:00:00Z", WindowEnd: "2026-02-01T00:00:00Z"}
	issue := query.Issue{ID: "linear-2", Identifier: "ENG-2", Source: "linear", TeamID: "team-eng", CreatedAt: "2026-01-02T00:00:00Z", EvidenceID: "ev-issue-2"}
	d := query.Dataset{Issues: []query.Issue{issue}}
	if err := c.projectLinearStages(plan, issue, []json.RawMessage{linearHistoryItem("h1", "2026-01-12T00:00:00Z", "Aberta", "QA")}, &d); err != nil {
		t.Fatal(err)
	}
	initial := stagesBySubject(d, issue.ID)["linear:Aberta"]
	if initial.StartedAt != issue.CreatedAt || initial.EvidenceID != "ev-issue-2" || initial.ElapsedSeconds == nil || *initial.ElapsedSeconds != 10*86400 || !initial.DurationValid {
		t.Fatalf("estado inicial desde a criação: %+v", initial)
	}
	// Refresh com janela que termina antes da saída observada: no novo
	// snapshot o item ainda estava na etapa; não há duração.
	next := query.Dataset{Issues: []query.Issue{issue}}
	referenced := map[string]bool{}
	retainLinearStages(&next, d.Stages, "2026-01-01T00:00:00Z", "2026-01-10T00:00:00Z", referenced)
	retained := stagesBySubject(next, issue.ID)
	if len(retained) != 1 || retained["linear:Aberta"].EndedAt != "" || retained["linear:Aberta"].DurationValid || !referenced["ev-issue-2"] {
		t.Fatalf("retenção não reavaliou a janela: %+v referenced=%v", retained, referenced)
	}
	// Issue não retida não traz intervalos.
	orphan := query.Dataset{}
	retainLinearStages(&orphan, d.Stages, plan.WindowStart, plan.WindowEnd, map[string]bool{})
	if len(orphan.Stages) != 0 {
		t.Fatalf("intervalos de issue fora do snapshot: %+v", orphan.Stages)
	}
}
