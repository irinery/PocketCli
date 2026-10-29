package core

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/query"
	"pocketcli/internal/investigation/storage"
)

// GitHub emits a mirror event on each issue. Keep the newest event for each
// directed edge so removals are respected even when the two timelines differ.
type githubDependencyEvent struct {
	id     int64
	at     string
	fromID string
	toID   string
	active bool
	raw    json.RawMessage
}

func parseGitHubDependencyEvents(plan contract.Plan, r contract.Resource, target githubChildTarget, items []json.RawMessage, d *query.Dataset) ([]githubDependencyEvent, int64, bool) {
	issues := make(map[string]string, len(d.Issues))
	for _, issue := range d.Issues {
		issues[strings.ToLower(issue.Identifier)] = issue.ID
	}
	var events []githubDependencyEvent
	var observed int64
	invalid := false
	for _, raw := range items {
		var entry struct {
			ID        int64  `json:"id"`
			Event     string `json:"event"`
			CreatedAt string `json:"created_at"`
			BlockedBy *struct {
				Number     int `json:"number"`
				Repository struct {
					FullName string `json:"full_name"`
				} `json:"repository"`
			} `json:"blocked_by"`
			Blocking *struct {
				Number     int `json:"number"`
				Repository struct {
					FullName string `json:"full_name"`
				} `json:"repository"`
			} `json:"blocking"`
		}
		if json.Unmarshal(raw, &entry) != nil {
			invalid = true
			continue
		}
		var otherNumber int
		var otherRepo string
		var fromID, toID string
		switch entry.Event {
		case "blocked_by_added", "blocked_by_removed":
			if entry.BlockedBy != nil {
				otherNumber, otherRepo = entry.BlockedBy.Number, entry.BlockedBy.Repository.FullName
			}
			fromID = target.EntityID
		case "blocking_added", "blocking_removed":
			if entry.Blocking != nil {
				otherNumber, otherRepo = entry.Blocking.Number, entry.Blocking.Repository.FullName
			}
			toID = target.EntityID
		default:
			continue
		}
		observed++
		if entry.ID <= 0 || entry.CreatedAt == "" || otherNumber <= 0 || !strings.EqualFold(otherRepo, r.Name) {
			invalid = true
			continue
		}
		if _, err := time.Parse(time.RFC3339, entry.CreatedAt); err != nil {
			invalid = true
			continue
		}
		if !timestampInWindow("", plan.WindowEnd, entry.CreatedAt) {
			continue
		}
		otherID := issues[strings.ToLower(fmt.Sprintf("%s#%d", r.Name, otherNumber))]
		if otherID == "" || otherID == target.EntityID {
			invalid = true
			continue
		}
		if fromID == "" {
			fromID = otherID
		} else {
			toID = otherID
		}
		events = append(events, githubDependencyEvent{id: entry.ID, at: entry.CreatedAt, fromID: fromID, toID: toID, active: strings.HasSuffix(entry.Event, "_added"), raw: raw})
	}
	return events, observed, invalid
}

func (c *Core) projectGitHubDependencyEvents(r contract.Resource, events []githubDependencyEvent, d *query.Dataset, run *contract.Run) error {
	latest := make(map[string]githubDependencyEvent)
	for _, event := range events {
		key := event.fromID + ":blocked_by:" + event.toID
		prior, exists := latest[key]
		if !exists || event.at > prior.at || (event.at == prior.at && event.id > prior.id) {
			latest[key] = event
		}
	}
	keys := make([]string, 0, len(latest))
	for key := range latest {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		event := latest[key]
		if !event.active {
			continue
		}
		locator := fmt.Sprintf("%s:dependency:%d", r.Name, event.id)
		evidence, err := c.evidence("github", locator, event.at, event.raw)
		if err != nil {
			return err
		}
		upsertEvidence(d, evidence)
		upsertLink(d, contract.LinkRecord{ID: "link-" + storage.Hash([]byte(key))[:24], SourceID: event.fromID, Relation: "blocked_by", TargetID: event.toID, Status: "direct", EvidenceID: evidence.ID, ValidFrom: event.at})
		run.Items++
		if !c.saveRun(run) {
			return fault("ERR_AUDIT_FAILED", "falha ao auditar dependência GitHub")
		}
	}
	return nil
}
