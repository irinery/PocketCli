package core

import (
	"context"
	"encoding/json"
	"os"
	"path"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/query"
	"pocketcli/internal/investigation/storage"
)

func isManifestComponent(kind string) bool {
	switch kind {
	case "go_module", "node_package", "container", "compose", "api_contract":
		return true
	}
	return false
}

// A direct path mapping is emitted only when the PR head equals the observed
// ref and a single nearest manifest directory owns that path in that tree.
func (c *Core) mapCurrentRefFiles(ctx context.Context, plan contract.Plan, r contract.Resource, d *query.Dataset) error {
	resource := contract.Resource{Source: "git", ID: r.ID}
	journalID := storage.Hash([]byte(plan.ID + ":" + r.ID + ":git_ref"))
	var journal gitRunObservation
	if err := c.store.Get("gitobservations", journalID, &journal); err != nil {
		if os.IsNotExist(err) {
			d.Coverage = append(d.Coverage, c.coverage(resource, "file_components", "partial", 0, "ref Git não observada nesta rodada"))
			return nil
		}
		return fault("ERR_AUDIT_FAILED", "observação Git indisponível")
	}
	if journal.Observation.NewOID == "" || journal.ObservedAt == "" {
		return fault("ERR_AUDIT_FAILED", "observação Git inválida")
	}
	files := make(map[string]contract.ComponentRecord)
	manifests := make([]contract.ComponentRecord, 0)
	for _, component := range d.Components {
		if component.Repository != r.Name {
			continue
		}
		if component.Kind == "file_change" {
			files[component.ID] = component
		} else if isManifestComponent(component.Kind) {
			manifests = append(manifests, component)
		}
	}
	links := append([]contract.LinkRecord(nil), d.Links...)
	var mapped int64
	for _, pr := range d.PullRequests {
		if pr.Repository != r.Name || pr.HeadSHA == "" || pr.HeadSHA != journal.Observation.NewOID {
			continue
		}
		for _, fileLink := range links {
			if fileLink.SourceID != pr.ID || fileLink.Relation != "modifies_file" {
				continue
			}
			file, ok := files[fileLink.TargetID]
			if !ok || file.ID != prFileComponentID(r.ID, pr.HeadSHA, file.Path) {
				continue
			}
			bestDepth := -1
			best := contract.ComponentRecord{}
			ambiguous := false
			for _, candidate := range manifests {
				dir := path.Dir(candidate.Path)
				if dir != "." && file.Path != dir && len(file.Path) <= len(dir) {
					continue
				}
				if dir != "." && file.Path != dir && file.Path[:len(dir)+1] != dir+"/" {
					continue
				}
				depth := len(dir)
				if depth > bestDepth {
					bestDepth, best, ambiguous = depth, candidate, false
				} else if depth == bestDepth {
					ambiguous = true
				}
			}
			if bestDepth < 0 || ambiguous {
				continue
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			record, err := json.Marshal(struct {
				Repository        string `json:"repository"`
				ObservedRef       string `json:"observed_ref"`
				HeadOID           string `json:"head_oid"`
				FilePath          string `json:"file_path"`
				ManifestPath      string `json:"manifest_path"`
				FileEvidenceID    string `json:"file_evidence_id"`
				ManifestComponent string `json:"manifest_component_id"`
			}{r.Name, journal.Observation.Ref, pr.HeadSHA, file.Path, best.Path, fileLink.EvidenceID, best.ID})
			if err != nil {
				return fault("ERR_AUDIT_FAILED", "vínculo de arquivo inválido")
			}
			e, err := c.evidence("git", r.Name+":"+file.Path+"@"+pr.HeadSHA+":component", journal.ObservedAt, record)
			if err != nil {
				return err
			}
			upsertEvidence(d, e)
			linkID := "lnk-" + storage.Hash([]byte(file.ID + ":maps_to_component:" + best.ID + ":" + e.ID))[:24]
			upsertLink(d, contract.LinkRecord{ID: linkID, SourceID: file.ID, Relation: "maps_to_component", TargetID: best.ID, Status: "direct", EvidenceID: e.ID, ValidFrom: journal.ObservedAt})
			mapped++
		}
	}
	d.Coverage = append(d.Coverage, c.coverage(resource, "file_components", "partial", mapped, "mapeamento direto só quando PR head coincide com ref observada e há um manifesto de diretório inequívoco"))
	return nil
}
