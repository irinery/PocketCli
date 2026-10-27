package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/query"
	"pocketcli/internal/investigation/rules"
	"pocketcli/internal/investigation/storage"
)

type githubJournal struct {
	PlanID, ResourceID, Collection string
	Items                          []json.RawMessage
	Cursor                         string
	Pages                          int
	Complete                       bool
}

type linearJournal struct {
	PlanID, ResourceID string
	Items              []json.RawMessage
	Cursor             string
	Pages              int
	Complete           bool
}

func (c *Core) collectDataset(ctx context.Context, plan contract.Plan, run *contract.Run) (query.Dataset, error) {
	d := query.Dataset{Snapshot: contract.Snapshot{WindowStart: plan.WindowStart, WindowEnd: plan.WindowEnd, AnalysisReferenceAt: plan.WindowEnd}, Components: []contract.ComponentRecord{}, Findings: []contract.Finding{}, Evidence: []contract.Evidence{}, Coverage: []contract.Coverage{}, Issues: []query.Issue{}, PullRequests: []query.PullRequest{}, Links: []contract.LinkRecord{}}
	var previous contract.Snapshot
	snapshots, err := c.ListSnapshots(ctx, plan.InvestigationID)
	if err != nil {
		return d, fault("ERR_AUDIT_FAILED", "snapshots anteriores indisponíveis")
	}
	if len(snapshots) > 0 {
		candidate := snapshots[len(snapshots)-1]
		canReuse, err := c.canReuse(candidate.PlanID, plan.ID)
		if err != nil {
			return d, err
		}
		if canReuse && sameResources(candidate.Resources, plan.Resources) && candidate.Modules == plan.Input.Modules {
			previous = candidate
		}
		if previous.ID != "" {
			prior, err := c.dataset(previous.ID)
			if err != nil {
				return d, err
			}
			evidenceHashes := make(map[string]string, len(prior.Evidence))
			for _, evidence := range prior.Evidence {
				evidenceHashes[evidence.ID] = evidence.SHA256
			}
			referenced := make(map[string]bool)
			for _, i := range prior.Issues {
				if activeInWindow(plan.WindowStart, plan.WindowEnd, i.CreatedAt, i.UpdatedAt, i.State, plan.Input.Period.Preset != "custom") {
					if i.Outcome == "" && (i.Source == "github" || i.Source == "linear") {
						i, err = c.classifyRetainedIssue(i, evidenceHashes[i.EvidenceID])
						if err != nil {
							return d, err
						}
					}
					d.Issues = append(d.Issues, i)
					referenced[i.EvidenceID] = true
				}
			}
			for _, p := range prior.PullRequests {
				if activeInWindow(plan.WindowStart, plan.WindowEnd, p.CreatedAt, p.UpdatedAt, p.State, plan.Input.Period.Preset != "custom") {
					p, err = c.classifyRetainedPR(p, evidenceHashes[p.EvidenceID])
					if err != nil {
						return d, err
					}
					d.PullRequests = append(d.PullRequests, p)
					referenced[p.EvidenceID] = true
				}
			}
			for _, event := range prior.Events {
				if timestampInWindow(plan.WindowStart, plan.WindowEnd, event.EventAt) {
					event, err = c.classifyRetainedActorEvent(event, d.PullRequests, evidenceHashes[event.EvidenceID])
					if err != nil {
						return d, err
					}
					d.Events = append(d.Events, event)
					referenced[event.EvidenceID] = true
				}
			}
			reusePriorFileGraph(&d, prior, plan.WindowEnd, referenced)
			reusePriorPRCommitGraph(&d, prior, plan.WindowEnd, referenced)
			for _, evidence := range prior.Evidence {
				if referenced[evidence.ID] || timestampInWindow(plan.WindowStart, plan.WindowEnd, evidence.SourceUpdatedAt) {
					d.Evidence = append(d.Evidence, evidence)
				}
			}
		}
	}
	for _, r := range plan.Resources {
		if err := ctx.Err(); err != nil {
			return d, err
		}
		switch r.Source {
		case "github":
			if err := c.collectRepository(ctx, plan, r, previous, &d, run); err != nil {
				return d, err
			}
			if plan.Input.Modules.CI {
				if err := c.collectActions(ctx, plan, r, &d, run); err != nil {
					return d, err
				}
				if err := c.collectChecks(ctx, plan, r, &d, run); err != nil {
					return d, err
				}
			}
			if plan.Input.Modules.Deployments {
				if err := c.collectDeployments(ctx, plan, r, &d, run); err != nil {
					return d, err
				}
			}
			if plan.Input.Modules.CodeHistory {
				if err := c.collectGitRef(ctx, plan, r, &d, run); err != nil {
					return d, err
				}
				if err := c.collectGitBranches(ctx, plan, r, &d, run); err != nil {
					return d, err
				}
				if err := c.collectGitBranchGraphs(ctx, plan, r, &d, run); err != nil {
					return d, err
				}
				if err := c.collectGitCommits(ctx, plan, r, &d, run); err != nil {
					return d, err
				}
				if err := c.collectGitCommitFiles(ctx, plan, r, &d, run); err != nil {
					return d, err
				}
				if err := c.collectPRFiles(ctx, plan, r, &d, run); err != nil {
					return d, err
				}
				if err := c.collectPRCommits(ctx, plan, r, &d, run); err != nil {
					return d, err
				}
				if err := c.mapCurrentRefFiles(ctx, plan, r, &d); err != nil {
					return d, err
				}
				if err := c.mapPRHeadFiles(ctx, plan, r, &d, run); err != nil {
					return d, err
				}
				c.completeCodeHistoryCoverage(r.ID, &d)
			}
		case "linear":
			if err := c.collectTeam(ctx, plan, r, &d, run); err != nil {
				return d, err
			}
		case "runtime":
			if err := c.collectRuntime(ctx, r, &d, run); err != nil {
				return d, err
			}
		}
	}
	if !plan.Input.Modules.CI {
		d.Coverage = append(d.Coverage, c.coverage(contract.Resource{Source: "github", ID: "all"}, "actions", "not_selected", 0, "módulo CI não selecionado"))
		d.Coverage = append(d.Coverage, c.coverage(contract.Resource{Source: "github", ID: "all"}, "checks", "not_selected", 0, "módulo CI não selecionado"))
	}
	if plan.Input.Modules.CodeHistory {
		hasGitHub := false
		for _, r := range plan.Resources {
			hasGitHub = hasGitHub || r.Source == "github"
		}
		if !hasGitHub {
			d.Coverage = append(d.Coverage, c.coverage(contract.Resource{Source: "git", ID: "all"}, "code_history", "not_selected", 0, "nenhum repositório GitHub habilitado"))
		}
	}
	if err := c.linkDataset(plan, &d); err != nil {
		return d, err
	}
	d = rules.Evaluate(d)
	canonicalizeDataset(&d)
	return d, nil
}

