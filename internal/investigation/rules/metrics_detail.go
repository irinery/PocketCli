package rules

import (
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/query"
	"pocketcli/internal/investigation/storage"
)

func metricID(prefix, subject string) string {
	return prefix + "-" + storage.Hash([]byte(subject))[:16]
}

func integerMetric(id, name string, value, sample int64, eligible *int64, definition string, limitations ...string) contract.Metric {
	return contract.Metric{ID: id, Name: name, Value: contract.Cell{Kind: "integer", Value: strconv.FormatInt(value, 10)}, Unit: "items", SampleSize: sample, EligibleSize: eligible, Definition: definition, Limitations: limitations}
}

func observedInWindow(at string, snapshot contract.Snapshot) bool {
	if at == "" {
		return false
	}
	stamp, err := time.Parse(time.RFC3339, at)
	if err != nil {
		return false
	}
	if snapshot.WindowStart != "" {
		start, err := time.Parse(time.RFC3339, snapshot.WindowStart)
		if err != nil || stamp.Before(start) {
			return false
		}
	}
	if snapshot.WindowEnd != "" {
		end, err := time.Parse(time.RFC3339, snapshot.WindowEnd)
		if err != nil || !stamp.Before(end) {
			return false
		}
	}
	return true
}

func actorMetrics(d query.Dataset) []contract.Metric {
	issueByID := make(map[string]query.Issue, len(d.Issues))
	assigned := make(map[string]int64)
	completedEligible, assigneeUnknown := int64(0), int64(0)
	for _, issue := range d.Issues {
		issueByID[issue.ID] = issue
		if issue.IsContext || !observedInWindow(issue.CompletedAt, d.Snapshot) {
			continue
		}
		completedEligible++
		if issue.AssigneeID == "" {
			assigneeUnknown++
		} else {
			assigned[issue.AssigneeID]++
		}
	}
	closures := make(map[string]int64)
	reviews := make(map[string]map[string]bool)
	merged := make(map[string]int64)
	for _, event := range d.Events {
		if event.ActorID == "" || !observedInWindow(event.EventAt, d.Snapshot) {
			continue
		}
		if event.Type == "state_changed" && isDone(event.NewValue) && !isDone(event.OldValue) {
			if issue, ok := issueByID[event.EntityID]; ok && !issue.IsContext {
				closures[event.ActorID]++
			}
		}
		if event.Type == "human_reviewed" {
			if reviews[event.ActorID] == nil {
				reviews[event.ActorID] = make(map[string]bool)
			}
			reviews[event.ActorID][event.EntityID] = true
		}
	}
	for _, pr := range d.PullRequests {
		if pr.AuthorID != "" && observedInWindow(pr.MergedAt, d.Snapshot) {
			merged[pr.AuthorID]++
		}
	}
	actors := make(map[string]bool)
	for actor := range closures {
		actors[actor] = true
	}
	for actor := range reviews {
		actors[actor] = true
	}
	for actor := range merged {
		actors[actor] = true
	}
	for actor := range assigned {
		actors[actor] = true
	}
	ids := make([]string, 0, len(actors))
	for actor := range actors {
		ids = append(ids, actor)
	}
	sort.Strings(ids)
	out := make([]contract.Metric, 0, len(ids)*4+1)
	for _, actor := range ids {
		label := strings.TrimSpace(actor)
		if len(label) > 128 {
			label = label[:128]
		}
		limit := "Contagens observadas, sem atribuição causal ou score de produtividade; bots e identidade histórica podem ser desconhecidos."
		if closures[actor] > 0 {
			out = append(out, integerMetric(metricID("metric-closures-actor", actor), "Conclusões registradas por "+label, closures[actor], closures[actor], nil, "Transições de estado para concluído registradas com este ator na janela; quem registrou não necessariamente resolveu", limit))
		}
		if assigned[actor] > 0 {
			out = append(out, integerMetric(metricID("metric-completed-assignee", actor), "Issues concluídas com responsável observado "+label, assigned[actor], assigned[actor], &completedEligible, "Issues não contextuais com completed_at na janela e assignee_id observado no snapshot", "Responsável do snapshot pode ser diferente do responsável no encerramento; não atribui execução, autoria ou produtividade."))
		}
		if merged[actor] > 0 {
			out = append(out, integerMetric(metricID("metric-merged-prs-author", actor), "PRs mesclados de "+label, merged[actor], merged[actor], nil, "PRs com autor informado e merged_at na janela", limit))
		}
		if len(reviews[actor]) > 0 {
			count := int64(len(reviews[actor]))
			out = append(out, integerMetric(metricID("metric-reviewed-prs-actor", actor), "PRs revisados por "+label, count, count, nil, "PRs distintos com evento de revisão humana deste ator na janela", limit))
		}
	}
	if assigneeUnknown > 0 {
		out = append(out, integerMetric("metric-completed-assignee-unknown", "Issues concluídas sem responsável conhecido", assigneeUnknown, assigneeUnknown, &completedEligible, "Issues não contextuais com completed_at na janela e assignee_id ausente no snapshot", "Ausência de responsável não identifica quem executou o trabalho."))
	}
	return out
}

