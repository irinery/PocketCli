package core

import (
	"context"

	"pocketcli/internal/investigation/contract"
)

// Run.Progress is a 0..100 estimate for display. Collection fills 0..90 in
// equal shares per resource; each collector reports the fraction of its own
// share it has finished, and the publication stages fill the rest.
const (
	progressCollected = 90
	progressBuilding  = 92
	progressPublished = 95
	progressReporting = 97
	progressCompleted = 100
)

type progressRangeKey struct{}

type progressRange struct{ from, to float64 }

// withProgressRange narrows the caller's share to [from, to] of itself, so a
// nested collector reports fractions of its own work without knowing where
// that work sits in the run.
func withProgressRange(ctx context.Context, from, to float64) context.Context {
	parent, ok := ctx.Value(progressRangeKey{}).(progressRange)
	if !ok {
		parent = progressRange{0, progressCollected}
	}
	span := parent.to - parent.from
	return context.WithValue(ctx, progressRangeKey{}, progressRange{parent.from + span*clampFraction(from), parent.from + span*clampFraction(to)})
}

// reportProgress records that a fraction of the current share is done. The
// value never moves backwards, so a resumed run keeps its previous estimate
// until it catches up. The next saveRun persists it.
func reportProgress(ctx context.Context, run *contract.Run, fraction float64) {
	r, ok := ctx.Value(progressRangeKey{}).(progressRange)
	if !ok {
		r = progressRange{0, progressCollected}
	}
	setProgress(run, int(r.from+(r.to-r.from)*clampFraction(fraction)))
}

func setProgress(run *contract.Run, percent int) {
	if percent > progressCompleted {
		percent = progressCompleted
	}
	if percent > run.Progress {
		run.Progress = percent
	}
}

func clampFraction(f float64) float64 {
	if f < 0 {
		return 0
	}
	if f > 1 {
		return 1
	}
	return f
}