func sameResources(previous, current []contract.Resource) bool {
	if len(previous) != len(current) {
		return false
	}
	for i := range previous {
		if previous[i] != current[i] {
			return false
		}
	}
	return true
}

func (c *Core) coverage(r contract.Resource, collection, status string, n int64, reason string) contract.Coverage {
	return contract.Coverage{Source: r.Source, ResourceID: r.ID, Collection: collection, Status: status, Observed: n, Reason: reason, CheckedAt: c.setup.now().UTC().Format(time.RFC3339)}
}

func (c *Core) collectRepository(ctx context.Context, plan contract.Plan, r contract.Resource, previous contract.Snapshot, d *query.Dataset, run *contract.Run) error {
	d.Components = append(d.Components, contract.ComponentRecord{ID: "repo-" + r.ID, Name: r.Name, Repository: r.Name, Path: ".", Kind: "repository", ImplementationStatus: "unknown", DeploymentStatus: "unknown", Coverage: "partial"})
	collections := []struct {
		name, path string
		enabled    bool
	}{{"pull_requests", "/repos/" + r.Name + "/pulls?state=all&sort=updated&direction=desc&per_page=100", true}, {"issues", "/repos/" + r.Name + "/issues?state=all&sort=updated&direction=desc&per_page=100", plan.Input.Modules.GitHubIssues}}
	for _, col := range collections {
		if !col.enabled {
			d.Coverage = append(d.Coverage, c.coverage(r, col.name, "not_selected", 0, "módulo não selecionado"))
			continue
		}
		cursor, err := c.githubCursor(ctx, plan, r, col.name)
		if err != nil {
			return err
		}
		cutoff := ""
		if cursor.value.Watermark != "" && cursor.coversWindowStart(plan.WindowStart) {
			cutoff = cursor.deltaStart(plan)
		}
		for _, old := range previous.Coverage {
			if cursor.value.Watermark != "" {
				break
			}
			if old.Source == "github" && old.ResourceID == r.ID && old.Collection == col.name && old.Status == "complete" {
				// An expanded window needs its older gap; the recent delta alone is insufficient.
				if previous.WindowStart != "" && plan.WindowStart >= previous.WindowStart {
					if when, e := time.Parse(time.RFC3339, previous.ObservationFinishedAt); e == nil {
						cutoff = when.Add(-48 * time.Hour).Format(time.RFC3339)
					}
				}
			}
		}
		journalID := storage.Hash([]byte(plan.ID + ":" + r.ID + ":" + col.name))
		journal := githubJournal{PlanID: plan.ID, ResourceID: r.ID, Collection: col.name, Items: []json.RawMessage{}}
		if err := c.store.Get("journals", journalID, &journal); err == nil {
			if journal.PlanID != plan.ID || journal.ResourceID != r.ID || journal.Collection != col.name {
				return fault("ERR_AUDIT_FAILED", "journal de coleta incompatível")
			}
		} else if !os.IsNotExist(err) {
			return fault("ERR_AUDIT_FAILED", "journal de coleta corrompido")
		}
		err = nil
		if !journal.Complete && journal.Pages < 20 {
			path := col.path
			if journal.Cursor != "" {
				path = journal.Cursor
			}
			successful := 0
			var count int
			count, err = c.collector.GitHubWalk(ctx, path, 20-journal.Pages, cutoff, func(page []json.RawMessage, next string, responseBytes int64) error {
				successful++
				journal.Items = append(journal.Items, page...)
				journal.Pages++
				journal.Cursor = next
				journal.Complete = next == ""
				if e := c.store.Put("journals", journalID, journal); e != nil {
					return fault("ERR_AUDIT_FAILED", "falha ao persistir página")
				}
				if e := c.saveSourceCheckpoint(&cursor, plan.ID, next); e != nil {
					return e
				}
				run.Requests++
				run.Bytes += responseBytes
				if !c.saveRun(run) {
					return fault("ERR_AUDIT_FAILED", "falha ao auditar request")
				}
				if run.Bytes > plan.Input.Limits.MaxTransferBytes {
					return fault("ERR_RESOURCE_EXHAUSTED", "orçamento de transferência atingido")
				}
				return nil
			})
			if count > successful {
				run.Requests += int64(count - successful)
				if !c.saveRun(run) {
					return fault("ERR_AUDIT_FAILED", "falha ao auditar request")
				}
			}
		}
		if !journal.Complete && err == nil {
			err = fault("ERR_RESOURCE_EXHAUSTED", "paginação GitHub excedeu limite")
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var collectionErr *contract.ErrorResponse
		if errors.As(err, &collectionErr) && (collectionErr.Code == "ERR_AUDIT_FAILED" || collectionErr.Code == "ERR_RESOURCE_EXHAUSTED") {
			return err
		}
		items := journal.Items
		status := "complete"
		reason := ""
		if err != nil {
			status = "partial"
			reason = "paginação ou acesso incompleto"
			var ce *contract.ErrorResponse
			if errors.As(err, &ce) && ce.Code == "ERR_FORBIDDEN" {
				status = "unauthorized"
			}
			if len(items) == 0 && status == "partial" {
				status = "unavailable"
			}
		} else if cutoff != "" || journal.Pages > 1 {
			// GitHub list pagination has no snapshot isolation. An item moved
			// between pages can be missed even when each response succeeds.
			status = "partial"
			reason = "lista paginada ou delta sem snapshot estável; reconciliação pendente"
		}
		for _, raw := range items {
			if err := ctx.Err(); err != nil {
				return err
			}
			var obj struct {
				ID          int64   `json:"id"`
				Number      int     `json:"number"`
				Title       string  `json:"title"`
				State       string  `json:"state"`
				StateReason string  `json:"state_reason"`
				CreatedAt   string  `json:"created_at"`
				UpdatedAt   string  `json:"updated_at"`
				ClosedAt    *string `json:"closed_at"`
				MergedAt    *string `json:"merged_at"`
				Draft       bool    `json:"draft"`
				User        struct {
					ID   int64  `json:"id"`
					Type string `json:"type"`
				} `json:"user"`
				Head struct {
					SHA string `json:"sha"`
				} `json:"head"`
				MergeCommitSHA *string         `json:"merge_commit_sha"`
				PullRequest    json.RawMessage `json:"pull_request"`
			}
			if json.Unmarshal(raw, &obj) != nil || obj.ID == 0 {
				status = "partial"
				reason = "item inválido"
				continue
			}
			if col.name == "issues" && len(obj.PullRequest) > 0 {
				continue
			}
			if !activeInWindow(plan.WindowStart, plan.WindowEnd, obj.CreatedAt, obj.UpdatedAt, obj.State, plan.Input.Period.Preset != "custom") {
				continue
			}
			e, err := c.evidence("github", fmt.Sprintf("%s#%d", r.Name, obj.Number), obj.UpdatedAt, raw)
			if err != nil {
				return err
			}
			upsertEvidence(d, e)
			if col.name == "pull_requests" {
				p := query.PullRequest{ID: fmt.Sprintf("github-pr-%d", obj.ID), Repository: r.Name, Number: obj.Number, Title: c.safeTitle(ctx, obj.Title), State: obj.State, CreatedAt: obj.CreatedAt, UpdatedAt: obj.UpdatedAt, HeadSHA: obj.Head.SHA, EvidenceID: e.ID}
				if obj.User.ID > 0 {
					p.AuthorID = fmt.Sprintf("github-%d", obj.User.ID)
					p.AuthorKind = githubAccountKind(obj.User.Type)
				}
				if obj.MergedAt != nil {
					p.MergedAt = *obj.MergedAt
				}
				if obj.MergeCommitSHA != nil {
					p.MergeSHA = *obj.MergeCommitSHA
				}
				upsertPR(d, p)
			} else {
				i := query.Issue{ID: fmt.Sprintf("github-issue-%d", obj.ID), Identifier: fmt.Sprintf("%s#%d", r.Name, obj.Number), Source: "github", Title: c.safeTitle(ctx, obj.Title), State: obj.State, CreatedAt: obj.CreatedAt, UpdatedAt: obj.UpdatedAt, EvidenceID: e.ID, IsContext: contextOnly(plan.WindowStart, obj.CreatedAt, obj.UpdatedAt)}
				i.Outcome = githubIssueOutcome(obj.State, obj.StateReason)
				if i.Outcome == "completed" && obj.ClosedAt != nil {
					i.CompletedAt = *obj.ClosedAt
				}
				upsertIssue(d, i)
			}
			run.Items++
			if !c.saveRun(run) {
				return fault("ERR_AUDIT_FAILED", "falha ao auditar item")
			}
		}
		d.Coverage = append(d.Coverage, c.coverage(r, col.name, status, int64(len(items)), reason))
		if err := c.finishSourceCursor(&cursor, plan, status); err != nil {
			return err
		}
	}
	if err := c.collectComments(ctx, plan, r, previous, d, run); err != nil {
		return err
	}
	if err := c.collectReviewComments(ctx, plan, r, previous, d, run); err != nil {
		return err
	}
	if err := c.collectGitHubChildren(ctx, plan, r, d, run); err != nil {
		return err
	}
	return nil
}

func (c *Core) collectComments(ctx context.Context, plan contract.Plan, r contract.Resource, previous contract.Snapshot, d *query.Dataset, run *contract.Run) error {
	return c.collectRepositoryComments(ctx, plan, r, previous, d, run, "comments", "issues/comments", "issue_comment")
}

func (c *Core) collectReviewComments(ctx context.Context, plan contract.Plan, r contract.Resource, previous contract.Snapshot, d *query.Dataset, run *contract.Run) error {
	return c.collectRepositoryComments(ctx, plan, r, previous, d, run, "review_comments", "pulls/comments", "review_comment")
}

func (c *Core) collectRepositoryComments(ctx context.Context, plan contract.Plan, r contract.Resource, previous contract.Snapshot, d *query.Dataset, run *contract.Run, collection, endpoint, locatorKind string) error {
	path := "/repos/" + r.Name + "/" + endpoint + "?sort=updated&direction=desc&per_page=100"
	cursor, err := c.githubCursor(ctx, plan, r, collection)
	if err != nil {
		return err
	}
	since := plan.WindowStart
	if cursor.value.Watermark != "" {
		since = cursor.deltaStart(plan)
	}
	for _, old := range previous.Coverage {
		if cursor.value.Watermark != "" {
			break
		}
		if old.Source == "github" && old.ResourceID == r.ID && old.Collection == collection && old.Status == "complete" && (previous.WindowStart == "" || plan.WindowStart >= previous.WindowStart) {
			if observed, err := time.Parse(time.RFC3339, previous.ObservationFinishedAt); err == nil {
				candidate := observed.Add(-48 * time.Hour)
				if since == "" {
					since = candidate.Format(time.RFC3339)
				} else if start, err := time.Parse(time.RFC3339, since); err == nil && candidate.After(start) {
					since = candidate.Format(time.RFC3339)
				}
			}
		}
	}
	if since != "" {
		start, err := time.Parse(time.RFC3339, since)
		if err != nil {
			return fault("ERR_VALIDATION", "janela de comentários inválida")
		}
		path += "&since=" + url.QueryEscape(start.Add(-time.Second).Format(time.RFC3339))
	}
	journalID := storage.Hash([]byte(plan.ID + ":" + r.ID + ":github:" + collection))
	journal := githubJournal{PlanID: plan.ID, ResourceID: r.ID, Collection: collection, Items: []json.RawMessage{}}
	if err := c.store.Get("journals", journalID, &journal); err == nil {
		if journal.PlanID != plan.ID || journal.ResourceID != r.ID || journal.Collection != collection {
			return fault("ERR_AUDIT_FAILED", "journal de comentários incompatível")
		}
	} else if !os.IsNotExist(err) {
		return fault("ERR_AUDIT_FAILED", "journal de comentários corrompido")
	}
	status, reason := "complete", ""
	if !journal.Complete && journal.Pages < 20 {
		start := path
		if journal.Cursor != "" {
			start = journal.Cursor
		}
		successful := 0
		count, err := c.collector.GitHubWalk(ctx, start, 20-journal.Pages, "", func(page []json.RawMessage, next string, responseBytes int64) error {
			successful++
			journal.Items = append(journal.Items, page...)
			journal.Pages++
			journal.Cursor = next
			journal.Complete = next == ""
			if err := c.store.Put("journals", journalID, journal); err != nil {
				return fault("ERR_AUDIT_FAILED", "falha ao persistir comentários")
			}
			if err := c.saveSourceCheckpoint(&cursor, plan.ID, next); err != nil {
				return err
			}
			run.Requests++
			run.Bytes += responseBytes
			if !c.saveRun(run) {
				return fault("ERR_AUDIT_FAILED", "falha ao auditar request")
			}
			if run.Bytes > plan.Input.Limits.MaxTransferBytes {
				return fault("ERR_RESOURCE_EXHAUSTED", "orçamento de transferência atingido")
			}
			return nil
		})
		if count > successful {
			run.Requests += int64(count - successful)
			if !c.saveRun(run) {
				return fault("ERR_AUDIT_FAILED", "falha ao auditar request")
			}
		}
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			var coded *contract.ErrorResponse
			if errors.As(err, &coded) {
				if coded.Code == "ERR_AUDIT_FAILED" || coded.Code == "ERR_RESOURCE_EXHAUSTED" || coded.Code == "ERR_RATE_LIMITED" {
					return err
				}
				if coded.Code == "ERR_FORBIDDEN" {
					status = "unauthorized"
				}
			}
			if status == "complete" {
				status = "partial"
			}
			reason = "comentários não totalmente acessíveis"
		}
	}
	if !journal.Complete && status == "complete" {
		status = "partial"
		reason = "paginação de comentários incompleta"
	} else if journal.Pages > 1 && status == "complete" {
		status = "partial"
		reason = "lista de comentários paginada sem snapshot estável"
	}
	for _, raw := range journal.Items {
		var comment struct {
			ID        int64  `json:"id"`
			CreatedAt string `json:"created_at"`
			UpdatedAt string `json:"updated_at"`
		}
		if json.Unmarshal(raw, &comment) != nil || comment.ID == 0 || comment.UpdatedAt == "" {
			status = "partial"
			reason = "comentário inválido"
			continue
		}
		if !timestampInWindow(plan.WindowStart, plan.WindowEnd, comment.UpdatedAt) && plan.WindowEnd != "" {
			continue
		}
		locator := fmt.Sprintf("%s:%s:%d", r.Name, locatorKind, comment.ID)
		e, err := c.evidence("github", locator, comment.UpdatedAt, raw)
		if err != nil {
			return err
		}
		upsertEvidence(d, e)
		if comment.CreatedAt != comment.UpdatedAt {
			eventID := "evt-" + storage.Hash([]byte(locator + ":" + e.SHA256))[:24]
			event := query.Event{ID: eventID, EntityID: locator, Type: locatorKind + "_edited_observed", EventAt: comment.UpdatedAt, ObservedAt: e.ObservedAt, EvidenceID: e.ID}
			upsertEvent(d, event)
		}
		run.Items++
		if !c.saveRun(run) {
			return fault("ERR_AUDIT_FAILED", "falha ao auditar comentário")
		}
	}
	d.Coverage = append(d.Coverage, c.coverage(r, collection, status, int64(len(journal.Items)), reason))
	if err := c.finishSourceCursor(&cursor, plan, status); err != nil {
		return err
	}
	return nil
}

