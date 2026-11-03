package contract

import (
	"math"
	"strings"
)

// Snapshot statuses. A snapshot is provisional while a reconciliation queue
// is pending, unless the operator accepted publishing it as it is.
const (
	SnapshotPublished   = "published"
	SnapshotProvisional = "provisional"
)

// LowCoverageShare is the share of a team's issues with history below which a
// history-based metric is labelled low coverage.
const LowCoverageShare = 0.5

// NewMetricBase states that observed of total issues fed a metric.
func NewMetricBase(observed, total int64) *MetricBase {
	return &MetricBase{Observed: observed, Total: total, Low: total > 0 && float64(observed) < LowCoverageShare*float64(total)}
}

// Percent is the base's share, rounded down, or 0 for an empty base.
func (b MetricBase) Percent() int {
	if b.Total == 0 {
		return 0
	}
	return int(math.Floor(float64(b.Observed) * 100 / float64(b.Total)))
}

// PendingQueues lists the coverage rows whose collection stopped at a
// per-round cap and left items for a later round, as "resource/collection".
func PendingQueues(items []Coverage) []string {
	var out []string
	for _, c := range items {
		if c.Status != "complete" && strings.Contains(c.Reason, "fila de ") {
			out = append(out, c.ResourceID+"/"+c.Collection)
		}
	}
	return out
}
