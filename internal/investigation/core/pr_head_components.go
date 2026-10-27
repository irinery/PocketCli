package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"sort"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/gitmirror"
	"pocketcli/internal/investigation/query"
	"pocketcli/internal/investigation/storage"
)

type prHeadCommit struct {
	SHA  string `json:"sha"`
	Tree struct {
		SHA string `json:"sha"`
	} `json:"tree"`
}

// mapPRHeadFiles uses each selected PR's own commit tree. A manifest seen at
// today's default ref is never applied retroactively to a different head.
func (c *Core) mapPRHeadFiles(ctx context.Context, plan contract.Plan, r contract.Resource, d *query.Dataset, run *contract.Run) error {
	resource := contract.Resource{Source: "git", ID: r.ID}
	var auth planAuth
	if err := c.store.Get("planauth", plan.ID, &auth); err != nil || auth.GitHub == "" {
		return fault("ERR_AUDIT_FAILED", "identidade GitHub indisponível")
	}
	currentOID := ""
	var observed gitRunObservation
	if err := c.store.Get("gitobservations", storage.Hash([]byte(plan.ID+":"+r.ID+":git_ref")), &observed); err == nil {
		currentOID = observed.Observation.NewOID
	} else if !os.IsNotExist(err) {
		return fault("ERR_AUDIT_FAILED", "observação Git corrompida")
	}
	files := make(map[string]contract.ComponentRecord)
	fileLinks := make(map[string][]contract.LinkRecord)
	for _, component := range d.Components {
		if component.Repository == r.Name && component.Kind == "file_change" {
			files[component.ID] = component
		}
	}
	for _, link := range d.Links {
		if link.Relation == "modifies_file" {
			if _, ok := files[link.TargetID]; ok {
				fileLinks[link.SourceID] = append(fileLinks[link.SourceID], link)
			}
		}
	}
	prs := make([]query.PullRequest, 0)
	missingHead := false
	for _, pr := range d.PullRequests {
		if pr.Repository != r.Name || len(fileLinks[pr.ID]) == 0 || pr.HeadSHA == currentOID {
			continue
		}
		if !validGitOID(pr.HeadSHA) {
			missingHead = true
			continue
		}
		prs = append(prs, pr)
	}
	sort.Slice(prs, func(i, j int) bool { return prs[i].ID < prs[j].ID })
	status, reason := "complete", "árvores dos heads de PRs consultadas na versão correspondente"
	if missingHead {
		status, reason = "partial", "um ou mais PRs sem SHA válido"
	}
	selected := prs
	cursorID := ""
	if len(prs) > 20 {
		status, reason = "partial", fmt.Sprintf("fila de árvores de PR: %d PRs pendentes", len(prs)-20)
		cursorID = storage.Hash([]byte(plan.InvestigationID + ":" + r.ID + ":pr_head_components:" + auth.GitHub))
		var cursor githubReconcileCursor
		if err := c.store.Get("reconcile", cursorID, &cursor); err != nil && !os.IsNotExist(err) {
			return fault("ERR_AUDIT_FAILED", "cursor de árvores de PR corrompido")
		}
		start := sort.Search(len(prs), func(i int) bool { return prs[i].ID > cursor.LastEntityID })
		if start >= len(prs) {
			start = 0
		}
		selected = append(append([]query.PullRequest{}, prs[start:]...), prs[:start]...)
		selected = selected[:20]
	}
	var mapped int64
	lastProcessed := ""
	for _, pr := range selected {
		if err := ctx.Err(); err != nil {
			return err
		}
		commitRaw, err := c.cachedGitHubObject(ctx, plan, r, run, "gitcommits", auth.GitHub, pr.HeadSHA, "/git/commits/"+pr.HeadSHA)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if fatalCollectionError(err) {
				return err
			}
			status, reason = "partial", "commit de um ou mais PRs indisponível"
			continue
		}
		var commit prHeadCommit
		if json.Unmarshal(commitRaw, &commit) != nil || commit.SHA != pr.HeadSHA || !validGitOID(commit.Tree.SHA) {
			status, reason = "partial", "commit de PR sem árvore válida"
			continue
		}
		treeRaw, err := c.cachedGitHubObject(ctx, plan, r, run, "gittrees", auth.GitHub, commit.Tree.SHA, "/git/trees/"+commit.Tree.SHA+"?recursive=1")
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if fatalCollectionError(err) {
				return err
			}
			status, reason = "partial", "árvore de um ou mais PRs indisponível"
			continue
		}
		var tree gitTreeResponse
		if json.Unmarshal(treeRaw, &tree) != nil || tree.SHA != commit.Tree.SHA || tree.Tree == nil {
			status, reason = "partial", "árvore de PR inválida"
			continue
		}
		if tree.Truncated {
			status, reason = "partial", "árvore de PR truncada"
		}
		manifests := make([]gitTreeEntry, 0)
		for _, entry := range tree.Tree {
			if entry.Type == "blob" && (entry.Mode == "100644" || entry.Mode == "100755") && validGitOID(entry.SHA) && validGitPath(entry.Path) && manifestKind(path.Base(entry.Path)) != "" {
				manifests = append(manifests, entry)
				if len(manifests) >= 100 {
					status, reason = "partial", "limite de 100 manifestos por árvore de PR"
					break
				}
			}
		}
		currentFiles := 0
		for _, fileLink := range fileLinks[pr.ID] {
			file := files[fileLink.TargetID]
			if file.ID != prFileComponentID(r.ID, pr.HeadSHA, file.Path) {
				continue // Kept for historical evidence after a force-push.
			}
			currentFiles++
			bestDepth := -1
			var best gitTreeEntry
			ambiguous := false
			for _, candidate := range manifests {
				dir := path.Dir(candidate.Path)
				if dir != "." && file.Path != dir && (len(file.Path) <= len(dir) || file.Path[:len(dir)+1] != dir+"/") {
					continue
				}
				if len(dir) > bestDepth {
					bestDepth, best, ambiguous = len(dir), candidate, false
				} else if len(dir) == bestDepth {
					ambiguous = true
				}
			}
			if bestDepth < 0 || ambiguous {
				status, reason = "partial", "arquivo sem manifesto inequívoco no head do PR"
				continue
			}
			kind := manifestKind(path.Base(best.Path))
			componentID := "cmp-" + storage.Hash([]byte(r.ID + ":" + best.Path + ":" + kind))[:24]
			found := false
			for _, component := range d.Components {
				if component.ID == componentID {
					found = true
					break
				}
			}
			if !found {
				d.Components = append(d.Components, contract.ComponentRecord{ID: componentID, Name: best.Path, Repository: r.Name, Path: best.Path, Kind: kind, ImplementationStatus: "identified", DeploymentStatus: "unknown", Coverage: "partial"})
			}
			record, err := json.Marshal(struct {
				Repository     string `json:"repository"`
				PRHead         string `json:"pr_head"`
				TreeSHA        string `json:"tree_sha"`
				FilePath       string `json:"file_path"`
				ManifestPath   string `json:"manifest_path"`
				ManifestBlob   string `json:"manifest_blob"`
				FileEvidenceID string `json:"file_evidence_id"`
			}{r.Name, pr.HeadSHA, commit.Tree.SHA, file.Path, best.Path, best.SHA, fileLink.EvidenceID})
			if err != nil {
				return fault("ERR_AUDIT_FAILED", "vínculo temporal inválido")
			}
			e, err := c.evidence("git", r.Name+":"+file.Path+"@"+pr.HeadSHA+":component", "", record)
			if err != nil {
				return err
			}
			for _, existing := range d.Evidence {
				if existing.ID == e.ID {
					e = existing // Keep the first observed validity start on refresh.
					break
				}
			}
			upsertEvidence(d, e)
			linkID := "lnk-" + storage.Hash([]byte(file.ID + ":maps_to_component:" + componentID + ":" + e.ID))[:24]
			upsertLink(d, contract.LinkRecord{ID: linkID, SourceID: file.ID, Relation: "maps_to_component", TargetID: componentID, Status: "direct", EvidenceID: e.ID, ValidFrom: e.ObservedAt})
			mapped++
			run.Items++
		}
		if currentFiles == 0 {
			status, reason = "partial", "arquivos preservados pertencem a head anterior; head atual ainda não foi inventariado"
		}
		lastProcessed = pr.ID
	}
	if cursorID != "" && lastProcessed != "" {
		if err := c.store.Put("reconcile", cursorID, githubReconcileCursor{LastEntityID: lastProcessed}); err != nil {
			return fault("ERR_AUDIT_FAILED", "falha ao persistir cursor de árvores de PR")
		}
	}
	if !c.saveRun(run) {
		return fault("ERR_AUDIT_FAILED", "falha ao auditar árvores de PR")
	}
	d.Coverage = append(d.Coverage, c.coverage(resource, "pr_head_file_components", status, mapped, reason))
	return nil
}