func upsertEvent(d *query.Dataset, event query.Event) {
	for i := range d.Events {
		if d.Events[i].ID == event.ID {
			d.Events[i] = event
			return
		}
	}
	d.Events = append(d.Events, event)
}

func (c *Core) collectTeam(ctx context.Context, plan contract.Plan, r contract.Resource, d *query.Dataset, run *contract.Run) error {
	cursor, err := c.linearCursor(ctx, plan, r, "issues")
	if err != nil {
		return err
	}
	const fullQuery = `query TeamIssues($team: ID!, $after: String) { issues(first: 100, after: $after, includeArchived: true, orderBy: updatedAt, filter: {team: {id: {eq: $team}}}) { nodes { id identifier title description createdAt updatedAt completedAt startedAt estimate assignee { id } state { name type } project { id name } } pageInfo { hasNextPage endCursor } } }`
	const deltaQuery = `query TeamIssuesDelta($team: ID!, $after: String, $since: DateTimeOrDuration!) { issues(first: 100, after: $after, includeArchived: true, orderBy: updatedAt, filter: {team: {id: {eq: $team}}, updatedAt: {gte: $since}}) { nodes { id identifier title description createdAt updatedAt completedAt startedAt estimate assignee { id } state { name type } project { id name } } pageInfo { hasNextPage endCursor } } }`
	const gapQuery = `query TeamIssuesGap($team: ID!, $after: String, $since: DateTimeOrDuration!, $until: DateTimeOrDuration!) { issues(first: 100, after: $after, includeArchived: true, orderBy: updatedAt, filter: {team: {id: {eq: $team}}, updatedAt: {gte: $since, lt: $until}}) { nodes { id identifier title description createdAt updatedAt completedAt startedAt estimate assignee { id } state { name type } project { id name } } pageInfo { hasNextPage endCursor } } }`
	status, reason := "complete", ""
	items := []json.RawMessage{}
	spans := cursor.linearIssueSpans(plan)
	for _, span := range spans {
		gql := fullQuery
		if span.End != "" {
			gql = gapQuery
		} else if span.Start != "" {
			gql = deltaQuery
		}
		journalKey := plan.ID + ":" + r.ID + ":linear:issues"
		if len(spans) > 1 {
			journalKey += ":" + span.Start + ":" + span.End
		}
		journalID := storage.Hash([]byte(journalKey))
		journal := linearJournal{PlanID: plan.ID, ResourceID: r.ID, Items: []json.RawMessage{}}
		if err := c.store.Get("journals", journalID, &journal); err == nil {
			if journal.PlanID != plan.ID || journal.ResourceID != r.ID {
				return fault("ERR_AUDIT_FAILED", "journal Linear incompatível")
			}
		} else if !os.IsNotExist(err) {
			return fault("ERR_AUDIT_FAILED", "journal Linear corrompido")
		}
		spanStatus, spanReason := "complete", ""
		for !journal.Complete && journal.Pages < 20 {
			vars := map[string]string{"team": r.ID}
			if span.Start != "" {
				vars["since"] = span.Start
			}
			if span.End != "" {
				vars["until"] = span.End
			}
			if journal.Cursor != "" {
				vars["after"] = journal.Cursor
			}
			data, err := c.collector.LinearQuery(ctx, gql, vars)
			run.Requests++
			if !c.saveRun(run) {
				return fault("ERR_AUDIT_FAILED", "falha ao auditar request")
			}
			if err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				var coded *contract.ErrorResponse
				if errors.As(err, &coded) && coded.Code == "ERR_RATE_LIMITED" {
					return err
				}
				spanStatus, spanReason = "partial", "consulta Linear falhou"
				break
			}
			var result struct {
				Issues struct {
					Nodes    []json.RawMessage `json:"nodes"`
					PageInfo struct {
						HasNextPage bool   `json:"hasNextPage"`
						EndCursor   string `json:"endCursor"`
					} `json:"pageInfo"`
				} `json:"issues"`
			}
			if json.Unmarshal(data, &result) != nil || result.Issues.Nodes == nil {
				spanStatus, spanReason = "partial", "resposta Linear inválida"
				break
			}
			next := result.Issues.PageInfo.EndCursor
			if result.Issues.PageInfo.HasNextPage && (next == "" || next == journal.Cursor) {
				spanStatus, spanReason = "partial", "paginação Linear sem cursor válido"
				break
			}
			journal.Items = append(journal.Items, result.Issues.Nodes...)
			journal.Pages++
			journal.Complete = !result.Issues.PageInfo.HasNextPage
			journal.Cursor = next
			if err := c.store.Put("journals", journalID, journal); err != nil {
				return fault("ERR_AUDIT_FAILED", "falha ao persistir página Linear")
			}
			if err := c.saveSourceCheckpoint(&cursor, plan.ID, next); err != nil {
				return err
			}
			run.Bytes += int64(len(data))
			if !c.saveRun(run) {
				return fault("ERR_AUDIT_FAILED", "falha ao auditar página Linear")
			}
			if run.Bytes > plan.Input.Limits.MaxTransferBytes {
				return fault("ERR_RESOURCE_EXHAUSTED", "orçamento de transferência atingido")
			}
		}
		if !journal.Complete && spanStatus == "complete" {
			spanStatus, spanReason = "partial", "paginação Linear incompleta"
		} else if journal.Pages > 1 && spanStatus == "complete" {
			spanStatus, spanReason = "partial", "lista Linear paginada sem snapshot estável"
		}
		if spanStatus != "complete" && status == "complete" {
			status, reason = spanStatus, spanReason
		}
		items = append(items, journal.Items...)
	}
	projectIDs := make(map[string]bool)
	for _, raw := range items {
		var node struct {
			ID, Identifier, Title, CreatedAt, UpdatedAt, CompletedAt, StartedAt string
			Estimate                                                            *float64
			Assignee                                                            *struct{ ID string }
			State                                                               *struct{ Name, Type string }
			Project                                                             *struct{ ID, Name string }
		}
		if json.Unmarshal(raw, &node) != nil || node.ID == "" {
			status = "partial"
			reason = "item Linear inválido"
			continue
		}
		state := ""
		if node.State != nil {
			state = node.State.Type
			if state == "" {
				state = node.State.Name
			}
		}
		if !activeInWindow(plan.WindowStart, plan.WindowEnd, node.CreatedAt, node.UpdatedAt, state, plan.Input.Period.Preset != "custom") {
			continue
		}
		e, err := c.evidence("linear", node.Identifier, node.UpdatedAt, raw)
		if err != nil {
			return err
		}
		upsertEvidence(d, e)
		i := query.Issue{ID: "linear-" + node.ID, Identifier: node.Identifier, Source: "linear", Title: c.safeTitle(ctx, node.Title), Outcome: "unclassified", CreatedAt: node.CreatedAt, UpdatedAt: node.UpdatedAt, StartedAt: node.StartedAt, EstimateCurrent: node.Estimate, TeamID: r.ID, EvidenceID: e.ID, IsContext: contextOnly(plan.WindowStart, node.CreatedAt, node.UpdatedAt)}
		if node.State != nil {
			i.Outcome = linearIssueOutcome(node.State.Type, node.State.Name)
		}
		if i.Outcome == "completed" {
			i.CompletedAt = node.CompletedAt
		}
		if node.Assignee != nil {
			i.AssigneeID = node.Assignee.ID
		}
		i.State = state
		setIssueCycle(&i)
		upsertIssue(d, i)
		if node.Project != nil && node.Project.ID != "" {
			projectID := "proj-" + storage.Hash([]byte(node.Project.ID))[:24]
			projectIDs[projectID] = true
			found := false
			for _, component := range d.Components {
				if component.ID == projectID {
					found = true
					break
				}
			}
			if !found {
				d.Components = append(d.Components, contract.ComponentRecord{ID: projectID, Name: c.safeTitle(ctx, node.Project.Name), Kind: "linear_project", ImplementationStatus: "unknown", DeploymentStatus: "unknown", Coverage: "partial"})
			}
			linkID := "link-" + storage.Hash([]byte(i.ID + ":project:" + projectID + ":" + e.ID))[:24]
			d.Links = append(d.Links, contract.LinkRecord{ID: linkID, SourceID: i.ID, Relation: "belongs_to_project", TargetID: projectID, Status: "direct", EvidenceID: e.ID})
		}
		run.Items++
		if !c.saveRun(run) {
			return fault("ERR_AUDIT_FAILED", "falha ao auditar item")
		}
	}
	d.Coverage = append(d.Coverage, c.coverage(r, "issues", status, int64(len(items)), reason))
	if err := c.finishSourceCursor(&cursor, plan, status); err != nil {
		return err
	}
	if status != "complete" {
		for _, name := range []string{"history", "comments"} {
			d.Coverage = append(d.Coverage, c.coverage(r, name, "partial", 0, "coleta de issues pendente"))
		}
	} else if plan.Input.LinearEnabled {
		if err := c.collectLinearChildren(ctx, plan, r, d, run); err != nil {
			return err
		}
	} else {
		for _, name := range []string{"history", "comments"} {
			d.Coverage = append(d.Coverage, c.coverage(r, name, "not_selected", 0, "fonte Linear desabilitada no plano"))
		}
	}
	if plan.Input.LinearEnabled {
		if err := c.collectLinearProjects(ctx, plan, r, d, run); err != nil {
			return err
		}
	} else {
		d.Coverage = append(d.Coverage, c.coverage(r, "projects", "partial", int64(len(projectIDs)), "projetos associados a issues observadas; inventário indisponível sem fonte Linear"))
	}
	return nil
}

