package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/gitmirror"
	"pocketcli/internal/investigation/query"
	"pocketcli/internal/investigation/storage"
)

const commitsPerRound = 100

// collectGitCommits walks only the locally mirrored commit graph. The commit
// events in the previous snapshot act as the durable completed set; a crash
// before publication can safely repeat this read-only walk.
func (c *Core) collectGitCommits(ctx context.Context, plan contract.Plan, r contract.Resource, d *query.Dataset, run *contract.Run) error {
	resource := contract.Resource{Source: "git", ID: r.ID}
	journalID := storage.Hash([]byte(plan.ID + ":" + r.ID + ":git_ref"))
	var journal gitRunObservation
	if err := c.store.Get("gitobservations", journalID, &journal); os.IsNotExist(err) {
		return nil // collectGitRef already recorded why the mirror is unavailable.
	} else if err != nil || !validGitOID(journal.Observation.NewOID) || journal.ObservedAt == "" {
		return fault("ERR_AUDIT_FAILED", "observação Git inválida")
	}
	var auth planAuth
	if err := c.store.Get("planauth", plan.ID, &auth); err != nil || auth.GitHub == "" {
		return fault("ERR_AUDIT_FAILED", "identidade Git indisponível")
	}
	key := storage.Hash([]byte(plan.InvestigationID + ":" + r.ID + ":" + auth.GitHub))
	heads, graphComplete, err := c.observedGitHeads(plan, r, auth)
	if err != nil {
		return fault("ERR_AUDIT_FAILED", "heads Git observadas indisponíveis")
	}
	observer, err := c.gitMirror(filepath.Join(c.store.Root, "git", key))
	if errors.Is(err, gitmirror.ErrUnavailable) {
		d.Coverage = append(d.Coverage, c.coverage(resource, "git_commits", "unsupported", 0, "git não está disponível neste Agent"))
		return nil
	}
	if err != nil {
		return fault("ERR_AUDIT_FAILED", "espelho Git privado indisponível")
	}
	entityID := "repo-" + r.ID
	seen := make(map[string]bool)
	for _, event := range d.Events {
		if event.EntityID == entityID && event.Type == "commit_reachable_observed" && validGitOID(event.NewValue) {
			seen[event.NewValue] = true
		}
	}
	newCount, invalidDates, selectedReachable := 0, 0, 0
	seenInWalk := make(map[string]bool)
	complete := true
	for _, head := range heads {
		walkComplete, walkErr := observer.WalkCommits(ctx, head.OID, func(commit gitmirror.Commit) (bool, error) {
			if err := ctx.Err(); err != nil {
				return false, err
			}
			if _, err := time.Parse(time.RFC3339, commit.CommittedAt); err != nil {
				if !seenInWalk[commit.OID] {
					invalidDates++
					seenInWalk[commit.OID] = true
				}
				return true, nil
			}
			if !timestampInWindow(plan.WindowStart, plan.WindowEnd, commit.CommittedAt) || seenInWalk[commit.OID] {
				return true, nil
			}
			seenInWalk[commit.OID] = true
			selectedReachable++
			if seen[commit.OID] {
				return true, nil
			}
			record, err := json.Marshal(struct {
				Repository  string `json:"repository"`
				Ref         string `json:"ref"`
				CommitOID   string `json:"commit_oid"`
				CommittedAt string `json:"committed_at"`
				ObservedAt  string `json:"observed_at"`
			}{r.Name, head.Name, commit.OID, commit.CommittedAt, journal.ObservedAt})
			if err != nil {
				return false, fault("ERR_AUDIT_FAILED", "commit inválido")
			}
			evidence, err := c.evidence("git", fmt.Sprintf("%s:commit:%s", r.Name, commit.OID), commit.CommittedAt, record)
			if err != nil {
				return false, err
			}
			upsertEvidence(d, evidence)
			upsertEvent(d, query.Event{ID: "evt-" + storage.Hash([]byte(r.ID + ":commit:" + commit.OID))[:24], EntityID: entityID, Type: "commit_reachable_observed", EventAt: commit.CommittedAt, ObservedAt: journal.ObservedAt, NewValue: commit.OID, EvidenceID: evidence.ID})
			target, inspected, err := observer.DeclaredRevertTarget(ctx, commit.OID)
			if err != nil {
				return false, fault("ERR_AUDIT_FAILED", "mensagem de commit Git indisponível")
			}
			if !inspected {
				upsertEvent(d, query.Event{ID: "evt-" + storage.Hash([]byte(r.ID + ":revert-uninspected:" + commit.OID))[:24], EntityID: entityID, Type: "commit_revert_message_uninspected", EventAt: commit.CommittedAt, ObservedAt: journal.ObservedAt, NewValue: commit.OID, EvidenceID: evidence.ID})
			} else if target != "" {
				revertRecord, err := json.Marshal(struct {
					Repository string `json:"repository"`
					RevertOID  string `json:"revert_oid"`
					TargetOID  string `json:"target_oid"`
					Relation   string `json:"relation"`
				}{r.Name, commit.OID, target, "declared_revert"})
				if err != nil {
					return false, fault("ERR_AUDIT_FAILED", "declaração de revert inválida")
				}
				revertEvidence, err := c.evidence("git", fmt.Sprintf("%s:revert:%s", r.Name, commit.OID), commit.CommittedAt, revertRecord)
				if err != nil {
					return false, err
				}
				upsertEvidence(d, revertEvidence)
				upsertEvent(d, query.Event{ID: "evt-" + storage.Hash([]byte(r.ID + ":revert-declared:" + commit.OID + ":" + target))[:24], EntityID: entityID, Type: "commit_revert_declared", EventAt: commit.CommittedAt, ObservedAt: journal.ObservedAt, OldValue: target, NewValue: commit.OID, EvidenceID: revertEvidence.ID})
			}
			seen[commit.OID] = true
			newCount++
			run.Items++
			return newCount < commitsPerRound, nil
		})
		if walkErr != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fault("ERR_AUDIT_FAILED", "grafo de commits indisponível")
		}
		if !walkComplete {
			complete = false
			break
		}
	}
	if !c.saveRun(run) {
		return fault("ERR_AUDIT_FAILED", "falha ao auditar commits Git")
	}
	status, reason := "complete", "commits alcançáveis selecionados pela data do commit"
	uninspectedMessages := 0
	for _, event := range d.Events {
		if event.EntityID == entityID && event.Type == "commit_revert_message_uninspected" && timestampInWindow(plan.WindowStart, plan.WindowEnd, event.EventAt) {
			uninspectedMessages++
		}
	}
	if !complete || !graphComplete || invalidDates > 0 || uninspectedMessages > 0 {
		status = "partial"
		switch {
		case !complete:
			reason = "fila de commits pendente; continue com refresh"
		case !graphComplete:
			reason = "branches pendentes ou grafo Git raso/incompleto"
		case uninspectedMessages > 0:
			reason = "mensagem de commit acima do limite de 64 KiB; declaração de revert não verificada"
		default:
			reason = "um ou mais commits sem data válida"
		}
	}
	d.Coverage = append(d.Coverage, c.coverage(resource, "git_commits", status, int64(selectedReachable), reason))
	return nil
}
