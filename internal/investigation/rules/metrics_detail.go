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
		if !completedIssueInWindow(issue, d.Snapshot) {
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
	botReviews := make(map[string]map[string]bool)
	unknownReviews := make(map[string]map[string]bool)
	merged := make(map[string]int64)
	accountKinds := make(map[string]string)
	observeKind := func(actor, kind string) {
		if actor == "" || (kind != "user" && kind != "bot") {
			return
		}
		if prior := accountKinds[actor]; prior == "" {
			accountKinds[actor] = kind
		} else if prior != kind {
			accountKinds[actor] = "conflict"
		}
	}
	for _, event := range d.Events {
		observeKind(event.ActorID, event.ActorKind)
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
		if event.Type == "bot_reviewed" {
			if botReviews[event.ActorID] == nil {
				botReviews[event.ActorID] = make(map[string]bool)
			}
			botReviews[event.ActorID][event.EntityID] = true
		}
		if event.Type == "reviewed_unknown" {
			if unknownReviews[event.ActorID] == nil {
				unknownReviews[event.ActorID] = make(map[string]bool)
			}
			unknownReviews[event.ActorID][event.EntityID] = true
		}
	}
	for _, pr := range d.PullRequests {
		observeKind(pr.AuthorID, pr.AuthorKind)
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
	for actor := range botReviews {
		actors[actor] = true
	}
	for actor := range unknownReviews {
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
	var botAccounts, userAccounts, unknownAccounts int64
	for _, actor := range ids {
		label := strings.TrimSpace(actor)
		if len(label) > 128 {
			label = label[:128]
		}
		limit := "Contagens observadas, sem atribuição causal ou score de produtividade; tipo de conta não verificado."
		switch accountKinds[actor] {
		case "bot":
			botAccounts++
			limit = "GitHub informa tipo Bot; contagem não atribui causa, culpa ou produtividade."
		case "user":
			userAccounts++
			limit = "GitHub informa tipo User; automação operando essa conta não pode ser descartada."
		default:
			unknownAccounts++
		}
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
			out = append(out, integerMetric(metricID("metric-reviewed-prs-actor", actor), "PRs revisados por "+label, count, count, nil, "PRs distintos com evento de revisão de conta GitHub User deste ator na janela", limit))
		}
	}
	if len(ids) > 0 {
		eligible := int64(len(ids))
		out = append(out,
			integerMetric("metric-actor-kind-bot", "Atores com conta GitHub Bot observada", botAccounts, botAccounts, &eligible, "Atores distintos nas métricas de atividade cujo tipo Bot foi informado pela API", "Somente tipo explícito; outras automações podem usar contas User ou identidades sem tipo conhecido."),
			integerMetric("metric-actor-kind-user-account", "Atores com conta GitHub User observada", userAccounts, userAccounts, &eligible, "Atores distintos nas métricas de atividade cujo tipo User foi informado pela API", "User descreve o tipo da conta, não prova ação humana nem produtividade."),
			integerMetric("metric-actor-kind-unknown", "Atores sem tipo de conta verificado", unknownAccounts, unknownAccounts, &eligible, "Atores distintos nas métricas de atividade sem tipo User/Bot consistente", "Inclui atores Linear e metadados ausentes ou conflitantes; não presumir pessoa humana."),
		)
	}
	botPRs, unknownPRs := make(map[string]bool), make(map[string]bool)
	for _, prs := range botReviews {
		for pr := range prs {
			botPRs[pr] = true
		}
	}
	for _, prs := range unknownReviews {
		for pr := range prs {
			unknownPRs[pr] = true
		}
	}
	if len(botPRs) > 0 || len(unknownPRs) > 0 {
		botCount, unknownCount := int64(len(botPRs)), int64(len(unknownPRs))
		out = append(out,
			integerMetric("metric-bot-reviewed-prs", "PRs com revisão de conta Bot", botCount, botCount, nil, "PRs distintos com revisão submetida por conta GitHub tipo Bot na janela", "Revisão de Bot não compõe a primeira revisão de conta User."),
			integerMetric("metric-unknown-kind-reviewed-prs", "PRs com revisão de tipo desconhecido", unknownCount, unknownCount, nil, "PRs distintos com revisão submetida por conta sem tipo User/Bot reconhecido na janela", "Tipo ausente não é tratado como revisão humana."),
		)
	}
	if assigneeUnknown > 0 {
		out = append(out, integerMetric("metric-completed-assignee-unknown", "Issues concluídas sem responsável conhecido", assigneeUnknown, assigneeUnknown, &completedEligible, "Issues não contextuais com completed_at na janela e assignee_id ausente no snapshot", "Ausência de responsável não identifica quem executou o trabalho."))
	}
	return out
}

func issueOutcomeMetrics(d query.Dataset) []contract.Metric {
	counts := map[string]int64{"completed": 0, "cancelled": 0, "duplicate": 0, "not_planned": 0, "unknown": 0}
	var terminal int64
	for _, issue := range d.Issues {
		if issue.IsContext {
			continue
		}
		if _, ok := counts[issue.Outcome]; ok {
			counts[issue.Outcome]++
			terminal++
		}
	}
	if terminal == 0 {
		return nil
	}
	labels := []struct{ outcome, name string }{
		{"completed", "Issues com conclusão explícita"},
		{"cancelled", "Issues canceladas"},
		{"duplicate", "Issues marcadas como duplicadas"},
		{"not_planned", "Issues fechadas como não planejadas"},
		{"unknown", "Issues fechadas sem motivo conhecido"},
	}
	out := make([]contract.Metric, 0, len(labels))
	for _, item := range labels {
		count := counts[item.outcome]
		id := "metric-issue-outcome-" + strings.ReplaceAll(item.outcome, "_", "-")
		out = append(out, integerMetric(id, item.name, count, count, &terminal,
			"Classificação do estado atual de issues não contextuais na representação observada neste snapshot",
			"Não é uma contagem de transições no período nem uma taxa de entrega; status anteriores podem ter sido diferentes."))
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
			if completedIssueInWindow(issue, d.Snapshot) {
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
		name := "Retrabalho observado: " + kind
		definition := "Ocorrências com evidência direta deste tipo na janela"
		limitation := "Contagem de ocorrências; não atribui causa, pessoa culpada ou taxa por entrega."
		if kind == "declared_reversal" {
			name = "Reversões declaradas em commits"
			definition = "Commits com trailer exato de git revert apontando para commit também observado no mesmo repositório"
			limitation = "O trailer declara a reversão; a equivalência inversa do patch não foi demonstrada e não identifica culpa ou causa."
		} else if kind == "declared_correction" {
			name = "Correções declaradas após reabertura"
			definition = "PRs mesclados com Fixes #N explícito para issue com reabertura observada antes do merge"
			limitation = "A declaração não demonstra efeito do patch, defeito, produtividade ou causa; o histórico da issue pode estar incompleto."
		}
		out = append(out, integerMetric(metricID("metric-rework", kind), name, count, count, nil, definition, limitation))
	}
	out = append(out, integerMetric("metric-rework-undetermined-cause", "Retrabalho sem causa determinada", unknownCause, unknownCause, nil, "Ocorrências de retrabalho direto cuja causa permanece indeterminada", "Ausência de causa documentada não identifica responsável."))
	return out
}

func reworkWindowMetrics(d query.Dataset) []contract.Metric {
	anchorText := d.Snapshot.AnalysisReferenceAt
	if anchorText == "" {
		anchorText = d.Snapshot.WindowEnd
	}
	anchor, err := time.Parse(time.RFC3339, anchorText)
	if err != nil {
		return nil
	}
	issues := make(map[string]bool, len(d.Issues))
	for _, issue := range d.Issues {
		if !issue.IsContext {
			issues[issue.ID] = true
		}
	}
	completions := make(map[string][]time.Time)
	seenEvents := make(map[string]bool)
	var observedCompletions, elapsed, incomplete int64
	for _, event := range d.Events {
		if event.ID == "" || seenEvents[event.ID] || !issues[event.EntityID] || event.Type != "state_changed" || !isDone(event.NewValue) || isDone(event.OldValue) || !observedInWindow(event.EventAt, d.Snapshot) {
			continue
		}
		seenEvents[event.ID] = true
		at, err := time.Parse(time.RFC3339, event.EventAt)
		if err != nil || at.After(anchor) {
			continue
		}
		completions[event.EntityID] = append(completions[event.EntityID], at)
		observedCompletions++
		if !anchor.Before(at.Add(30 * 24 * time.Hour)) {
			elapsed++
		} else {
			incomplete++
		}
	}
	for id := range completions {
		sort.Slice(completions[id], func(i, j int) bool { return completions[id][i].Before(completions[id][j]) })
	}
	type windowCounts struct{ direct, within, afterThirtyDays, noPrior int64 }
	reopen, correction := windowCounts{}, windowCounts{}
	seenRework := make(map[string]bool)
	for _, record := range d.Rework {
		if record.ID == "" || seenRework[record.ID] || (record.Kind != "reopening" && record.Kind != "declared_correction") || record.EvidenceStatus != "direct" || !issues[record.SubjectID] || !observedInWindow(record.EventAt, d.Snapshot) {
			continue
		}
		at, err := time.Parse(time.RFC3339, record.EventAt)
		if err != nil || at.After(anchor) {
			continue
		}
		seenRework[record.ID] = true
		counts := &reopen
		if record.Kind == "declared_correction" {
			counts = &correction
		}
		counts.direct++
		var prior time.Time
		for _, completed := range completions[record.SubjectID] {
			if !completed.Before(at) {
				break
			}
			prior = completed
		}
		if prior.IsZero() {
			counts.noPrior++
		} else if at.Sub(prior) <= 30*24*time.Hour {
			counts.within++
		} else {
			counts.afterThirtyDays++
		}
	}
	if observedCompletions == 0 && reopen.direct == 0 && correction.direct == 0 {
		return nil
	}
	out := []contract.Metric{
		integerMetric("metric-rework-30d-observed", "Reaberturas observadas até 30 dias após conclusão", reopen.within, reopen.within, &reopen.direct, "Reaberturas diretas com transição de conclusão anterior observada no mesmo item, até 30 dias UTC depois", "Contagem de ocorrências, não taxa por entrega; histórico parcial pode ocultar transições e zero não comprova ausência."),
		integerMetric("metric-rework-after-30d-observed", "Reaberturas observadas após 30 dias da conclusão", reopen.afterThirtyDays, reopen.afterThirtyDays, &reopen.direct, "Reaberturas diretas com transição de conclusão anterior observada no mesmo item, mais de 30 dias UTC depois", "Contagem de ocorrências fora da janela padrão; histórico parcial pode ocultar transições intermediárias."),
		integerMetric("metric-rework-30d-elapsed-windows", "Janelas de 30 dias já decorridas", elapsed, elapsed, &observedCompletions, "Transições de conclusão observadas na janela cujo prazo de 30 dias já decorreu no analysis_reference_at", "Prazo decorrido não comprova cobertura completa da fonte nem ausência de reabertura."),
		integerMetric("metric-rework-30d-incomplete-windows", "Janelas de 30 dias ainda incompletas", incomplete, incomplete, &observedCompletions, "Transições de conclusão observadas com menos de 30 dias até analysis_reference_at", "Essas entregas ainda não tiveram a janela temporal completa de observação."),
		integerMetric("metric-rework-no-prior-completion", "Reaberturas sem conclusão anterior observada", reopen.noPrior, reopen.noPrior, &reopen.direct, "Reaberturas diretas cujo item não tem transição anterior de conclusão observada na janela", "Histórico anterior ou cobertura parcial podem explicar a ausência da transição; não atribui causa."),
	}
	if correction.direct > 0 {
		out = append(out,
			integerMetric("metric-declared-correction-30d-observed", "Correções declaradas até 30 dias após conclusão", correction.within, correction.within, &correction.direct, "PRs com Fixes explícito, após reabertura e até 30 dias UTC da última conclusão observada da issue", "Declaração não comprova efeito do patch; histórico incompleto pode ocultar outra conclusão."),
			integerMetric("metric-declared-correction-after-30d-observed", "Correções declaradas após 30 dias da conclusão", correction.afterThirtyDays, correction.afterThirtyDays, &correction.direct, "PRs com Fixes explícito, após reabertura e mais de 30 dias UTC da última conclusão observada", "Contagem fora da janela padrão, sem atribuição de causa ou culpa."),
			integerMetric("metric-declared-correction-no-prior-completion", "Correções declaradas sem conclusão anterior observada", correction.noPrior, correction.noPrior, &correction.direct, "PRs com Fixes explícito após reabertura, mas sem transição anterior de conclusão observada na janela", "A ausência da âncora pode resultar do período ou cobertura parcial; não afirmar prazo de 30 dias."),
		)
	}
	return out
}

func cycleTimeMetrics(d query.Dataset) []contract.Metric {
	cohorts := make(map[string][]int64)
	eligible := make(map[string]int64)
	for _, issue := range d.Issues {
		if issue.TeamID == "" || !completedIssueInWindow(issue, d.Snapshot) {
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
