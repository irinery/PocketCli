package rules

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/query"
	"pocketcli/internal/investigation/storage"
)

const Version = "1.13.0"

func Evaluate(d query.Dataset) query.Dataset {
	d.Findings = []contract.Finding{}
	d.Issues = fillEstimateBeforeStart(d)
	d.Stages = append([]contract.StageRecord{}, d.Stages...)
	d.Rework = append([]contract.ReworkRecord{}, d.Rework...)
	reworkIDs := make(map[string]bool, len(d.Rework))
	for _, record := range d.Rework {
		reworkIDs[record.ID] = true
	}
	linkIDs := make(map[string]bool, len(d.Links))
	for _, link := range d.Links {
		linkIDs[link.ID] = true
	}
	commitEvents := make(map[string]query.Event)
	reopenEvents := make(map[string][]query.Event)
	for _, event := range d.Events {
		if event.Type == "commit_reachable_observed" && event.EvidenceID != "" {
			commitEvents[event.EntityID+"\x00"+event.NewValue] = event
		}
		if event.ID != "" && event.EvidenceID != "" && observedInWindow(event.EventAt, d.Snapshot) && (event.Type == "reopened" || (isDone(event.OldValue) && isOpen(event.NewValue))) {
			reopenEvents[event.EntityID] = append(reopenEvents[event.EntityID], event)
		}
	}
	d.Coverage = revertLinkCoverage(d.Coverage, d.Events, commitEvents, d.Snapshot.AnalysisReferenceAt)
	issueByID := make(map[string]query.Issue)
	prByID := make(map[string]query.PullRequest)
	for _, i := range d.Issues {
		issueByID[i.ID] = i
	}
	for _, p := range d.PullRequests {
		prByID[p.ID] = p
	}
	completeRuntime := false
	for _, c := range d.Coverage {
		if c.Collection == "import" && c.Source == "runtime" && c.Status == "complete" {
			completeRuntime = true
		}
		if c.Status != "complete" && c.Status != "not_selected" {
			d.Findings = append(d.Findings, finding("R-COVER-001", "info", c.Source+":"+c.ResourceID+":"+c.Collection, "Lacuna de cobertura", fmt.Sprintf("A coleção %s de %s está %s: %s", c.Collection, c.ResourceID, c.Status, c.Reason), "unknown", nil, "Nenhuma conclusão negativa sobre a fonte decorre desta lacuna."))
		}
	}
	for _, issue := range d.Issues {
		if issue.IsContext || issue.EvidenceID == "" {
			continue
		}
		var title, body string
		switch issue.Outcome {
		case "cancelled":
			title, body = "Issue cancelada no estado observado", "A fonte classifica o estado atual desta issue como cancelado."
		case "duplicate":
			title, body = "Issue duplicada no estado observado", "A fonte classifica o estado atual desta issue como duplicado."
		case "not_planned":
			title, body = "Issue não planejada no estado observado", "O GitHub registra o fechamento atual desta issue como não planejado."
		default:
			continue
		}
		d.Findings = append(d.Findings, finding("R-OUTCOME-001", "info", issue.ID, title, body, "direct", []string{issue.EvidenceID}, "Classificação da representação atual; não determina quando ou por que essa decisão ocorreu."))
	}
	for _, i := range d.Issues {
		if i.IsContext || i.EvidenceID == "" || !completePRLinkCoverage(i, d) {
			continue
		}
		linked := false
		for _, l := range d.Links {
			if l.SourceID == i.ID && l.Relation == "references_pr" && l.Status == "direct" {
				linked = true
				break
			}
		}
		if !linked {
			d.Findings = append(d.Findings, finding("R-LINK-001", "medium", i.ID, "Sem vínculo explícito localizado", "Não foi localizado vínculo explícito com PR no conjunto coletado; isso não demonstra ausência de implementação.", "direct", []string{i.EvidenceID}, "Conclusão limitada ao escopo coletado."))
		}
	}
	for _, p := range d.PullRequests {
		if p.ReadyAt == "" || p.FirstHumanReviewAt == "" {
			continue
		}
		seconds := p.ReviewWaitSeconds
		if seconds == nil {
			continue
		}
		readyEvidence, reviewEvidence := "", ""
		for _, event := range d.Events {
			if event.EntityID != p.ID {
				continue
			}
			if event.Type == "ready_for_review" && event.EventAt == p.ReadyAt {
				readyEvidence = event.EvidenceID
			}
			if event.Type == "human_reviewed" && event.EventAt == p.FirstHumanReviewAt {
				reviewEvidence = event.EvidenceID
			}
		}
		if readyEvidence == "" || reviewEvidence == "" {
			continue
		}
		stageID := "stage-" + storage.Hash([]byte(p.ID + ":review_wait:" + p.ReadyAt + ":" + p.FirstHumanReviewAt + ":" + readyEvidence + ":" + reviewEvidence))[:20]
		d.Stages = append(d.Stages, contract.StageRecord{ID: stageID, SubjectID: p.ID, Stage: "review_wait", StartedAt: p.ReadyAt, EndedAt: p.FirstHumanReviewAt, ElapsedSeconds: seconds, DurationValid: true, EvidenceID: reviewEvidence})
		d.Findings = append(d.Findings, finding("R-FLOW-001", "info", p.ID, "Tempo de espera observado", fmt.Sprintf("A primeira revisão de conta GitHub User foi observada %d segundos após o PR ficar pronto.", *seconds), "direct", []string{readyEvidence, reviewEvidence}, "Tempo decorrido, não horas trabalhadas; tipo User não descarta automação nessa conta."))
	}
	for _, e := range d.Events {
		if e.Type == "reopened" || (isDone(e.OldValue) && isOpen(e.NewValue)) {
			id := "rework-" + storage.Hash([]byte(e.ID + ":reopen"))[:20]
			if !reworkIDs[id] {
				d.Rework = append(d.Rework, contract.ReworkRecord{ID: id, SubjectID: e.EntityID, Kind: "reopening", EvidenceStatus: "direct", EventAt: e.EventAt, CauseStatus: "undetermined", EvidenceID: e.EvidenceID})
				reworkIDs[id] = true
			}
			d.Findings = append(d.Findings, finding("R-REOPEN-001", "medium", e.EntityID, "Reabertura registrada", "Foi observada uma transição de concluído para aberto.", "direct", []string{e.EvidenceID}, "A transição não comprova defeito ou causa."))
		}
		if e.Type == "commit_revert_declared" && e.EvidenceID != "" && e.OldValue != e.NewValue {
			target, targetSeen := commitEvents[e.EntityID+"\x00"+e.OldValue]
			revertCommit, revertSeen := commitEvents[e.EntityID+"\x00"+e.NewValue]
			if targetSeen && revertSeen {
				linkID := "link-" + storage.Hash([]byte(e.ID + ":declares_revert_of:" + target.ID))[:24]
				if !linkIDs[linkID] {
					d.Links = append(d.Links, contract.LinkRecord{ID: linkID, SourceID: revertCommit.ID, Relation: "declares_revert_of", TargetID: target.ID, Status: "direct", EvidenceID: e.EvidenceID, ValidFrom: e.EventAt})
					linkIDs[linkID] = true
				}
				id := "rework-" + storage.Hash([]byte(e.ID + ":declared_reversal:" + target.ID))[:20]
				if !reworkIDs[id] {
					d.Rework = append(d.Rework, contract.ReworkRecord{ID: id, SubjectID: target.ID, Kind: "declared_reversal", EvidenceStatus: "direct", EventAt: e.EventAt, CauseStatus: "undetermined", EvidenceID: e.EvidenceID})
					reworkIDs[id] = true
				}
			}
		}
	}
	if completeRuntime {
		for _, observation := range latestRuntimeObservations(d.Events, d.Snapshot) {
			if observation.ambiguous || observation.event.Type != "disabled" || observation.event.EvidenceID == "" {
				continue
			}
			d.Findings = append(d.Findings, finding("R-DISABLE-001", "medium", observation.event.EntityID, "Recurso observado desativado", "A observação operacional mais recente no período mostra o recurso desativado.", "direct", []string{observation.event.EvidenceID}, "A observação não reconstrói o passado nem outro ambiente."))
		}
	}
	for _, e := range d.Estimates {
		if e.Timing == "after_start" && e.Before != nil && e.After != nil {
			d.Findings = append(d.Findings, finding("R-EST-001", "info", e.IssueID, "Estimativa alterada após início", fmt.Sprintf("A estimativa observada mudou de %g para %g após o início registrado.", *e.Before, *e.After), "direct", []string{e.EvidenceID}, "Quem registrou a mudança não necessariamente a decidiu."))
		}
	}
	for _, l := range d.Links {
		if l.Relation == "declares_fix_of" && l.Status == "direct" && l.EvidenceID != "" {
			issue, issueFound := issueByID[l.TargetID]
			pr, prFound := prByID[l.SourceID]
			if issueFound && prFound && !issue.IsContext && observedInWindow(pr.MergedAt, d.Snapshot) {
				mergedAt, err := time.Parse(time.RFC3339, pr.MergedAt)
				if err == nil {
					var latest query.Event
					var latestAt time.Time
					for _, event := range reopenEvents[issue.ID] {
						at, err := time.Parse(time.RFC3339, event.EventAt)
						if err == nil && at.Before(mergedAt) && (latest.ID == "" || at.After(latestAt) || (at.Equal(latestAt) && event.ID < latest.ID)) {
							latest, latestAt = event, at
						}
					}
					if latest.ID != "" {
						id := "rework-" + storage.Hash([]byte(l.ID + ":declared_correction:" + latest.ID + ":" + pr.MergedAt))[:20]
						if !reworkIDs[id] {
							d.Rework = append(d.Rework, contract.ReworkRecord{ID: id, SubjectID: issue.ID, Kind: "declared_correction", EvidenceStatus: "direct", EventAt: pr.MergedAt, CauseStatus: "undetermined", EvidenceID: l.EvidenceID})
							reworkIDs[id] = true
						}
						d.Findings = append(d.Findings, finding("R-CORRECTION-001", "info", issue.ID, "Correção declarada após reabertura", "Um PR mesclado declarou Fixes para uma issue com reabertura anterior observada.", "direct", []string{l.EvidenceID, latest.EvidenceID}, "A declaração e a ordem dos registros não comprovam efeito do patch, defeito ou causa."))
					}
				}
			}
		}
		if l.Relation == "blocked_by" && l.Status == "direct" && l.EvidenceID != "" {
			source, sourceFound := issueByID[l.SourceID]
			target, targetFound := issueByID[l.TargetID]
			if sourceFound && targetFound && source.EvidenceID != "" && target.EvidenceID != "" && issueActiveObserved(source) && issueActiveObserved(target) {
				d.Findings = append(d.Findings, finding("R-BLOCK-001", "medium", l.SourceID, "Dependência pendente observada", "Há relação explícita de bloqueio entre dois itens com estado ativo observado.", "direct", []string{l.EvidenceID, source.EvidenceID, target.EvidenceID}, "A relação e os estados observados não demonstram quanto tempo houve bloqueio nem sua causa adicional."))
			}
		}
	}
	for _, delivery := range d.Deliveries {
		if delivery.RevisionSHA == "" || (delivery.ProvenanceStatus != "declared" && delivery.ProvenanceStatus != "observed") || delivery.EvidenceID == "" || !observedInWindow(delivery.ObservedAt, d.Snapshot) {
			continue
		}
		observedAt, err := time.Parse(time.RFC3339, delivery.ObservedAt)
		if err != nil {
			continue
		}
		for _, l := range d.Links {
			if l.Relation != "modifies_component" || l.TargetID != delivery.ComponentID || l.Status != "direct" || l.EvidenceID == "" {
				continue
			}
			p, ok := prByID[l.SourceID]
			if !ok || p.MergeSHA == "" || strings.EqualFold(p.MergeSHA, delivery.RevisionSHA) || p.EvidenceID == "" || !observedInWindow(p.MergedAt, d.Snapshot) {
				continue
			}
			mergedAt, err := time.Parse(time.RFC3339, p.MergedAt)
			if err != nil || observedAt.Before(mergedAt) {
				continue
			}
			correctionIDs := correctionEvidenceIDs(p, issueByID, d.Links)
			if len(correctionIDs) == 0 {
				continue
			}
			included, notAncestorEvidence := false, ""
			for _, relation := range d.Links {
				if relation.SourceID != delivery.ID || relation.TargetID != p.ID || relation.Status != "direct" || relation.EvidenceID == "" {
					continue
				}
				if relation.Relation == "contains_merge_commit" {
					included = true
				}
				if relation.Relation == "merge_commit_not_ancestor" {
					notAncestorEvidence = relation.EvidenceID
				}
			}
			if included || notAncestorEvidence == "" {
				continue
			}
			evidence := uniqueEvidenceIDs(append([]string{p.EvidenceID, delivery.EvidenceID, l.EvidenceID, notAncestorEvidence}, correctionIDs...))
			d.Findings = append(d.Findings, finding("R-DELIVERY-001", "high", delivery.ComponentID, "Correção não comprovada no artefato observado", "O commit de merge da correção declarada não é ancestral da revisão observada do artefato.", "direct", evidence, "A ausência do commit no grafo não exclui patch equivalente por cherry-pick nem erro na proveniência declarada do artefato."))
		}
	}
	d.Metrics = metrics(d)
	sort.Slice(d.Findings, func(i, j int) bool { return d.Findings[i].ID < d.Findings[j].ID })
	return d
}

