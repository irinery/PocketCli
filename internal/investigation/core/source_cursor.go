package core

import (
	"context"
	"os"
	"sort"
	"time"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/storage"
)

// sourceCursor records confirmed coverage separately from the current page
// checkpoint. A partial run may update Checkpoint but never advances Watermark.
type sourceCursor struct {
	Source               string           `json:"source"`
	ResourceID           string           `json:"resource_id"`
	Collection           string           `json:"collection"`
	QueryHash            string           `json:"query_hash"`
	AuthScopeFingerprint string           `json:"auth_scope_fingerprint"`
	CoveredIntervals     []sourceInterval `json:"covered_intervals"`
	Watermark            string           `json:"watermark,omitempty"`
	CheckpointPlanID     string           `json:"checkpoint_plan_id,omitempty"`
	Checkpoint           string           `json:"checkpoint,omitempty"`
	CheckedAt            string           `json:"checked_at,omitempty"`
}

type sourceInterval struct {
	Start string `json:"start,omitempty"`
	End   string `json:"end"`
}

type cursorHandle struct {
	id    string
	value sourceCursor
}

func (c *Core) githubCursor(ctx context.Context, plan contract.Plan, r contract.Resource, collection string) (cursorHandle, error) {
	return c.collectionCursor(ctx, plan, r, "github", collection, "github-rest-v1:updated-desc:per-page-100")
}

func (c *Core) linearCursor(ctx context.Context, plan contract.Plan, r contract.Resource, collection string) (cursorHandle, error) {
	return c.collectionCursor(ctx, plan, r, "linear", collection, "linear-graphql-v1:updated-desc:first-100")
}

func (c *Core) collectionCursor(ctx context.Context, plan contract.Plan, r contract.Resource, source, collection, selector string) (cursorHandle, error) {
	var auth planAuth
	err := c.store.Get("planauth", plan.ID, &auth)
	if os.IsNotExist(err) {
		// Direct collector callers may not have prepared a plan. Production
		// plans always have planauth and are checked again before a run.
		probe := contract.PlanInput{GitHubEnabled: source == "github", LinearEnabled: source == "linear"}
		if auth, err = c.currentAuth(ctx, probe); err != nil {
			return cursorHandle{}, err
		}
	} else if err != nil {
		return cursorHandle{}, fault("ERR_AUDIT_FAILED", "identidade do plano corrompida")
	}
	fingerprint := auth.GitHub
	if source == "linear" {
		fingerprint = auth.Linear
	}
	if fingerprint == "" {
		return cursorHandle{}, fault("ERR_AUDIT_FAILED", "identidade da fonte ausente")
	}
	queryHash := storage.Hash([]byte(selector + ":" + collection))
	id := storage.Hash([]byte(plan.InvestigationID + ":" + source + ":" + r.ID + ":" + collection + ":" + queryHash + ":" + fingerprint))
	h := cursorHandle{id: id, value: sourceCursor{Source: source, ResourceID: r.ID, Collection: collection, QueryHash: queryHash, AuthScopeFingerprint: fingerprint, CoveredIntervals: []sourceInterval{}}}
	err = c.store.Get("sourcecursors", id, &h.value)
	if err == nil {
		if h.value.Source != source || h.value.ResourceID != r.ID || h.value.Collection != collection || h.value.QueryHash != queryHash || h.value.AuthScopeFingerprint != fingerprint || !validSourceCursor(h.value) {
			return cursorHandle{}, fault("ERR_AUDIT_FAILED", "cursor de fonte incompatível")
		}
		return h, nil
	}
	if !os.IsNotExist(err) {
		return cursorHandle{}, fault("ERR_AUDIT_FAILED", "cursor de fonte corrompido")
	}
	return h, nil
}

func validSourceCursor(v sourceCursor) bool {
	if v.Watermark != "" && !validCursorTime(v.Watermark) {
		return false
	}
	watermarkCovered := v.Watermark == ""
	for i, interval := range v.CoveredIntervals {
		if interval.End == "" || !validCursorTime(interval.End) || (interval.Start != "" && (!validCursorTime(interval.Start) || interval.Start >= interval.End)) {
			return false
		}
		if i > 0 && interval.Start <= v.CoveredIntervals[i-1].End {
			return false
		}
		if interval.End >= v.Watermark {
			watermarkCovered = true
		}
	}
	return watermarkCovered
}

func validCursorTime(value string) bool {
	parsed, err := time.Parse(time.RFC3339, value)
	return err == nil && parsed.UTC().Format(time.RFC3339) == value
}

