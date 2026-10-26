package core

import (
	"bytes"
	"encoding/json"
	"os"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/query"
	"pocketcli/internal/investigation/storage"
)

type runBuild struct {
	RunID    string        `json:"run_id"`
	PlanID   string        `json:"plan_id"`
	PlanHash string        `json:"plan_hash"`
	Dataset  query.Dataset `json:"dataset"`
}

func (c *Core) loadRunBuild(runID string, plan contract.Plan) (query.Dataset, bool, error) {
	var build runBuild
	err := c.store.Get("runbuilds", runID, &build)
	if os.IsNotExist(err) {
		return query.Dataset{}, false, nil
	}
	if err != nil || build.RunID != runID || build.PlanID != plan.ID || build.PlanHash != plan.Hash {
		return query.Dataset{}, false, fault("ERR_AUDIT_FAILED", "build de run corrompido")
	}
	d := build.Dataset
	snapshot := d.Snapshot
	if snapshot.PlanID != plan.ID || snapshot.PlanHash != plan.Hash || snapshot.InvestigationID != plan.InvestigationID || len(snapshot.DatasetHash) != 64 || snapshot.ID != "snap-"+snapshot.DatasetHash[:24] {
		return query.Dataset{}, false, fault("ERR_AUDIT_FAILED", "snapshot de build incompatível")
	}
	d.Snapshot.ID = ""
	d.Snapshot.DatasetHash = ""
	d.Snapshot.PublishedAt = ""
	canonical, err := json.Marshal(d)
	if err != nil || storage.Hash(canonical) != snapshot.DatasetHash {
		return query.Dataset{}, false, fault("ERR_AUDIT_FAILED", "hash de build incompatível")
	}
	build.Dataset.Snapshot = snapshot
	return build.Dataset, true, nil
}

func (c *Core) saveRunBuild(runID string, plan contract.Plan, d query.Dataset) error {
	if err := c.store.Put("runbuilds", runID, runBuild{RunID: runID, PlanID: plan.ID, PlanHash: plan.Hash, Dataset: d}); err != nil {
		return fault("ERR_AUDIT_FAILED", "falha ao persistir build")
	}
	return nil
}

func (c *Core) ensureProjection(d query.Dataset) error {
	if err := c.query.Publish(d.Snapshot.ID, d); err == nil {
		return nil
	}
	// Publish renames a complete, synced SQLite file atomically. A crash
	// after that rename leaves a projection with this snapshot ID.
	return c.query.VerifyPublished(d.Snapshot.ID, d.Snapshot.DatasetHash)
}

func (c *Core) putImmutable(kind, id string, value interface{}) error {
	expected, err := json.Marshal(value)
	if err != nil {
		return fault("ERR_AUDIT_FAILED", "artefato inválido")
	}
	var existing json.RawMessage
	err = c.store.Get(kind, id, &existing)
	if err == nil {
		var normalized bytes.Buffer
		if json.Compact(&normalized, existing) != nil || !bytes.Equal(normalized.Bytes(), expected) {
			return fault("ERR_AUDIT_FAILED", "artefato publicado incompatível")
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return fault("ERR_AUDIT_FAILED", "artefato publicado corrompido")
	}
	if err := c.store.Put(kind, id, value); err != nil {
		return fault("ERR_AUDIT_FAILED", "falha ao publicar artefato")
	}
	return nil
}
