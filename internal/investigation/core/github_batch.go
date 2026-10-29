package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/storage"
)

// Twenty items per GraphQL operation keeps the node count (reviews and
// filtered timeline items, 100 each) far below GitHub's per-query limits.
const githubChildBatchSize = 20

// Timeline events that projectTimeline turns into dataset events. GraphQL
// does not expose their REST numeric IDs, so the batch only identifies which
// items have one in the window; those items still read the REST timeline and
// keep their event identifiers.
const (
	githubPRTimelineTypes    = "READY_FOR_REVIEW_EVENT,REOPENED_EVENT"
	githubIssueTimelineTypes = "REOPENED_EVENT,CLOSED_EVENT"
)

type githubChildFetch struct {
	target            githubChildTarget
	reviews, timeline bool
}

// prefetchGitHubChildren reads reviews and identifies relevant timeline events
// for several pull requests and issues in one read-only GraphQL operation.
// Reviews keep their REST shape and ID (databaseId), and every result is
// committed to the journal githubChildPages reads, so a failed batch or a
// resumed run falls back to the REST path item by item.
func (c *Core) prefetchGitHubChildren(ctx context.Context, plan contract.Plan, r contract.Resource, reviews, timeline []githubChildTarget, run *contract.Run) error {
	parts := strings.SplitN(r.Name, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil
	}
	byNumber := make(map[int]*githubChildFetch)
	order := make([]int, 0, len(reviews)+len(timeline))
	add := func(target githubChildTarget, collection string) error {
		var journal githubJournal
		err := c.store.Get("journals", githubChildJournalID(plan, r, collection, target.Number), &journal)
		if err == nil {
			return nil
		}
		if !os.IsNotExist(err) {
			return fault("ERR_AUDIT_FAILED", "journal de item corrompido")
		}
		fetch := byNumber[target.Number]
		if fetch == nil {
			fetch = &githubChildFetch{target: target}
			byNumber[target.Number] = fetch
			order = append(order, target.Number)
		}
		if collection == "reviews" {
			fetch.reviews = true
		} else {
			fetch.timeline = true
		}
		return nil
	}
	for _, target := range reviews {
		if err := add(target, "reviews"); err != nil {
			return err
		}
	}
	for _, target := range timeline {
		if err := add(target, "timeline"); err != nil {
			return err
		}
	}
	if len(order) < 2 {
		return nil // the ordinary REST request is as cheap
	}
	for start := 0; start < len(order); start += githubChildBatchSize {
		end := start + githubChildBatchSize
		if end > len(order) {
			end = len(order)
		}
		batch := make([]*githubChildFetch, 0, end-start)
		for _, number := range order[start:end] {
			batch = append(batch, byNumber[number])
		}
		if err := c.githubChildBatch(ctx, plan, r, parts[0], parts[1], batch, run); err != nil {
			return err
		}
		reportProgress(ctx, run, float64(end)/float64(len(order)))
	}
	return nil
}

