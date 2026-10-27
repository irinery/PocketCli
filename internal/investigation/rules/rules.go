package rules

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/query"
	"pocketcli/internal/investigation/storage"
)

const Version = "1.0.0"

func Evaluate(d query.Dataset) query.Dataset {
	d.Findings = []contract.Finding{}
	d.Issues = fillEstimateBeforeStart(d)
	d.Stages = append([]contract.StageRecord{}, d.Stages...)
	d.Rework = append([]contract.ReworkRecord{}, d.Rework...)
	issueByID := make(map[string]query.Issue)
	prByID := make(map[string]query.PullRequest)
	for _, i := range d.Issues {
		issueByID[i.ID] = i
	}
	for _, p := range d.PullRequests {
		prByID[p.ID] = p
	}
	completeLinks := false
	completeRuntime := false
	for _, c := range d.Coverage {
		if c.Collection == "links" && c.Status == "complete" {
			completeLinks = true
		}
		if c.Collection == "import" && c.Source == "runtime" && c.Status == "complete" {
			completeRuntime = true
		}
		if c.Status != "complete" && c.Status != "not_selected" {
			d.Findings = append(d.Findings, finding("R-COVER-001", "info", c.Source+":"+c.ResourceID+":"+c.Collection, "Lacuna de cobertura", fmt.Sprintf("A coleção %s de %s está %s: %s", c.Collection, c.ResourceID, c.Status, c.Reason), "unknown", nil, "Nenhuma conclusão negativa sobre a fonte decorre desta lacuna."))
		}
	}
	if completeLinks {
		for _, i := range d.Issues {
			if i.IsContext || i.EvidenceID == "" {
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
		d.Findings = append(d.Findings, finding("R-FLOW-001", "info", p.ID, "Tempo de espera observado", fmt.Sprintf("A primeira revisão humana foi observada %d segundos após o PR ficar pronto.", *seconds), "direct", []string{readyEvidence, reviewEvidence}, "Tempo decorrido, não horas trabalhadas."))
	}
	for _, e := range d.Events {
		if e.Type == "reopened" || (isDone(e.OldValue) && isOpen(e.NewValue)) {
			d.Rework = append(d.Rework, contract.ReworkRecord{ID: "rework-" + storage.Hash([]byte(e.ID + ":reopen"))[:20], SubjectID: e.EntityID, Kind: "reopening", EvidenceStatus: "direct", EventAt: e.EventAt, CauseStatus: "undetermined", EvidenceID: e.EvidenceID})
			d.Findings = append(d.Findings, finding("R-REOPEN-001", "medium", e.EntityID, "Reabertura registrada", "Foi observada uma transição de concluído para aberto.", "direct", []string{e.EvidenceID}, "A transição não comprova defeito ou causa."))
		}
		if e.Type == "disabled" && completeRuntime {
			d.Findings = append(d.Findings, finding("R-DISABLE-001", "medium", e.EntityID, "Recurso observado desativado", "Uma desativação foi observada no snapshot operacional aplicável.", "direct", []string{e.EvidenceID}, "A observação não reconstrói o passado."))
		}
	}
	for _, e := range d.Estimates {
		if e.Timing == "after_start" && e.Before != nil && e.After != nil {
			d.Findings = append(d.Findings, finding("R-EST-001", "info", e.IssueID, "Estimativa alterada após início", fmt.Sprintf("A estimativa observada mudou de %g para %g após o início registrado.", *e.Before, *e.After), "direct", []string{e.EvidenceID}, "Quem registrou a mudança não necessariamente a decidiu."))
		}
	}
	for _, l := range d.Links {
		if l.Relation == "blocked_by" && l.Status == "direct" {
			target, ok := issueByID[l.TargetID]
			if ok && !isDone(target.State) {
				d.Findings = append(d.Findings, finding("R-BLOCK-001", "medium", l.SourceID, "Dependência pendente observada", "Há relação explícita de bloqueio com item ainda não concluído.", "direct", []string{l.EvidenceID, target.EvidenceID}, "Correlação temporal não prova causa adicional."))
			}
		}
	}
	for _, delivery := range d.Deliveries {
		if delivery.RevisionSHA == "" || delivery.ProvenanceStatus == "unknown" {
			continue
		}
		for _, l := range d.Links {
			if l.Relation != "modifies_component" || l.TargetID != delivery.ComponentID || l.Status != "direct" {
				continue
			}
			p, ok := prByID[l.SourceID]
			if !ok || p.MergeSHA == "" || p.MergeSHA == delivery.RevisionSHA {
				continue
			}
			d.Findings = append(d.Findings, finding("R-DELIVERY-001", "high", delivery.ComponentID, "Correção não comprovada no artefato observado", "Há correção registrada; sua presença no artefato observado não foi comprovada.", "direct", []string{p.EvidenceID, delivery.EvidenceID}, "Revisões diferentes não demonstram sozinhas ausência do código por cherry-pick."))
		}
	}
	d.Metrics = metrics(d)
	sort.Slice(d.Findings, func(i, j int) bool { return d.Findings[i].ID < d.Findings[j].ID })
	return d
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
	case "open", "todo", "in_progress", "started", "reopened":
		return true
	}
	return false
}

func metrics(d query.Dataset) []contract.Metric {
	completed := int64(0)
	for _, i := range d.Issues {
		if !i.IsContext && observedInWindow(i.CompletedAt, d.Snapshot) {
			completed++
		}
	}
	out := []contract.Metric{{ID: "metric-completed", Name: "Entregas registradas", Value: contract.Cell{Kind: "integer", Value: strconv.FormatInt(completed, 10)}, Unit: "items", SampleSize: completed, Definition: "Issues não contextuais com completed_at na janela selecionada", Limitations: []string{"Não é score de produtividade; depende da cobertura."}}}
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
	out = append(out, estimateMetrics(d)...)
	out = append(out, reworkMetrics(d)...)
	out = append(out, cycleTimeMetrics(d)...)
	out = append(out, openAgingMetrics(d)...)
	return out
}
