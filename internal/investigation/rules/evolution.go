package rules

import (
	"sort"
	"strconv"
	"time"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/query"
)

// Series of the time-series metrics. The report's "Evolução" section reads
// them by Dimensions["series"].
const (
	SeriesCreated        = "created"
	SeriesCompleted      = "completed"
	SeriesCanceled       = "canceled"
	SeriesCycleScope     = "cycle_scope"
	SeriesCycleCompleted = "cycle_completed"
	SeriesProjectIssues  = "project_issues"
	SeriesProjectDone    = "project_completed"
)

func monthOf(at string, snapshot contract.Snapshot) string {
	if !observedInWindow(at, snapshot) {
		return ""
	}
	stamp, err := time.Parse(time.RFC3339, at)
	if err != nil {
		return ""
	}
	return stamp.UTC().Format("2006-01")
}

// evolutionMetrics counts, per team and UTC month, the issues created,
// completed and canceled inside the window; per Linear cycle, its final
// scope and completed issues; and per Linear project, its issues.
func evolutionMetrics(d query.Dataset) []contract.Metric {
	type cell struct{ team, month string }
	counts := map[cell]map[string]int64{}
	add := func(team, month, series string) {
		if team == "" || month == "" {
			return
		}
		key := cell{team, month}
		if counts[key] == nil {
			counts[key] = map[string]int64{}
		}
		counts[key][series]++
	}
	cycleScope, cycleDone := map[string]int64{}, map[string]int64{}
	issueByID := make(map[string]query.Issue, len(d.Issues))
	for _, issue := range d.Issues {
		issueByID[issue.ID] = issue
		if issue.IsContext {
			continue
		}
		add(issue.TeamID, monthOf(issue.CreatedAt, d.Snapshot), SeriesCreated)
		if completedIssueInWindow(issue, d.Snapshot) {
			add(issue.TeamID, monthOf(issue.CompletedAt, d.Snapshot), SeriesCompleted)
		}
		if issue.Outcome == "cancelled" {
			add(issue.TeamID, monthOf(issue.CanceledAt, d.Snapshot), SeriesCanceled)
		}
		if issue.CycleID != "" {
			cycleScope[issue.CycleID]++
			if issue.Outcome == "completed" {
				cycleDone[issue.CycleID]++
			}
		}
	}
	keys := make([]cell, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].month != keys[j].month {
			return keys[i].month < keys[j].month
		}
		return keys[i].team < keys[j].team
	})
	limit := "Contagem por mês UTC no estado observado neste snapshot; não é score de produtividade nem taxa de entrega."
	names := map[string]string{SeriesCreated: "Issues criadas", SeriesCompleted: "Issues concluídas", SeriesCanceled: "Issues canceladas"}
	definitions := map[string]string{SeriesCreated: "Issues não contextuais com created_at no mês e na janela", SeriesCompleted: "Issues não contextuais com completed_at no mês e na janela, sem classificação contrária à conclusão", SeriesCanceled: "Issues canceladas com canceled_at no mês e na janela; duplicadas ficam fora"}
	var out []contract.Metric
	for _, key := range keys {
		for _, series := range []string{SeriesCreated, SeriesCompleted, SeriesCanceled} {
			value := counts[key][series]
			m := integerMetric(metricID("metric-evolution-"+series, key.team+"\x00"+key.month), names[series]+" em "+key.month+" no team "+key.team, value, value, nil, definitions[series], limit)
			m.Dimensions = map[string]string{"series": series, "month": key.month, "team": key.team}
			out = append(out, m)
		}
	}
	cycles := append([]query.Cycle(nil), d.Cycles...)
	sort.Slice(cycles, func(i, j int) bool {
		if cycles[i].TeamID != cycles[j].TeamID {
			return cycles[i].TeamID < cycles[j].TeamID
		}
		return cycles[i].Number < cycles[j].Number
	})
	for _, cycle := range cycles {
		number := strconv.Itoa(cycle.Number)
		dims := func(series string) map[string]string {
			return map[string]string{"series": series, "team": cycle.TeamID, "cycle": number, "starts_at": cycle.StartsAt, "ends_at": cycle.EndsAt, "completed_at": cycle.CompletedAt}
		}
		scope, done := cycleScope[cycle.ID], cycleDone[cycle.ID]
		m := integerMetric(metricID("metric-cycle-scope", cycle.ID), "Escopo final do ciclo "+number+" do team "+cycle.TeamID, scope, scope, nil, "Issues observadas neste snapshot que pertencem ao ciclo", "Escopo em issues, não em pontos; issues removidas do ciclo antes do snapshot não aparecem.")
		m.Dimensions = dims(SeriesCycleScope)
		completed := integerMetric(metricID("metric-cycle-completed", cycle.ID), "Issues concluídas do ciclo "+number+" do team "+cycle.TeamID, done, done, &scope, "Issues do escopo final do ciclo com conclusão explícita", "Conclusão pode ter ocorrido depois do fim do ciclo; não é score de produtividade.")
		completed.Dimensions = dims(SeriesCycleCompleted)
		out = append(out, m, completed)
	}
	projectNames := map[string]string{}
	for _, component := range d.Components {
		if component.Kind == "linear_project" {
			projectNames[component.ID] = component.Name
		}
	}
	projectIssues, projectDone := map[string]int64{}, map[string]int64{}
	seen := map[string]bool{}
	for _, link := range d.Links {
		if link.Relation != "belongs_to_project" || seen[link.SourceID+"\x00"+link.TargetID] {
			continue
		}
		issue, ok := issueByID[link.SourceID]
		if !ok || issue.IsContext {
			continue
		}
		seen[link.SourceID+"\x00"+link.TargetID] = true
		projectIssues[link.TargetID]++
		if issue.Outcome == "completed" {
			projectDone[link.TargetID]++
		}
	}
	projects := make([]string, 0, len(projectIssues))
	for id := range projectIssues {
		projects = append(projects, id)
	}
	sort.Strings(projects)
	for _, id := range projects {
		name := projectNames[id]
		if name == "" {
			name = id
		}
		total, done := projectIssues[id], projectDone[id]
		m := integerMetric(metricID("metric-project-issues", id), "Issues no projeto "+name, total, total, nil, "Issues não contextuais observadas com vínculo direto ao projeto", "Projeto atual da issue; mudanças anteriores de projeto não são contadas.")
		m.Dimensions = map[string]string{"series": SeriesProjectIssues, "project": id, "name": name}
		completed := integerMetric(metricID("metric-project-completed", id), "Issues concluídas no projeto "+name, done, done, &total, "Issues do projeto com conclusão explícita", "Não é score de produtividade; depende da cobertura.")
		completed.Dimensions = map[string]string{"series": SeriesProjectDone, "project": id, "name": name}
		out = append(out, m, completed)
	}
	return out
}
