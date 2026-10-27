package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/gitmirror"
	"pocketcli/internal/investigation/query"
	"pocketcli/internal/investigation/storage"
)

const (
	commitFilesPerRound = 20
	maxFilesPerCommit   = 3000
	maxTreeEntries      = 100000
	maxTreeManifests    = 1000
)

type localManifests struct {
	entries  []gitmirror.TreeEntry
	complete bool
}

func (c *Core) collectGitCommitFiles(ctx context.Context, plan contract.Plan, r contract.Resource, d *query.Dataset, run *contract.Run) error {
	resource := contract.Resource{Source: "git", ID: r.ID}
	journalID := storage.Hash([]byte(plan.ID + ":" + r.ID + ":git_ref"))
	var journal gitRunObservation
	if err := c.store.Get("gitobservations", journalID, &journal); os.IsNotExist(err) {
		return nil
	} else if err != nil || !validGitOID(journal.Observation.NewOID) || journal.Observation.Ref == "" {
		return fault("ERR_AUDIT_FAILED", "observação Git inválida")
	}
	var auth planAuth
	if err := c.store.Get("planauth", plan.ID, &auth); err != nil || auth.GitHub == "" {
		return fault("ERR_AUDIT_FAILED", "identidade Git indisponível")
	}
	key := storage.Hash([]byte(plan.InvestigationID + ":" + r.ID + ":" + auth.GitHub))
	heads, refsComplete, err := c.observedGitHeads(plan, r, auth)
	if err != nil {
		return fault("ERR_AUDIT_FAILED", "heads Git observadas indisponíveis")
	}
	observer, err := c.gitMirror(filepath.Join(c.store.Root, "git", key))
	if errors.Is(err, gitmirror.ErrUnavailable) {
		d.Coverage = append(d.Coverage, c.coverage(resource, "git_commit_files", "unsupported", 0, "git não está disponível neste Agent"))
		return nil
	}
	if err != nil {
		return fault("ERR_AUDIT_FAILED", "espelho Git privado indisponível")
	}
	commitEvents := make(map[string]query.Event)
	done := make(map[string]query.Event)
	for _, event := range d.Events {
		if !validGitOID(event.NewValue) {
			continue
		}
		if event.EntityID == "repo-"+r.ID && event.Type == "commit_reachable_observed" {
			commitEvents[event.NewValue] = event
		}
		if event.Type == "commit_files_observed" {
			done[event.NewValue] = event
		}
	}
	newCandidates := make([]string, 0)
	retryCandidates := make([]string, 0)
	current := make(map[string]bool)
	commitHead := make(map[string]gitmirror.HeadRef)
	markerStatus := make(map[string]string)
	graphComplete := refsComplete
	for _, head := range heads {
		walkComplete, walkErr := observer.WalkCommits(ctx, head.OID, func(commit gitmirror.Commit) (bool, error) {
			event, selected := commitEvents[commit.OID]
			if !selected || current[commit.OID] {
				return true, nil
			}
			current[commit.OID] = true
			commitHead[commit.OID] = head
			if marker, ok := done[commit.OID]; ok && marker.EntityID == event.ID {
				markerStatus[commit.OID] = marker.OldValue
				if marker.OldValue != "complete" {
					retryCandidates = append(retryCandidates, commit.OID)
				}
			} else {
				newCandidates = append(newCandidates, commit.OID)
			}
			return true, nil
		})
		if walkErr != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fault("ERR_AUDIT_FAILED", "grafo de commits indisponível")
		}
		if !walkComplete {
			graphComplete = false
		}
	}
	sort.Strings(newCandidates)
	sort.Strings(retryCandidates)
	selected := append(newCandidates, retryCandidates...)
	if len(selected) > commitFilesPerRound {
		selected = selected[:commitFilesPerRound]
	}
	processed := len(markerStatus)
	unavailableHeads := make(map[string]bool)
	if len(selected) > 0 {
		if c.collector.Secrets == nil {
			return fault("ERR_CREDENTIAL_REQUIRED", "credencial GitHub ausente")
		}
		remote := "https://github.com/" + r.Name + ".git"
		neededHeads := make(map[string]gitmirror.HeadRef)
		for _, oid := range selected {
			head := commitHead[oid]
			neededHeads[head.Name] = head
		}
		orderedHeads := make([]string, 0, len(neededHeads))
		for name := range neededHeads {
			orderedHeads = append(orderedHeads, name)
		}
		sort.Strings(orderedHeads)
		for _, name := range orderedHeads {
			head := neededHeads[name]
			usage, err := gitmirror.StorageBytes(c.store.Root)
			if err != nil {
				return fault("ERR_AUDIT_FAILED", "uso local indisponível")
			}
			budget := plan.Input.Limits.MaxTransferBytes - run.Bytes
			if remaining := plan.Input.Limits.MaxLocalBytes - usage; remaining < budget {
				budget = remaining
			}
			var used int64
			err = c.collector.Secrets.WithSecret(ctx, "github", func(secret []byte) error {
				var ensureErr error
				used, ensureErr = observer.EnsureTrees(ctx, remote, secret, head.Name, head.OID, budget)
				return ensureErr
			})
			run.Bytes += used
			if !c.saveRun(run) {
				return fault("ERR_AUDIT_FAILED", "falha ao auditar transferência Git")
			}
			if errors.Is(err, gitmirror.ErrBudget) || run.Bytes > plan.Input.Limits.MaxTransferBytes {
				return fault("ERR_RESOURCE_EXHAUSTED", "orçamento de transferência Git atingido")
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if errors.Is(err, gitmirror.ErrMoved) || errors.Is(err, gitmirror.ErrUnavailable) {
				unavailableHeads[name] = true
				continue
			}
			if err != nil {
				return fault("ERR_AUDIT_FAILED", "árvores Git corrompidas ou credencial indisponível")
			}
		}
	}
	manifestCache := make(map[string]localManifests)
	for _, oid := range selected {
		if unavailableHeads[commitHead[oid].Name] {
			continue
		}
		complete, err := c.projectCommitFiles(ctx, observer, r, commitEvents[oid], d, run, manifestCache)
		if err != nil {
			return err
		}
		if markerStatus[oid] == "" {
			processed++
		}
		markerStatus[oid] = "partial"
		if complete {
			markerStatus[oid] = "complete"
		}
	}
	status, reason := "complete", "arquivos e componentes dos commits selecionados mapeados na árvore correspondente"
	partialOrPending := false
	for oid := range current {
		if markerStatus[oid] != "complete" {
			partialOrPending = true
			break
		}
	}
	if !graphComplete || partialOrPending || gitCommitCoveragePartial(d, r.ID) {
		status, reason = "partial", "fila de commits pendente, grafo incompleto ou arquivo sem mapeamento temporal inequívoco"
	}
	d.Coverage = append(d.Coverage, c.coverage(resource, "git_commit_files", status, int64(processed), reason))
	return nil
}