func activeInWindow(start, end, created, updated, state string, allowOpenCarry bool) bool {
	if start == "" && end == "" {
		return true
	}
	if end != "" {
		endAt, err := time.Parse(time.RFC3339, end)
		if err != nil {
			return false
		}
		if updatedAt, err := time.Parse(time.RFC3339, updated); err == nil && !updatedAt.Before(endAt) {
			return false // The observed representation is newer than the snapshot anchor.
		}
	}
	if allowOpenCarry && state != "" && !terminalState(state) {
		createdAt, err := time.Parse(time.RFC3339, created)
		if err == nil && (end == "" || timestampInWindow("", end, createdAt.Format(time.RFC3339))) {
			return true
		}
	}
	return timestampInWindow(start, end, created) || timestampInWindow(start, end, updated)
}

func timestampInWindow(start, end, value string) bool {
	at, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return false
	}
	if start != "" {
		lower, err := time.Parse(time.RFC3339, start)
		if err != nil || at.Before(lower) {
			return false
		}
	}
	if end != "" {
		upper, err := time.Parse(time.RFC3339, end)
		if err != nil || !at.Before(upper) {
			return false
		}
	}
	return true
}
func terminalState(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "closed", "done", "completed", "canceled", "cancelled", "duplicate":
		return true
	}
	return false
}

