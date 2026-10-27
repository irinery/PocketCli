package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/query"
	"pocketcli/internal/investigation/storage"
)

const linearHistoryQuery = `query IssueHistory($id: String!, $after: String) { issue(id: $id) { id history(first: 100, after: $after, includeArchived: true) { nodes { id createdAt actor { id } fromState { name type } toState { name type } fromEstimate toEstimate fromProjectId toProjectId } pageInfo { hasNextPage endCursor } } } }`
const linearCommentsQuery = `query IssueComments($id: String!, $after: String) { issue(id: $id) { id comments(first: 100, after: $after, includeArchived: true) { nodes { id body createdAt updatedAt user { id } } pageInfo { hasNextPage endCursor } } } }`
const linearRelationsQuery = `query IssueRelations($id: String!, $after: String) { issue(id: $id) { id relations(first: 100, after: $after, includeArchived: false) { nodes { id type createdAt updatedAt issue { id } relatedIssue { id } } pageInfo { hasNextPage endCursor } } } }`
const linearInverseRelationsQuery = `query IssueInverseRelations($id: String!, $after: String) { issue(id: $id) { id inverseRelations(first: 100, after: $after, includeArchived: false) { nodes { id type createdAt updatedAt issue { id } relatedIssue { id } } pageInfo { hasNextPage endCursor } } } }`
const linearIssueFieldsQuery = `query IssueFields { __type(name: "Issue") { fields { name } } }`

type linearChildSchema struct {
	History          bool `json:"history"`
	Comments         bool `json:"comments"`
	Relations        bool `json:"relations"`
	InverseRelations bool `json:"inverse_relations"`
}

func (c *Core) linearSchema(ctx context.Context, plan contract.Plan, r contract.Resource, run *contract.Run) (linearChildSchema, error) {
	id := storage.Hash([]byte(plan.ID + ":" + r.ID + ":schema"))
	var schema linearChildSchema
	if err := c.store.Get("linearschema", id, &schema); err == nil {
		return schema, nil
	} else if !os.IsNotExist(err) {
		return schema, fault("ERR_AUDIT_FAILED", "schema Linear em cache corrompido")
	}
	data, err := c.collector.LinearQuery(ctx, linearIssueFieldsQuery, nil)
	run.Requests++
	run.Bytes += int64(len(data))
	if !c.saveRun(run) {
		return schema, fault("ERR_AUDIT_FAILED", "falha ao auditar schema Linear")
	}
	if run.Bytes > plan.Input.Limits.MaxTransferBytes {
		return schema, fault("ERR_RESOURCE_EXHAUSTED", "orçamento de transferência atingido")
	}
	if err != nil {
		return schema, err
	}
	var response struct {
		Type struct {
			Fields []struct {
				Name string `json:"name"`
			} `json:"fields"`
		} `json:"__type"`
	}
	if json.Unmarshal(data, &response) != nil || response.Type.Fields == nil {
		return schema, fault("ERR_UNAVAILABLE", "introspecção Linear inválida")
	}
	for _, field := range response.Type.Fields {
		schema.History = schema.History || field.Name == "history"
		schema.Comments = schema.Comments || field.Name == "comments"
		schema.Relations = schema.Relations || field.Name == "relations"
		schema.InverseRelations = schema.InverseRelations || field.Name == "inverseRelations"
	}
	if err := c.store.Put("linearschema", id, schema); err != nil {
		return schema, fault("ERR_AUDIT_FAILED", "falha ao persistir schema Linear")
	}
	return schema, nil
}

