package insights

import (
	"strings"

	"pocketcli/internal/investigation/insightcontract"
)

const timelineVersion = "intent-timeline-v1"

func actorLabel(source insightcontract.SourceBundle, id *string) string {
	if id == nil || *id == "" {
		return "Não identificado na fonte"
	}
	for _, actor := range source.Actors {
		if actor.ID == *id && actor.Label != "" {
			return actor.Label
		}
	}
	return *id // Preserve the recorded identity; never substitute an assignee.
}

type timelineEvidence map[string]insightcontract.EvidenceView

func timelineEvidenceIndex(source insightcontract.SourceBundle) timelineEvidence {
	out := make(timelineEvidence, len(source.Evidence))
	for _, evidence := range source.Evidence {
		out[evidence.ID] = evidence
	}
	return out
}

func verifiedTimelineEvidence(index timelineEvidence, ids []string) bool {
	if len(ids) == 0 {
		return false
	}
	for _, id := range ids {
		if evidence, ok := index[id]; !ok || evidence.IntegrityStatus != "verified" {
			return false
		}
	}
	return true
}

func decisionTimelineItem(source insightcontract.SourceBundle, decision insightcontract.DecisionRecord, evidenceIDs []string, index timelineEvidence) insightcontract.TimelineItem {
	item := insightcontract.TimelineItem{ID: decision.ID, Kind: "decision", At: decision.DecidedAt, Scope: decision.Scope.Description, Source: "analysis", Summary: decision.Question + " → " + decision.Choice, EvidenceIDs: evidenceIDs, DecisionMakerID: decision.DecisionMakerID, RecordedByID: decision.RecordedByID, Rationale: decision.Rationale, RationaleBasis: "unknown", Confidence: "undetermined"}
	item.DecisionMakerLabel = actorLabel(source, decision.DecisionMakerID)
	item.RecordedByLabel = actorLabel(source, decision.RecordedByID)
	item.Limitations = []string{"Decisor e autor do registro são os informados pela análise; a atribuição precisa de confirmação na fonte."}
	if strings.TrimSpace(decision.Rationale) == "" {
		return item
	}
	if !verifiedTimelineEvidence(index, evidenceIDs) {
		item.Limitations = append(item.Limitations, "Evidências insuficientes para verificar a justificativa da análise.")
		return item
	}
	item.RationaleBasis, item.Confidence = "hypothesis", "low"
	item.Limitations = append(item.Limitations, "Justificativa interpretada pela análise; confirme com quem tomou a decisão.")
	// A model's method label alone cannot prove that its paraphrase was stated.
	// Only a rationale preserved in a verified source excerpt is declared.
	if decision.Method == "source" && len([]rune(decision.Rationale)) >= 20 && verifiedTimelineEvidence(index, evidenceIDs) {
		for _, citation := range decision.Citations {
			if evidence, ok := index[citation.EvidenceID]; ok {
				if evidence.ID == citation.EvidenceID && strings.TrimSpace(evidence.Excerpt) == strings.TrimSpace(citation.Quote) && strings.TrimSpace(citation.Quote) == strings.TrimSpace(decision.Rationale) {
					item.RationaleBasis, item.Confidence = "source", "recorded"
					item.Limitations = []string{"Motivo registrado na fonte; não comprova que foi a única motivação.", "Decisor e autor do registro são os informados pela análise; a atribuição precisa de confirmação na fonte."}
					return item
				}
			}
		}
	}
	return item
}

