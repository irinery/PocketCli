package core

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/query"
	"pocketcli/internal/investigation/storage"
)

// Kinds that hold no collected data and stay: the Linear schema is only the
// API's own field list. The HTTP cache holds raw API responses that cannot
// be attributed to one investigation, so it is cleared whole (it only saves
// requests on a later run).
var (
	purgeKeptKinds    = map[string]bool{"linearschema": true, "locks": true}
	purgeClearedKinds = map[string]bool{"httpcache": true}
)

// Names under which per-plan journals, branch and cursor state are keyed by
// a hash of the plan (or investigation) and the resource. The records hold no
// readable owner, so their keys are recomputed here. Keep this in sync with
// the key derivations in git*.go, collect.go, linear_*.go and pr_*.go;
// TestPurgeLeavesNothingOfTheInvestigation fails when a new kind is missed.
var (
	purgePlanSuffixes = []string{"git_ref", "git_branches", "git_branch_graphs", "schema:v2", "linear:projects", "linear:history", "linear:comments", "linear:relations", "linear:inverseRelations", "linear:attachments",
		"github:pull_requests", "github:issues", "pull_requests", "issues", "check_runs", "workflow_runs", "deployments", "environments"}
	purgeAuthSuffixes = []string{"", "pr_files:", "pr_commits:", "pr_head_components:", "checks:"}
	purgeLinearNames  = []string{"history", "comments", "relations", "inverseRelations", "attachments"}
)

type purgeOwner struct {
	investigationID string
	ids             []string        // plan, run and snapshot IDs: unique, generated
	otherQuoted     [][]byte        // quoted IDs of every other investigation
	derived         map[string]bool // hash-keyed records recomputed from plans
	objects         map[string]bool // evidence payloads no other snapshot reads
}

// PurgeInvestigation removes everything collected for one investigation from
// this machine: plans, runs, snapshots, datasets, journals, Git mirrors,
// evidence payloads, comments, reports and analyses. A new analysis is needed
// to get it back. Data another investigation also reads is kept and listed.
func (c *Core) PurgeInvestigation(ctx context.Context, investigationID string) (contract.PurgeResult, error) {
	result := contract.PurgeResult{InvestigationID: investigationID}
	if investigationID == "" {
		return result, fault("ERR_VALIDATION", "investigação obrigatória")
	}
	unlock, err := c.store.Lock("purge-" + investigationID)
	if err != nil {
		return result, fault("ERR_CONFLICT", "outra remoção desta investigação está em andamento")
	}
	defer unlock()
	owner, plans, err := c.purgeOwner(investigationID)
	if err != nil {
		return result, err
	}
	if c.investigationRunning(investigationID, owner) {
		return result, fault("ERR_CONFLICT", "há uma coleta em andamento nesta investigação; pause ou cancele antes de remover os dados")
	}
	result.Plans, result.Runs, result.Snapshots = plans, 0, 0
	for _, kind := range []string{"runs", "snapshots"} {
		ids, _ := c.store.List(kind)
		for _, id := range ids {
			if owner.owns(id) {
				if kind == "runs" {
					result.Runs++
				} else {
					result.Snapshots++
				}
			}
		}
	}
	files, bytesRemoved, shared, err := c.purgeFiles(ctx, owner)
	result.Files, result.Bytes, result.Shared, result.CachesCleared = files, bytesRemoved, shared, true
	if err != nil {
		return result, err
	}
	result.Leftover = c.purgeLeftover(owner)
	return result, nil
}

func (o purgeOwner) owns(id string) bool {
	for _, owned := range o.ids {
		if owned == id {
			return true
		}
	}
	return false
}

