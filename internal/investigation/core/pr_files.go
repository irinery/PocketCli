package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"sort"
	"strings"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/query"
	"pocketcli/internal/investigation/storage"
)

func (c *Core) collectPRFiles(ctx context.Context, plan contract.Plan, r contract.Resource, d *query.Dataset, run *contract.Run) error {
	prs := make([]query.PullRequest, 0)
	for _, pr := range d.PullRequests {
		if pr.Repository == r.Name && pr.Number > 0 {
			prs = append(prs, pr)
		}
	}
	sort.Slice(prs, func(i, j int) bool { return prs[i].ID < prs[j].ID })
	selected := prs
	status, reason := "complete", "arquivos alterados diretamente declarados pela API do PR; vínculo com componente temporal ainda parcial"
	cursorID := ""
	if len(prs) > 100 {
		status, reason = "partial", fmt.Sprintf("fila de arquivos de PR: %d PRs pendentes", len(prs)-100)
		var auth planAuth
		if err := c.store.Get("planauth", plan.ID, &auth); err != nil {
			return fault("ERR_AUDIT_FAILED", "identidade GitHub indisponível")
		}
		cursorID = storage.Hash([]byte(plan.InvestigationID + ":" + r.ID + ":pr_files:" + auth.GitHub))
		var cursor githubReconcileCursor
		if err := c.store.Get("reconcile", cursorID, &cursor); err != nil && !os.IsNotExist(err) {
			return fault("ERR_AUDIT_FAILED", "cursor de arquivos de PR corrompido")
		}
		start := sort.Search(len(prs), func(i int) bool { return prs[i].ID > cursor.LastEntityID })
		if start >= len(prs) {
			start = 0
		}
		selected = append(append([]query.PullRequest{}, prs[start:]...), prs[:start]...)
		selected = selected[:100]
	}
	var observed int64
	lastComplete := ""
	for _, pr := range selected {
		if err := ctx.Err(); err != nil {
			return err
		}
		endpoint := fmt.Sprintf("/repos/%s/pulls/%d/files?per_page=100", r.Name, pr.Number)
		items, complete, fetchErr := c.githubChildPages(ctx, plan, r, "pr_files", pr.Number, endpoint, run)
		if fetchErr != nil {
			var coded *contract.ErrorResponse
			if errors.As(fetchErr, &coded) && (coded.Code == "ERR_AUDIT_FAILED" || (coded.Code == "ERR_RESOURCE_EXHAUSTED" && run.Bytes > plan.Input.Limits.MaxTransferBytes) || coded.Code == "ERR_RATE_LIMITED") {
				return fetchErr
			}
			status, reason = "partial", "arquivos de um ou mais PRs indisponíveis"
		}
		if !complete {
			status, reason = "partial", "paginação de arquivos de PR incompleta"
		} else {
			lastComplete = pr.ID
		}
		observed += int64(len(items))
		for _, raw := range items {
			var item struct {
				Filename         string `json:"filename"`
				PreviousFilename string `json:"previous_filename"`
				Status           string `json:"status"`
				SHA              string `json:"sha"`
			}
			if json.Unmarshal(raw, &item) != nil || !validGitPath(item.Filename) {
				status, reason = "partial", "caminho de arquivo de PR inválido"
				continue
			}
			locator := fmt.Sprintf("%s:pr:%d:file:%s", r.Name, pr.Number, item.Filename)
			// The file entry has no updated_at of its own. PR.updated_at can
			// change because of unrelated activity on the pull request.
			e, err := c.evidence("github", locator, "", raw)
			if err != nil {
				return err
			}
			upsertEvidence(d, e)
			version := pr.HeadSHA
			if version == "" {
				version = pr.ID
			}
			fileID := prFileComponentID(r.ID, version, item.Filename)
			upsertComponent(d, contract.ComponentRecord{ID: fileID, Name: item.Filename, Repository: r.Name, Path: item.Filename, Kind: "file_change", ImplementationStatus: "identified", DeploymentStatus: "unknown", Coverage: "partial"})
			linkID := "lnk-" + storage.Hash([]byte(pr.ID + ":modifies_file:" + fileID + ":" + e.ID))[:24]
			upsertLink(d, contract.LinkRecord{ID: linkID, SourceID: pr.ID, Relation: "modifies_file", TargetID: fileID, Status: "direct", EvidenceID: e.ID})
			if item.Status == "renamed" && validGitPath(item.PreviousFilename) {
				// The PR files API reports the rename but not its time. PR.updated_at
				// can reflect unrelated activity, so only the observation is dated.
				upsertEvent(d, query.Event{ID: "evt-" + storage.Hash([]byte(linkID + ":rename"))[:24], EntityID: fileID, Type: "file_rename_reported", ObservedAt: e.ObservedAt, OldValue: item.PreviousFilename, NewValue: item.Filename, EvidenceID: e.ID})
			}
			run.Items++
		}
	}
	if cursorID != "" && lastComplete != "" {
		if err := c.store.Put("reconcile", cursorID, githubReconcileCursor{LastEntityID: lastComplete}); err != nil {
			return fault("ERR_AUDIT_FAILED", "falha ao persistir cursor de arquivos de PR")
		}
	}
	if !c.saveRun(run) {
		return fault("ERR_AUDIT_FAILED", "falha ao auditar arquivos de PR")
	}
	d.Coverage = append(d.Coverage, c.coverage(r, "pr_files", status, observed, reason))
	return nil
}

func prFileComponentID(resourceID, version, filename string) string {
	return "file-" + storage.Hash([]byte(resourceID + ":" + version + ":" + filename))[:24]
}

func validGitPath(value string) bool {
	return value != "" && len(value) <= 1024 && !path.IsAbs(value) && path.Clean(value) == value && value != ".." && !strings.HasPrefix(value, "../") && !strings.ContainsAny(value, "\x00\r\n")
}