func gitCommitCoveragePartial(d *query.Dataset, resourceID string) bool {
	for _, coverage := range d.Coverage {
		if coverage.Source == "git" && coverage.ResourceID == resourceID && coverage.Collection == "git_commits" {
			return coverage.Status != "complete"
		}
	}
	return true
}

func (c *Core) projectCommitFiles(ctx context.Context, observer gitObserver, r contract.Resource, commitEvent query.Event, d *query.Dataset, run *contract.Run, cache map[string]localManifests) (bool, error) {
	oid := commitEvent.NewValue
	changes, allFiles, err := observer.ChangedFiles(ctx, oid, maxFilesPerCommit)
	if err != nil {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		return false, fault("ERR_AUDIT_FAILED", "diff Git local indisponível")
	}
	complete := allFiles
	mapped, observed := 0, 0
	for _, change := range changes {
		if !utf8.ValidString(change.Path) || !validGitPath(change.Path) || !validGitOID(change.TreeOID) {
			complete = false
			continue
		}
		record, err := json.Marshal(struct {
			Repository string `json:"repository"`
			CommitOID  string `json:"commit_oid"`
			Path       string `json:"path"`
			Status     string `json:"status"`
			TreeOID    string `json:"tree_oid"`
		}{r.Name, oid, change.Path, change.Status, change.TreeOID})
		if err != nil {
			return false, fault("ERR_AUDIT_FAILED", "diff Git inválido")
		}
		evidence, err := c.evidence("git", r.Name+":commit:"+oid+":file:"+change.Path, commitEvent.EventAt, record)
		if err != nil {
			return false, err
		}
		upsertEvidence(d, evidence)
		fileID := prFileComponentID(r.ID, oid, change.Path)
		upsertComponent(d, contract.ComponentRecord{ID: fileID, Name: change.Path, Repository: r.Name, Path: change.Path, Kind: "file_change", ImplementationStatus: "identified", DeploymentStatus: "unknown", Coverage: "partial"})
		linkID := "lnk-" + storage.Hash([]byte(commitEvent.ID + ":modifies_file:" + fileID + ":" + evidence.ID))[:24]
		upsertLink(d, contract.LinkRecord{ID: linkID, SourceID: commitEvent.ID, Relation: "modifies_file", TargetID: fileID, Status: "direct", EvidenceID: evidence.ID, ValidFrom: commitEvent.EventAt})
		observed++
		run.Items++
		manifests, ok := cache[change.TreeOID]
		if !ok {
			manifests, err = collectLocalManifests(ctx, observer, change.TreeOID)
			if err != nil {
				if ctx.Err() != nil {
					return false, ctx.Err()
				}
				complete = false
				continue
			}
			cache[change.TreeOID] = manifests
		}
		if !manifests.complete {
			complete = false
			continue
		}
		best, found := closestLocalManifest(change.Path, manifests.entries)
		if !found {
			complete = false
			continue
		}
		kind := manifestKind(path.Base(best.Path))
		componentID := "cmp-" + storage.Hash([]byte(r.ID + ":" + best.Path + ":" + kind))[:24]
		componentPresent := false
		for _, component := range d.Components {
			if component.ID == componentID {
				componentPresent = true
				break
			}
		}
		if !componentPresent {
			upsertComponent(d, contract.ComponentRecord{ID: componentID, Name: best.Path, Repository: r.Name, Path: best.Path, Kind: kind, ImplementationStatus: "identified", DeploymentStatus: "unknown", Coverage: "partial"})
		}
		mappingRecord, err := json.Marshal(struct {
			Repository     string `json:"repository"`
			CommitOID      string `json:"commit_oid"`
			FilePath       string `json:"file_path"`
			FileStatus     string `json:"file_status"`
			TreeOID        string `json:"tree_oid"`
			ManifestPath   string `json:"manifest_path"`
			ManifestOID    string `json:"manifest_oid"`
			FileEvidenceID string `json:"file_evidence_id"`
		}{r.Name, oid, change.Path, change.Status, change.TreeOID, best.Path, best.OID, evidence.ID})
		if err != nil {
			return false, fault("ERR_AUDIT_FAILED", "mapeamento temporal inválido")
		}
		mapEvidence, err := c.evidence("git", r.Name+":commit:"+oid+":file:"+change.Path+":component", commitEvent.EventAt, mappingRecord)
		if err != nil {
			return false, err
		}
		upsertEvidence(d, mapEvidence)
		mapID := "lnk-" + storage.Hash([]byte(fileID + ":maps_to_component:" + componentID + ":" + mapEvidence.ID))[:24]
		upsertLink(d, contract.LinkRecord{ID: mapID, SourceID: fileID, Relation: "maps_to_component", TargetID: componentID, Status: "direct", EvidenceID: mapEvidence.ID, ValidFrom: commitEvent.EventAt})
		mapped++
		run.Items++
	}
	status := "complete"
	if !complete {
		status = "partial"
	}
	summary, err := json.Marshal(struct {
		CommitOID string `json:"commit_oid"`
		Observed  int    `json:"observed_files"`
		Mapped    int    `json:"mapped_files"`
		Status    string `json:"status"`
	}{oid, observed, mapped, status})
	if err != nil {
		return false, fault("ERR_AUDIT_FAILED", "resumo Git inválido")
	}
	summaryEvidence, err := c.evidence("git", fmt.Sprintf("%s:commit:%s:files", r.Name, oid), commitEvent.EventAt, summary)
	if err != nil {
		return false, err
	}
	upsertEvidence(d, summaryEvidence)
	upsertEvent(d, query.Event{ID: "evt-" + storage.Hash([]byte(r.ID + ":commit_files:" + oid))[:24], EntityID: commitEvent.ID, Type: "commit_files_observed", EventAt: commitEvent.EventAt, ObservedAt: commitEvent.ObservedAt, OldValue: status, NewValue: oid, EvidenceID: summaryEvidence.ID})
	run.Items++
	if !c.saveRun(run) {
		return false, fault("ERR_AUDIT_FAILED", "falha ao auditar arquivos Git")
	}
	return complete, nil
}

