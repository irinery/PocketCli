package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/query"
	"pocketcli/internal/investigation/storage"
)

// collectChecks observes checks at the exact heads of PRs in the investigation.
// GitHub's check-runs endpoint is scoped to a ref, not a repository-wide history.
func (c *Core) collectChecks(ctx context.Context, plan contract.Plan, r contract.Resource, d *query.Dataset, run *contract.Run) error {
	prs := make([]query.PullRequest, 0)
	missingHead := false
	for _, pr := range d.PullRequests {
		if pr.Repository != r.Name || pr.Number <= 0 {
			continue
		}
		if !validGitOID(pr.HeadSHA) {
			missingHead = true
			continue
		}
		prs = append(prs, pr)
	}
	sort.Slice(prs, func(i, j int) bool { return prs[i].ID < prs[j].ID })
	selected := prs
	status, reason := "complete", "checks consultados nos heads dos PRs selecionados; não representam todo o histórico de CI"
	if missingHead {
		status, reason = "partial", "um ou mais PRs sem SHA de head válido"
	}
	cursorID := ""
	if len(prs) > 100 {
		status, reason = "partial", fmt.Sprintf("fila de checks: %d PRs pendentes", len(prs)-100)
		var auth planAuth
		if err := c.store.Get("planauth", plan.ID, &auth); err != nil {
			return fault("ERR_AUDIT_FAILED", "identidade GitHub indisponível")
		}
		cursorID = storage.Hash([]byte(plan.InvestigationID + ":" + r.ID + ":checks:" + auth.GitHub))
		var cursor githubReconcileCursor
		if err := c.store.Get("reconcile", cursorID, &cursor); err != nil && !os.IsNotExist(err) {
			return fault("ERR_AUDIT_FAILED", "cursor de checks corrompido")
		}
		start := sort.Search(len(prs), func(i int) bool { return prs[i].ID > cursor.LastEntityID })
		if start >= len(prs) {
			start = 0
		}
		selected = append(append([]query.PullRequest{}, prs[start:]...), prs[:start]...)
		selected = selected[:100]
	}
	var observed int64
	lastComplete := ""
	for _, pr := range selected {
		if err := ctx.Err(); err != nil {
			return err
		}
		endpoint := fmt.Sprintf("/repos/%s/commits/%s/check-runs?per_page=100", r.Name, pr.HeadSHA)
		items, complete, fetchErr := c.githubChildPagesField(ctx, plan, r, "checks", pr.Number, endpoint, "check_runs", run)
		if fetchErr != nil {
			var coded *contract.ErrorResponse
			if errors.As(fetchErr, &coded) && (coded.Code == "ERR_AUDIT_FAILED" || coded.Code == "ERR_RATE_LIMITED" || (coded.Code == "ERR_RESOURCE_EXHAUSTED" && run.Bytes > plan.Input.Limits.MaxTransferBytes)) {
				return fetchErr
			}
			status, reason = "partial", "checks de um ou mais PRs indisponíveis"
			if errors.As(fetchErr, &coded) && coded.Code == "ERR_FORBIDDEN" {
				status, reason = "unauthorized", "permissão Checks: read indisponível"
			}
		}
		if !complete {
			if status != "unauthorized" {
				status, reason = "partial", "paginação de checks incompleta"
			}
		} else {
			lastComplete = pr.ID
		}
		observed += int64(len(items))
		for _, raw := range items {
			var item struct {
				ID          int64  `json:"id"`
				HeadSHA     string `json:"head_sha"`
				Name        string `json:"name"`
				Status      string `json:"status"`
				Conclusion  string `json:"conclusion"`
				StartedAt   string `json:"started_at"`
				CompletedAt string `json:"completed_at"`
			}
			if json.Unmarshal(raw, &item) != nil || item.ID <= 0 || !strings.EqualFold(item.HeadSHA, pr.HeadSHA) {
				status, reason = "partial", "check inválido ou associado a outro commit"
				continue
			}
			at := item.CompletedAt
			if at == "" {
				at = item.StartedAt
			}
			if at == "" {
				status, reason = "partial", "check sem timestamp"
				continue
			}
			if !timestampInWindow(plan.WindowStart, plan.WindowEnd, at) {
				continue
			}
			e, err := c.evidence("github", fmt.Sprintf("%s/check-runs/%d", r.Name, item.ID), at, raw)
			if err != nil {
				return err
			}
			upsertEvidence(d, e)
			value := item.Status
			if item.Conclusion != "" {
				value = item.Conclusion
			}
			kind := "ci_check_observed"
			if value != "" {
				kind = "ci_check_" + strings.ToLower(value) + "_observed"
			}
			id := "evt-" + storage.Hash([]byte(fmt.Sprintf("%s:%d:%s:%s", pr.ID, item.ID, pr.HeadSHA, e.ID)))[:24]
			upsertEvent(d, query.Event{ID: id, EntityID: pr.ID, Type: kind, EventAt: at, ObservedAt: e.ObservedAt, NewValue: item.Name, EvidenceID: e.ID})
			run.Items++
		}
	}
	if cursorID != "" && lastComplete != "" {
		if err := c.store.Put("reconcile", cursorID, githubReconcileCursor{LastEntityID: lastComplete}); err != nil {
			return fault("ERR_AUDIT_FAILED", "falha ao persistir cursor de checks")
		}
	}
	if !c.saveRun(run) {
		return fault("ERR_AUDIT_FAILED", "falha ao auditar checks")
	}
	d.Coverage = append(d.Coverage, c.coverage(r, "checks", status, observed, reason))
	return nil
}
