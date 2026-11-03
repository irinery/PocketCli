package report

import (
	"sort"
	"strconv"
	"strings"

	"pocketcli/internal/investigation/contract"
)

// Metric series the rules emit for the "Evolução" section; the names mirror
// rules.Series*, which this package cannot import.
const (
	seriesCreated        = "created"
	seriesCompleted      = "completed"
	seriesCanceled       = "canceled"
	seriesCycleScope     = "cycle_scope"
	seriesCycleCompleted = "cycle_completed"
	seriesProjectIssues  = "project_issues"
	seriesProjectDone    = "project_completed"
)

// snapshotStatusLabel explains a provisional snapshot instead of printing
// the bare status.
func snapshotStatusLabel(s contract.Snapshot) string {
	switch s.Status {
	case contract.SnapshotProvisional:
		return "provisório — fila de reconciliação pendente (" + strings.Join(contract.PendingQueues(s.Coverage), ", ") + "); rode refresh até esvaziá-la ou publique com --publish-partial MOTIVO"
	case contract.SnapshotPublished:
		if s.PublishOverride != nil {
			return "publicado com fila pendente, autorizado pelo operador"
		}
		return "publicado"
	}
	return s.Status
}

// activityWindowLabel is the span between the first and the last observed
// event, which can be much shorter than the resources' lifetimes.
func activityWindowLabel(s contract.Snapshot) string {
	if s.ActivityStart == "" {
		return "nenhum evento observado"
	}
	return s.ActivityStart + " — " + s.ActivityEnd + " (1º → último evento observado)"
}

func (v *reportView) evolutionMetrics() []contract.Metric {
	var out []contract.Metric
	for _, r := range v.doc.Records {
		if m := r.Metric; m != nil && m.Dimensions["series"] != "" {
			out = append(out, *m)
		}
	}
	return out
}

func (v *reportView) hasEvolution() bool { return len(v.evolutionMetrics()) > 0 }

// renderEvolution lays out the time series before the records: issues per
// month and team, the Linear cycles and the Linear projects.
func (v *reportView) renderEvolution(l *pdfLayout) {
	type cell struct{ month, team string }
	months := map[cell]map[string]string{}
	type cycle struct {
		team   string
		number int
		values map[string]string
		dims   map[string]string
	}
	cycles := map[string]*cycle{}
	projects := map[string]map[string]string{}
	projectNames := map[string]string{}
	for _, m := range v.evolutionMetrics() {
		d := m.Dimensions
		switch d["series"] {
		case seriesCreated, seriesCompleted, seriesCanceled:
			key := cell{d["month"], d["team"]}
			if months[key] == nil {
				months[key] = map[string]string{}
			}
			months[key][d["series"]] = m.Value.Value
		case seriesCycleScope, seriesCycleCompleted:
			key := d["team"] + "\x00" + d["cycle"]
			if cycles[key] == nil {
				n, _ := strconv.Atoi(d["cycle"])
				cycles[key] = &cycle{team: d["team"], number: n, values: map[string]string{}, dims: d}
			}
			cycles[key].values[d["series"]] = m.Value.Value
		case seriesProjectIssues, seriesProjectDone:
			if projects[d["project"]] == nil {
				projects[d["project"]] = map[string]string{}
			}
			projects[d["project"]][d["series"]] = m.Value.Value
			projectNames[d["project"]] = d["name"]
		}
	}
	or0 := func(s string) string {
		if s == "" {
			return "0"
		}
		return s
	}
	day := func(s string) string {
		if len(s) >= 10 {
			return s[:10]
		}
		if s == "" {
			return "—"
		}
		return s
	}
	l.paragraph(9, colorMuted, plain("Contagens por mês UTC e por team no estado observado neste snapshot. Não são score de produtividade; dependem da cobertura da seção 2."))
	if len(months) > 0 {
		keys := make([]cell, 0, len(months))
		for k := range months {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			if keys[i].month != keys[j].month {
				return keys[i].month < keys[j].month
			}
			return v.resource("linear", keys[i].team) < v.resource("linear", keys[j].team)
		})
		rows := make([][]string, 0, len(keys))
		for _, k := range keys {
			c := months[k]
			rows = append(rows, []string{k.month, v.resource("linear", k.team), or0(c[seriesCreated]), or0(c[seriesCompleted]), or0(c[seriesCanceled])})
		}
		l.heading("Issues por mês e team", 11, 6, false)
		l.table([]float64{0.16, 0.36, 0.16, 0.16, 0.16}, []string{"Mês", "Team", "Criadas", "Concluídas", "Canceladas"}, rows)
	}
	if len(cycles) > 0 {
		list := make([]*cycle, 0, len(cycles))
		for _, c := range cycles {
			list = append(list, c)
		}
		sort.Slice(list, func(i, j int) bool {
			a, b := v.resource("linear", list[i].team), v.resource("linear", list[j].team)
			if a != b {
				return a < b
			}
			return list[i].number < list[j].number
		})
		rows := make([][]string, 0, len(list))
		for _, c := range list {
			rows = append(rows, []string{v.resource("linear", c.team), strconv.Itoa(c.number), day(c.dims["starts_at"]), day(c.dims["ends_at"]), or0(c.values[seriesCycleScope]), or0(c.values[seriesCycleCompleted])})
		}
		l.heading("Ciclos do Linear", 11, 6, false)
		l.paragraph(9, colorMuted, plain("Escopo final: issues observadas que pertencem ao ciclo neste snapshot."))
		l.table([]float64{0.22, 0.1, 0.18, 0.18, 0.16, 0.16}, []string{"Team", "Ciclo", "Início", "Fim", "Escopo final", "Concluídas"}, rows)
	}
	if len(projects) > 0 {
		ids := make([]string, 0, len(projects))
		for id := range projects {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool {
			if projectNames[ids[i]] != projectNames[ids[j]] {
				return projectNames[ids[i]] < projectNames[ids[j]]
			}
			return ids[i] < ids[j]
		})
		rows := make([][]string, 0, len(ids))
		for _, id := range ids {
			rows = append(rows, []string{projectNames[id], or0(projects[id][seriesProjectIssues]), or0(projects[id][seriesProjectDone])})
		}
		l.heading("Projetos do Linear", 11, 6, false)
		l.table([]float64{0.6, 0.2, 0.2}, []string{"Projeto", "Issues", "Concluídas"}, rows)
	}
}
