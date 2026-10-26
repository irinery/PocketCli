package core

import (
	"pocketcli/internal/investigation/query"
)

// Preserve mappings made against a PR's earlier head. Refresh can observe a
// new head, but the older file identities and their evidence remain historical.
func reusePriorPRFileGraph(d *query.Dataset, prior query.Dataset, windowEnd string, referenced map[string]bool) {
	activePRs := make(map[string]bool, len(d.PullRequests))
	for _, pr := range d.PullRequests {
		activePRs[pr.ID] = true
	}
	fileComponents := make(map[string]bool)
	for _, link := range prior.Links {
		if link.Relation == "modifies_file" && activePRs[link.SourceID] {
			upsertLink(d, link)
			fileComponents[link.TargetID] = true
			referenced[link.EvidenceID] = true
		}
	}
	mappedComponents := make(map[string]bool)
	for _, link := range prior.Links {
		if link.Relation != "maps_to_component" || !fileComponents[link.SourceID] {
			continue
		}
		if link.ValidFrom != "" && windowEnd != "" && !timestampInWindow("", windowEnd, link.ValidFrom) {
			continue
		}
		upsertLink(d, link)
		mappedComponents[link.TargetID] = true
		referenced[link.EvidenceID] = true
	}
	for _, component := range prior.Components {
		if (component.Kind == "file_change" && fileComponents[component.ID]) || mappedComponents[component.ID] {
			upsertComponent(d, component)
		}
	}
}
