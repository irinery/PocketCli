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

// GitHub has no useful updated-since filter on these endpoints. Bound each
// reconciliation run and make the unvisited portion explicit in coverage.
const githubChildTargetLimit = 100

type githubChildTarget struct {
	Number, Index int
	EntityID      string
	IsPR          bool
}

type githubReconcileCursor struct {
	LastEntityID string `json:"last_entity_id"`
}

func (c *Core) collectGitHubChildren(ctx context.Context, plan contract.Plan, r contract.Resource, d *query.Dataset, run *contract.Run) error {
	prs := make([]githubChildTarget, 0)
	timeline := make([]githubChildTarget, 0)
	for i, p := range d.PullRequests {
		if p.Repository == r.Name && p.Number > 0 {
			target := githubChildTarget{Number: p.Number, Index: i, EntityID: p.ID, IsPR: true}
			prs = append(prs, target)
			timeline = append(timeline, target)
		}
	}
	for i, issue := range d.Issues {
		if issue.Source != "github" || !strings.HasPrefix(issue.Identifier, r.Name+"#") {
			continue
		}
		var n int
		if _, err := fmt.Sscanf(issue.Identifier, r.Name+"#%d", &n); err == nil && n > 0 {
			timeline = append(timeline, githubChildTarget{Number: n, Index: i, EntityID: issue.ID})
		}
	}
	type childPlan struct {
		name           string
		targets        []githubChildTarget
		status, reason string
		cursorID       string
	}
	plans := []*childPlan{{name: "reviews", targets: prs}, {name: "timeline", targets: timeline}}
	for _, collection := range plans {
		targets := collection.targets
		sort.Slice(targets, func(i, j int) bool { return targets[i].EntityID < targets[j].EntityID })
		collection.status = "complete"
		if len(targets) > githubChildTargetLimit {
			collection.status, collection.reason = "partial", fmt.Sprintf("fila de reconciliação: %d itens pendentes nesta rodada", len(targets)-githubChildTargetLimit)
			scopeID := plan.InvestigationID
			if scopeID == "" {
				scopeID = plan.ID
			}
			auth, err := c.currentAuth(ctx, plan.Input)
			if err != nil {
				return err
			}
			collection.cursorID = storage.Hash([]byte(scopeID + ":" + r.ID + ":" + collection.name + ":" + auth.GitHub))
			var cursor githubReconcileCursor
			if err := c.store.Get("reconcile", collection.cursorID, &cursor); err != nil && !os.IsNotExist(err) {
				return fault("ERR_AUDIT_FAILED", "cursor de reconciliação corrompido")
			}
			start := sort.Search(len(targets), func(i int) bool { return targets[i].EntityID > cursor.LastEntityID })
			if start >= len(targets) {
				start = 0
			}
			ordered := append([]githubChildTarget{}, targets[start:]...)
			targets = append(ordered, targets[:start]...)
			targets = targets[:githubChildTargetLimit]
		}
		collection.targets = targets
	}
	if err := c.prefetchGitHubChildren(withProgressRange(ctx, 0, 0.2), plan, r, plans[0].targets, plans[1].targets, run); err != nil {
		return err
	}
	total := len(plans[0].targets) + len(plans[1].targets)
	done := 0
	for _, collection := range plans {
		targets := collection.targets
		status, reason := collection.status, collection.reason
		cursorID := collection.cursorID
		var observed int64
		lastComplete := ""
		for _, target := range targets {
			if err := ctx.Err(); err != nil {
				return err
			}
			done++
			reportProgress(ctx, run, 0.2+0.8*float64(done)/float64(total))
			path := fmt.Sprintf("/repos/%s/issues/%d/timeline?per_page=100", r.Name, target.Number)
			if collection.name == "reviews" {
				path = fmt.Sprintf("/repos/%s/pulls/%d/reviews?per_page=100", r.Name, target.Number)
			}
			items, complete, err := c.githubChildPages(ctx, plan, r, collection.name, target.Number, path, run)
			if err != nil {
				var coded *contract.ErrorResponse
				if errors.As(err, &coded) && (coded.Code == "ERR_AUDIT_FAILED" || coded.Code == "ERR_RESOURCE_EXHAUSTED" || coded.Code == "ERR_RATE_LIMITED") {
					return err
				}
				status, reason = "partial", "revisões ou timeline não totalmente acessíveis"
			}
			if !complete {
				status, reason = "partial", "paginação de revisões ou timeline incompleta"
			} else {
				lastComplete = target.EntityID
			}
			observed += int64(len(items))
			for _, raw := range items {
				if collection.name == "reviews" {
					if err := c.projectReview(plan, r, target, raw, d, run); err != nil {
						status, reason = "partial", "revisão inválida ou evidência indisponível"
						var coded *contract.ErrorResponse
						if errors.As(err, &coded) && coded.Code == "ERR_AUDIT_FAILED" {
							return err
						}
					}
				} else if err := c.projectTimeline(plan, r, target, raw, d, run); err != nil {
					status, reason = "partial", "evento inválido ou evidência indisponível"
					var coded *contract.ErrorResponse
					if errors.As(err, &coded) && coded.Code == "ERR_AUDIT_FAILED" {
						return err
					}
				}
			}
		}
		if cursorID != "" && lastComplete != "" {
			if err := c.store.Put("reconcile", cursorID, githubReconcileCursor{LastEntityID: lastComplete}); err != nil {
				return fault("ERR_AUDIT_FAILED", "falha ao persistir cursor de reconciliação")
			}
		}
		d.Coverage = append(d.Coverage, c.coverage(r, collection.name, status, observed, reason))
	}
	for i := range d.PullRequests {
		p := &d.PullRequests[i]
		if p.Repository != r.Name {
			continue
		}
		for _, event := range d.Events {
			if event.EntityID != p.ID || event.Type != "human_reviewed" || (p.ReadyAt != "" && event.EventAt < p.ReadyAt) {
				continue
			}
			if p.FirstHumanReviewAt == "" || event.EventAt < p.FirstHumanReviewAt || (p.ReadyAt != "" && p.FirstHumanReviewAt < p.ReadyAt) {
				p.FirstHumanReviewAt = event.EventAt
			}
		}
		if p.ReadyAt == "" || p.FirstHumanReviewAt == "" {
			continue
		}
		start, e1 := time.Parse(time.RFC3339, p.ReadyAt)
		end, e2 := time.Parse(time.RFC3339, p.FirstHumanReviewAt)
		if e1 == nil && e2 == nil && !end.Before(start) {
			wait := int64(end.Sub(start).Seconds())
			p.ReviewWaitSeconds = &wait
		}
	}
	return nil
}

