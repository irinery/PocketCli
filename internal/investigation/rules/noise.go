package rules

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/query"
	"pocketcli/internal/investigation/storage"
)

// BatchCreationThreshold is how many issues of one project created within
// 24 hours make a batch: items generated together tend to be finer grained
// than the rest and inflate delivery counts.
const BatchCreationThreshold = 20

// findingEvidenceSample caps the evidence listed on an aggregated finding;
// the body states the full count.
const findingEvidenceSample = 5

// rejectionStage matches workflow stages that send work back: "Reprovado",
// "Rejeitado", "Devolvido", "Rejected", "Failed QA".
var rejectionStage = regexp.MustCompile(`(?i)reprova|rejeit|recusad|devolvid|reject|failed`)

// inProgress reports a state where someone is working on the item, as
// opposed to a planned one sitting in backlog, triage or todo. GitHub only
// says "open", which stays in progress so its findings keep their weight.
func inProgress(state string) bool {
	switch strings.ToLower(state) {
	case "started", "in_progress", "open", "reopened":
		return true
	}
	return false
}

// blockFindings reports, once per blocked item, the explicit blocked_by
// relations whose both ends are active. Only an item already in progress
// that waits on another is a medium finding; a dependency between planned
// items is the plan itself and stays informational.
func blockFindings(d query.Dataset, issueByID map[string]query.Issue) []contract.Finding {
	type group struct {
		evidence []string
		blockers map[string]bool
	}
	groups := make(map[string]*group)
	for _, l := range d.Links {
		if l.Relation != "blocked_by" || l.Status != "direct" || l.EvidenceID == "" {
			continue
		}
		source, sourceFound := issueByID[l.SourceID]
		target, targetFound := issueByID[l.TargetID]
		if !sourceFound || !targetFound || source.EvidenceID == "" || target.EvidenceID == "" || !issueActiveObserved(source) || !issueActiveObserved(target) {
			continue
		}
		g := groups[l.SourceID]
		if g == nil {
			g = &group{evidence: []string{source.EvidenceID}, blockers: map[string]bool{}}
			groups[l.SourceID] = g
		}
		if g.blockers[l.TargetID] {
			continue
		}
		g.blockers[l.TargetID] = true
		g.evidence = append(g.evidence, l.EvidenceID, target.EvidenceID)
	}
	ids := make([]string, 0, len(groups))
	for id := range groups {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]contract.Finding, 0, len(ids))
	for _, id := range ids {
		g := groups[id]
		n := len(g.blockers)
		if inProgress(issueByID[id].State) {
			out = append(out, finding("R-BLOCK-001", "medium", id, "Item em andamento bloqueado", fmt.Sprintf("O item está em andamento e tem %d relação(ões) explícita(s) de bloqueio com itens ainda ativos.", n), "direct", uniqueEvidenceIDs(g.evidence), "A relação e os estados observados não demonstram quanto tempo houve bloqueio nem sua causa adicional."))
			continue
		}
		out = append(out, finding("R-BLOCK-002", "info", id, "Dependência planejada observada", fmt.Sprintf("O item ainda não começou e depende de %d item(ns) ativo(s); a relação descreve a ordem planejada.", n), "direct", uniqueEvidenceIDs(g.evidence), "Dependência entre itens planejados não indica impedimento; vira bloqueio se o item começar antes dos itens de que depende."))
	}
	return out
}