func (c *Core) githubChildBatch(ctx context.Context, plan contract.Plan, r contract.Resource, owner, repo string, batch []*githubChildFetch, run *contract.Run) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var gql strings.Builder
	gql.WriteString("query PocketCliChildrenBatch($owner:String!,$repo:String!")
	variables := map[string]any{"owner": owner, "repo": repo}
	for i, fetch := range batch {
		fmt.Fprintf(&gql, ",$n%d:Int!", i)
		variables[fmt.Sprintf("n%d", i)] = fetch.target.Number
	}
	gql.WriteString(") { repository(owner:$owner,name:$repo) {")
	for i, fetch := range batch {
		kind, types := "issue", githubIssueTimelineTypes
		if fetch.target.IsPR {
			kind, types = "pullRequest", githubPRTimelineTypes
		}
		fmt.Fprintf(&gql, " t%d:%s(number:$n%d) {", i, kind, i)
		if fetch.reviews {
			gql.WriteString(" reviews(first:100) { nodes { databaseId state submittedAt author { __typename ... on User { databaseId } ... on Bot { databaseId } ... on Mannequin { databaseId } } } pageInfo { hasNextPage } }")
		}
		if fetch.timeline {
			fmt.Fprintf(&gql, " timelineItems(first:100,itemTypes:[%s]) { nodes { __typename ... on ReadyForReviewEvent { createdAt } ... on ReopenedEvent { createdAt } ... on ClosedEvent { createdAt } } pageInfo { hasNextPage } }", types)
		}
		gql.WriteString(" }")
	}
	gql.WriteString(" } }")
	data, wireBytes, err := c.collector.GitHubQueryWithBytes(ctx, gql.String(), variables)
	run.Requests++
	run.Bytes += wireBytes
	if !c.saveRun(run) {
		return fault("ERR_AUDIT_FAILED", "falha ao auditar lote GraphQL")
	}
	if run.Bytes > plan.Input.Limits.MaxTransferBytes {
		return fault("ERR_RESOURCE_EXHAUSTED", "orçamento de transferência atingido")
	}
	if err != nil {
		var coded *contract.ErrorResponse
		if errors.As(err, &coded) && (coded.Code == "ERR_AUDIT_FAILED" || coded.Code == "ERR_RESOURCE_EXHAUSTED" || coded.Code == "ERR_RATE_LIMITED") {
			return err
		}
		return nil // GraphQL unavailable or one item unreadable: REST per item
	}
	var response struct {
		Repository map[string]json.RawMessage `json:"repository"`
	}
	if json.Unmarshal(data, &response) != nil || response.Repository == nil {
		return nil
	}
	for i, fetch := range batch {
		var item struct {
			Reviews *struct {
				Nodes []struct {
					DatabaseID  int64  `json:"databaseId"`
					State       string `json:"state"`
					SubmittedAt string `json:"submittedAt"`
					Author      *struct {
						Type       string `json:"__typename"`
						DatabaseID int64  `json:"databaseId"`
					} `json:"author"`
				} `json:"nodes"`
				PageInfo struct {
					HasNextPage bool `json:"hasNextPage"`
				} `json:"pageInfo"`
			} `json:"reviews"`
			Timeline *struct {
				Nodes []struct {
					CreatedAt string `json:"createdAt"`
				} `json:"nodes"`
				PageInfo struct {
					HasNextPage bool `json:"hasNextPage"`
				} `json:"pageInfo"`
			} `json:"timelineItems"`
		}
		raw := response.Repository[fmt.Sprintf("t%d", i)]
		if len(raw) == 0 || string(raw) == "null" || json.Unmarshal(raw, &item) != nil {
			continue
		}
		if fetch.reviews && item.Reviews != nil && item.Reviews.Nodes != nil && !item.Reviews.PageInfo.HasNextPage {
			items := make([]json.RawMessage, 0, len(item.Reviews.Nodes))
			for _, node := range item.Reviews.Nodes {
				// A deleted author is a "ghost" user in REST; leave those reviews
				// to REST so the actor classification does not change.
				if node.DatabaseID == 0 || node.Author == nil || node.Author.DatabaseID == 0 {
					items = nil
					break
				}
				review := map[string]any{"id": node.DatabaseID, "state": node.State, "user": map[string]any{"id": node.Author.DatabaseID, "type": node.Author.Type}}
				if node.SubmittedAt != "" {
					review["submitted_at"] = node.SubmittedAt
				}
				encoded, err := json.Marshal(review)
				if err != nil {
					items = nil
					break
				}
				items = append(items, encoded)
			}
			if items != nil {
				if err := c.putGitHubChildJournal(plan, r, "reviews", fetch.target.Number, items); err != nil {
					return err
				}
			}
		}
		// Issue dependency events are only exposed by the REST timeline. Its
		// GraphQL state-event sample cannot prove that an issue has no links.
		if fetch.timeline && fetch.target.IsPR && item.Timeline != nil && item.Timeline.Nodes != nil && !item.Timeline.PageInfo.HasNextPage {
			relevant := false
			for _, node := range item.Timeline.Nodes {
				relevant = relevant || node.CreatedAt == "" || timestampInWindow(plan.WindowStart, plan.WindowEnd, node.CreatedAt)
			}
			if !relevant {
				// Nothing in the window to project: no REST timeline read needed.
				if err := c.putGitHubChildJournal(plan, r, "timeline", fetch.target.Number, []json.RawMessage{}); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (c *Core) putGitHubChildJournal(plan contract.Plan, r contract.Resource, collection string, number int, items []json.RawMessage) error {
	journal := githubJournal{PlanID: plan.ID, ResourceID: r.ID, Collection: collection, Items: items, Pages: 1, Complete: true}
	if err := c.store.Put("journals", githubChildJournalID(plan, r, collection, number), journal); err != nil {
		return fault("ERR_AUDIT_FAILED", "falha ao persistir lote GraphQL")
	}
	return nil
}

func githubChildJournalID(plan contract.Plan, r contract.Resource, collection string, number int) string {
	return storage.Hash([]byte(fmt.Sprintf("%s:%s:%s:%d", plan.ID, r.ID, collection, number)))
}