// A value before start is reported only when the team's issue history was
// completely read from the start of the selected window. A later snapshot
// value is never projected backwards as a historical estimate.
func fillEstimateBeforeStart(d query.Dataset) []query.Issue {
	completeTeams := make(map[string]bool)
	seenTeams := make(map[string]bool)
	for _, coverage := range d.Coverage {
		if coverage.Source == "linear" && coverage.Collection == "history" {
			if !seenTeams[coverage.ResourceID] {
				completeTeams[coverage.ResourceID] = true
				seenTeams[coverage.ResourceID] = true
			}
			completeTeams[coverage.ResourceID] = completeTeams[coverage.ResourceID] && coverage.Status == "complete"
		}
	}
	changes := make(map[string][]contract.EstimateRecord)
	for _, change := range d.Estimates {
		changes[change.IssueID] = append(changes[change.IssueID], change)
	}
	out := append([]query.Issue(nil), d.Issues...)
	for i := range out {
		issue := &out[i]
		if issue.Source != "linear" || issue.EstimateBeforeStart != nil || !completeTeams[issue.TeamID] {
			continue
		}
		if !observedInWindow(issue.StartedAt, d.Snapshot) {
			continue
		}
		started, err := time.Parse(time.RFC3339, issue.StartedAt)
		if err != nil {
			continue
		}
		items := append([]contract.EstimateRecord(nil), changes[issue.ID]...)
		sort.Slice(items, func(a, b int) bool {
			if items[a].EventAt == items[b].EventAt {
				return items[a].ID < items[b].ID
			}
			return items[a].EventAt < items[b].EventAt
		})
		var latestBefore *contract.EstimateRecord
		var earliestAfter *contract.EstimateRecord
		for j := range items {
			at, err := time.Parse(time.RFC3339, items[j].EventAt)
			if err != nil {
				continue
			}
			if at.Before(started) {
				latestBefore = &items[j]
			} else if earliestAfter == nil {
				earliestAfter = &items[j]
			}
		}
		var value *float64
		if latestBefore != nil {
			value = latestBefore.After
		} else if earliestAfter != nil {
			value = earliestAfter.Before
		}
		if value != nil {
			copy := *value
			issue.EstimateBeforeStart = &copy
		}
	}
	return out
}