func (c *Core) saveSourceCheckpoint(h *cursorHandle, planID, next string) error {
	h.value.CheckpointPlanID = planID
	h.value.Checkpoint = next
	h.value.CheckedAt = c.setup.now().UTC().Format(time.RFC3339)
	if err := c.store.Put("sourcecursors", h.id, h.value); err != nil {
		return fault("ERR_AUDIT_FAILED", "falha ao persistir checkpoint de fonte")
	}
	return nil
}

func (c *Core) finishSourceCursor(h *cursorHandle, plan contract.Plan, status string) error {
	h.value.CheckedAt = c.setup.now().UTC().Format(time.RFC3339)
	if status == "complete" && validCursorTime(plan.WindowEnd) && (plan.WindowStart == "" || validCursorTime(plan.WindowStart)) {
		h.value.CoveredIntervals = mergeSourceIntervals(append(h.value.CoveredIntervals, sourceInterval{Start: plan.WindowStart, End: plan.WindowEnd}))
		if plan.WindowEnd > h.value.Watermark {
			h.value.Watermark = plan.WindowEnd
		}
		h.value.CheckpointPlanID = ""
		h.value.Checkpoint = ""
	}
	if err := c.store.Put("sourcecursors", h.id, h.value); err != nil {
		return fault("ERR_AUDIT_FAILED", "falha ao persistir cobertura da fonte")
	}
	return nil
}

func mergeSourceIntervals(intervals []sourceInterval) []sourceInterval {
	sort.Slice(intervals, func(i, j int) bool { return intervals[i].Start < intervals[j].Start })
	out := make([]sourceInterval, 0, len(intervals))
	for _, interval := range intervals {
		if len(out) == 0 || interval.Start > out[len(out)-1].End {
			out = append(out, interval)
			continue
		}
		if interval.End > out[len(out)-1].End {
			out[len(out)-1].End = interval.End
		}
	}
	return out
}

func (h cursorHandle) deltaStart(plan contract.Plan) string {
	if h.value.Watermark == "" || !validSourceCursor(h.value) || !h.coversWindowStart(plan.WindowStart) {
		return plan.WindowStart
	}
	watermark, _ := time.Parse(time.RFC3339, h.value.Watermark)
	candidate := watermark.Add(-48 * time.Hour).UTC().Format(time.RFC3339)
	if plan.WindowEnd != "" && candidate >= plan.WindowEnd {
		return plan.WindowStart
	}
	if plan.WindowStart != "" && candidate < plan.WindowStart {
		return plan.WindowStart
	}
	return candidate
}

func (h cursorHandle) coversWindowStart(start string) bool {
	for _, interval := range h.value.CoveredIntervals {
		if (interval.Start == "" || (start != "" && interval.Start <= start)) && interval.End >= h.value.Watermark {
			return true
		}
	}
	return false
}

// linearIssueSpans schedules uncovered history separately from the recent
// update delta. Linear supports both bounds on updatedAt; an unbounded
// all-history window still needs the full inventory query.
func (h cursorHandle) linearIssueSpans(plan contract.Plan) []sourceInterval {
	if h.value.Watermark == "" || plan.WindowStart == "" || !validSourceCursor(h.value) {
		return []sourceInterval{{}}
	}
	upper := h.value.Watermark
	if plan.WindowEnd != "" && plan.WindowEnd < upper {
		upper = plan.WindowEnd
	}
	spans := uncoveredSourceIntervals(plan.WindowStart, upper, h.value.CoveredIntervals)
	watermark, _ := time.Parse(time.RFC3339, h.value.Watermark)
	since := watermark.Add(-48 * time.Hour).UTC().Format(time.RFC3339)
	if since < plan.WindowStart || (plan.WindowEnd != "" && since >= plan.WindowEnd) {
		since = plan.WindowStart
	}
	spans = append(spans, sourceInterval{Start: since})
	return spans
}

func uncoveredSourceIntervals(start, end string, covered []sourceInterval) []sourceInterval {
	if start == "" || end == "" || start >= end {
		return nil
	}
	current := start
	var gaps []sourceInterval
	for _, interval := range covered {
		if interval.End <= current {
			continue
		}
		if interval.Start > current {
			gapEnd := interval.Start
			if gapEnd > end {
				gapEnd = end
			}
			gaps = append(gaps, sourceInterval{Start: current, End: gapEnd})
		}
		if interval.End > current {
			current = interval.End
		}
		if current >= end {
			return gaps
		}
	}
	return append(gaps, sourceInterval{Start: current, End: end})
}