func githubIssueOutcome(state, reason string) string {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "open":
		return "active"
	case "closed":
		// Only a closed issue has a terminal outcome to classify.
	default:
		return "unclassified"
	}
	switch strings.ToLower(strings.TrimSpace(reason)) {
	case "completed", "duplicate", "not_planned":
		return strings.ToLower(strings.TrimSpace(reason))
	default:
		return "unknown"
	}
}

func githubAccountKind(accountType string) string {
	switch strings.ToLower(strings.TrimSpace(accountType)) {
	case "user":
		return "user"
	case "bot":
		return "bot"
	default:
		return ""
	}
}

func linearIssueOutcome(stateType, stateName string) string {
	kind := strings.ToLower(strings.TrimSpace(stateType))
	name := strings.ToLower(strings.TrimSpace(stateName))
	if name == "duplicate" && (kind == "canceled" || kind == "cancelled" || kind == "completed" || kind == "") {
		return "duplicate"
	}
	switch kind {
	case "completed":
		return "completed"
	case "canceled", "cancelled":
		return "cancelled"
	case "":
		switch name {
		case "completed", "done":
			return "completed"
		case "canceled", "cancelled":
			return "cancelled"
		}
	case "started", "unstarted", "backlog", "triage":
		return "active"
	}
	return "unclassified"
}