// batchFindings reports Linear projects that received at least
// BatchCreationThreshold issues within 24 hours.
func batchFindings(d query.Dataset, issueByID map[string]query.Issue) []contract.Finding {
	byProject := make(map[string][]query.Issue)
	seen := make(map[string]bool)
	for _, l := range d.Links {
		if l.Relation != "belongs_to_project" || l.Status != "direct" || seen[l.SourceID+"\x00"+l.TargetID] {
			continue
		}
		issue, ok := issueByID[l.SourceID]
		if !ok || issue.IsContext || issue.EvidenceID == "" || !observedInWindow(issue.CreatedAt, d.Snapshot) {
			continue
		}
		seen[l.SourceID+"\x00"+l.TargetID] = true
		byProject[l.TargetID] = append(byProject[l.TargetID], issue)
	}
	names := make(map[string]string)
	for _, c := range d.Components {
		names[c.ID] = c.Name
	}
	projects := make([]string, 0, len(byProject))
	for id := range byProject {
		projects = append(projects, id)
	}
	sort.Strings(projects)
	var out []contract.Finding
	for _, project := range projects {
		type dated struct {
			issue query.Issue
			at    time.Time
		}
		var kept []dated
		for _, issue := range byProject[project] {
			if at, err := time.Parse(time.RFC3339, issue.CreatedAt); err == nil {
				kept = append(kept, dated{issue, at})
			}
		}
		sort.Slice(kept, func(i, j int) bool {
			if !kept[i].at.Equal(kept[j].at) {
				return kept[i].at.Before(kept[j].at)
			}
			return kept[i].issue.ID < kept[j].issue.ID
		})
		times := make([]time.Time, len(kept))
		for i, k := range kept {
			times[i] = k.at
		}
		best, bestStart := 0, 0
		start := 0
		for end := range times {
			for times[end].Sub(times[start]) > 24*time.Hour {
				start++
			}
			if n := end - start + 1; n > best {
				best, bestStart = n, start
			}
		}
		if best < BatchCreationThreshold {
			continue
		}
		evidence := make([]string, 0, findingEvidenceSample)
		for _, k := range kept[bestStart:] {
			if len(evidence) == findingEvidenceSample {
				break
			}
			evidence = append(evidence, k.issue.EvidenceID)
		}
		name := names[project]
		if name == "" {
			name = project
		}
		from := times[bestStart].UTC().Format(time.RFC3339)
		to := times[bestStart+best-1].UTC().Format(time.RFC3339)
		out = append(out, finding("R-BATCH-001", "info", project, "Criação em lote observada", fmt.Sprintf("%d issues do projeto %s foram criadas em até 24 horas (%s a %s), de %d issues do projeto na janela.", best, name, from, to, len(kept)), "direct", uniqueEvidenceIDs(evidence), "Criação em lote não indica baixa qualidade; itens criados juntos costumam ter granularidade diferente e pesam igual nas contagens de entrega."))
	}
	return out
}

// teamMoveFindings reports, once per Linear team, issues created before the
// team itself: they were moved in from another team, so the team's history
// starts earlier than the team and its identifiers do not tell the order.
func teamMoveFindings(d query.Dataset) []contract.Finding {
	created := make(map[string]time.Time)
	for _, r := range d.Snapshot.Resources {
		if r.Source != "linear" || r.CreatedAt == "" {
			continue
		}
		if at, err := time.Parse(time.RFC3339, r.CreatedAt); err == nil {
			created[r.ID] = at
		}
	}
	type moved struct {
		count    int
		evidence []string
	}
	byTeam := make(map[string]*moved)
	issues := append([]query.Issue{}, d.Issues...)
	sort.Slice(issues, func(i, j int) bool { return issues[i].ID < issues[j].ID })
	for _, issue := range issues {
		teamAt, ok := created[issue.TeamID]
		if !ok || issue.Source != "linear" || issue.EvidenceID == "" {
			continue
		}
		at, err := time.Parse(time.RFC3339, issue.CreatedAt)
		if err != nil || !at.Before(teamAt) {
			continue
		}
		m := byTeam[issue.TeamID]
		if m == nil {
			m = &moved{}
			byTeam[issue.TeamID] = m
		}
		m.count++
		if len(m.evidence) < findingEvidenceSample {
			m.evidence = append(m.evidence, issue.EvidenceID)
		}
	}
	teams := make([]string, 0, len(byTeam))
	for id := range byTeam {
		teams = append(teams, id)
	}
	sort.Strings(teams)
	out := make([]contract.Finding, 0, len(teams))
	for _, team := range teams {
		m := byTeam[team]
		out = append(out, finding("R-MOVE-001", "info", "linear:"+team, "Issues anteriores ao team", fmt.Sprintf("%d issues do team %s foram criadas antes do próprio team (%s); provavelmente vieram de outro team.", m.count, team, created[team].UTC().Format(time.RFC3339)), "direct", uniqueEvidenceIDs(m.evidence), "A data de criação não identifica o team de origem nem quando a movimentação ocorreu."))
	}
	return out
}

