package core

import (
	"encoding/json"
	"sort"
	"strings"
	"time"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/query"
	"pocketcli/internal/investigation/storage"
)

// LinearStagePrefix marks workflow-stage intervals derived from Linear issue
// history, e.g. "linear:Em revisão". The suffix is the state name observed in
// the history entry, so team-specific workflows stay distinguishable.
const LinearStagePrefix = "linear:"

type linearTransition struct {
	id, at, from, to string
	raw              json.RawMessage
}

func linearStateLabel(state *struct {
	Name string `json:"name"`
	Type string `json:"type"`
}) string {
	if state == nil {
		return ""
	}
	if name := strings.TrimSpace(state.Name); name != "" {
		return name
	}
	return strings.TrimSpace(state.Type)
}

// projectLinearStages turns one issue's complete history into stage
// intervals. Callers must pass only histories whose pagination completed: a
// missing page could hide a transition and fabricate a duration.
//
// Each transition into state X opens an interval closed by the next state
// transition. A duration exists only when that next transition leaves X; a
// different origin proves a missing transition, so the interval keeps its
// boundaries without elapsed seconds. The interval before the first
// transition starts at issue creation in the first transition's origin state.
// DurationValid additionally requires the exit inside the plan window, like
// deliveries; an interval still open at the window end has no duration.
func (c *Core) projectLinearStages(plan contract.Plan, issue query.Issue, items []json.RawMessage, d *query.Dataset) error {
	transitions := make([]linearTransition, 0, len(items))
	for _, raw := range items {
		var item struct {
			ID        string `json:"id"`
			CreatedAt string `json:"createdAt"`
			FromState *struct {
				Name string `json:"name"`
				Type string `json:"type"`
			} `json:"fromState"`
			ToState *struct {
				Name string `json:"name"`
				Type string `json:"type"`
			} `json:"toState"`
		}
		if json.Unmarshal(raw, &item) != nil || item.ID == "" || (item.FromState == nil && item.ToState == nil) {
			continue
		}
		if _, err := time.Parse(time.RFC3339, item.CreatedAt); err != nil {
			continue
		}
		transitions = append(transitions, linearTransition{id: item.ID, at: item.CreatedAt, from: linearStateLabel(item.FromState), to: linearStateLabel(item.ToState), raw: raw})
	}
	sort.SliceStable(transitions, func(i, j int) bool {
		a, _ := time.Parse(time.RFC3339, transitions[i].at)
		b, _ := time.Parse(time.RFC3339, transitions[j].at)
		if !a.Equal(b) {
			return a.Before(b)
		}
		return transitions[i].id < transitions[j].id
	})
	kept := d.Stages[:0]
	for _, stage := range d.Stages {
		if stage.SubjectID == issue.ID && strings.HasPrefix(stage.Stage, LinearStagePrefix) {
			continue
		}
		kept = append(kept, stage)
	}
	d.Stages = kept
	if len(transitions) == 0 {
		return nil
	}
	type opening struct {
		state, at, evidenceID string
	}
	openings := make([]opening, 0, len(transitions)+1)
	if first := transitions[0]; first.from != "" && issue.CreatedAt != "" && issue.EvidenceID != "" {
		openings = append(openings, opening{state: first.from, at: issue.CreatedAt, evidenceID: issue.EvidenceID})
	}
	evidenceIDs := make([]string, len(transitions))
	for i, t := range transitions {
		e, err := c.evidence("linear", issue.Identifier+":history:"+t.id, t.at, t.raw)
		if err != nil {
			return err
		}
		upsertEvidence(d, e)
		evidenceIDs[i] = e.ID
	}
	for i, t := range transitions {
		if t.to != "" {
			openings = append(openings, opening{state: t.to, at: t.at, evidenceID: evidenceIDs[i]})
		}
	}
	nextAfter := func(at string) (linearTransition, bool) {
		start, _ := time.Parse(time.RFC3339, at)
		for _, t := range transitions {
			when, _ := time.Parse(time.RFC3339, t.at)
			if when.After(start) {
				return t, true
			}
		}
		return linearTransition{}, false
	}
	for _, open := range openings {
		if plan.WindowEnd != "" && !beforeInstant(open.at, plan.WindowEnd) {
			continue
		}
		record := contract.StageRecord{SubjectID: issue.ID, ComponentID: issue.ComponentID, Stage: LinearStagePrefix + open.state, StartedAt: open.at, EvidenceID: open.evidenceID}
		next, found := nextAfter(open.at)
		if found && (plan.WindowEnd == "" || beforeInstant(next.at, plan.WindowEnd)) {
			record.EndedAt = next.at
			if next.from == open.state {
				start, _ := time.Parse(time.RFC3339, open.at)
				end, _ := time.Parse(time.RFC3339, next.at)
				seconds := int64(end.Sub(start) / time.Second)
				record.ElapsedSeconds = &seconds
			}
		}
		if record.EndedAt != "" && plan.WindowStart != "" && !beforeInstant(plan.WindowStart, record.EndedAt) {
			continue // Closed before the window: outside this snapshot.
		}
		record.DurationValid = record.ElapsedSeconds != nil && instantInWindow(record.EndedAt, plan.WindowStart, plan.WindowEnd)
		record.ID = linearStageID(record)
		d.Stages = append(d.Stages, record)
	}
	return nil
}

func linearStageID(record contract.StageRecord) string {
	return "stage-" + storage.Hash([]byte(record.SubjectID + "\x00" + record.Stage + "\x00" + record.StartedAt + "\x00" + record.EndedAt + "\x00" + record.EvidenceID))[:24]
}

// retainLinearStages keeps stage intervals of issues retained from the
// previous snapshot and not revisited in this round, re-evaluated against the
// new window. Issues revisited with complete history replace their intervals.
func retainLinearStages(d *query.Dataset, prior []contract.StageRecord, windowStart, windowEnd string, referenced map[string]bool) {
	issues := make(map[string]bool, len(d.Issues))
	for _, issue := range d.Issues {
		issues[issue.ID] = true
	}
	for _, stage := range prior {
		if !strings.HasPrefix(stage.Stage, LinearStagePrefix) || !issues[stage.SubjectID] {
			continue
		}
		if windowEnd != "" && !beforeInstant(stage.StartedAt, windowEnd) {
			continue
		}
		if stage.EndedAt != "" && windowEnd != "" && !beforeInstant(stage.EndedAt, windowEnd) {
			// The exit was observed after the new window end: inside this
			// snapshot the stage is still open.
			stage.EndedAt, stage.ElapsedSeconds = "", nil
		}
		if stage.EndedAt != "" && windowStart != "" && !beforeInstant(windowStart, stage.EndedAt) {
			continue
		}
		stage.DurationValid = stage.ElapsedSeconds != nil && instantInWindow(stage.EndedAt, windowStart, windowEnd)
		stage.ID = linearStageID(stage)
		d.Stages = append(d.Stages, stage)
		referenced[stage.EvidenceID] = true
	}
}

func beforeInstant(a, b string) bool {
	x, errA := time.Parse(time.RFC3339, a)
	y, errB := time.Parse(time.RFC3339, b)
	return errA == nil && errB == nil && x.Before(y)
}

func instantInWindow(at, start, end string) bool {
	when, err := time.Parse(time.RFC3339, at)
	if err != nil {
		return false
	}
	if start != "" {
		s, err := time.Parse(time.RFC3339, start)
		if err != nil || when.Before(s) {
			return false
		}
	}
	if end != "" {
		e, err := time.Parse(time.RFC3339, end)
		if err != nil || !when.Before(e) {
			return false
		}
	}
	return true
}
