package core

import "pocketcli/internal/investigation/query"

// The first Git observation marks code_history provisional. Promote it only
// after every collector that can leave a code-history gap has finished.
func (c *Core) completeCodeHistoryCoverage(resourceID string, d *query.Dataset) {
	required := map[string]bool{
		"git/git_refs": false, "git/manifests": false, "git/manifest_contents": false,
		"git/git_branches": false, "git/git_branch_graphs": false, "git/git_commits": false,
		"git/git_commit_files": false, "github/pr_files": false, "github/pr_commits": false,
		"git/file_components": false, "git/pr_head_file_components": false,
	}
	index := -1
	var commits int64
	for i, item := range d.Coverage {
		if item.ResourceID != resourceID {
			continue
		}
		if item.Source == "git" && item.Collection == "code_history" {
			index = i
		}
		key := item.Source + "/" + item.Collection
		if _, ok := required[key]; ok {
			required[key] = item.Status == "complete"
		}
		if key == "git/git_commits" {
			commits = item.Observed
		}
	}
	if index < 0 || d.Coverage[index].Status != "partial" {
		return
	}
	for _, complete := range required {
		if !complete {
			return
		}
	}
	d.Coverage[index].Status = "complete"
	d.Coverage[index].Observed = commits
	d.Coverage[index].Reason = "refs, branches, commits, arquivos e mapeamentos concluídos no escopo observado"
}