type runtimeObservation struct {
	event     query.Event
	at        time.Time
	ambiguous bool
}

func latestRuntimeObservations(events []query.Event, snapshot contract.Snapshot) map[string]runtimeObservation {
	latest := make(map[string]runtimeObservation)
	for _, event := range events {
		if (event.Type != "enabled" && event.Type != "disabled") || event.EntityID == "" || !observedInWindow(event.EventAt, snapshot) {
			continue
		}
		at, err := time.Parse(time.RFC3339, event.EventAt)
		if err != nil {
			continue
		}
		key := event.EntityID + "\x00" + event.NewValue // Environment is part of the observed state.
		previous, found := latest[key]
		if !found || at.After(previous.at) {
			latest[key] = runtimeObservation{event: event, at: at}
			continue
		}
		if at.Equal(previous.at) {
			previous.ambiguous = previous.ambiguous || event.Type != previous.event.Type
			if event.ID < previous.event.ID {
				previous.event = event
			}
			latest[key] = previous
		}
	}
	return latest
}

func correctionEvidenceIDs(pr query.PullRequest, issues map[string]query.Issue, links []contract.LinkRecord) []string {
	ids := make([]string, 0)
	for _, link := range links {
		if link.Relation != "declares_fix_of" || link.SourceID != pr.ID || link.Status != "direct" || link.EvidenceID == "" {
			continue
		}
		if issue, ok := issues[link.TargetID]; ok && !issue.IsContext {
			ids = append(ids, link.EvidenceID)
		}
	}
	return uniqueEvidenceIDs(ids)
}

