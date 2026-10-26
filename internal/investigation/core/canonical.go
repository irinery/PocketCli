package core

import (
	"sort"

	"pocketcli/internal/investigation/query"
)

func canonicalizeDataset(d *query.Dataset) {
	sort.Slice(d.Components, func(i, j int) bool { return d.Components[i].ID < d.Components[j].ID })
	sort.Slice(d.Findings, func(i, j int) bool { return d.Findings[i].ID < d.Findings[j].ID })
	sort.Slice(d.Evidence, func(i, j int) bool { return d.Evidence[i].ID < d.Evidence[j].ID })
	sort.Slice(d.Coverage, func(i, j int) bool {
		a, b := d.Coverage[i], d.Coverage[j]
		if a.Source != b.Source {
			return a.Source < b.Source
		}
		if a.ResourceID != b.ResourceID {
			return a.ResourceID < b.ResourceID
		}
		return a.Collection < b.Collection
	})
	sort.Slice(d.Issues, func(i, j int) bool { return d.Issues[i].ID < d.Issues[j].ID })
	sort.Slice(d.PullRequests, func(i, j int) bool { return d.PullRequests[i].ID < d.PullRequests[j].ID })
	sort.Slice(d.Links, func(i, j int) bool { return d.Links[i].ID < d.Links[j].ID })
	sort.Slice(d.Events, func(i, j int) bool { return d.Events[i].ID < d.Events[j].ID })
	sort.Slice(d.Stages, func(i, j int) bool { return d.Stages[i].ID < d.Stages[j].ID })
	sort.Slice(d.Estimates, func(i, j int) bool { return d.Estimates[i].ID < d.Estimates[j].ID })
	sort.Slice(d.Rework, func(i, j int) bool { return d.Rework[i].ID < d.Rework[j].ID })
	sort.Slice(d.Deliveries, func(i, j int) bool { return d.Deliveries[i].ID < d.Deliveries[j].ID })
	sort.Slice(d.Metrics, func(i, j int) bool { return d.Metrics[i].ID < d.Metrics[j].ID })
}
