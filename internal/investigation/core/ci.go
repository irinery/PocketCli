package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/query"
	"pocketcli/internal/investigation/storage"
)

func (c *Core) collectActions(ctx context.Context, plan contract.Plan, r contract.Resource, d *query.Dataset, run *contract.Run) error {
	const collection = "actions"
	journalID := storage.Hash([]byte(plan.ID + ":" + r.ID + ":" + collection))
	journal := githubJournal{PlanID: plan.ID, ResourceID: r.ID, Collection: collection, Items: []json.RawMessage{}}
	if err := c.store.Get("journals", journalID, &journal); err == nil {
		if journal.PlanID != plan.ID || journal.ResourceID != r.ID || journal.Collection != collection {
			return fault("ERR_AUDIT_FAILED", "journal CI incompatível")
		}
	} else if !os.IsNotExist(err) {
		return fault("ERR_AUDIT_FAILED", "journal CI corrompido")
	}
	var fetchErr error
	if !journal.Complete && journal.Pages < 20 {
		endpoint := "/repos/" + r.Name + "/actions/runs?per_page=100"
		if journal.Cursor != "" {
			endpoint = journal.Cursor
		}
		committed := 0
		var requests int
		requests, fetchErr = c.collector.GitHubWalkField(ctx, endpoint, 20-journal.Pages, "workflow_runs", func(page []json.RawMessage, next string, responseBytes int64) error {
			committed++
			journal.Items = append(journal.Items, page...)
			journal.Pages++
			journal.Cursor = next
			journal.Complete = next == ""
			if err := c.store.Put("journals", journalID, journal); err != nil {
				return fault("ERR_AUDIT_FAILED", "falha ao persistir página CI")
			}
			run.Requests++
			run.Bytes += responseBytes
			if !c.saveRun(run) {
				return fault("ERR_AUDIT_FAILED", "falha ao auditar página CI")
			}
			if run.Bytes > plan.Input.Limits.MaxTransferBytes {
				return fault("ERR_RESOURCE_EXHAUSTED", "orçamento de transferência atingido")
			}
			return nil
		})
		if requests > committed {
			run.Requests += int64(requests - committed)
			if !c.saveRun(run) {
				return fault("ERR_AUDIT_FAILED", "falha ao auditar request CI")
			}
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var coded *contract.ErrorResponse
	if errors.As(fetchErr, &coded) && (coded.Code == "ERR_AUDIT_FAILED" || coded.Code == "ERR_RESOURCE_EXHAUSTED") {
		return fetchErr
	}
	status, reason := "complete", "execuções de workflow observadas; sucesso de CI não comprova implantação"
	if !journal.Complete || fetchErr != nil {
		status, reason = "partial", "paginação ou acesso CI incompleto"
		if errors.As(fetchErr, &coded) && coded.Code == "ERR_FORBIDDEN" {
			status = "unauthorized"
		}
	}
	for _, raw := range journal.Items {
		var item struct {
			ID         int64  `json:"id"`
			RunAttempt int64  `json:"run_attempt"`
			HeadSHA    string `json:"head_sha"`
			Name       string `json:"name"`
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
			CreatedAt  string `json:"created_at"`
			UpdatedAt  string `json:"updated_at"`
		}
		if json.Unmarshal(raw, &item) != nil || item.ID == 0 || item.UpdatedAt == "" {
			status, reason = "partial", "item CI inválido"
			continue
		}
		if !timestampInWindow(plan.WindowStart, plan.WindowEnd, item.CreatedAt) && !timestampInWindow(plan.WindowStart, plan.WindowEnd, item.UpdatedAt) {
			continue
		}
		e, err := c.evidence("github", fmt.Sprintf("%s/actions/runs/%d", r.Name, item.ID), item.UpdatedAt, raw)
		if err != nil {
			return err
		}
		upsertEvidence(d, e)
		value := item.Status
		if item.Conclusion != "" {
			value = item.Conclusion
		}
		kind := "ci_workflow_" + strings.ToLower(value) + "_observed"
		if value == "" {
			kind = "ci_workflow_observed"
		}
		id := "evt-" + storage.Hash([]byte(fmt.Sprintf("%s:%d:%d:%s", r.ID, item.ID, item.RunAttempt, e.ID)))[:24]
		upsertEvent(d, query.Event{ID: id, EntityID: "repo-" + r.ID, Type: kind, EventAt: item.UpdatedAt, ObservedAt: c.setup.now().UTC().Format("2006-01-02T15:04:05Z"), NewValue: item.HeadSHA, EvidenceID: e.ID})
		run.Items++
	}
	if !c.saveRun(run) {
		return fault("ERR_AUDIT_FAILED", "falha ao auditar CI")
	}
	d.Coverage = append(d.Coverage, c.coverage(r, collection, status, int64(len(journal.Items)), reason))
	return nil
}