func estimateMetrics(d query.Dataset) []contract.Metric {
	eligible, known, zero, beforeKnown := int64(0), int64(0), int64(0), int64(0)
	type teamCount struct{ eligible, known, completed int64 }
	teams := make(map[string]teamCount)
	for _, issue := range d.Issues {
		if issue.IsContext {
			continue
		}
		eligible++
		if issue.EstimateBeforeStart != nil {
			beforeKnown++
		}
		if issue.EstimateCurrent != nil {
			known++
			if *issue.EstimateCurrent == 0 {
				zero++
			}
		}
		if issue.TeamID != "" {
			team := teams[issue.TeamID]
			team.eligible++
			if issue.EstimateCurrent != nil {
				team.known++
			}
			if observedInWindow(issue.CompletedAt, d.Snapshot) {
				team.completed++
			}
			teams[issue.TeamID] = team
		}
	}
	if eligible == 0 {
		return nil
	}
	missing := eligible - known
	limit := "Escala de pontos não verificada; valores de teams ou épocas diferentes não são somados."
	out := []contract.Metric{
		integerMetric("metric-estimate-before-start-known", "Issues com estimativa antes do início conhecida", beforeKnown, beforeKnown, &eligible, "Valor antes de started_at reconstruído apenas com histórico Linear completo na janela", "Histórico anterior à janela ou sem cobertura completa permanece NULL; escala de pontos não verificada."),
		integerMetric("metric-estimate-current-known", "Issues com estimativa atual", known, known, &eligible, "Issues elegíveis cujo valor atual de estimativa foi observado, incluindo zero", limit),
		integerMetric("metric-estimate-current-zero", "Issues com estimativa zero", zero, zero, &eligible, "Estimativa atual explicitamente igual a zero; diferente de NULL", limit),
		integerMetric("metric-estimate-current-missing", "Issues sem estimativa atual", missing, missing, &eligible, "Estimativa atual NULL ou ausente nas issues elegíveis", limit),
	}
	changed := int64(0)
	for _, estimate := range d.Estimates {
		if estimate.Timing == "after_start" && estimate.Before != nil && estimate.After != nil && (d.Snapshot.WindowEnd == "" || observedInWindow(estimate.EventAt, d.Snapshot)) {
			changed++
		}
	}
	out = append(out, integerMetric("metric-estimate-after-start-changes", "Alterações de estimativa após início", changed, changed, nil, "Mudanças com valor anterior e posterior observados após início conhecido", "Quem registrou a alteração não necessariamente decidiu; histórico incompleto reduz a contagem."))
	teamIDs := make([]string, 0, len(teams))
	for id := range teams {
		teamIDs = append(teamIDs, id)
	}
	sort.Strings(teamIDs)
	for _, id := range teamIDs {
		team := teams[id]
		label := strings.TrimSpace(id)
		if len(label) > 128 {
			label = label[:128]
		}
		out = append(out,
			integerMetric(metricID("metric-estimate-known-team", id), "Issues com estimativa no team "+label, team.known, team.known, &team.eligible, "Contagem de estimativas atuais observadas no team; não soma pontos", limit),
			integerMetric(metricID("metric-completed-team", id), "Entregas registradas no team "+label, team.completed, team.completed, &team.eligible, "Issues do team com completed_at dentro da janela", "Não é score de produtividade; depende da cobertura e do vínculo de team observado."),
		)
	}
	return out
}

