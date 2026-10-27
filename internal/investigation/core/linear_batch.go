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

// Linear scores a query by its nested page sizes. Ten issues with five child
// connections of 25 nodes each stays well below the per-request complexity
// limit while replacing up to 50 single-issue requests with one.
const (
	linearChildBatchSize = 10
	linearChildBatchPage = 25
)

var linearChildSelections = map[string]struct{ args, nodes string }{
	"history":          {", includeArchived: true", linearHistoryNodes},
	"comments":         {", includeArchived: true", linearCommentNodes},
	"relations":        {", includeArchived: false", linearRelationNodes},
	"inverseRelations": {", includeArchived: false", linearRelationNodes},
	"attachments":      {"", linearAttachmentNode},
}

type linearChildFetch struct {
	issue       query.Issue
	collections []string
}

// prefetchLinearChildren reads the first page of every wanted child
// collection for several issues in one read-only operation. Each page is
// committed to the same journal linearChildPages uses, so later pages, a
// failed batch and a resumed run all continue through the single-issue path.
func (c *Core) prefetchLinearChildren(ctx context.Context, plan contract.Plan, r contract.Resource, targets []query.Issue, wanted map[string][]string, run *contract.Run) error {
	fetches := make([]linearChildFetch, 0, len(targets))
	pending := 0
	for _, issue := range targets {
		fetch := linearChildFetch{issue: issue}
		for _, collection := range wanted[issue.ID] {
			var journal linearJournal
			err := c.store.Get("journals", linearChildJournalID(plan, r, collection, issue), &journal)
			if err == nil {
				continue
			}
			if !os.IsNotExist(err) {
				return fault("ERR_AUDIT_FAILED", "journal Linear corrompido")
			}
			fetch.collections = append(fetch.collections, collection)
		}
		if len(fetch.collections) > 0 {
			fetches = append(fetches, fetch)
			pending += len(fetch.collections)
		}
	}
	if pending < 2 {
		return nil // the ordinary single-issue request is as cheap
	}
	for start := 0; start < len(fetches); start += linearChildBatchSize {
		end := start + linearChildBatchSize
		if end > len(fetches) {
			end = len(fetches)
		}
		if err := c.linearChildBatch(ctx, plan, r, fetches[start:end], run); err != nil {
			return err
		}
		reportProgress(ctx, run, float64(end)/float64(len(fetches)))
	}
	return nil
}

func (c *Core) linearChildBatch(ctx context.Context, plan contract.Plan, r contract.Resource, batch []linearChildFetch, run *contract.Run) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var gql strings.Builder
	gql.WriteString("query IssueChildrenBatch(")
	vars := make(map[string]string, len(batch))
	for i, fetch := range batch {
		if i > 0 {
			gql.WriteString(", ")
		}
		fmt.Fprintf(&gql, "$i%d: String!", i)
		vars[fmt.Sprintf("i%d", i)] = strings.TrimPrefix(fetch.issue.ID, "linear-")
	}
	gql.WriteString(") {")
	for i, fetch := range batch {
		fmt.Fprintf(&gql, " i%d: issue(id: $i%d) { id", i, i)
		for _, collection := range fetch.collections {
			selection := linearChildSelections[collection]
			fmt.Fprintf(&gql, " %s(first: %d%s) { nodes { %s } pageInfo { hasNextPage endCursor } }", collection, linearChildBatchPage, selection.args, selection.nodes)
		}
		gql.WriteString(" }")
	}
	gql.WriteString(" }")
	data, err := c.collector.LinearQuery(ctx, gql.String(), vars)
	run.Requests++
	run.Bytes += int64(len(data))
	if !c.saveRun(run) {
		return fault("ERR_AUDIT_FAILED", "falha ao auditar lote Linear")
	}
	if run.Bytes > plan.Input.Limits.MaxTransferBytes {
		return fault("ERR_RESOURCE_EXHAUSTED", "orçamento de transferência atingido")
	}
	if err != nil {
		var coded *contract.ErrorResponse
		if errors.As(err, &coded) && (coded.Code == "ERR_AUDIT_FAILED" || coded.Code == "ERR_RESOURCE_EXHAUSTED" || coded.Code == "ERR_RATE_LIMITED") {
			return err
		}
		return nil // e.g. one inaccessible issue: each issue is retried on its own
	}
	var response map[string]map[string]json.RawMessage
	if json.Unmarshal(data, &response) != nil {
		return nil
	}
	for i, fetch := range batch {
		issue := response[fmt.Sprintf("i%d", i)]
		var returnedID string
		if issue == nil || json.Unmarshal(issue["id"], &returnedID) != nil || returnedID != vars[fmt.Sprintf("i%d", i)] {
			continue
		}
		for _, collection := range fetch.collections {
			var page struct {
				Nodes    []json.RawMessage `json:"nodes"`
				PageInfo struct {
					HasNextPage bool   `json:"hasNextPage"`
					EndCursor   string `json:"endCursor"`
				} `json:"pageInfo"`
			}
			if json.Unmarshal(issue[collection], &page) != nil || page.Nodes == nil || (page.PageInfo.HasNextPage && page.PageInfo.EndCursor == "") {
				continue
			}
			journal := linearJournal{PlanID: plan.ID, ResourceID: r.ID, Items: page.Nodes, Pages: 1, Cursor: page.PageInfo.EndCursor, Complete: !page.PageInfo.HasNextPage}
			if err := c.store.Put("journals", linearChildJournalID(plan, r, collection, fetch.issue), journal); err != nil {
				return fault("ERR_AUDIT_FAILED", "falha ao persistir lote Linear")
			}
		}
	}
	return nil
}

func linearChildJournalID(plan contract.Plan, r contract.Resource, collection string, issue query.Issue) string {
	return storage.Hash([]byte(plan.ID + ":" + r.ID + ":linear:" + collection + ":" + issue.ID))
}