func enrichDecisionTimeline(source insightcontract.SourceBundle, items []insightcontract.TimelineItem) []insightcontract.TimelineItem {
	index := timelineEvidenceIndex(source)
	entities := map[string]insightcontract.Entity{}
	partial := map[string]bool{}
	for _, coverage := range source.Coverage {
		if coverage.Status != "complete" && coverage.Status != "not_selected" {
			partial[coverage.Key.Source+":"+coverage.Key.ResourceID] = true
		}
	}
	for _, entity := range source.Entities {
		entities[entity.ID] = entity
	}
	for n := range items {
		item := &items[n]
		entity := entities[item.EntityID]
		item.EntityIdentifier = entity.Identifier
		if item.ActorID != nil {
			item.ActorLabel = actorLabel(source, item.ActorID)
		}
		if item.Kind != "observation" {
			continue
		}
		title, rationale, alternative := changeIntent(item.EventType, item.OldValue, item.NewValue)
		if title == "" {
			continue
		}
		item.Summary = title
		item.RationaleBasis, item.Confidence = "unknown", "undetermined"
		item.Limitations = []string{"O ator registrado executou a mudança; isso não identifica automaticamente quem decidiu ou aprovou."}
		if rationale != "" && verifiedTimelineEvidence(index, item.EvidenceIDs) {
			item.Rationale, item.RationaleBasis, item.Confidence = rationale, "hypothesis", "low"
			item.Alternatives = []string{alternative}
			item.Limitations = append(item.Limitations, "Hipótese operacional baseada na mudança; não atribui motivação pessoal nem demonstra causa.")
		} else {
			item.Limitations = append(item.Limitations, "Mudança ou evidência insuficiente para sugerir intenção.")
		}
		if partial[entity.Source+":"+entity.ResourceID] {
			item.Limitations = append(item.Limitations, "Histórico parcial neste recurso: pode haver decisões e mudanças não observadas.")
		}
	}
	return items
}

func changeIntent(kind string, before, after *string) (title, rationale, alternative string) {
	old, next := "", ""
	if before != nil {
		old = strings.ToLower(strings.TrimSpace(*before))
	}
	if after != nil {
		next = strings.ToLower(strings.TrimSpace(*after))
	}
	switch kind {
	case "state_changed":
		title = "Estado alterado"
		if old == "" || next == "" || old == next {
			return
		}
		switch next {
		case "started", "in_progress":
			rationale = "Pode indicar a intenção de iniciar ou retomar a execução."
		case "backlog", "unstarted":
			rationale = "Pode indicar adiamento ou replanejamento do trabalho."
		case "canceled", "cancelled":
			rationale = "Pode indicar a retirada deste item do escopo previsto."
		case "completed", "done":
			rationale = "Pode indicar a intenção de encerrar o trabalho ou registrar sua conclusão."
		default:
			rationale = "Pode indicar a intenção de mover o item para outra etapa do fluxo."
		}
		alternative = "Também pode ser uma correção administrativa do estado, sem mudança na prioridade ou no trabalho real."
	case "project_changed":
		title = "Projeto alterado"
		if old == next {
			return
		}
		rationale, alternative = "Pode indicar reorganização do escopo ou do agrupamento do trabalho.", "Também pode corrigir um vínculo cadastrado no projeto errado."
	case "estimate_changed":
		title = "Estimativa alterada"
		if old == "" || next == "" || old == next {
			return
		}
		rationale, alternative = "Pode indicar revisão do planejamento após novo entendimento do trabalho.", "Também pode ser uma correção de cadastro ou da escala usada, sem alteração do escopo."
	case "reopened":
		title, rationale, alternative = "Item reaberto", "Pode indicar a intenção de retomar o trabalho ou rever seu encerramento.", "Também pode corrigir um fechamento indevido; a reabertura não comprova defeito."
	case "closed", "merged":
		title, rationale, alternative = "Item encerrado ou integrado", "Pode indicar encerramento ou integração do trabalho observado.", "Também pode ser uma consolidação administrativa; não comprova aceite de negócio."
	case "human_reviewed":
		title = "Revisão registrada"
		if next == "approved" {
			rationale = "Pode indicar a intenção de permitir que a mudança avance para integração."
		}
		if next == "changes_requested" {
			rationale = "Pode indicar a intenção de obter ajustes antes de avançar."
		}
		alternative = "A revisão pode estar limitada a parte do conteúdo e não representa a motivação completa da decisão."
	case "commit_revert_declared":
		title, rationale, alternative = "Reversão declarada", "Pode indicar a intenção de desfazer uma alteração anterior.", "Também pode integrar uma reorganização temporária da implementação; não comprova falha da mudança anterior."
	}
	return
}

func decisionTimeline(source insightcontract.SourceBundle, decisions []insightcontract.DecisionRecord) []insightcontract.TimelineItem {
	entities := map[string]bool{}
	for _, entity := range source.Entities {
		entities[entity.ID] = true
	}
	return buildTimeline(source, decisions, entities, true)
}