func fatalCollectionError(err error) bool {
	var coded *contract.ErrorResponse
	return errors.As(err, &coded) && (coded.Code == "ERR_AUDIT_FAILED" || coded.Code == "ERR_RATE_LIMITED" || coded.Code == "ERR_RESOURCE_EXHAUSTED")
}

func (c *Core) cachedGitHubObject(ctx context.Context, plan contract.Plan, r contract.Resource, run *contract.Run, kind, authID, oid, suffix string) (json.RawMessage, error) {
	cacheID := storage.Hash([]byte(authID + ":" + r.ID + ":" + oid))
	var raw json.RawMessage
	err := c.store.Get(kind, cacheID, &raw)
	if err == nil {
		var identity struct {
			SHA string `json:"sha"`
		}
		if json.Unmarshal(raw, &identity) != nil || identity.SHA != oid {
			return nil, fault("ERR_AUDIT_FAILED", "objeto Git em cache inválido")
		}
		return raw, nil
	}
	if !os.IsNotExist(err) {
		return nil, fault("ERR_AUDIT_FAILED", "objeto Git em cache corrompido")
	}
	body, _, _, wireBytes, fetchErr := c.collector.GetGitHubWithBytes(ctx, c.collector.GitHubURL+"/repos/"+r.Name+suffix)
	run.Requests++
	run.Bytes += wireBytes
	if !c.saveRun(run) {
		return nil, fault("ERR_AUDIT_FAILED", "falha ao auditar objeto Git")
	}
	if run.Bytes > plan.Input.Limits.MaxTransferBytes {
		return nil, fault("ERR_RESOURCE_EXHAUSTED", "orçamento de transferência atingido")
	}
	if fetchErr != nil {
		return nil, fetchErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var identity struct {
		SHA string `json:"sha"`
	}
	if json.Unmarshal(body, &identity) != nil || identity.SHA != oid {
		return nil, fault("ERR_UNAVAILABLE", "objeto Git inválido")
	}
	usage, err := gitmirror.StorageBytes(c.store.Root)
	if err != nil {
		return nil, fault("ERR_AUDIT_FAILED", "uso local indisponível")
	}
	if usage+int64(len(body)) > plan.Input.Limits.MaxLocalBytes {
		return nil, fault("ERR_RESOURCE_EXHAUSTED", "orçamento local atingido")
	}
	raw = body
	if err := c.store.Put(kind, cacheID, raw); err != nil {
		return nil, fault("ERR_AUDIT_FAILED", "falha ao persistir objeto Git")
	}
	return raw, nil
}