// rejectionRework turns each entry into a rejection stage ("Reprovado",
// "Rejected"…) into a stage_rejection rework record.
func rejectionRework(d query.Dataset, issueByID map[string]query.Issue, known map[string]bool) []contract.ReworkRecord {
	var out []contract.ReworkRecord
	for _, s := range d.Stages {
		name := strings.TrimPrefix(s.Stage, "linear:")
		if s.EvidenceID == "" || s.StartedAt == "" || !rejectionStage.MatchString(name) {
			continue
		}
		if issue, ok := issueByID[s.SubjectID]; !ok || issue.IsContext {
			continue
		}
		id := "rework-" + storage.Hash([]byte(s.ID + ":stage_rejection"))[:20]
		if known[id] {
			continue
		}
		known[id] = true
		out = append(out, contract.ReworkRecord{ID: id, SubjectID: s.SubjectID, Kind: "stage_rejection", EvidenceStatus: "direct", EventAt: s.StartedAt, CauseStatus: "undetermined", EvidenceID: s.EvidenceID})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// weightMetrics adds, per team, the estimate points of the issues delivered
// in the window, the share of closed issues marked duplicate and, for the
// whole history, how many identifier numbers are missing from the sequence.
// Points are not summed across teams: their scales are not comparable.
func weightMetrics(d query.Dataset) []contract.Metric {
	type tally struct {
		completed, withPoints int64
		points                float64
		closed, duplicates    int64
		numbers               map[int]bool
		max                   int
		prefix                string
	}
	teams := make(map[string]*tally)
	get := func(id string) *tally {
		t := teams[id]
		if t == nil {
			t = &tally{numbers: map[int]bool{}}
			teams[id] = t
		}
		return t
	}
	for _, issue := range d.Issues {
		if issue.Source != "linear" || issue.TeamID == "" {
			continue
		}
		t := get(issue.TeamID)
		if prefix, number, ok := strings.Cut(issue.Identifier, "-"); ok {
			if n, err := strconv.Atoi(number); err == nil && n > 0 {
				t.numbers[n] = true
				t.prefix = prefix
				if n > t.max {
					t.max = n
				}
			}
		}
		if issue.IsContext {
			continue
		}
		if completedIssueInWindow(issue, d.Snapshot) {
			t.completed++
			if issue.EstimateCurrent != nil {
				t.withPoints++
				t.points += *issue.EstimateCurrent
			}
		}
		switch issue.Outcome {
		case "completed", "cancelled", "duplicate", "not_planned":
			t.closed++
		}
		if issue.Outcome == "duplicate" {
			t.duplicates++
		}
	}
	ids := make([]string, 0, len(teams))
	for id := range teams {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var out []contract.Metric
	for _, id := range ids {
		t := teams[id]
		if t.completed > 0 {
			completed := t.completed
			out = append(out, contract.Metric{ID: metricID("metric-completed-points-team", id), Name: "Pontos entregues no team " + id, Value: contract.Cell{Kind: "real", Value: strconv.FormatFloat(t.points, 'f', -1, 64)}, Unit: "points", SampleSize: t.withPoints, EligibleSize: &completed, Definition: "Soma da estimativa atual das issues do team concluídas na janela; a amostra é quantas delas têm estimativa", Limitations: []string{"Escala de pontos própria do team; não some com outros teams. Estimativa atual pode diferir da usada no planejamento."}})
		}
		if t.closed > 0 {
			closed := t.closed
			out = append(out, integerMetric(metricID("metric-duplicates-team", id), "Issues duplicadas no team "+id, t.duplicates, t.duplicates, &closed, "Issues fechadas do team classificadas como duplicadas, sobre o total de fechadas", "Duplicadas indicam triagem repetida ou abertura em paralelo; não atribuem causa."))
		}
		if d.Snapshot.WindowStart == "" && t.max > 0 {
			missing := int64(t.max - len(t.numbers))
			highest := int64(t.max)
			out = append(out, integerMetric(metricID("metric-identifier-gaps-team", id), fmt.Sprintf("Números de identificador ausentes no team %s (%s-1 a %s-%d)", id, t.prefix, t.prefix, t.max), missing, missing, &highest, "Números da sequência de identificadores do team sem issue observada no histórico completo", "Pode ser exclusão, movimentação para outro team ou item não coletado; não distingue entre eles."))
		}
	}
	return out
}
