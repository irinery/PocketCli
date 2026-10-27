package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/query"
	"pocketcli/internal/investigation/storage"
)

// collectPRCommits reads the PR's explicit commit list. Membership is an
// observation at collection time, not a claim that a commit was on the PR at
// its authored date. A removed membership is closed only after a full,
// head-consistent list has been received.
func (c *Core) collectPRCommits(ctx context.Context, plan contract.Plan, r contract.Resource, d *query.Dataset, run *contract.Run) error {
	prs := make([]query.PullRequest, 0)
	for _, pr := range d.PullRequests {
		if pr.Repository == r.Name && pr.Number > 0 {
			prs = append(prs, pr)
		}
	}
	sort.Slice(prs, func(i, j int) bool { return prs[i].ID < prs[j].ID })
	selected := prs
	status, reason := "complete", "listas de commits dos PRs selecionados observadas"
	cursorID := ""
	if len(prs) > githubChildTargetLimit {
		status, reason = "partial", fmt.Sprintf("fila de commits de PR: %d PRs pendentes", len(prs)-githubChildTargetLimit)
		var auth planAuth
		if err := c.store.Get("planauth", plan.ID, &auth); err != nil || auth.GitHub == "" {
			return fault("ERR_AUDIT_FAILED", "identidade GitHub indisponível")
		}
		cursorID = storage.Hash([]byte(plan.InvestigationID + ":" + r.ID + ":pr_commits:" + auth.GitHub))
		var cursor githubReconcileCursor
		if err := c.store.Get("reconcile", cursorID, &cursor); err != nil && !os.IsNotExist(err) {
			return fault("ERR_AUDIT_FAILED", "cursor de commits de PR corrompido")
		}
		start := sort.Search(len(prs), func(i int) bool { return prs[i].ID > cursor.LastEntityID })
		if start >= len(prs) {
			start = 0
		}
		selected = append(append([]query.PullRequest{}, prs[start:]...), prs[:start]...)
		selected = selected[:githubChildTargetLimit]
	}
	var observed int64
	lastVisited := ""
	for _, pr := range selected {
		if err := ctx.Err(); err != nil {
			return err
		}
		endpoint := fmt.Sprintf("/repos/%s/pulls/%d/commits?per_page=100", r.Name, pr.Number)
		items, complete, fetchErr := c.githubChildPages(ctx, plan, r, "pr_commits", pr.Number, endpoint, run)
		if fetchErr != nil {
			var coded *contract.ErrorResponse
			if errors.As(fetchErr, &coded) && (coded.Code == "ERR_AUDIT_FAILED" || (coded.Code == "ERR_RESOURCE_EXHAUSTED" && run.Bytes > plan.Input.Limits.MaxTransferBytes) || coded.Code == "ERR_RATE_LIMITED") {
				return fetchErr
			}
			status, reason = "partial", "commits de um ou mais PRs indisponíveis"
		}
		stable := complete && len(items) > 0 && len(items) <= 100 && validGitOID(pr.HeadSHA)
		commits := make(map[string]json.RawMessage, len(items))
		lastSHA := ""
		for _, raw := range items {
			var item struct {
				SHA string `json:"sha"`
			}
			if json.Unmarshal(raw, &item) != nil || !validGitOID(item.SHA) {
				stable = false
				continue
			}
			commits[item.SHA] = raw
			lastSHA = item.SHA
		}
		if lastSHA != pr.HeadSHA || len(commits) != len(items) {
			stable = false
		}
		if !stable {
			status, reason = "partial", "lista paginada, incompleta, inválida ou head do PR mudou durante a observação"
		}
		observed += int64(len(commits))
		if err := c.projectPRCommitMemberships(plan, r, pr, commits, stable, d, run); err != nil {
			return err
		}
		lastVisited = pr.ID
	}
	if cursorID != "" && lastVisited != "" {
		if err := c.store.Put("reconcile", cursorID, githubReconcileCursor{LastEntityID: lastVisited}); err != nil {
			return fault("ERR_AUDIT_FAILED", "falha ao persistir cursor de commits de PR")
		}
	}
	if !c.saveRun(run) {
		return fault("ERR_AUDIT_FAILED", "falha ao auditar commits de PR")
	}
	d.Coverage = append(d.Coverage, c.coverage(r, "pr_commits", status, observed, reason))
	return nil
}