func collectLocalManifests(ctx context.Context, observer gitObserver, treeOID string) (localManifests, error) {
	var result localManifests
	visited := 0
	complete, err := observer.WalkTree(ctx, treeOID, func(entry gitmirror.TreeEntry) (bool, error) {
		visited++
		if visited > maxTreeEntries {
			return false, nil
		}
		if entry.Type == "blob" && (entry.Mode == "100644" || entry.Mode == "100755") && validGitOID(entry.OID) && utf8.ValidString(entry.Path) && validGitPath(entry.Path) && manifestKind(path.Base(entry.Path)) != "" {
			result.entries = append(result.entries, entry)
			if len(result.entries) >= maxTreeManifests {
				return false, nil
			}
		}
		return true, nil
	})
	result.complete = complete
	return result, err
}

func closestLocalManifest(filePath string, manifests []gitmirror.TreeEntry) (gitmirror.TreeEntry, bool) {
	bestDepth := -1
	var best gitmirror.TreeEntry
	ambiguous := false
	for _, candidate := range manifests {
		dir := path.Dir(candidate.Path)
		if dir != "." && filePath != dir && !strings.HasPrefix(filePath, dir+"/") {
			continue
		}
		if len(dir) > bestDepth {
			bestDepth, best, ambiguous = len(dir), candidate, false
		} else if len(dir) == bestDepth {
			ambiguous = true
		}
	}
	return best, bestDepth >= 0 && !ambiguous
}
