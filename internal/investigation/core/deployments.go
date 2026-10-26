package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/query"
	"pocketcli/internal/investigation/storage"
)

func (c *Core) collectDeployments(ctx context.Context, plan contract.Plan, r contract.Resource, d *query.Dataset, run *contract.Run) error {
	path := fmt.Sprintf("/repos/%s/deployments?per_page=100", r.Name)
	items, complete, err := c.githubChildPages(ctx, plan, r, "deployments", 0, path, run)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	status, reason := "complete", "registros de deployment e respectivos status consultados; status da API não comprova estado atual do servidor"
	if start, parseErr := time.Parse(time.RFC3339, plan.WindowStart); parseErr == nil && start.Before(c.setup.now().Add(-90*24*time.Hour)) {
		status, reason = "partial", "histórico de status anterior à retenção de 90 dias da API pode estar ausente"
	}
	if !complete || err != nil {
		status, reason = "partial", "lista de deployments incompleta"
		var coded *contract.ErrorResponse
		if errors.As(err, &coded) {
			if coded.Code == "ERR_AUDIT_FAILED" || (coded.Code == "ERR_RESOURCE_EXHAUSTED" && run.Bytes > plan.Input.Limits.MaxTransferBytes) {
				return err
			}
			if coded.Code == "ERR_FORBIDDEN" {
				status = "unauthorized"
			}
		}
	}
	processed := 0
	for _, raw := range items {
		var deployment struct {
			ID          int    `json:"id"`
			SHA         string `json:"sha"`
			Environment string `json:"environment"`
			CreatedAt   string `json:"created_at"`
			UpdatedAt   string `json:"updated_at"`
		}
		if json.Unmarshal(raw, &deployment) != nil || deployment.ID <= 0 || deployment.CreatedAt == "" {
			status, reason = "partial", "deployment inválido"
			continue
		}
		if !timestampInWindow(plan.WindowStart, plan.WindowEnd, deployment.CreatedAt) && !timestampInWindow(plan.WindowStart, plan.WindowEnd, deployment.UpdatedAt) {
			continue
		}
		if processed >= 100 {
			status, reason = "partial", "fila de status de deployment excedeu 100 alvos nesta rodada"
			break
		}
		processed++
		location := fmt.Sprintf("%s:deployment:%d", r.Name, deployment.ID)
		e, eErr := c.evidence("github", location, deployment.UpdatedAt, raw)
		if eErr != nil {
			return eErr
		}
		upsertEvidence(d, e)
		upsertEvent(d, query.Event{ID: "evt-" + storage.Hash([]byte(location + ":" + e.ID))[:24], EntityID: "repo-" + r.ID, Type: "deployment_requested", EventAt: deployment.CreatedAt, ObservedAt: e.ObservedAt, NewValue: deployment.SHA, EvidenceID: e.ID})
		statusPath := fmt.Sprintf("/repos/%s/deployments/%d/statuses?per_page=100", r.Name, deployment.ID)
		statuses, allStatuses, statusErr := c.githubChildPages(ctx, plan, r, "deployment_statuses", deployment.ID, statusPath, run)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !allStatuses || statusErr != nil {
			status, reason = "partial", "status de deployment incompleto ou indisponível"
			var coded *contract.ErrorResponse
			if errors.As(statusErr, &coded) && (coded.Code == "ERR_AUDIT_FAILED" || (coded.Code == "ERR_RESOURCE_EXHAUSTED" && run.Bytes > plan.Input.Limits.MaxTransferBytes)) {
				return statusErr
			}
		}
		for _, statusRaw := range statuses {
			var state struct {
				ID        int64  `json:"id"`
				State     string `json:"state"`
				CreatedAt string `json:"created_at"`
			}
			if json.Unmarshal(statusRaw, &state) != nil || state.ID == 0 || state.State == "" || state.CreatedAt == "" {
				status, reason = "partial", "status de deployment inválido"
				continue
			}
			if !timestampInWindow(plan.WindowStart, plan.WindowEnd, state.CreatedAt) {
				continue
			}
			stateLocator := fmt.Sprintf("%s:deployment:%d:status:%d", r.Name, deployment.ID, state.ID)
			stateEvidence, eErr := c.evidence("github", stateLocator, state.CreatedAt, statusRaw)
			if eErr != nil {
				return eErr
			}
			upsertEvidence(d, stateEvidence)
			upsertEvent(d, query.Event{ID: "evt-" + storage.Hash([]byte(stateLocator))[:24], EntityID: "repo-" + r.ID, Type: "deployment_" + state.State + "_reported", EventAt: state.CreatedAt, ObservedAt: stateEvidence.ObservedAt, OldValue: deployment.SHA, NewValue: deployment.Environment, EvidenceID: stateEvidence.ID})
			if state.State == "success" && validGitOID(deployment.SHA) {
				d.Deliveries = append(d.Deliveries, contract.DeliveryRecord{ID: "art-" + storage.Hash([]byte(stateLocator))[:24], ComponentID: "repo-" + r.ID, RevisionSHA: deployment.SHA, Environment: deployment.Environment, ObservedAt: state.CreatedAt, ProvenanceStatus: "declared", EvidenceID: stateEvidence.ID})
			}
			run.Items++
		}
	}
	if !c.saveRun(run) {
		return fault("ERR_AUDIT_FAILED", "falha ao auditar deployments")
	}
	d.Coverage = append(d.Coverage, c.coverage(r, "deployments", status, int64(len(items)), reason))
	return nil
}
