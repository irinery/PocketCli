package rules

import (
	"sort"
	"strings"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/query"
)

// pseudonyms gives every actor in the dataset a stable label, "Pessoa
// A", "Pessoa B"…, ordered by first observed activity and then by ID, so an
// actor keeps its letter while later snapshots only add new people at the
// end. Metric names carry the pseudonym; the source name stays in the
// snapshot's actor map for a report that the user asked to show names.
func pseudonyms(d query.Dataset) ([]contract.Actor, map[string]string) {
	first := make(map[string]string)
	note := func(id, at string) {
		if id == "" {
			return
		}
		if prior, ok := first[id]; !ok || (at != "" && (prior == "" || at < prior)) {
			first[id] = at
		}
	}
	for _, actor := range d.Snapshot.Actors {
		note(actor.ID, "")
	}
	for _, event := range d.Events {
		note(event.ActorID, contract.NormalizeInstant(event.EventAt))
	}
	for _, pr := range d.PullRequests {
		note(pr.AuthorID, contract.NormalizeInstant(pr.CreatedAt))
	}
	for _, issue := range d.Issues {
		note(issue.AssigneeID, contract.NormalizeInstant(issue.CreatedAt))
	}
	ids := make([]string, 0, len(first))
	for id := range first {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		a, b := first[ids[i]], first[ids[j]]
		if (a == "") != (b == "") {
			return a != ""
		}
		if a != b {
			return a < b
		}
		return ids[i] < ids[j]
	})
	known := make(map[string]contract.Actor, len(d.Snapshot.Actors))
	for _, actor := range d.Snapshot.Actors {
		known[actor.ID] = actor
	}
	labels := make(map[string]string, len(ids))
	actors := make([]contract.Actor, 0, len(ids))
	for n, id := range ids {
		actor := known[id]
		actor.ID = id
		if actor.Source == "" {
			actor.Source = "linear"
			if strings.HasPrefix(id, "github-") {
				actor.Source = "github"
			}
		}
		actor.Pseudonym = "Pessoa " + pseudonymLetters(n)
		labels[id] = actor.Pseudonym
		actors = append(actors, actor)
	}
	sort.Slice(actors, func(i, j int) bool { return actors[i].ID < actors[j].ID })
	return actors, labels
}

// pseudonymLetters is the spreadsheet column name of n: A…Z, AA, AB…
func pseudonymLetters(n int) string {
	out := ""
	for n >= 0 {
		out = string(rune('A'+n%26)) + out
		n = n/26 - 1
	}
	return out
}

// actorLabel names an actor in metric text; never the raw source ID.
func actorLabel(labels map[string]string, id string) string {
	if label := labels[id]; label != "" {
		return label
	}
	return "pessoa não identificada"
}