func (c *Core) classifyRetainedIssue(issue query.Issue, evidenceHash string) (query.Issue, error) {
	raw, err := c.verifiedRetainedEvidence(issue.EvidenceID, evidenceHash)
	if err != nil {
		return issue, err
	}
	switch issue.Source {
	case "github":
		var item struct {
			State       string  `json:"state"`
			StateReason string  `json:"state_reason"`
			ClosedAt    *string `json:"closed_at"`
		}
		if json.Unmarshal(raw, &item) != nil {
			return issue, fault("ERR_AUDIT_FAILED", "evidência de issue GitHub inválida")
		}
		issue.Outcome = githubIssueOutcome(item.State, item.StateReason)
		issue.CompletedAt = ""
		if issue.Outcome == "completed" && item.ClosedAt != nil {
			issue.CompletedAt = *item.ClosedAt
		}
	case "linear":
		var item struct {
			CompletedAt string                       `json:"completedAt"`
			State       *struct{ Type, Name string } `json:"state"`
		}
		if json.Unmarshal(raw, &item) != nil {
			return issue, fault("ERR_AUDIT_FAILED", "evidência de issue Linear inválida")
		}
		issue.Outcome, issue.CompletedAt = "unclassified", ""
		if item.State != nil {
			issue.Outcome = linearIssueOutcome(item.State.Type, item.State.Name)
			if issue.Outcome == "completed" {
				issue.CompletedAt = item.CompletedAt
			}
		}
	}
	setIssueCycle(&issue)
	return issue, nil
}

