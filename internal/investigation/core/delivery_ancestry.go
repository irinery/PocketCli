package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/gitmirror"
	"pocketcli/internal/investigation/query"
	"pocketcli/internal/investigation/storage"
)

const deliveryAncestryPairLimit = 100

type deliveryAncestryPair struct {
	Delivery contract.DeliveryRecord
	PR       query.PullRequest
	Repo     contract.Resource
}

// mapDeliveryAncestry checks only objects already in the private Git mirror.
// It never fetches an artifact revision just to force a conclusion.
func (c *Core) mapDeliveryAncestry(ctx context.Context, plan contract.Plan, d *query.Dataset, run *contract.Run) error {
	repositories := make(map[string]contract.Resource)
	for _, resource := range plan.Resources {
		if resource.Source == "github" {
			repositories[strings.ToLower(resource.Name)] = resource
		}
	}
	componentRepo := make(map[string]string)
	for _, component := range d.Components {
		componentRepo[component.ID] = component.Repository
	}
	issues := make(map[string]query.Issue)
	for _, issue := range d.Issues {
		issues[issue.ID] = issue
	}
	prs := make(map[string]query.PullRequest)
	for _, pr := range d.PullRequests {
		prs[pr.ID] = pr
	}
	corrections := make(map[string]bool)
	modifications := make(map[string]map[string]bool)
	for _, link := range d.Links {
		if link.Status != "direct" || link.EvidenceID == "" {
			continue
		}
		if link.Relation == "declares_fix_of" {
			if issue, ok := issues[link.TargetID]; ok && !issue.IsContext {
				corrections[link.SourceID] = true
			}
		}
		if link.Relation == "modifies_component" {
			if modifications[link.TargetID] == nil {
				modifications[link.TargetID] = make(map[string]bool)
			}
			modifications[link.TargetID][link.SourceID] = true
		}
	}
	pairsByRepo := make(map[string][]deliveryAncestryPair)
	seen := make(map[string]bool)
	for _, delivery := range d.Deliveries {
		repoName := componentRepo[delivery.ComponentID]
		repo, repoFound := repositories[strings.ToLower(repoName)]
		if !repoFound || delivery.RevisionSHA == "" || delivery.EvidenceID == "" ||
			(delivery.ProvenanceStatus != "declared" && delivery.ProvenanceStatus != "observed") ||
			!timestampInWindow(plan.WindowStart, plan.WindowEnd, delivery.ObservedAt) {
			continue
		}
		observedAt, _ := time.Parse(time.RFC3339, delivery.ObservedAt)
		for prID := range modifications[delivery.ComponentID] {
			pr, ok := prs[prID]
			if !ok || !corrections[prID] || !strings.EqualFold(pr.Repository, repo.Name) || pr.MergeSHA == "" ||
				strings.EqualFold(pr.MergeSHA, delivery.RevisionSHA) || pr.EvidenceID == "" ||
				!timestampInWindow(plan.WindowStart, plan.WindowEnd, pr.MergedAt) {
				continue
			}
			mergedAt, _ := time.Parse(time.RFC3339, pr.MergedAt)
			if observedAt.Before(mergedAt) {
				continue
			}
			key := delivery.ID + ":" + pr.ID
			if seen[key] {
				continue
			}
			seen[key] = true
			pairsByRepo[repo.ID] = append(pairsByRepo[repo.ID], deliveryAncestryPair{Delivery: delivery, PR: pr, Repo: repo})
		}
	}
	if len(pairsByRepo) == 0 {
		return nil
	}
	repoIDs := make([]string, 0, len(pairsByRepo))
	for id := range pairsByRepo {
		repoIDs = append(repoIDs, id)
	}
	sort.Strings(repoIDs)
	if !plan.Input.Modules.CodeHistory {
		for _, repoID := range repoIDs {
			d.Coverage = append(d.Coverage, c.coverage(contract.Resource{Source: "git", ID: repoID}, "delivery_ancestry", "not_selected", 0, "histórico Git não selecionado para verificar a revisão do artefato"))
		}
		return nil
	}
	var auth planAuth
	if err := c.store.Get("planauth", plan.ID, &auth); err != nil || auth.GitHub == "" {
		return fault("ERR_AUDIT_FAILED", "identidade Git para ancestralidade indisponível")
	}
	for _, repoID := range repoIDs {
		pairs := pairsByRepo[repoID]
		sort.Slice(pairs, func(i, j int) bool {
			if pairs[i].Delivery.ID != pairs[j].Delivery.ID {
				return pairs[i].Delivery.ID < pairs[j].Delivery.ID
			}
			return pairs[i].PR.ID < pairs[j].PR.ID
		})
		repo := pairs[0].Repo
		status, reason := "complete", "ancestralidade dos commits presentes no espelho Git verificada"
		if len(pairs) > deliveryAncestryPairLimit {
			status, reason = "partial", fmt.Sprintf("fila de ancestralidade: %d pares pendentes", len(pairs)-deliveryAncestryPairLimit)
			pairs = pairs[:deliveryAncestryPairLimit]
		}
		key := storage.Hash([]byte(plan.InvestigationID + ":" + repo.ID + ":" + auth.GitHub))
		observer, err := c.gitMirror(filepath.Join(c.store.Root, "git", key))
		if errors.Is(err, gitmirror.ErrUnavailable) {
			d.Coverage = append(d.Coverage, c.coverage(contract.Resource{Source: "git", ID: repo.ID}, "delivery_ancestry", "unsupported", 0, "Git indisponível no Agent"))
			continue
		}
		if err != nil {
			return fault("ERR_AUDIT_FAILED", "espelho Git de ancestralidade indisponível")
		}
		var observed int64
		for _, pair := range pairs {
			if err := ctx.Err(); err != nil {
				return err
			}
			if !validGitOID(pair.PR.MergeSHA) || !validGitOID(pair.Delivery.RevisionSHA) {
				status, reason = "partial", "SHA de PR ou artefato inválido para teste de ancestralidade"
				continue
			}
			included, known, err := observer.IsAncestor(ctx, pair.PR.MergeSHA, pair.Delivery.RevisionSHA)
			if err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return fault("ERR_AUDIT_FAILED", "falha ao verificar ancestralidade Git")
			}
			if !known {
				status, reason = "partial", "commit ausente ou grafo Git raso; ancestralidade inconclusiva"
				continue
			}
			result, relation := "not_ancestor", "merge_commit_not_ancestor"
			if included {
				result, relation = "ancestor", "contains_merge_commit"
			}
			raw, err := json.Marshal(struct {
				Repository  string `json:"repository"`
				ArtifactID  string `json:"artifact_id"`
				PRID        string `json:"pr_id"`
				MergeSHA    string `json:"merge_sha"`
				RevisionSHA string `json:"revision_sha"`
				Result      string `json:"result"`
			}{repo.Name, pair.Delivery.ID, pair.PR.ID, pair.PR.MergeSHA, pair.Delivery.RevisionSHA, result})
			if err != nil {
				return fault("ERR_AUDIT_FAILED", "resultado Git inválido")
			}
			evidence, err := c.evidence("git", fmt.Sprintf("%s:delivery-ancestry:%s:%s:%s", repo.Name, pair.Delivery.ID, pair.PR.ID, pair.Delivery.RevisionSHA), pair.Delivery.ObservedAt, raw)
			if err != nil {
				return err
			}
			upsertEvidence(d, evidence)
			linkID := "link-" + storage.Hash([]byte(pair.Delivery.ID + ":" + relation + ":" + pair.PR.ID + ":" + pair.Delivery.RevisionSHA))[:24]
			upsertLink(d, contract.LinkRecord{ID: linkID, SourceID: pair.Delivery.ID, Relation: relation, TargetID: pair.PR.ID, Status: "direct", EvidenceID: evidence.ID, ValidFrom: pair.Delivery.ObservedAt})
			observed++
			run.Items++
			if !c.saveRun(run) {
				return fault("ERR_AUDIT_FAILED", "falha ao auditar ancestralidade Git")
			}
		}
		d.Coverage = append(d.Coverage, c.coverage(contract.Resource{Source: "git", ID: repo.ID}, "delivery_ancestry", status, observed, reason))
	}
	return nil
}