func (c *Core) projectPRCommitMemberships(plan contract.Plan, r contract.Resource, pr query.PullRequest, commits map[string]json.RawMessage, stable bool, d *query.Dataset, run *contract.Run) error {
	observedAt := c.setup.now().UTC().Format("2006-01-02T15:04:05Z")
	events := make(map[string]query.Event, len(d.Events))
	for _, event := range d.Events {
		if event.Type == "pr_commit_observed" && event.EntityID == pr.ID {
			events[event.ID] = event
		}
	}
	active := make(map[string]contract.LinkRecord)
	for _, link := range d.Links {
		if link.SourceID != pr.ID || link.Relation != "includes_commit" || link.ValidTo != "" {
			continue
		}
		event, ok := events[link.TargetID]
		if !ok || !validGitOID(event.NewValue) {
			return fault("ERR_AUDIT_FAILED", "vínculo de commit de PR sem evento válido")
		}
		active[event.NewValue] = link
	}
	shas := make([]string, 0, len(commits))
	for sha := range commits {
		shas = append(shas, sha)
	}
	sort.Strings(shas)
	for _, sha := range shas {
		if _, ok := active[sha]; ok {
			continue
		}
		evidence, err := c.evidence("github", fmt.Sprintf("%s:pr:%d:commit:%s", r.Name, pr.Number, sha), observedAt, commits[sha])
		if err != nil {
			return err
		}
		upsertEvidence(d, evidence)
		eventID := "evt-" + storage.Hash([]byte(pr.ID + ":commit:" + sha + ":" + plan.ID))[:24]
		upsertEvent(d, query.Event{ID: eventID, EntityID: pr.ID, Type: "pr_commit_observed", EventAt: observedAt, ObservedAt: observedAt, NewValue: sha, EvidenceID: evidence.ID})
		linkID := "lnk-" + storage.Hash([]byte(pr.ID + ":includes_commit:" + eventID))[:24]
		upsertLink(d, contract.LinkRecord{ID: linkID, SourceID: pr.ID, Relation: "includes_commit", TargetID: eventID, Status: "direct", EvidenceID: evidence.ID, ValidFrom: observedAt})
		run.Items++
	}
	if !stable {
		return nil
	}
	for sha, link := range active {
		if _, ok := commits[sha]; ok {
			continue
		}
		link.ValidTo = observedAt
		upsertLink(d, link)
		summary, err := json.Marshal(struct {
			Repository string   `json:"repository"`
			PR         int      `json:"pr"`
			HeadSHA    string   `json:"head_sha"`
			CommitSHAs []string `json:"commit_shas"`
			ObservedAt string   `json:"observed_at"`
		}{r.Name, pr.Number, pr.HeadSHA, shas, observedAt})
		if err != nil {
			return fault("ERR_AUDIT_FAILED", "resumo de commits de PR inválido")
		}
		evidence, err := c.evidence("github", fmt.Sprintf("%s:pr:%d:commits", r.Name, pr.Number), observedAt, summary)
		if err != nil {
			return err
		}
		upsertEvidence(d, evidence)
		removedID := "evt-" + storage.Hash([]byte(link.ID + ":removed:" + plan.ID))[:24]
		upsertEvent(d, query.Event{ID: removedID, EntityID: pr.ID, Type: "pr_commit_removed_observed", EventAt: observedAt, ObservedAt: observedAt, OldValue: sha, EvidenceID: evidence.ID})
		run.Items++
	}
	return nil
}