func (c *Core) collectLinearChildren(ctx context.Context, plan contract.Plan, r contract.Resource, d *query.Dataset, run *contract.Run) error {
	schema, err := c.linearSchema(ctx, plan, r, run)
	if err != nil {
		var coded *contract.ErrorResponse
		if errors.As(err, &coded) && (coded.Code == "ERR_AUDIT_FAILED" || coded.Code == "ERR_RESOURCE_EXHAUSTED" || coded.Code == "ERR_RATE_LIMITED") {
			return err
		}
		for _, collection := range []string{"history", "comments", "relations", "inverseRelations"} {
			d.Coverage = append(d.Coverage, c.coverage(r, collection, "partial", 0, "schema Linear não acessível"))
		}
		return nil
	}
	targets := make([]query.Issue, 0)
	for _, issue := range d.Issues {
		if issue.Source == "linear" && issue.TeamID == r.ID && strings.HasPrefix(issue.ID, "linear-") {
			targets = append(targets, issue)
		}
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].ID < targets[j].ID })
	for _, collection := range []struct {
		name, gql string
		available bool
	}{{"history", linearHistoryQuery, schema.History}, {"comments", linearCommentsQuery, schema.Comments}, {"relations", linearRelationsQuery, schema.Relations}, {"inverseRelations", linearInverseRelationsQuery, schema.InverseRelations}} {
		if !collection.available {
			d.Coverage = append(d.Coverage, c.coverage(r, collection.name, "unsupported", 0, "campo ausente no schema Linear consultado"))
			continue
		}
		status, reason := "complete", "coleção por issue consultada com paginação"
		selected := targets
		cursorID := ""
		if len(targets) > 100 {
			status, reason = "partial", fmt.Sprintf("fila de reconciliação Linear: %d issues pendentes", len(targets)-100)
			var auth planAuth
			if err := c.store.Get("planauth", plan.ID, &auth); err != nil {
				return fault("ERR_AUDIT_FAILED", "identidade Linear indisponível")
			}
			cursorID = storage.Hash([]byte(plan.InvestigationID + ":" + r.ID + ":" + collection.name + ":" + auth.Linear))
			var cursor githubReconcileCursor
			if err := c.store.Get("reconcile", cursorID, &cursor); err != nil && !os.IsNotExist(err) {
				return fault("ERR_AUDIT_FAILED", "cursor Linear corrompido")
			}
			start := sort.Search(len(targets), func(i int) bool { return targets[i].ID > cursor.LastEntityID })
			if start >= len(targets) {
				start = 0
			}
			selected = append(append([]query.Issue{}, targets[start:]...), targets[:start]...)
			selected = selected[:100]
		}
		var observed int64
		lastComplete := ""
		for _, issue := range selected {
			if err := ctx.Err(); err != nil {
				return err
			}
			items, complete, err := c.linearChildPages(ctx, plan, r, issue, collection.name, collection.gql, run)
			if err != nil {
				var coded *contract.ErrorResponse
				if errors.As(err, &coded) && (coded.Code == "ERR_AUDIT_FAILED" || coded.Code == "ERR_RESOURCE_EXHAUSTED" || coded.Code == "ERR_RATE_LIMITED") {
					return err
				}
				status, reason = "partial", "coleção Linear incompleta ou inacessível"
			}
			if !complete {
				status, reason = "partial", "paginação Linear por issue incompleta"
			} else {
				lastComplete = issue.ID
			}
			observed += int64(len(items))
			for _, raw := range items {
				if collection.name == "history" {
					if err := c.projectLinearHistory(plan, issue, raw, d, run); err != nil {
						return err
					}
				} else if collection.name == "comments" {
					if err := c.projectLinearComment(plan, issue, raw, d, run); err != nil {
						return err
					}
				} else {
					valid, err := c.projectLinearRelation(plan, issue, collection.name, raw, d, run)
					if err != nil {
						return err
					}
					if !valid {
						status, reason = "partial", "relação Linear inválida, divergente ou posterior à janela"
					}
				}
			}
		}
		if cursorID != "" && lastComplete != "" {
			if err := c.store.Put("reconcile", cursorID, githubReconcileCursor{LastEntityID: lastComplete}); err != nil {
				return fault("ERR_AUDIT_FAILED", "falha ao persistir cursor Linear")
			}
		}
		d.Coverage = append(d.Coverage, c.coverage(r, collection.name, status, observed, reason))
	}
	return nil
}

