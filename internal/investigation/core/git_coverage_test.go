package core

import (
	"testing"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/query"
)

func TestCodeHistoryCoverageRequiresEveryCollection(t *testing.T) {
	resourceID := "repo-1"
	collections := []struct{ source, name string }{
		{"git", "git_refs"}, {"git", "manifests"}, {"git", "manifest_contents"},
		{"git", "git_branches"}, {"git", "git_branch_graphs"}, {"git", "git_commits"},
		{"git", "git_commit_files"}, {"github", "pr_files"}, {"github", "pr_commits"},
		{"git", "file_components"}, {"git", "pr_head_file_components"},
	}
	makeDataset := func() query.Dataset {
		d := query.Dataset{Coverage: []contract.Coverage{{Source: "git", ResourceID: resourceID, Collection: "code_history", Status: "partial"}}}
		for _, collection := range collections {
			observed := int64(0)
			if collection.name == "git_commits" {
				observed = 7
			}
			d.Coverage = append(d.Coverage, contract.Coverage{Source: collection.source, ResourceID: resourceID, Collection: collection.name, Status: "complete", Observed: observed})
		}
		return d
	}
	c := &Core{}
	complete := makeDataset()
	c.completeCodeHistoryCoverage(resourceID, &complete)
	if complete.Coverage[0].Status != "complete" || complete.Coverage[0].Observed != 7 || coverageStatus(complete.Coverage) != "complete" {
		t.Fatalf("all collections finished but code history remained partial: %+v", complete.Coverage[0])
	}
	partial := makeDataset()
	partial.Coverage[5].Status = "partial" // branch graph is still pending.
	c.completeCodeHistoryCoverage(resourceID, &partial)
	if partial.Coverage[0].Status != "partial" || coverageStatus(partial.Coverage) != "partial" {
		t.Fatalf("pending branch graph was hidden: %+v", partial.Coverage[0])
	}
	missing := makeDataset()
	missing.Coverage = missing.Coverage[:len(missing.Coverage)-1]
	c.completeCodeHistoryCoverage(resourceID, &missing)
	if missing.Coverage[0].Status != "partial" {
		t.Fatalf("missing PR-head mapping was hidden: %+v", missing.Coverage[0])
	}
}
