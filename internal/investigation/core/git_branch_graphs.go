package core

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"

	"pocketcli/internal/investigation/collect"
	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/gitmirror"
	"pocketcli/internal/investigation/query"
	"pocketcli/internal/investigation/storage"
)

const branchGraphsPerRound = 3

type gitBranchGraphHead struct {
	OID           string `json:"oid"`
	GraphComplete bool   `json:"graph_complete"`
}

type gitBranchGraphState struct {
	Heads  map[string]gitBranchGraphHead `json:"heads"`
	Cursor string                        `json:"cursor,omitempty"`
}

type gitBranchGraphRound struct {
	Selected     []gitmirror.HeadRef              `json:"selected"`
	Previous     map[string]gitBranchGraphHead    `json:"previous"`
	Observations map[string]gitmirror.Observation `json:"observations"`
	ObservedAt   string                           `json:"observed_at"`
}

func validBranchGraphHeads(heads map[string]gitBranchGraphHead) bool {
	for ref, head := range heads {
		if !gitmirror.ValidHeadRef(ref) || !validGitOID(head.OID) {
			return false
		}
	}
	return true
}

func (c *Core) collectGitBranchGraphs(ctx context.Context, plan contract.Plan, r contract.Resource, d *query.Dataset, run *contract.Run) error {
	resource := contract.Resource{Source: "git", ID: r.ID}
	var refs gitBranchObservation
	refsID := storage.Hash([]byte(plan.ID + ":" + r.ID + ":git_branches"))
	if err := c.store.Get("gitbranchobservations", refsID, &refs); os.IsNotExist(err) {
		return nil // Branch inventory already reported why it was unavailable.
	} else if err != nil || refs.ObservedAt == "" {
		return fault("ERR_AUDIT_FAILED", "listagem de branches inválida")
	}
	var auth planAuth
	if err := c.store.Get("planauth", plan.ID, &auth); err != nil || auth.GitHub == "" {
		return fault("ERR_AUDIT_FAILED", "identidade Git indisponível")
	}
	key := storage.Hash([]byte(plan.InvestigationID + ":" + r.ID + ":" + auth.GitHub))
	var state gitBranchGraphState
	if err := c.store.Get("gitbranchgraphs", key, &state); err != nil && !os.IsNotExist(err) {
		return fault("ERR_AUDIT_FAILED", "estado de grafos de branches corrompido")
	}
	if !validBranchGraphHeads(state.Heads) || (state.Cursor != "" && !gitmirror.ValidHeadRef(state.Cursor)) {
		return fault("ERR_AUDIT_FAILED", "estado de grafos de branches inválido")
	}
	if state.Heads == nil {
		state.Heads = make(map[string]gitBranchGraphHead)
	}
	var defaultRef gitRunObservation
	defaultID := storage.Hash([]byte(plan.ID + ":" + r.ID + ":git_ref"))
	if err := c.store.Get("gitobservations", defaultID, &defaultRef); err != nil && !os.IsNotExist(err) {
		return fault("ERR_AUDIT_FAILED", "observação da ref padrão inválida")
	}
	roundID := storage.Hash([]byte(plan.ID + ":" + r.ID + ":git_branch_graphs"))
	var round gitBranchGraphRound
	roundErr := c.store.Get("gitbranchgraphrounds", roundID, &round)
	if roundErr != nil && !os.IsNotExist(roundErr) {
		return fault("ERR_AUDIT_FAILED", "journal de grafos de branches corrompido")
	}
	if os.IsNotExist(roundErr) {
		round = gitBranchGraphRound{Previous: make(map[string]gitBranchGraphHead, len(state.Heads)), Observations: make(map[string]gitmirror.Observation), ObservedAt: c.setup.now().UTC().Format(time.RFC3339)}
		for ref, head := range state.Heads {
			round.Previous[ref] = head
		}
		pending := make([]gitmirror.HeadRef, 0)
		for _, head := range refs.Heads {
			if head.Name == defaultRef.Observation.Ref {
				continue
			}
			if !gitmirror.ValidHeadRef(head.Name) || !validGitOID(head.OID) {
				return fault("ERR_AUDIT_FAILED", "head Git inválida")
			}
			if state.Heads[head.Name].OID != head.OID {
				pending = append(pending, head)
			}
		}
		sort.Slice(pending, func(i, j int) bool { return pending[i].Name < pending[j].Name })
		if len(pending) > 0 {
			start := sort.Search(len(pending), func(i int) bool { return pending[i].Name > state.Cursor })
			for i := 0; i < len(pending) && i < branchGraphsPerRound; i++ {
				round.Selected = append(round.Selected, pending[(start+i)%len(pending)])
			}
		}
		if err := c.store.Put("gitbranchgraphrounds", roundID, round); err != nil {
			return fault("ERR_AUDIT_FAILED", "falha ao persistir fila de grafos Git")
		}
	}
	if round.ObservedAt == "" || round.Previous == nil || round.Observations == nil || !validBranchGraphHeads(round.Previous) {
		return fault("ERR_AUDIT_FAILED", "journal de grafos Git inválido")
	}
	observer, err := c.gitMirror(filepath.Join(c.store.Root, "git", key))
	if errors.Is(err, gitmirror.ErrUnavailable) {
		d.Coverage = append(d.Coverage, c.coverage(resource, "git_branch_graphs", "unsupported", 0, "git não está disponível neste Agent"))
		return nil
	}
	if err != nil {
		return fault("ERR_AUDIT_FAILED", "espelho Git privado indisponível")
	}
	partial := false
	remote := "https://github.com/" + r.Name + ".git"
	for _, head := range round.Selected {
		if !gitmirror.ValidHeadRef(head.Name) || !validGitOID(head.OID) {
			return fault("ERR_AUDIT_FAILED", "head Git inválida no journal")
		}
		observation, recorded := round.Observations[head.Name]
		if !recorded {
			if c.collector.Secrets == nil {
				return fault("ERR_CREDENTIAL_REQUIRED", "credencial GitHub ausente")
			}
			usage, err := gitmirror.StorageBytes(c.store.Root)
			if err != nil {
				return fault("ERR_AUDIT_FAILED", "uso local indisponível")
			}
			budget := plan.Input.Limits.MaxTransferBytes - run.Bytes
			if left := plan.Input.Limits.MaxLocalBytes - usage; left < budget {
				budget = left
			}
			if budget <= 0 {
				return fault("ERR_RESOURCE_EXHAUSTED", "orçamento Git atingido")
			}
			prior := gitmirror.RefState{Ref: head.Name, OID: round.Previous[head.Name].OID}
			err = c.collector.Secrets.WithSecret(ctx, "github", func(secret []byte) error {
				var fetchErr error
				observation, fetchErr = observer.ObserveHead(ctx, remote, secret, head, prior, budget)
				return fetchErr
			})
			run.Bytes += observation.LocalBytes
			if !c.saveRun(run) {
				return fault("ERR_AUDIT_FAILED", "falha ao auditar transferência Git")
			}
			if err := collect.RecordGitObjectBytes(c.store, run.ID, collect.GitBranchObjects, observation.LocalBytes); err != nil {
				return err
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if errors.Is(err, gitmirror.ErrBudget) || run.Bytes > plan.Input.Limits.MaxTransferBytes {
				return fault("ERR_RESOURCE_EXHAUSTED", "orçamento Git atingido")
			}
			if errors.Is(err, gitmirror.ErrCorrupt) {
				return fault("ERR_AUDIT_FAILED", "espelho Git corrompido")
			}
			if err != nil {
				partial = true // Remote ref moved or became unavailable; preserve the old graph.
				continue
			}
			round.Observations[head.Name] = observation
			if err := c.store.Put("gitbranchgraphrounds", roundID, round); err != nil {
				return fault("ERR_AUDIT_FAILED", "falha ao persistir grafo Git observado")
			}
		}
		if observation.Ref != head.Name || observation.NewOID != head.OID || observation.OldOID != round.Previous[head.Name].OID || !validGitOID(observation.TreeOID) ||
			(observation.Relationship != "initial" && observation.Relationship != "unchanged" && observation.Relationship != "advanced" && observation.Relationship != "ref_diverged" && observation.Relationship != "unknown") {
			return fault("ERR_AUDIT_FAILED", "observação de branch Git inválida")
		}
		if err := c.projectGitBranchGraph(ctx, plan, r, observation, round.ObservedAt, d, run); err != nil {
			return err
		}
		state.Heads[head.Name] = gitBranchGraphHead{OID: observation.NewOID, GraphComplete: observation.GraphComplete}
	}
	if len(round.Selected) > 0 {
		state.Cursor = round.Selected[len(round.Selected)-1].Name
	}
	if err := c.store.Put("gitbranchgraphs", key, state); err != nil {
		return fault("ERR_AUDIT_FAILED", "falha ao persistir grafos Git")
	}
	if !c.saveRun(run) {
		return fault("ERR_AUDIT_FAILED", "falha ao auditar grafos Git")
	}
	observed, expected := 0, 0
	for _, head := range refs.Heads {
		if head.Name == defaultRef.Observation.Ref {
			continue
		}
		expected++
		if state.Heads[head.Name].OID == head.OID {
			observed++
			if !state.Heads[head.Name].GraphComplete {
				partial = true
			}
		}
	}
	status, reason := "complete", "grafos das heads listadas observados com ancestralidade conclusiva"
	if partial || !refs.Complete || observed != expected {
		status, reason = "partial", "fila de branches, listagem incompleta ou grafo sem ancestralidade conclusiva"
	}
	d.Coverage = append(d.Coverage, c.coverage(resource, "git_branch_graphs", status, int64(observed), reason))
	return nil
}

func (c *Core) projectGitBranchGraph(ctx context.Context, plan contract.Plan, r contract.Resource, observation gitmirror.Observation, observedAt string, d *query.Dataset, run *contract.Run) error {
	record := gitRefEvidence{Repository: r.Name, Ref: observation.Ref, OldOID: observation.OldOID, NewOID: observation.NewOID, TreeOID: observation.TreeOID, Relationship: observation.Relationship, GraphComplete: observation.GraphComplete, ObservedAt: observedAt}
	raw, err := json.Marshal(record)
	if err != nil {
		return fault("ERR_AUDIT_FAILED", "observação de grafo Git inválida")
	}
	evidence, err := c.evidence("git", r.Name+":"+c.safeTitle(ctx, observation.Ref), observedAt, raw)
	if err != nil {
		return err
	}
	upsertEvidence(d, evidence)
	if observation.Relationship != "unchanged" {
		eventID := "evt-" + storage.Hash([]byte(r.ID + ":" + observation.Ref + ":graph:" + plan.ID))[:24]
		entityID := "gitref-" + storage.Hash([]byte(r.ID + ":" + observation.Ref))[:24]
		upsertEvent(d, query.Event{ID: eventID, EntityID: entityID, Type: "branch_graph_" + observation.Relationship + "_observed", EventAt: observedAt, ObservedAt: observedAt, OldValue: observation.OldOID, NewValue: observation.NewOID, EvidenceID: evidence.ID})
	}
	run.Items++
	return nil
}

// observedGitHeads returns only refs whose inventoried OID was fetched. Old
// graphs remain on disk but are not attributed to a changed or missing head.
func (c *Core) observedGitHeads(plan contract.Plan, r contract.Resource, auth planAuth) ([]gitmirror.HeadRef, bool, error) {
	var current gitRunObservation
	defaultID := storage.Hash([]byte(plan.ID + ":" + r.ID + ":git_ref"))
	if err := c.store.Get("gitobservations", defaultID, &current); err != nil {
		return nil, false, err
	}
	if !gitmirror.ValidHeadRef(current.Observation.Ref) || !validGitOID(current.Observation.NewOID) {
		return nil, false, fault("ERR_AUDIT_FAILED", "ref Git padrão inválida")
	}
	heads := []gitmirror.HeadRef{{Name: current.Observation.Ref, OID: current.Observation.NewOID}}
	complete := current.Observation.GraphComplete
	var refs gitBranchObservation
	refsID := storage.Hash([]byte(plan.ID + ":" + r.ID + ":git_branches"))
	if err := c.store.Get("gitbranchobservations", refsID, &refs); err != nil {
		if os.IsNotExist(err) {
			return heads, false, nil
		}
		return nil, false, err
	}
	if !refs.Complete {
		complete = false
	}
	key := storage.Hash([]byte(plan.InvestigationID + ":" + r.ID + ":" + auth.GitHub))
	var state gitBranchGraphState
	if err := c.store.Get("gitbranchgraphs", key, &state); err != nil && !os.IsNotExist(err) {
		return nil, false, err
	}
	if !validBranchGraphHeads(state.Heads) {
		return nil, false, fault("ERR_AUDIT_FAILED", "estado de grafos Git inválido")
	}
	for _, head := range refs.Heads {
		if head.Name == current.Observation.Ref {
			continue
		}
		cached := state.Heads[head.Name]
		if cached.OID != head.OID {
			complete = false
			continue
		}
		if !cached.GraphComplete {
			complete = false
		}
		heads = append(heads, head)
	}
	extra := heads[1:]
	sort.Slice(extra, func(i, j int) bool { return extra[i].Name < extra[j].Name })
	return heads, complete, nil
}