func (c *Core) githubChildPages(ctx context.Context, plan contract.Plan, r contract.Resource, collection string, number int, path string, run *contract.Run) ([]json.RawMessage, bool, error) {
	return c.githubChildPagesField(ctx, plan, r, collection, number, path, "", run)
}

func (c *Core) githubChildPagesField(ctx context.Context, plan contract.Plan, r contract.Resource, collection string, number int, path, field string, run *contract.Run) ([]json.RawMessage, bool, error) {
	journalID := githubChildJournalID(plan, r, collection, number)
	journal := githubJournal{PlanID: plan.ID, ResourceID: r.ID, Collection: collection, Items: []json.RawMessage{}}
	if err := c.store.Get("journals", journalID, &journal); err == nil {
		if journal.PlanID != plan.ID || journal.ResourceID != r.ID || journal.Collection != collection {
			return nil, false, fault("ERR_AUDIT_FAILED", "journal de item incompatível")
		}
	} else if !os.IsNotExist(err) {
		return nil, false, fault("ERR_AUDIT_FAILED", "journal de item corrompido")
	}
	if journal.Complete {
		return journal.Items, true, nil
	}
	if journal.Cursor != "" {
		path = journal.Cursor
	}
	successful := 0
	commitPage := func(page []json.RawMessage, next string, responseBytes int64) error {
		successful++
		journal.Items = append(journal.Items, page...)
		journal.Pages++
		journal.Cursor = next
		journal.Complete = next == ""
		if e := c.store.Put("journals", journalID, journal); e != nil {
			return fault("ERR_AUDIT_FAILED", "falha ao persistir página de item")
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
	}
	var count int
	var err error
	if field == "" {
		count, err = c.collector.GitHubWalk(ctx, path, 10-journal.Pages, "", commitPage)
	} else {
		count, err = c.collector.GitHubWalkField(ctx, path, 10-journal.Pages, field, commitPage)
	}
	if count > successful {
		run.Requests += int64(count - successful)
		if !c.saveRun(run) {
			return nil, false, fault("ERR_AUDIT_FAILED", "falha ao auditar request")
		}
	}
	if err == nil && !journal.Complete {
		err = fault("ERR_RESOURCE_EXHAUSTED", "paginação de item excedeu limite")
	}
	return journal.Items, journal.Complete, err
}

func (c *Core) projectReview(plan contract.Plan, r contract.Resource, target githubChildTarget, raw json.RawMessage, d *query.Dataset, run *contract.Run) error {
	var review struct {
		ID          int64  `json:"id"`
		SubmittedAt string `json:"submitted_at"`
		State       string `json:"state"`
		User        struct {
			ID   int64  `json:"id"`
			Type string `json:"type"`
		} `json:"user"`
	}
	if json.Unmarshal(raw, &review) != nil || review.ID == 0 {
		return fmt.Errorf("revisão inválida")
	}
	if review.SubmittedAt == "" || !timestampInWindow(plan.WindowStart, plan.WindowEnd, review.SubmittedAt) {
		return nil
	}
	locator := fmt.Sprintf("%s:review:%d", r.Name, review.ID)
	e, err := c.evidence("github", locator, review.SubmittedAt, raw)
	if err != nil {
		return err
	}
	upsertEvidence(d, e)
	actor, actorKind := "", ""
	if review.User.ID > 0 {
		actor = fmt.Sprintf("github-%d", review.User.ID)
		actorKind = githubAccountKind(review.User.Type)
	}
	p := &d.PullRequests[target.Index]
	eventType := githubReviewEventType(actor, actorKind, p.AuthorID, review.State)
	upsertEvent(d, query.Event{ID: "evt-" + storage.Hash([]byte(locator))[:24], EntityID: target.EntityID, Type: eventType, EventAt: review.SubmittedAt, ObservedAt: e.ObservedAt, ActorID: actor, ActorKind: actorKind, NewValue: review.State, EvidenceID: e.ID})
	run.Items++
	if !c.saveRun(run) {
		return fault("ERR_AUDIT_FAILED", "falha ao auditar revisão")
	}
	return nil
}

func githubReviewEventType(actor, actorKind, author, state string) string {
	if actor == "" || actor == author || strings.EqualFold(state, "PENDING") || state == "" {
		return "reviewed"
	}
	switch actorKind {
	case "user":
		return "human_reviewed"
	case "bot":
		return "bot_reviewed"
	default:
		return "reviewed_unknown"
	}
}

func (c *Core) projectTimeline(plan contract.Plan, r contract.Resource, target githubChildTarget, raw json.RawMessage, d *query.Dataset, run *contract.Run) error {
	var entry struct {
		ID          int64  `json:"id"`
		Event       string `json:"event"`
		CreatedAt   string `json:"created_at"`
		StateReason string `json:"state_reason"`
		Actor       struct {
			ID   int64  `json:"id"`
			Type string `json:"type"`
		} `json:"actor"`
	}
	if json.Unmarshal(raw, &entry) != nil {
		return fmt.Errorf("evento inválido")
	}
	if entry.Event != "ready_for_review" && entry.Event != "reopened" && !(entry.Event == "closed" && !target.IsPR) {
		return nil
	}
	if entry.CreatedAt == "" {
		return fmt.Errorf("evento sem data")
	}
	if !timestampInWindow(plan.WindowStart, plan.WindowEnd, entry.CreatedAt) {
		return nil
	}
	if entry.ID == 0 {
		return fmt.Errorf("evento sem ID")
	}
	locator := fmt.Sprintf("%s:timeline:%d", r.Name, entry.ID)
	e, err := c.evidence("github", locator, entry.CreatedAt, raw)
	if err != nil {
		return err
	}
	upsertEvidence(d, e)
	actor := ""
	actorKind := ""
	if entry.Actor.ID > 0 {
		actor = fmt.Sprintf("github-%d", entry.Actor.ID)
		actorKind = githubAccountKind(entry.Actor.Type)
	}
	event := query.Event{ID: "evt-" + storage.Hash([]byte(locator))[:24], EntityID: target.EntityID, Type: entry.Event, EventAt: entry.CreatedAt, ObservedAt: e.ObservedAt, ActorID: actor, ActorKind: actorKind, EvidenceID: e.ID}
	if entry.Event == "closed" {
		event.Type, event.OldValue, event.NewValue = "state_changed", "open", "closed_unclassified"
		if entry.StateReason == "completed" {
			event.NewValue = "completed"
		} else if target.Index >= 0 && target.Index < len(d.Issues) {
			issue := d.Issues[target.Index]
			if issue.CompletedAt != "" && issue.CompletedAt == entry.CreatedAt {
				event.NewValue = "completed"
			}
		}
	}
	upsertEvent(d, event)
	if target.IsPR && entry.Event == "ready_for_review" {
		p := &d.PullRequests[target.Index]
		if p.ReadyAt == "" || entry.CreatedAt < p.ReadyAt {
			p.ReadyAt = entry.CreatedAt
		}
	}
	run.Items++
	if !c.saveRun(run) {
		return fault("ERR_AUDIT_FAILED", "falha ao auditar evento")
	}
	return nil
}
