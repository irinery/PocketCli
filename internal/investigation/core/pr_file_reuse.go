package core

import (
	"pocketcli/internal/investigation/query"
)

// Preserve version-specific PR and commit file mappings. A refreshed head can
// differ while earlier file identities and their evidence remain historical.
func reusePriorFileGraph(d *query.Dataset, prior query.Dataset, windowEnd string, referenced map[string]bool) {
	activeSources := make(map[string]bool, len(d.PullRequests)+len(d.Events))
	for _, pr := range d.PullRequests {
		activeSources[pr.ID] = true
	}
	for _, event := range d.Events {
		if event.Type == "commit_reachable_observed" {
			activeSources[event.ID] = true
		}
	}
	fileComponents := make(map[string]bool)
	for _, link := range prior.Links {
		if link.Relation == "modifies_file" && activeSources[link.SourceID] {
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
