package insights

import (
	"fmt"
	"sort"
	"time"

	"pocketcli/internal/investigation/insightcontract"
)

// caseTimeline projects only records tied to the case's entities. Unknown or
// invalid event times remain visible after dated records, without an inferred
// position in history.
func caseTimeline(source insightcontract.SourceBundle, decisions []insightcontract.DecisionRecord, entityIDs map[string]bool) []insightcontract.TimelineItem {
	return buildTimeline(source, decisions, entityIDs, false)
}

func buildTimeline(source insightcontract.SourceBundle, decisions []insightcontract.DecisionRecord, entityIDs map[string]bool, onlyChanges bool) []insightcontract.TimelineItem {
	items := make([]insightcontract.TimelineItem, 0)
	index := timelineEvidenceIndex(source)
	entitySource := make(map[string]string, len(source.Entities))
	for _, entity := range source.Entities {
		entitySource[entity.ID] = entity.Source
	}
	for _, decision := range decisions {
		evidenceIDs := make([]string, 0, len(decision.Citations))
		for _, citation := range decision.Citations {
			evidenceIDs = append(evidenceIDs, citation.EvidenceID)
		}
		items = append(items, decisionTimelineItem(source, decision, evidenceIDs, index))
	}
	for _, event := range source.Events {
		if !entityIDs[event.EntityID] {
			continue
		}
		if onlyChanges {
			title, _, _ := changeIntent(event.Type, event.OldValue, event.NewValue)
			if title == "" {
				continue
			}
		}
		observedAt := event.ObservedAt
		items = append(items, insightcontract.TimelineItem{ID: event.ID, Kind: "observation", EventType: event.Type, EntityID: event.EntityID, At: event.EventAt, ObservedAt: &observedAt, ActorID: event.ActorID, OldValue: event.OldValue, NewValue: event.NewValue, Source: entitySource[event.EntityID], Summary: event.Type, EvidenceIDs: append([]string(nil), event.EvidenceIDs...)})
	}
	for _, change := range source.Estimates {
		if !entityIDs[change.EntityID] {
			continue
		}
		var before, after *string
		if change.Before != nil {
			value := fmt.Sprint(*change.Before)
			before = &value
		}
		if change.After != nil {
			value := fmt.Sprint(*change.After)
			after = &value
		}
		items = append(items, insightcontract.TimelineItem{ID: change.ID, Kind: "observation", EventType: "estimate_changed", EntityID: change.EntityID, At: change.EventAt, ActorID: change.ActorID, OldValue: before, NewValue: after, Source: entitySource[change.EntityID], Summary: "Estimativa alterada", EvidenceIDs: append([]string(nil), change.EvidenceIDs...)})
	}
	if !onlyChanges {
		for _, stage := range source.Stages {
			if !entityIDs[stage.EntityID] {
				continue
			}
			items = append(items, insightcontract.TimelineItem{ID: stage.ID, Kind: "stage", EntityID: stage.EntityID, At: stage.Start, End: stage.End, Source: entitySource[stage.EntityID], Summary: stage.Name, EvidenceIDs: append([]string(nil), stage.EvidenceIDs...)})
		}
		for _, delivery := range source.Deliveries {
			if !entityIDs[delivery.EntityID] {
				continue
			}
			items = append(items, insightcontract.TimelineItem{ID: delivery.ID, Kind: "result", EntityID: delivery.EntityID, At: delivery.EventAt, Scope: delivery.Environment, Source: entitySource[delivery.EntityID], Summary: delivery.Status, EvidenceIDs: append([]string(nil), delivery.EvidenceIDs...)})
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		a, aOK := timelineTime(items[i].At)
		b, bOK := timelineTime(items[j].At)
		if aOK != bOK {
			return aOK
		}
		return aOK && a.Before(b)
	})
	return enrichDecisionTimeline(source, items)
}

func timelineTime(value *string) (time.Time, bool) {
	if value == nil {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339Nano, *value)
	return parsed, err == nil
}
