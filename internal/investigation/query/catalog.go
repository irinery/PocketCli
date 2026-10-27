package query

import "pocketcli/internal/investigation/contract"

// CatalogVersion 1.1.0 adds Q11 (time per stage). Q01-Q10 keep the C.8 text.
const CatalogVersion = "1.1.0"

func Catalog() []contract.SuggestedQuery {
	return []contract.SuggestedQuery{
		{ID: "Q01", Title: "Mapa por componente", SQL: `SELECT component_id, name, repository, path, implementation_status, deployment_status, coverage FROM v_components ORDER BY repository, path, component_id`},
		{ID: "Q02", Title: "Achados por prioridade", SQL: `SELECT finding_id, rule_id, severity, subject_id, title, evidence_status, coverage FROM v_findings ORDER BY CASE severity WHEN 'critical' THEN 0 WHEN 'high' THEN 1 WHEN 'medium' THEN 2 WHEN 'low' THEN 3 ELSE 4 END, finding_id`},
		{ID: "Q03", Title: "Espera por revisão", SQL: `SELECT pr_id, repository, number, review_wait_seconds / 3600.0 AS wait_hours FROM v_pull_requests WHERE review_wait_seconds IS NOT NULL AND review_wait_seconds >= :min_seconds ORDER BY review_wait_seconds DESC, pr_id`, Parameters: []contract.Parameter{{Name: "min_seconds", Value: contract.Cell{Kind: "integer", Value: "86400"}}}},
		{ID: "Q04", Title: "Issues reabertas", SQL: `SELECT issue_id, identifier, title, reopen_count FROM v_issues WHERE reopen_count > 0 ORDER BY reopen_count DESC, issue_id`},
		{ID: "Q05", Title: "Estimativa alterada após início", SQL: `SELECT issue_id, before_value, after_value, event_at, actor_id FROM v_estimate_changes WHERE timing = 'after_start' ORDER BY event_at, change_id`},
		{ID: "Q06", Title: "Sem vínculo explícito localizado", Description: "Lista issues sem references_pr direto no snapshot. Com cobertura parcial de vínculos, o resultado não comprova ausência; confira Q07 antes de interpretar.", SQL: `SELECT i.issue_id, i.identifier, i.title FROM v_issues i WHERE NOT EXISTS (SELECT 1 FROM v_links l WHERE l.source_id = i.issue_id AND l.relation = 'references_pr' AND l.status = 'direct') ORDER BY i.issue_id`},
		{ID: "Q07", Title: "Cobertura", SQL: `SELECT source, resource_id, collection, status, observed_count, expected_count, reason FROM v_coverage ORDER BY source, resource_id, collection`},
		{ID: "Q08", Title: "Timeline", Description: "Escolha o ID do alvo antes de executar a consulta.", SQL: `SELECT event_id, event_type, event_at, observed_at, old_value, new_value FROM v_events WHERE entity_id = :subject_id ORDER BY event_at, observed_at, event_id`, Parameters: []contract.Parameter{{Name: "subject_id", Value: contract.Cell{Kind: "text", Value: ""}}}},
		{ID: "Q09", Title: "Volume observado por responsável", Description: "Não é score de produtividade", SQL: `SELECT assignee_id, COUNT(*) AS completed_items FROM v_issues WHERE completed_at IS NOT NULL AND is_context = 0 GROUP BY assignee_id ORDER BY completed_items DESC, assignee_id`},
		{ID: "Q10", Title: "Retrabalho registrado", SQL: `SELECT occurrence_id, subject_id, kind, evidence_status, cause_status FROM v_rework ORDER BY event_at, occurrence_id`},
		{ID: "Q11", Title: "Permanência por etapa", Description: "Intervalos por etapa: espera de revisão de PR e estados do workflow Linear (prefixo linear:). Média só orienta; mediana e p85 com amostra mínima de 10 estão em v_metrics. Duração ausente indica transição faltante ou saída fora da janela, não tempo zero.", SQL: `SELECT stage, COUNT(*) AS intervals, SUM(duration_valid) AS valid_durations, SUM(CASE WHEN ended_at IS NULL THEN 1 ELSE 0 END) AS open_at_window_end, AVG(CASE WHEN duration_valid = 1 THEN elapsed_seconds END) / 3600.0 AS avg_valid_hours, MAX(CASE WHEN duration_valid = 1 THEN elapsed_seconds END) / 3600.0 AS max_valid_hours FROM v_stage_times GROUP BY stage ORDER BY stage`},
	}
}