func (c *Core) linearChildPages(ctx context.Context, plan contract.Plan, r contract.Resource, issue query.Issue, collection, gql string, run *contract.Run) ([]json.RawMessage, bool, error) {
	journalID := storage.Hash([]byte(plan.ID + ":" + r.ID + ":linear:" + collection + ":" + issue.ID))
	journal := linearJournal{PlanID: plan.ID, ResourceID: r.ID, Items: []json.RawMessage{}}
	if err := c.store.Get("journals", journalID, &journal); err == nil {
		if journal.PlanID != plan.ID || journal.ResourceID != r.ID {
			return nil, false, fault("ERR_AUDIT_FAILED", "journal Linear incompatível")
		}
	} else if !os.IsNotExist(err) {
		return nil, false, fault("ERR_AUDIT_FAILED", "journal Linear corrompido")
	}
	for !journal.Complete && journal.Pages < 10 {
		vars := map[string]string{"id": strings.TrimPrefix(issue.ID, "linear-")}
		if journal.Cursor != "" {
			vars["after"] = journal.Cursor
		}
		data, err := c.collector.LinearQuery(ctx, gql, vars)
		run.Requests++
		run.Bytes += int64(len(data))
		if !c.saveRun(run) {
			return journal.Items, false, fault("ERR_AUDIT_FAILED", "falha ao auditar child Linear")
		}
		if run.Bytes > plan.Input.Limits.MaxTransferBytes {
			return journal.Items, false, fault("ERR_RESOURCE_EXHAUSTED", "orçamento de transferência atingido")
		}
		if err != nil {
			return journal.Items, false, err
		}
		var result struct {
			Issue map[string]json.RawMessage `json:"issue"`
		}
		if json.Unmarshal(data, &result) != nil || result.Issue == nil {
			return journal.Items, false, fault("ERR_UNAVAILABLE", "issue Linear inválida")
		}
		var returnedID string
		if json.Unmarshal(result.Issue["id"], &returnedID) != nil || returnedID != vars["id"] {
			return journal.Items, false, fault("ERR_UNAVAILABLE", "issue Linear mudou durante a coleta")
		}
		var page struct {
			Nodes    []json.RawMessage `json:"nodes"`
			PageInfo struct {
				HasNextPage bool   `json:"hasNextPage"`
				EndCursor   string `json:"endCursor"`
			} `json:"pageInfo"`
		}
		if json.Unmarshal(result.Issue[collection], &page) != nil || page.Nodes == nil || (page.PageInfo.HasNextPage && (page.PageInfo.EndCursor == "" || page.PageInfo.EndCursor == journal.Cursor)) {
			return journal.Items, false, fault("ERR_UNAVAILABLE", "paginação Linear inválida")
		}
		journal.Items = append(journal.Items, page.Nodes...)
		journal.Pages++
		journal.Cursor = page.PageInfo.EndCursor
		journal.Complete = !page.PageInfo.HasNextPage
		if err := c.store.Put("journals", journalID, journal); err != nil {
			return journal.Items, false, fault("ERR_AUDIT_FAILED", "falha ao persistir página Linear")
		}
	}
	return journal.Items, journal.Complete, nil
}

