package core

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path"
	"strings"

	"pocketcli/internal/investigation/collect"
	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/gitmirror"
	"pocketcli/internal/investigation/query"
	"pocketcli/internal/investigation/storage"
)

type gitTreeEntry struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
	Size int64  `json:"size"`
}

type gitTreeResponse struct {
	SHA       string         `json:"sha"`
	Truncated bool           `json:"truncated"`
	Tree      []gitTreeEntry `json:"tree"`
}

func manifestKind(filename string) string {
	switch strings.ToLower(filename) {
	case "go.mod":
		return "go_module"
	case "package.json", "pnpm-workspace.yaml":
		return "node_package"
	case "dockerfile":
		return "container"
	case "compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml":
		return "compose"
	case "openapi.yaml", "openapi.yml", "openapi.json":
		return "api_contract"
	}
	return ""
}

func validGitOID(oid string) bool {
	if len(oid) != 40 && len(oid) != 64 {
		return false
	}
	for _, ch := range oid {
		if ch < '0' || ch > '9' {
			if ch < 'a' || ch > 'f' {
				return false
			}
		}
	}
	return true
}

// collectManifestPaths observes only the tree of the ref pinned for this run.
// A manifest path identifies a technical component; content and history remain
// separate work, so this never claims that the component was deployed.
func (c *Core) collectManifestPaths(ctx context.Context, plan contract.Plan, r contract.Resource, treeOID, authID, observedAt string, d *query.Dataset, run *contract.Run) error {
	resource := contract.Resource{Source: "git", ID: r.ID}
	if !validGitOID(treeOID) {
		d.Coverage = append(d.Coverage, c.coverage(resource, "manifests", "partial", 0, "árvore do commit indisponível"))
		return nil
	}
	cacheID := storage.Hash([]byte(authID + ":" + r.ID + ":" + treeOID))
	var raw json.RawMessage
	err := c.store.Get("gittrees", cacheID, &raw)
	wasCached := err == nil
	if err != nil && !os.IsNotExist(err) {
		return fault("ERR_AUDIT_FAILED", "cache de árvore Git corrompido")
	}
	if os.IsNotExist(err) {
		endpoint := c.collector.GitHubURL + "/repos/" + r.Name + "/git/trees/" + url.PathEscape(treeOID) + "?recursive=1"
		body, _, _, wireBytes, fetchErr := c.collector.GetGitHubWithBytes(ctx, endpoint)
		run.Requests++
		run.Bytes += wireBytes
		if !c.saveRun(run) {
			return fault("ERR_AUDIT_FAILED", "falha ao auditar árvore Git")
		}
		if run.Bytes > plan.Input.Limits.MaxTransferBytes {
			return fault("ERR_RESOURCE_EXHAUSTED", "orçamento de transferência atingido")
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if fetchErr != nil {
			var ce *contract.ErrorResponse
			status := "partial"
			if errors.As(fetchErr, &ce) && ce.Code == "ERR_FORBIDDEN" {
				status = "unauthorized"
			}
			d.Coverage = append(d.Coverage, c.coverage(resource, "manifests", status, 0, "árvore GitHub não acessível"))
			return nil
		}
		usage, usageErr := gitmirror.StorageBytes(c.store.Root)
		if usageErr != nil {
			return fault("ERR_AUDIT_FAILED", "uso local indisponível")
		}
		if usage+int64(len(body)) > plan.Input.Limits.MaxLocalBytes {
			return fault("ERR_RESOURCE_EXHAUSTED", "orçamento local atingido")
		}
		raw = body
		if err := c.store.Put("gittrees", cacheID, raw); err != nil {
			return fault("ERR_AUDIT_FAILED", "falha ao persistir árvore Git")
		}
	}
	var tree gitTreeResponse
	if json.Unmarshal(raw, &tree) != nil || tree.SHA != treeOID {
		d.Coverage = append(d.Coverage, c.coverage(resource, "manifests", "partial", 0, "resposta de árvore Git inválida"))
		return nil
	}
	if wasCached {
		if err := collect.RecordReuse(c.store, run.ID); err != nil {
			return err
		}
	}
	observed := int64(0)
	parsed := int64(0)
	status := "complete"
	reason := "caminhos de manifestos na árvore observada"
	contentStatus, contentReason := "complete", "conteúdo de manifestos até 2 MiB verificado por OID e analisado"
	if tree.Truncated {
		status, reason = "partial", "árvore truncada pela fonte; caminhos faltantes"
	}
	for _, entry := range tree.Tree {
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.Type != "blob" || (entry.Mode != "100644" && entry.Mode != "100755") || !validGitOID(entry.SHA) || len(entry.Path) > 1024 || strings.ContainsAny(entry.Path, "\x00\r\n") || strings.HasPrefix(entry.Path, "/") || strings.Contains(entry.Path, "../") {
			continue
		}
		kind := manifestKind(path.Base(entry.Path))
		if kind == "" {
			continue
		}
		if observed >= 100 {
			status, reason = "partial", "limite de 100 manifestos por repositório atingido"
			break
		}
		manifest := struct {
			Repository string `json:"repository"`
			CommitTree string `json:"commit_tree"`
			Path       string `json:"path"`
			BlobSHA    string `json:"blob_sha"`
			Kind       string `json:"kind"`
		}{r.Name, treeOID, entry.Path, entry.SHA, kind}
		item, err := json.Marshal(manifest)
		if err != nil {
			return fault("ERR_AUDIT_FAILED", "manifesto inválido")
		}
		e, err := c.evidence("git", r.Name+":"+entry.Path+"@"+treeOID, observedAt, item)
		if err != nil {
			return err
		}
		upsertEvidence(d, e)
		componentID := "cmp-" + storage.Hash([]byte(r.ID + ":" + entry.Path + ":" + kind))[:24]
		componentIndex := upsertComponent(d, contract.ComponentRecord{ID: componentID, Name: entry.Path, Repository: r.Name, Path: entry.Path, Kind: kind, ImplementationStatus: "identified", DeploymentStatus: "unknown", Coverage: "partial"})
		linkID := "lnk-" + storage.Hash([]byte("repo-" + r.ID + ":" + componentID + ":" + e.ID))[:24]
		upsertLink(d, contract.LinkRecord{ID: linkID, SourceID: "repo-" + r.ID, Relation: "contains_manifest", TargetID: componentID, Status: "direct", EvidenceID: e.ID})
		observed++
		if entry.Size > 2*1024*1024 {
			contentStatus, contentReason = "partial", "manifestos acima de 2 MiB não foram processados"
			continue
		}
		facts, blobEvidence, blobErr := c.collectManifestBlob(ctx, plan, r, entry, kind, authID, observedAt, d, run)
		if blobErr != nil {
			var coded *contract.ErrorResponse
			if errors.As(blobErr, &coded) && (coded.Code == "ERR_AUDIT_FAILED" || coded.Code == "ERR_RESOURCE_EXHAUSTED") {
				return blobErr
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			contentStatus, contentReason = "partial", "conteúdo de um ou mais manifestos indisponível ou inválido"
			continue
		}
		parsed++
		if facts.Name != "" {
			d.Components[componentIndex].Name = c.safeTitle(ctx, facts.Name)
		}
		upsertEvent(d, query.Event{ID: "evt-" + storage.Hash([]byte(componentID + ":" + entry.SHA))[:24], EntityID: componentID, Type: "manifest_content_observed", EventAt: observedAt, ObservedAt: blobEvidence.ObservedAt, NewValue: entry.SHA, EvidenceID: blobEvidence.ID})
		serviceIDs := make(map[string]string, len(facts.Services))
		for _, service := range facts.Services {
			serviceID := "cmp-" + storage.Hash([]byte(componentID + ":service:" + service))[:24]
			serviceIDs[service] = serviceID
			upsertComponent(d, contract.ComponentRecord{ID: serviceID, Name: service, Repository: r.Name, Path: entry.Path + "#" + service, Kind: "compose_service", ImplementationStatus: "identified", DeploymentStatus: "unknown", Coverage: "partial"})
			serviceLinkID := "lnk-" + storage.Hash([]byte(componentID + ":" + serviceID + ":" + blobEvidence.ID))[:24]
			upsertLink(d, contract.LinkRecord{ID: serviceLinkID, SourceID: componentID, Relation: "declares_service", TargetID: serviceID, Status: "direct", EvidenceID: blobEvidence.ID})
		}
		for _, dependency := range facts.Dependencies {
			sourceID, sourceExists := serviceIDs[dependency.Service]
			targetID, targetExists := serviceIDs[dependency.DependsOn]
			if !sourceExists || !targetExists {
				return fault("ERR_AUDIT_FAILED", "dependência Compose sem componente verificado")
			}
			linkID := "lnk-" + storage.Hash([]byte(sourceID + ":depends_on:" + targetID + ":" + blobEvidence.ID))[:24]
			upsertLink(d, contract.LinkRecord{ID: linkID, SourceID: sourceID, Relation: "depends_on", TargetID: targetID, Status: "direct", EvidenceID: blobEvidence.ID})
		}
		if len(facts.UnresolvedDependencies) > 0 {
			contentStatus, contentReason = "partial", "dependência Compose sem destino no manifesto observado"
		}
	}
	d.Coverage = append(d.Coverage, c.coverage(resource, "manifests", status, observed, reason))
	if tree.Truncated {
		contentStatus, contentReason = "partial", "árvore truncada; conteúdos de manifestos faltantes"
	}
	d.Coverage = append(d.Coverage, c.coverage(resource, "manifest_contents", contentStatus, parsed, contentReason))
	return nil
}
