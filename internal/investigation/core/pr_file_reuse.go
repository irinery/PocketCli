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

// A PR commit can disappear from the current head after a force-push. Keep
// its observed membership and validity interval when the PR stays in scope.
func reusePriorPRCommitGraph(d *query.Dataset, prior query.Dataset, windowEnd string, referenced map[string]bool) {
	activePRs := make(map[string]bool, len(d.PullRequests))
	for _, pr := range d.PullRequests {
		activePRs[pr.ID] = true
	}
	events := make(map[string]query.Event, len(prior.Events))
	removals := make(map[string]query.Event)
	for _, event := range prior.Events {
		if event.Type == "pr_commit_observed" {
			events[event.ID] = event
		} else if event.Type == "pr_commit_removed_observed" {
			removals[event.EntityID+":"+event.OldValue+":"+event.EventAt] = event
		}
	}
	for _, link := range prior.Links {
		if link.Relation != "includes_commit" || !activePRs[link.SourceID] {
			continue
		}
		if link.ValidFrom != "" && windowEnd != "" && !timestampInWindow("", windowEnd, link.ValidFrom) {
			continue
		}
		event, ok := events[link.TargetID]
		if !ok || event.EntityID != link.SourceID {
			continue
		}
		copyLink := link
		if copyLink.ValidTo != "" && windowEnd != "" && !timestampInWindow("", windowEnd, copyLink.ValidTo) {
			copyLink.ValidTo = ""
		}
		upsertEvent(d, event)
		upsertLink(d, copyLink)
		referenced[event.EvidenceID] = true
		referenced[link.EvidenceID] = true
		if copyLink.ValidTo != "" {
			if removal, ok := removals[link.SourceID+":"+event.NewValue+":"+copyLink.ValidTo]; ok {
				upsertEvent(d, removal)
				referenced[removal.EvidenceID] = true
			}
		}
	}
}