func reworkMetrics(d query.Dataset) []contract.Metric {
	seen := make(map[string]bool)
	byKind := make(map[string]int64)
	unknownCause := int64(0)
	for _, record := range d.Rework {
		if record.ID == "" || seen[record.ID] || record.EvidenceStatus != "direct" || (d.Snapshot.WindowEnd != "" && !observedInWindow(record.EventAt, d.Snapshot)) {
			continue
		}
		seen[record.ID] = true
		byKind[record.Kind]++
		if record.CauseStatus == "undetermined" {
			unknownCause++
		}
	}
	if len(seen) == 0 {
		return nil
	}
	kinds := make([]string, 0, len(byKind))
	for kind := range byKind {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	out := make([]contract.Metric, 0, len(kinds)+1)
	for _, kind := range kinds {
		count := byKind[kind]
		out = append(out, integerMetric(metricID("metric-rework", kind), "Retrabalho observado: "+kind, count, count, nil, "Ocorrências com evidência direta deste tipo na janela", "Contagem de ocorrências; não atribui causa, pessoa culpada ou taxa por entrega."))
	}
	out = append(out, integerMetric("metric-rework-undetermined-cause", "Retrabalho sem causa determinada", unknownCause, unknownCause, nil, "Ocorrências de retrabalho direto cuja causa permanece indeterminada", "Ausência de causa documentada não identifica responsável."))
	return out
}

func cycleTimeMetrics(d query.Dataset) []contract.Metric {
	cohorts := make(map[string][]int64)
	eligible := make(map[string]int64)
	for _, issue := range d.Issues {
		if issue.IsContext || issue.TeamID == "" || !observedInWindow(issue.CompletedAt, d.Snapshot) {
			continue
		}
		eligible[issue.TeamID]++
		if issue.DurationValid && issue.CycleSeconds != nil && *issue.CycleSeconds >= 0 {
			cohorts[issue.TeamID] = append(cohorts[issue.TeamID], *issue.CycleSeconds)
		}
	}
	ids := make([]string, 0, len(cohorts))
	for teamID := range cohorts {
		ids = append(ids, teamID)
	}
	sort.Strings(ids)
	out := make([]contract.Metric, 0, 2*len(ids))
	for _, teamID := range ids {
		values := cohorts[teamID]
		sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
		n := len(values)
		median := float64(values[n/2])
		if n%2 == 0 {
			median = (float64(values[n/2-1]) + float64(values[n/2])) / 2
		}
		p85 := values[int(math.Ceil(0.85*float64(n)))-1]
		label := strings.TrimSpace(teamID)
		if len(label) > 128 {
			label = label[:128]
		}
		limits := []string{"Segundos decorridos entre started_at e completed_at; não mede horas trabalhadas nem produtividade."}
		if n < 10 {
			limits = append(limits, "Amostra insuficiente para comparação entre grupos.")
		}
		count := int64(n)
		eligibleCount := eligible[teamID]
		out = append(out,
			contract.Metric{ID: metricID("metric-cycle-median-team", teamID), Name: "Tempo de ciclo mediano no team " + label, Value: contract.Cell{Kind: "real", Value: strconv.FormatFloat(median, 'f', -1, 64)}, Unit: "seconds", SampleSize: count, EligibleSize: &eligibleCount, Definition: "Mediana convencional de cycle_seconds válido para issues concluídas na janela deste team", Limitations: limits},
			contract.Metric{ID: metricID("metric-cycle-p85-team", teamID), Name: "Tempo de ciclo p85 no team " + label, Value: contract.Cell{Kind: "integer", Value: strconv.FormatInt(p85, 10)}, Unit: "seconds", SampleSize: count, EligibleSize: &eligibleCount, Definition: "Nearest rank ceil(0.85*n) de cycle_seconds válido para issues concluídas na janela deste team", Limitations: limits},
		)
	}
	return out
}

func openAgingMetrics(d query.Dataset) []contract.Metric {
	anchorText := d.Snapshot.AnalysisReferenceAt
	if anchorText == "" {
		anchorText = d.Snapshot.WindowEnd
	}
	anchor, err := time.Parse(time.RFC3339, anchorText)
	if err != nil {
		return nil
	}
	ages := make([]int64, 0)
	for _, issue := range d.Issues {
		if issue.IsContext || !isOpen(issue.State) {
			continue
		}
		created, err := time.Parse(time.RFC3339, issue.CreatedAt)
		if err != nil || created.After(anchor) {
			continue
		}
		ages = append(ages, int64(anchor.Sub(created)/time.Second))
	}
	if len(ages) == 0 {
		return nil
	}
	sort.Slice(ages, func(i, j int) bool { return ages[i] < ages[j] })
	n := len(ages)
	median := float64(ages[n/2])
	if n%2 == 0 {
		median = (float64(ages[n/2-1]) + float64(ages[n/2])) / 2
	}
	p85 := ages[int(math.Ceil(0.85*float64(n)))-1]
	limits := []string{"Idade desde criação, não tempo de trabalho; itens concluídos ficam fora desta coorte."}
	if n < 10 {
		limits = append(limits, "Amostra insuficiente para comparação entre grupos.")
	}
	return []contract.Metric{
		{ID: "metric-open-aging-median", Name: "Idade mediana de itens abertos", Value: contract.Cell{Kind: "real", Value: strconv.FormatFloat(median, 'f', -1, 64)}, Unit: "seconds", SampleSize: int64(n), Definition: "Mediana convencional da criação até analysis_reference_at para issues abertas", Limitations: limits},
		{ID: "metric-open-aging-p85", Name: "Idade p85 de itens abertos", Value: contract.Cell{Kind: "integer", Value: strconv.FormatInt(p85, 10)}, Unit: "seconds", SampleSize: int64(n), Definition: "Nearest rank ceil(0.85*n) da idade dos itens abertos", Limitations: limits},
	}
}