func setIssueCycle(issue *query.Issue) {
	issue.DurationValid, issue.CycleSeconds = false, nil
	if issue.StartedAt == "" || issue.CompletedAt == "" {
		return
	}
	start, startErr := time.Parse(time.RFC3339, issue.StartedAt)
	end, endErr := time.Parse(time.RFC3339, issue.CompletedAt)
	if startErr == nil && endErr == nil && end.After(start) {
		seconds := int64(end.Sub(start).Seconds())
		issue.DurationValid, issue.CycleSeconds = true, &seconds
	}
}
func contextOnly(start, created, updated string) bool {
	if start == "" {
		return false
	}
	cutoff, e := time.Parse(time.RFC3339, start)
	if e != nil {
		return false
	}
	a, e1 := time.Parse(time.RFC3339, created)
	b, e2 := time.Parse(time.RFC3339, updated)
	return e1 == nil && e2 == nil && a.Before(cutoff) && b.Before(cutoff)
}
func safeTitle(s string) string {
	s = strings.TrimSpace(s)
	s = potentialSecret.ReplaceAllString(s, "[REDACTED]")
	runes := []rune(s)
	if len(runes) > 500 {
		s = string(runes[:500])
	}
	return s
}

func (c *Core) safeTitle(ctx context.Context, s string) string {
	if c.collector.Secrets != nil {
		for _, source := range []string{"github", "linear", "claude"} {
			_ = c.collector.Secrets.WithSecret(ctx, source, func(secret []byte) error {
				if len(secret) >= 4 {
					s = strings.ReplaceAll(s, string(secret), "[REDACTED]")
				}
				return nil
			})
		}
	}
	return safeTitle(s)
}

