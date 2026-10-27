package core

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/gitmirror"
	"pocketcli/internal/investigation/query"
	"pocketcli/internal/investigation/storage"
)

type gitBranchState struct {
	Heads map[string]string `json:"heads"`
}

type gitBranchObservation struct {
	Previous   map[string]string   `json:"previous"`
	Heads      []gitmirror.HeadRef `json:"heads"`
	Complete   bool                `json:"complete"`
	ObservedAt string              `json:"observed_at"`
}

// collectGitBranches inventories refs without fetching every branch graph.
// Missing refs count as deleted only after an untruncated complete listing.
func (c *Core) collectGitBranches(ctx context.Context, plan contract.Plan, r contract.Resource, d *query.Dataset, run *contract.Run) error {
	resource := contract.Resource{Source: "git", ID: r.ID}
	if c.collector.Secrets == nil {
		return fault("ERR_CREDENTIAL_REQUIRED", "credencial GitHub ausente")
	}
	var auth planAuth
	if err := c.store.Get("planauth", plan.ID, &auth); err != nil || auth.GitHub == "" {
		return fault("ERR_AUDIT_FAILED", "identidade Git indisponível")
	}
	key := storage.Hash([]byte(plan.InvestigationID + ":" + r.ID + ":" + auth.GitHub))
	journalID := storage.Hash([]byte(plan.ID + ":" + r.ID + ":git_branches"))
	var journal gitBranchObservation
	journalErr := c.store.Get("gitbranchobservations", journalID, &journal)
	if journalErr != nil && !os.IsNotExist(journalErr) {
		return fault("ERR_AUDIT_FAILED", "journal de branches Git corrompido")
	}
	if os.IsNotExist(journalErr) {
		var prior gitBranchState
		if err := c.store.Get("gitbranches", key, &prior); err != nil && !os.IsNotExist(err) {
			return fault("ERR_AUDIT_FAILED", "estado anterior de branches corrompido")
		}
		if !validBranchState(prior.Heads) {
			return fault("ERR_AUDIT_FAILED", "estado anterior de branches inválido")
		}
		observer, err := c.gitMirror(filepath.Join(c.store.Root, "git", key))
		if errors.Is(err, gitmirror.ErrUnavailable) {
			d.Coverage = append(d.Coverage, c.coverage(resource, "git_branches", "unsupported", 0, "git não está disponível neste Agent"))
			return nil
		}
		if err != nil {
			return fault("ERR_AUDIT_FAILED", "espelho Git privado indisponível")
		}
		budget := plan.Input.Limits.MaxTransferBytes - run.Bytes
		if budget <= 0 {
			return fault("ERR_RESOURCE_EXHAUSTED", "orçamento de transferência Git atingido")
		}
		remote := "https://github.com/" + r.Name + ".git"
		var refs []gitmirror.HeadRef
		var complete bool
		var used int64
		called := false
		err = c.collector.Secrets.WithSecret(ctx, "github", func(secret []byte) error {
			called = true
			var listErr error
			refs, complete, used, listErr = observer.ListHeads(ctx, remote, secret, 10000, budget)
			return listErr
		})
		run.Bytes += used
		if called {
			run.Requests++
		}
		if !c.saveRun(run) {
			return fault("ERR_AUDIT_FAILED", "falha ao auditar listagem de branches")
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !called && err != nil {
			return err
		}
		if errors.Is(err, gitmirror.ErrBudget) || run.Bytes > plan.Input.Limits.MaxTransferBytes {
			return fault("ERR_RESOURCE_EXHAUSTED", "orçamento de transferência Git atingido")
		}
		if errors.Is(err, gitmirror.ErrCorrupt) {
			return fault("ERR_AUDIT_FAILED", "espelho Git corrompido")
		}
		if err != nil {
			d.Coverage = append(d.Coverage, c.coverage(resource, "git_branches", "unavailable", 0, "listagem de heads Git indisponível; nenhuma exclusão inferida"))
			return nil
		}
		journal = gitBranchObservation{Previous: prior.Heads, Heads: refs, Complete: complete, ObservedAt: c.setup.now().UTC().Format(time.RFC3339)}
		if err := c.store.Put("gitbranchobservations", journalID, journal); err != nil {
			return fault("ERR_AUDIT_FAILED", "falha ao persistir listagem de branches")
		}
	}
	if journal.ObservedAt == "" || !validBranchState(journal.Previous) {
		return fault("ERR_AUDIT_FAILED", "journal de branches Git inválido")
	}
	current := make(map[string]string, len(journal.Heads))
	for _, ref := range journal.Heads {
		if !gitmirror.ValidHeadRef(ref.Name) || !validGitOID(ref.OID) || current[ref.Name] != "" {
			return fault("ERR_AUDIT_FAILED", "head Git inválida no journal")
		}
		current[ref.Name] = ref.OID
	}
	names := make([]string, 0, len(current))
	for name := range current {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		oldOID, newOID := journal.Previous[name], current[name]
		if oldOID == newOID {
			continue
		}
		kind := "branch_ref_created_observed"
		if oldOID != "" {
			kind = "branch_ref_oid_changed_observed"
		}
		if err := c.projectGitBranchChange(ctx, plan, r, name, oldOID, newOID, kind, journal.ObservedAt, d, run); err != nil {
			return err
		}
	}
	if journal.Complete {
		deleted := make([]string, 0)
		for name := range journal.Previous {
			if current[name] == "" {
				deleted = append(deleted, name)
			}
		}
		sort.Strings(deleted)
		for _, name := range deleted {
			if err := c.projectGitBranchChange(ctx, plan, r, name, journal.Previous[name], "", "branch_ref_deleted_observed", journal.ObservedAt, d, run); err != nil {
				return err
			}
		}
	} else {
		for name, oid := range journal.Previous {
			if current[name] == "" {
				current[name] = oid
			}
		}
	}
	if err := c.store.Put("gitbranches", key, gitBranchState{Heads: current}); err != nil {
		return fault("ERR_AUDIT_FAILED", "falha ao persistir estado de branches")
	}
	if !c.saveRun(run) {
		return fault("ERR_AUDIT_FAILED", "falha ao auditar branches Git")
	}
	status, reason := "complete", "heads Git listadas sem truncamento; mudanças de OID não provam rebase"
	if !journal.Complete {
		status, reason = "partial", "listagem de heads limitada ou inválida; ausência não prova exclusão"
	}
	d.Coverage = append(d.Coverage, c.coverage(resource, "git_branches", status, int64(len(journal.Heads)), reason))
	return nil
}

func validBranchState(heads map[string]string) bool {
	for name, oid := range heads {
		if !gitmirror.ValidHeadRef(name) || !validGitOID(oid) {
			return false
		}
	}
	return true
}

func (c *Core) projectGitBranchChange(ctx context.Context, plan contract.Plan, r contract.Resource, name, oldOID, newOID, kind, observedAt string, d *query.Dataset, run *contract.Run) error {
	record, err := json.Marshal(struct {
		Repository string `json:"repository"`
		Ref        string `json:"ref"`
		OldOID     string `json:"old_oid,omitempty"`
		NewOID     string `json:"new_oid,omitempty"`
		Change     string `json:"change"`
		ObservedAt string `json:"observed_at"`
	}{r.Name, name, oldOID, newOID, kind, observedAt})
	if err != nil {
		return fault("ERR_AUDIT_FAILED", "mudança de branch inválida")
	}
	evidence, err := c.evidence("git", r.Name+":"+c.safeTitle(ctx, name), observedAt, record)
	if err != nil {
		return err
	}
	upsertEvidence(d, evidence)
	eventID := "evt-" + storage.Hash([]byte(r.ID + ":" + name + ":" + kind + ":" + plan.ID))[:24]
	entityID := "gitref-" + storage.Hash([]byte(r.ID + ":" + name))[:24]
	upsertEvent(d, query.Event{ID: eventID, EntityID: entityID, Type: kind, EventAt: observedAt, ObservedAt: observedAt, OldValue: oldOID, NewValue: newOID, EvidenceID: evidence.ID})
	run.Items++
	return nil
}