// purgeOwner collects the IDs the investigation owns and the IDs of every
// other investigation (to keep shared records), before anything is deleted.
func (c *Core) purgeOwner(investigationID string) (purgeOwner, int, error) {
	owner := purgeOwner{investigationID: investigationID, derived: map[string]bool{}, objects: map[string]bool{}}
	ownedPlans := map[string]contract.Plan{}
	otherInvestigations := map[string]bool{}
	planIDs, err := c.store.List("plans")
	if err != nil {
		return owner, 0, fault("ERR_AUDIT_FAILED", "planos ilegíveis")
	}
	var otherIDs []string
	for _, id := range planIDs {
		var plan contract.Plan
		if c.store.Get("plans", id, &plan) != nil {
			continue
		}
		if plan.InvestigationID == investigationID {
			ownedPlans[id] = plan
			owner.ids = append(owner.ids, id)
			continue
		}
		otherInvestigations[plan.InvestigationID] = true
		otherIDs = append(otherIDs, id)
	}
	runIDs, _ := c.store.List("runs")
	snapshots := map[string]bool{}
	otherSnapshots := map[string]bool{}
	for _, id := range runIDs {
		var run contract.Run
		if c.store.Get("runs", id, &run) != nil {
			continue
		}
		if _, ok := ownedPlans[run.PlanID]; ok {
			owner.ids = append(owner.ids, id)
			if run.SnapshotID != "" {
				snapshots[run.SnapshotID] = true
			}
			continue
		}
		otherIDs = append(otherIDs, id)
		if run.SnapshotID != "" {
			otherSnapshots[run.SnapshotID] = true
		}
	}
	snapshotIDs, _ := c.store.List("snapshots")
	for _, id := range snapshotIDs {
		var snapshot contract.Snapshot
		if c.store.Get("snapshots", id, &snapshot) != nil {
			continue
		}
		if snapshot.InvestigationID == investigationID {
			snapshots[id] = true
		} else {
			otherSnapshots[id] = true
			otherInvestigations[snapshot.InvestigationID] = true
		}
	}
	if len(ownedPlans) == 0 && len(snapshots) == 0 {
		return owner, 0, fault("ERR_NOT_FOUND", "nenhum dado desta investigação nesta máquina")
	}
	for id := range snapshots {
		if !otherSnapshots[id] {
			owner.ids = append(owner.ids, id)
		}
	}
	for id := range otherSnapshots {
		otherIDs = append(otherIDs, id)
	}
	for id := range otherInvestigations {
		if id != "" && id != investigationID {
			otherIDs = append(otherIDs, id)
		}
	}
	for _, id := range otherIDs {
		owner.otherQuoted = append(owner.otherQuoted, []byte(`"`+id+`"`))
	}
	// Evidence payloads are content-addressed and shared between snapshots:
	// remove only those no remaining snapshot reads.
	kept := map[string]bool{}
	for id := range otherSnapshots {
		var d query.Dataset
		if c.store.Get("datasets", id, &d) == nil {
			for _, e := range d.Evidence {
				kept[e.ID] = true
			}
		}
	}
	for id := range snapshots {
		var d query.Dataset
		if c.store.Get("datasets", id, &d) != nil {
			continue
		}
		for _, e := range d.Evidence {
			if !kept[e.ID] {
				owner.objects[e.ID] = true
			}
		}
	}
	for id, plan := range ownedPlans {
		var auth planAuth
		_ = c.store.Get("planauth", id, &auth)
		for _, r := range plan.Resources {
			for _, suffix := range purgePlanSuffixes {
				owner.derived[storage.Hash([]byte(plan.ID+":"+r.ID+":"+suffix))] = true
			}
			for _, suffix := range purgeAuthSuffixes {
				if auth.GitHub != "" {
					owner.derived[storage.Hash([]byte(investigationID+":"+r.ID+":"+suffix+auth.GitHub))] = true
				}
			}
			if auth.Linear != "" {
				for _, name := range purgeLinearNames {
					owner.derived[storage.Hash([]byte(investigationID+":"+r.ID+":"+name+":"+auth.Linear))] = true
				}
			}
		}
	}
	return owner, len(ownedPlans), nil
}

func (c *Core) investigationRunning(investigationID string, owner purgeOwner) bool {
	c.mu.Lock()
	active := make([]string, 0, len(c.runs))
	for id := range c.runs {
		active = append(active, id)
	}
	c.mu.Unlock()
	for _, id := range active {
		if owner.owns(id) {
			return true
		}
	}
	return false
}

