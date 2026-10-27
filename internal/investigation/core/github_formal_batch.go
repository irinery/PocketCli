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

// Fetch the first timeline page for several issues in one read-only GraphQL
// operation. Every issue has its own journal, so a resumed run can continue
// individual timelines without repeating a committed batch.
func (c *Core) prefetchGitHubFormalPages(ctx context.Context, plan contract.Plan, r contract.Resource, targets []githubChildTarget, owner, repo string, run *contract.Run) (map[int]error, error) {
	result := make(map[int]error)
	fresh := make([]githubChildTarget, 0, len(targets))
	for _, target := range targets {
		journalID := storage.Hash([]byte(fmt.Sprintf("%s:%s:formal_links:%d", plan.ID, r.ID, target.Number)))
		var journal githubJournal
		if err := c.store.Get("journals", journalID, &journal); err == nil {
			if journal.PlanID != plan.ID || journal.ResourceID != r.ID || journal.Collection != "formal_links" {
				return nil, fault("ERR_AUDIT_FAILED", "journal de vínculo incompatível")
			}
			if journal.Pages > 0 || journal.Complete {
				continue
			}
		} else if !os.IsNotExist(err) {
			return nil, fault("ERR_AUDIT_FAILED", "journal de vínculo corrompido")
		}
		fresh = append(fresh, target)
	}
	if len(fresh) < 2 {
		return result, nil // the ordinary single-issue request is cheaper
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var query strings.Builder
	query.WriteString("query PocketCliFormalLinksBatch($owner:String!,$repo:String!,$after:String")
	variables := map[string]any{"owner": owner, "repo": repo}
	for i, target := range fresh {
		fmt.Fprintf(&query, ",$n%d:Int!", i)
		variables[fmt.Sprintf("n%d", i)] = target.Number
	}
	query.WriteString(") { repository(owner:$owner,name:$repo) {")
	for i := range fresh {
		fmt.Fprintf(&query, "i%d:issue(number:$n%d) { %s }", i, i, githubFormalTimelineSelection)
	}
	query.WriteString("} }")
	data, wireBytes, err := c.collector.GitHubQueryWithBytes(ctx, query.String(), variables)
	run.Requests++
	run.Bytes += wireBytes
	if !c.saveRun(run) {
		return nil, fault("ERR_AUDIT_FAILED", "falha ao auditar request GraphQL")
	}
	if run.Bytes > plan.Input.Limits.MaxTransferBytes {
		return nil, fault("ERR_RESOURCE_EXHAUSTED", "orçamento de transferência atingido")
	}
	if err != nil {
		var coded *contract.ErrorResponse
		if errors.As(err, &coded) && (coded.Code == "ERR_AUDIT_FAILED" || coded.Code == "ERR_RESOURCE_EXHAUSTED" || coded.Code == "ERR_RATE_LIMITED") {
			return nil, err
		}
		for _, target := range fresh {
			result[target.Number] = err
		}
		return result, nil
	}
	var response struct {
		Repository map[string]json.RawMessage `json:"repository"`
	}
	if json.Unmarshal(data, &response) != nil || response.Repository == nil {
		for _, target := range fresh {
			result[target.Number] = fmt.Errorf("repositório GraphQL indisponível")
		}
		return result, nil
	}
	for i, target := range fresh {
		var issue struct {
			TimelineItems *struct {
				Nodes    []json.RawMessage `json:"nodes"`
				PageInfo *struct {
					HasNextPage bool   `json:"hasNextPage"`
					EndCursor   string `json:"endCursor"`
				} `json:"pageInfo"`
			} `json:"timelineItems"`
		}
		if raw := response.Repository[fmt.Sprintf("i%d", i)]; len(raw) == 0 || json.Unmarshal(raw, &issue) != nil || issue.TimelineItems == nil || issue.TimelineItems.PageInfo == nil || issue.TimelineItems.Nodes == nil || (issue.TimelineItems.PageInfo.HasNextPage && issue.TimelineItems.PageInfo.EndCursor == "") {
			result[target.Number] = fmt.Errorf("timeline GraphQL indisponível")
			continue
		}
		journal := githubJournal{PlanID: plan.ID, ResourceID: r.ID, Collection: "formal_links", Items: issue.TimelineItems.Nodes, Pages: 1, Complete: !issue.TimelineItems.PageInfo.HasNextPage, Cursor: issue.TimelineItems.PageInfo.EndCursor}
		journalID := storage.Hash([]byte(fmt.Sprintf("%s:%s:formal_links:%d", plan.ID, r.ID, target.Number)))
		if err := c.store.Put("journals", journalID, journal); err != nil {
			return nil, fault("ERR_AUDIT_FAILED", "falha ao persistir página GraphQL")
		}
	}
	return result, nil
}