func uniqueEvidenceIDs(ids []string) []string {
	seen := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id != "" && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

func revertLinkCoverage(existing []contract.Coverage, events []query.Event, commits map[string]query.Event, checkedAt string) []contract.Coverage {
	out := make([]contract.Coverage, 0, len(existing))
	for _, item := range existing {
		if item.Source != "analysis" || item.Collection != "revert_links" {
			out = append(out, item)
		}
	}
	unresolved := make(map[string]int64)
	resolved := make(map[string]int64)
	for _, event := range events {
		if event.Type != "commit_revert_declared" || event.EvidenceID == "" {
			continue
		}
		_, targetSeen := commits[event.EntityID+"\x00"+event.OldValue]
		_, revertSeen := commits[event.EntityID+"\x00"+event.NewValue]
		if !targetSeen || !revertSeen || event.OldValue == event.NewValue {
			unresolved[event.EntityID]++
		} else {
			resolved[event.EntityID]++
		}
	}
	ids := make([]string, 0, len(unresolved))
	for id := range unresolved {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		out = append(out, contract.Coverage{Source: "analysis", ResourceID: id, Collection: "revert_links", Status: "partial", Observed: resolved[id], Reason: fmt.Sprintf("%d declarações de revert sem os dois commits observados no período", unresolved[id]), CheckedAt: checkedAt})
	}
	return out
}

func completePRLinkCoverage(issue query.Issue, d query.Dataset) bool {
	if issue.Source == "github" {
		repository, _, ok := strings.Cut(issue.Identifier, "#")
		if !ok || repository == "" {
			return false
		}
		resourceID := ""
		for _, resource := range d.Snapshot.Resources {
			if resource.Source == "github" && strings.EqualFold(resource.Name, repository) {
				resourceID = resource.ID
				break
			}
		}
		if resourceID == "" {
			return false
		}
		complete := map[string]bool{"issues": false, "pull_requests": false, "formal_links": false}
		for _, item := range d.Coverage {
			if item.Source == "github" && item.ResourceID == resourceID {
				if _, required := complete[item.Collection]; required {
					if item.Status != "complete" {
						return false
					}
					complete[item.Collection] = true
				}
			}
		}
		return complete["issues"] && complete["pull_requests"] && complete["formal_links"]
	}
	for _, item := range d.Coverage {
		if item.Collection != "links" || item.Status != "complete" {
			continue
		}
		if issue.Source == "" && item.Source == "analysis" {
			return true // Contract fixtures without a source use aggregate coverage.
		}
		if issue.Source != "" && issue.TeamID != "" && item.Source == issue.Source && item.ResourceID == issue.TeamID {
			return true
		}
	}
	return false
}

func finding(rule, severity, subject, title, body, status string, evidence []string, limitations string) contract.Finding {
	ids := append([]string{}, evidence...)
	sort.Strings(ids)
	seed := rule + ":" + Version + ":" + subject + ":" + strings.Join(ids, ",")
	return contract.Finding{ID: "f-" + storage.Hash([]byte(seed))[:24], RuleID: rule, RuleVersion: Version, Severity: severity, SubjectID: subject, Title: title, DeterministicText: body, EvidenceStatus: status, EvidenceIDs: ids, Limitations: []string{limitations}}
}
func isDone(s string) bool {
	switch strings.ToLower(s) {
	case "done", "completed", "closed":
		return true
	}
	return false
}
func isOpen(s string) bool {
	switch strings.ToLower(s) {
	case "open", "todo", "in_progress", "started", "reopened", "unstarted", "backlog", "triage":
		return true
	}
	return false
}

func issueActiveObserved(issue query.Issue) bool {
	return (issue.Outcome == "" || issue.Outcome == "active") && isOpen(issue.State)
}

func metrics(d query.Dataset) []contract.Metric {
	completed := int64(0)
	for _, i := range d.Issues {
		if completedIssueInWindow(i, d.Snapshot) {
			completed++
		}
	}
	out := []contract.Metric{{ID: "metric-completed", Name: "Entregas registradas", Value: contract.Cell{Kind: "integer", Value: strconv.FormatInt(completed, 10)}, Unit: "items", SampleSize: completed, Definition: "Issues não contextuais com completed_at na janela e sem classificação contrária à conclusão", Limitations: []string{"Não é score de produtividade; depende da cobertura. Snapshots antigos sem classificação requerem cautela."}}}
	waits := make([]int64, 0)
	for _, p := range d.PullRequests {
		if p.ReviewWaitSeconds != nil && *p.ReviewWaitSeconds >= 0 && p.ReadyAt != "" && observedInWindow(p.FirstHumanReviewAt, d.Snapshot) {
			waits = append(waits, *p.ReviewWaitSeconds)
		}
	}
	sort.Slice(waits, func(i, j int) bool { return waits[i] < waits[j] })
	if len(waits) > 0 {
		n := len(waits)
		median := float64(waits[n/2])
		if n%2 == 0 {
			median = (float64(waits[n/2-1]) + float64(waits[n/2])) / 2
		}
		p85 := waits[int(math.Ceil(0.85*float64(n)))-1]
		limitations := []string{"Segundos decorridos, não horas trabalhadas."}
		if n < 10 {
			limitations = append(limitations, "Amostra insuficiente para comparação entre pessoas ou grupos.")
		}
		out = append(out, contract.Metric{ID: "metric-review-median", Name: "Espera mediana por revisão", Value: contract.Cell{Kind: "real", Value: strconv.FormatFloat(median, 'f', -1, 64)}, Unit: "seconds", SampleSize: int64(n), Definition: "Mediana convencional de ready_at até primeira revisão humana", Limitations: limitations}, contract.Metric{ID: "metric-review-p85", Name: "Espera p85 por revisão", Value: contract.Cell{Kind: "integer", Value: strconv.FormatInt(p85, 10)}, Unit: "seconds", SampleSize: int64(n), Definition: "Nearest rank ceil(0.85*n)", Limitations: limitations})
	}
	out = append(out, actorMetrics(d)...)
	out = append(out, issueOutcomeMetrics(d)...)
	out = append(out, estimateMetrics(d)...)
	out = append(out, reworkMetrics(d)...)
	out = append(out, reworkWindowMetrics(d)...)
	out = append(out, cycleTimeMetrics(d)...)
	out = append(out, openAgingMetrics(d)...)
	return out
}

func completedIssueInWindow(issue query.Issue, snapshot contract.Snapshot) bool {
	return !issue.IsContext && (issue.Outcome == "" || issue.Outcome == "completed") && observedInWindow(issue.CompletedAt, snapshot)
}