var potentialSecret = regexp.MustCompile(`(?i)(gh[pousr]_[A-Za-z0-9_]{16,}|github_pat_[A-Za-z0-9_]{16,}|sk-ant-[A-Za-z0-9_-]{16,}|bearer\s+[A-Za-z0-9._-]{16,})`)

func upsertEvidence(d *query.Dataset, v contract.Evidence) {
	for _, old := range d.Evidence {
		if old.ID == v.ID {
			return
		}
	}
	d.Evidence = append(d.Evidence, v)
}
func upsertIssue(d *query.Dataset, v query.Issue) {
	for i := range d.Issues {
		if d.Issues[i].ID == v.ID {
			d.Issues[i] = v
			return
		}
	}
	d.Issues = append(d.Issues, v)
}
func upsertPR(d *query.Dataset, v query.PullRequest) {
	for i := range d.PullRequests {
		if d.PullRequests[i].ID == v.ID {
			d.PullRequests[i] = v
			return
		}
	}
	d.PullRequests = append(d.PullRequests, v)
}

func upsertComponent(d *query.Dataset, v contract.ComponentRecord) {
	for i := range d.Components {
		if d.Components[i].ID == v.ID {
			d.Components[i] = v
			return
		}
	}
	d.Components = append(d.Components, v)
}

func upsertLink(d *query.Dataset, v contract.LinkRecord) {
	for i := range d.Links {
		if d.Links[i].ID == v.ID {
			d.Links[i] = v
			return
		}
	}
	d.Links = append(d.Links, v)
}

func (c *Core) evidence(source, locator, updated string, raw json.RawMessage) (contract.Evidence, error) {
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return contract.Evidence{}, fault("ERR_AUDIT_FAILED", "evidência JSON inválida")
	}
	// Store.Put marshals json.RawMessage with HTML escaping. Hash the same
	// representation that will be read back from disk.
	raw, err := json.Marshal(json.RawMessage(compact.Bytes()))
	if err != nil {
		return contract.Evidence{}, fault("ERR_AUDIT_FAILED", "evidência JSON inválida")
	}
	hash := storage.Hash(raw)
	id := "ev-" + storage.Hash([]byte(source + ":" + locator + ":" + hash))[:24]
	var existing json.RawMessage
	err = c.store.Get("objects", id, &existing)
	if os.IsNotExist(err) {
		if err := c.store.Put("objects", id, raw); err != nil {
			return contract.Evidence{}, fault("ERR_AUDIT_FAILED", "falha ao persistir evidência")
		}
	} else if err != nil || !json.Valid(existing) || storage.Hash(existing) != hash {
		return contract.Evidence{}, fault("ERR_AUDIT_FAILED", "objeto de evidência local corrompido")
	}
	if _, err := url.Parse(locator); err != nil {
		locator = ""
	}
	return contract.Evidence{ID: id, Source: source, Locator: locator, ObservedAt: c.setup.now().UTC().Format(time.RFC3339), SourceUpdatedAt: updated, SHA256: hash, Preview: "Metadados coletados; conteúdo protegido no armazenamento local.", Redacted: true, Truncated: false}, nil
}