// matches reports whether a record belongs to the investigation, by what its
// name or readable content says. shared is true when it also names another
// investigation's IDs, in which case it must stay.
func (o purgeOwner) matches(kind, name string, content func() []byte) (owned, shared bool) {
	id := strings.TrimSuffix(name, filepath.Ext(name))
	if o.derived[id] || (kind == "objects" && o.objects[id]) || id == o.investigationID {
		return true, false
	}
	for _, owned := range o.ids {
		if strings.Contains(name, owned) {
			return true, false
		}
	}
	data := content()
	if len(data) == 0 {
		return false, false
	}
	found := bytes.Contains(data, []byte(`"`+o.investigationID+`"`))
	for _, owned := range o.ids {
		if found {
			break
		}
		found = bytes.Contains(data, []byte(`"`+owned+`"`))
	}
	if !found {
		return false, false
	}
	for _, other := range o.otherQuoted {
		if bytes.Contains(data, other) {
			return false, true
		}
	}
	return true, false
}

func (c *Core) purgeFiles(ctx context.Context, owner purgeOwner) (int, int64, []string, error) {
	files, removed := 0, int64(0)
	var shared []string
	kinds, err := os.ReadDir(c.store.Root)
	if err != nil {
		return 0, 0, nil, fault("ERR_AUDIT_FAILED", "diretório de dados ilegível")
	}
	for _, kindEntry := range kinds {
		if !kindEntry.IsDir() || purgeKeptKinds[kindEntry.Name()] {
			continue
		}
		kind := kindEntry.Name()
		dir := filepath.Join(c.store.Root, kind)
		entries, err := os.ReadDir(dir)
		if err != nil {
			return files, removed, shared, fault("ERR_AUDIT_FAILED", "diretório de dados ilegível")
		}
		for i, entry := range entries {
			if i%200 == 0 {
				if err := ctx.Err(); err != nil {
					return files, removed, shared, err
				}
			}
			path := filepath.Join(dir, entry.Name())
			if purgeClearedKinds[kind] {
				n, size := removeTree(path)
				files, removed = files+n, removed+size
				continue
			}
			if entry.IsDir() {
				// Git mirrors and report folders: removed whole, by key or
				// owner ID in the folder name; unknown folders are left alone.
				if owned, _ := owner.matches(kind, entry.Name(), func() []byte { return nil }); owned {
					n, size := removeTree(path)
					files, removed = files+n, removed+size
				}
				continue
			}
			owned, isShared := owner.matches(kind, entry.Name(), func() []byte { return readCapped(path) })
			switch {
			case owned:
				n, size := removeTree(path)
				files, removed = files+n, removed+size
			case isShared:
				shared = append(shared, kind+"/"+entry.Name())
			}
		}
	}
	sort.Strings(shared)
	return files, removed, shared, nil
}

// purgeLeftover rescans the store for anything that still names the
// investigation, so a missed kind is reported instead of silently kept.
func (c *Core) purgeLeftover(owner purgeOwner) []string {
	var left []string
	kinds, _ := os.ReadDir(c.store.Root)
	for _, kindEntry := range kinds {
		if !kindEntry.IsDir() || purgeKeptKinds[kindEntry.Name()] || purgeClearedKinds[kindEntry.Name()] {
			continue
		}
		dir := filepath.Join(c.store.Root, kindEntry.Name())
		entries, _ := os.ReadDir(dir)
		for _, entry := range entries {
			path := filepath.Join(dir, entry.Name())
			content := func() []byte {
				if entry.IsDir() {
					return nil
				}
				return readCapped(path)
			}
			if owned, _ := owner.matches(kindEntry.Name(), entry.Name(), content); owned {
				left = append(left, kindEntry.Name()+"/"+entry.Name())
			}
		}
	}
	sort.Strings(left)
	return left
}

func readCapped(path string) []byte {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() > 256<<20 {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return data
}

// removeTree deletes a file or folder and returns how many files and bytes
// it held.
func removeTree(path string) (int, int64) {
	files, size := 0, int64(0)
	_ = filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			files++
			size += info.Size()
		}
		return nil
	})
	_ = os.RemoveAll(path)
	return files, size
}
