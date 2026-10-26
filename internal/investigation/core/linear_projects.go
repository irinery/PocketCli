package core

import (
	"context"
	"encoding/json"
	"errors"
	"os"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/query"
	"pocketcli/internal/investigation/storage"
)

const linearProjectsQuery = `query TeamProjects($team: ID!, $after: String) { projects(first: 100, after: $after, includeArchived: true, orderBy: updatedAt, filter: {accessibleTeams: {some: {id: {eq: $team}}}}) { nodes { id name createdAt updatedAt archivedAt } pageInfo { hasNextPage endCursor } } }`

func (c *Core) collectLinearProjects(ctx context.Context, plan contract.Plan, r contract.Resource, d *query.Dataset, run *contract.Run) error {
	journalID := storage.Hash([]byte(plan.ID + ":" + r.ID + ":linear:projects"))
	journal := linearJournal{PlanID: plan.ID, ResourceID: r.ID, Items: []json.RawMessage{}}
	if err := c.store.Get("journals", journalID, &journal); err == nil {
		if journal.PlanID != plan.ID || journal.ResourceID != r.ID {
			return fault("ERR_AUDIT_FAILED", "journal de projetos Linear incompatível")
		}
	} else if !os.IsNotExist(err) {
		return fault("ERR_AUDIT_FAILED", "journal de projetos Linear corrompido")
	}
	status, reason := "complete", "projetos do team, inclusive arquivados, inventariados na janela"
	for !journal.Complete && journal.Pages < 20 {
		vars := map[string]string{"team": r.ID}
		if journal.Cursor != "" {
			vars["after"] = journal.Cursor
		}
		data, queryErr := c.collector.LinearQuery(ctx, linearProjectsQuery, vars)
		run.Requests++
		run.Bytes += int64(len(data))
		if !c.saveRun(run) {
			return fault("ERR_AUDIT_FAILED", "falha ao auditar projetos Linear")
		}
		if run.Bytes > plan.Input.Limits.MaxTransferBytes {
			return fault("ERR_RESOURCE_EXHAUSTED", "orçamento de transferência atingido")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if queryErr != nil {
			var coded *contract.ErrorResponse
			if errors.As(queryErr, &coded) && coded.Code == "ERR_RATE_LIMITED" {
				return queryErr
			}
			status, reason = "partial", "consulta de projetos Linear falhou"
			if errors.As(queryErr, &coded) && coded.Code == "ERR_FORBIDDEN" {
				status = "unauthorized"
			}
			break
		}
		var result struct {
			Projects struct {
				Nodes    []json.RawMessage `json:"nodes"`
				PageInfo struct {
					HasNextPage bool   `json:"hasNextPage"`
					EndCursor   string `json:"endCursor"`
				} `json:"pageInfo"`
			} `json:"projects"`
		}
		if json.Unmarshal(data, &result) != nil || result.Projects.Nodes == nil || (result.Projects.PageInfo.HasNextPage && (result.Projects.PageInfo.EndCursor == "" || result.Projects.PageInfo.EndCursor == journal.Cursor)) {
			status, reason = "partial", "resposta ou cursor de projetos Linear inválido"
			break
		}
		journal.Items = append(journal.Items, result.Projects.Nodes...)
		journal.Pages++
		journal.Cursor = result.Projects.PageInfo.EndCursor
		journal.Complete = !result.Projects.PageInfo.HasNextPage
		if err := c.store.Put("journals", journalID, journal); err != nil {
			return fault("ERR_AUDIT_FAILED", "falha ao persistir projetos Linear")
		}
	}
	if !journal.Complete && status == "complete" {
		status, reason = "partial", "paginação de projetos Linear incompleta"
	}
	for _, raw := range journal.Items {
		var item struct {
			ID         string `json:"id"`
			Name       string `json:"name"`
			CreatedAt  string `json:"createdAt"`
			UpdatedAt  string `json:"updatedAt"`
			ArchivedAt string `json:"archivedAt"`
		}
		if json.Unmarshal(raw, &item) != nil || item.ID == "" || item.UpdatedAt == "" {
			status, reason = "partial", "projeto Linear inválido"
			continue
		}
		if !timestampInWindow(plan.WindowStart, plan.WindowEnd, item.CreatedAt) && !timestampInWindow(plan.WindowStart, plan.WindowEnd, item.UpdatedAt) && !timestampInWindow(plan.WindowStart, plan.WindowEnd, item.ArchivedAt) {
			continue
		}
		e, err := c.evidence("linear", "project:"+item.ID, item.UpdatedAt, raw)
		if err != nil {
			return err
		}
		upsertEvidence(d, e)
		projectID := "proj-" + storage.Hash([]byte(item.ID))[:24]
		upsertComponent(d, contract.ComponentRecord{ID: projectID, Name: c.safeTitle(ctx, item.Name), Kind: "linear_project", ImplementationStatus: "unknown", DeploymentStatus: "unknown", Coverage: "partial"})
		run.Items++
	}
	if !c.saveRun(run) {
		return fault("ERR_AUDIT_FAILED", "falha ao auditar projetos Linear")
	}
	d.Coverage = append(d.Coverage, c.coverage(r, "projects", status, int64(len(journal.Items)), reason))
	return nil
}
