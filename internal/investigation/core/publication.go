package core

import (
	"strings"
	"time"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/query"
)

// activityWindow is the span between the first and the last record observed
// inside the snapshot window: issue and pull request timestamps and events.
// Resource creation dates are not activity.
func activityWindow(d query.Dataset) (from, to string) {
	note := func(value string) {
		at := contract.NormalizeInstant(value)
		if at == "" || !timestampInWindow(d.Snapshot.WindowStart, d.Snapshot.WindowEnd, at) {
			return
		}
		if from == "" || at < from {
			from = at
		}
		if at > to {
			to = at
		}
	}
	for _, issue := range d.Issues {
		if issue.IsContext {
			continue
		}
		note(issue.CreatedAt)
		note(issue.StartedAt)
		note(issue.CompletedAt)
		note(issue.CanceledAt)
	}
	for _, pr := range d.PullRequests {
		note(pr.CreatedAt)
		note(pr.MergedAt)
	}
	for _, event := range d.Events {
		note(event.EventAt)
	}
	return from, to
}

// publicationStatus keeps a snapshot provisional while a reconciliation
// queue is pending. The operator may publish it anyway with a reason, which
// is recorded with the queues it covered.
func publicationStatus(coverage []contract.Coverage, reason string, now time.Time) (string, *contract.PublishOverride) {
	pending := contract.PendingQueues(coverage)
	if len(pending) == 0 {
		return contract.SnapshotPublished, nil
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return contract.SnapshotProvisional, nil
	}
	return contract.SnapshotPublished, &contract.PublishOverride{Reason: reason, At: now.UTC().Format(time.RFC3339), Pending: pending}
}

// noteActor records the display name a source reported for an actor. The
// pseudonym is assigned later by the rules, over every actor they label.
func noteActor(d *query.Dataset, id, source, name string) {
	name = strings.TrimSpace(name)
	if id == "" {
		return
	}
	for i := range d.Snapshot.Actors {
		if d.Snapshot.Actors[i].ID == id {
			if d.Snapshot.Actors[i].Name == "" {
				d.Snapshot.Actors[i].Name = name
			}
			return
		}
	}
	d.Snapshot.Actors = append(d.Snapshot.Actors, contract.Actor{ID: id, Source: source, Name: name})
}

func upsertCycle(d *query.Dataset, v query.Cycle) {
	for i := range d.Cycles {
		if d.Cycles[i].ID == v.ID {
			d.Cycles[i] = v
			return
		}
	}
	d.Cycles = append(d.Cycles, v)
}