func (c *Core) projectLinearHistory(plan contract.Plan, issue query.Issue, raw json.RawMessage, d *query.Dataset, run *contract.Run) error {
	var item struct {
		ID            string   `json:"id"`
		CreatedAt     string   `json:"createdAt"`
		FromEstimate  *float64 `json:"fromEstimate"`
		ToEstimate    *float64 `json:"toEstimate"`
		FromProjectID string   `json:"fromProjectId"`
		ToProjectID   string   `json:"toProjectId"`
		Actor         *struct {
			ID string `json:"id"`
		} `json:"actor"`
		FromState *struct {
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"fromState"`
		ToState *struct {
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"toState"`
	}
	if json.Unmarshal(raw, &item) != nil || item.ID == "" || item.CreatedAt == "" {
		return nil
	}
	if !timestampInWindow(plan.WindowStart, plan.WindowEnd, item.CreatedAt) {
		return nil
	}
	e, err := c.evidence("linear", issue.Identifier+":history:"+item.ID, item.CreatedAt, raw)
	if err != nil {
		return err
	}
	upsertEvidence(d, e)
	actorID := ""
	if item.Actor != nil {
		actorID = item.Actor.ID
	}
	if item.FromState != nil || item.ToState != nil {
		oldState, newState := "", ""
		if item.FromState != nil {
			oldState = item.FromState.Type
			if oldState == "" {
				oldState = item.FromState.Name
			}
		}
		if item.ToState != nil {
			newState = item.ToState.Type
			if newState == "" {
				newState = item.ToState.Name
			}
		}
		upsertEvent(d, query.Event{ID: "evt-" + storage.Hash([]byte(issue.ID + ":" + item.ID + ":state:" + e.ID))[:24], EntityID: issue.ID, Type: "state_changed", EventAt: item.CreatedAt, ObservedAt: e.ObservedAt, ActorID: actorID, OldValue: oldState, NewValue: newState, EvidenceID: e.ID})
	}
	if item.FromEstimate != nil || item.ToEstimate != nil {
		timing := "unknown"
		if started, err := time.Parse(time.RFC3339, issue.StartedAt); err == nil {
			if changed, err := time.Parse(time.RFC3339, item.CreatedAt); err == nil {
				timing = "before_start"
				if !changed.Before(started) {
					timing = "after_start"
				}
			}
		}
		d.Estimates = append(d.Estimates, contract.EstimateRecord{ID: "est-" + storage.Hash([]byte(issue.ID + ":" + item.ID + ":" + e.ID))[:24], IssueID: issue.ID, ActorID: actorID, EventAt: item.CreatedAt, Before: item.FromEstimate, After: item.ToEstimate, Timing: timing, EvidenceID: e.ID})
	}
	if item.FromProjectID != "" || item.ToProjectID != "" {
		upsertEvent(d, query.Event{ID: "evt-" + storage.Hash([]byte(issue.ID + ":" + item.ID + ":project:" + e.ID))[:24], EntityID: issue.ID, Type: "project_changed", EventAt: item.CreatedAt, ObservedAt: e.ObservedAt, ActorID: actorID, OldValue: item.FromProjectID, NewValue: item.ToProjectID, EvidenceID: e.ID})
	}
	run.Items++
	if !c.saveRun(run) {
		return fault("ERR_AUDIT_FAILED", "falha ao auditar history Linear")
	}
	return nil
}

func (c *Core) projectLinearComment(plan contract.Plan, issue query.Issue, raw json.RawMessage, d *query.Dataset, run *contract.Run) error {
	var item struct {
		ID        string `json:"id"`
		Body      string `json:"body"`
		CreatedAt string `json:"createdAt"`
		UpdatedAt string `json:"updatedAt"`
		User      *struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	if json.Unmarshal(raw, &item) != nil || item.ID == "" || item.CreatedAt == "" || item.UpdatedAt == "" {
		return nil
	}
	if !timestampInWindow(plan.WindowStart, plan.WindowEnd, item.CreatedAt) && !timestampInWindow(plan.WindowStart, plan.WindowEnd, item.UpdatedAt) {
		return nil
	}
	e, err := c.evidence("linear", issue.Identifier+":comment:"+item.ID, item.UpdatedAt, raw)
	if err != nil {
		return err
	}
	upsertEvidence(d, e)
	actorID := ""
	if item.User != nil {
		actorID = item.User.ID
	}
	kind := "commented"
	if item.UpdatedAt > item.CreatedAt {
		kind = "comment_edited"
	}
	upsertEvent(d, query.Event{ID: "evt-" + storage.Hash([]byte(issue.ID + ":" + item.ID + ":" + e.ID))[:24], EntityID: issue.ID, Type: kind, EventAt: item.UpdatedAt, ObservedAt: e.ObservedAt, ActorID: actorID, EvidenceID: e.ID})
	run.Items++
	if !c.saveRun(run) {
		return fault("ERR_AUDIT_FAILED", "falha ao auditar comentário Linear")
	}
	return nil
}

func (c *Core) projectLinearRelation(plan contract.Plan, issue query.Issue, collection string, raw json.RawMessage, d *query.Dataset, run *contract.Run) (bool, error) {
	var relation struct {
		ID        string `json:"id"`
		Type      string `json:"type"`
		UpdatedAt string `json:"updatedAt"`
		Issue     struct {
			ID string `json:"id"`
		} `json:"issue"`
		RelatedIssue struct {
			ID string `json:"id"`
		} `json:"relatedIssue"`
	}
	if json.Unmarshal(raw, &relation) != nil || relation.ID == "" || relation.Type == "" || !timestampInWindow("", plan.WindowEnd, relation.UpdatedAt) || relation.Issue.ID == "" || relation.RelatedIssue.ID == "" {
		return false, nil
	}
	queriedID := strings.TrimPrefix(issue.ID, "linear-")
	if (collection == "relations" && relation.Issue.ID != queriedID) || (collection == "inverseRelations" && relation.RelatedIssue.ID != queriedID) {
		return false, nil
	}
	if relation.Type != "blocks" {
		return true, nil
	}
	e, err := c.evidence("linear", "relation:"+relation.ID, relation.UpdatedAt, raw)
	if err != nil {
		return false, err
	}
	upsertEvidence(d, e)
	upsertLink(d, contract.LinkRecord{
		ID:         "link-" + storage.Hash([]byte("linear:relation:" + relation.ID))[:24],
		SourceID:   "linear-" + relation.RelatedIssue.ID,
		Relation:   "blocked_by",
		TargetID:   "linear-" + relation.Issue.ID,
		Status:     "direct",
		EvidenceID: e.ID,
	})
	run.Items++
	if !c.saveRun(run) {
		return false, fault("ERR_AUDIT_FAILED", "falha ao auditar relação Linear")
	}
	return true, nil
}
