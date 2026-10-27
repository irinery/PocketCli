package core

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/gitmirror"
	"pocketcli/internal/investigation/query"
	"pocketcli/internal/investigation/storage"
)

type gitObserver interface {
	Observe(context.Context, string, []byte, gitmirror.RefState, int64) (gitmirror.Observation, error)
	ObserveHead(context.Context, string, []byte, gitmirror.HeadRef, gitmirror.RefState, int64) (gitmirror.Observation, error)
	WalkCommits(context.Context, string, func(gitmirror.Commit) (bool, error)) (bool, error)
	DeclaredRevertTarget(context.Context, string) (string, bool, error)
	EnsureTrees(context.Context, string, []byte, string, string, int64) (int64, error)
	ChangedFiles(context.Context, string, int) ([]gitmirror.ChangedFile, bool, error)
	WalkTree(context.Context, string, func(gitmirror.TreeEntry) (bool, error)) (bool, error)
	ListHeads(context.Context, string, []byte, int, int64) ([]gitmirror.HeadRef, bool, int64, error)
}

type gitMirrorFactory func(string) (gitObserver, error)

func newGitMirror(path string) (gitObserver, error) { return gitmirror.New(path) }

type gitRefEvidence struct {
	Repository    string `json:"repository"`
	Ref           string `json:"ref"`
	OldOID        string `json:"old_oid,omitempty"`
	NewOID        string `json:"new_oid"`
	TreeOID       string `json:"tree_oid"`
	Relationship  string `json:"relationship"`
	GraphComplete bool   `json:"graph_complete"`
	ObservedAt    string `json:"observed_at"`
}

type gitRunObservation struct {
	Observation gitmirror.Observation `json:"observation"`
	ObservedAt  string                `json:"observed_at"`
}

func (c *Core) collectGitRef(ctx context.Context, plan contract.Plan, r contract.Resource, d *query.Dataset, run *contract.Run) error {
	if c.collector.Secrets == nil {
		return fault("ERR_CREDENTIAL_REQUIRED", "credencial GitHub ausente")
	}
	var auth planAuth
	if err := c.store.Get("planauth", plan.ID, &auth); err != nil || auth.GitHub == "" {
		return fault("ERR_AUDIT_FAILED", "identidade Git do plano indisponível")
	}
	key := storage.Hash([]byte(plan.InvestigationID + ":" + r.ID + ":" + auth.GitHub))
	journalID := storage.Hash([]byte(plan.ID + ":" + r.ID + ":git_ref"))
	var journal gitRunObservation
	journalErr := c.store.Get("gitobservations", journalID, &journal)
	if journalErr != nil && !os.IsNotExist(journalErr) {
		return fault("ERR_AUDIT_FAILED", "journal Git corrompido")
	}
	if journalErr == nil && (journal.Observation.NewOID == "" || journal.Observation.Ref == "" || journal.ObservedAt == "") {
		return fault("ERR_AUDIT_FAILED", "journal Git inválido")
	}
	var prior gitmirror.RefState
	if err := c.store.Get("gitrefs", key, &prior); err != nil && !os.IsNotExist(err) {
		return fault("ERR_AUDIT_FAILED", "estado Git anterior corrompido")
	}
	if os.IsNotExist(journalErr) {
		root := filepath.Join(c.store.Root, "git", key)
		observer, err := c.gitMirror(root)
		if err != nil {
			if errors.Is(err, gitmirror.ErrUnavailable) {
				d.Coverage = append(d.Coverage, c.coverage(contract.Resource{Source: "git", ID: r.ID}, "git_refs", "unsupported", 0, "git não está disponível neste Agent"))
				d.Coverage = append(d.Coverage, c.coverage(contract.Resource{Source: "git", ID: r.ID}, "code_history", "unsupported", 0, "espelho Git indisponível"))
				return nil
			}
			return fault("ERR_AUDIT_FAILED", "espelho Git privado indisponível")
		}
		usage, err := gitmirror.StorageBytes(c.store.Root)
		if err != nil {
			return fault("ERR_AUDIT_FAILED", "uso local indisponível")
		}
		if usage > plan.Input.Limits.MaxLocalBytes || run.Bytes > plan.Input.Limits.MaxTransferBytes {
			return fault("ERR_RESOURCE_EXHAUSTED", "orçamento Git já atingido")
		}
		budget := plan.Input.Limits.MaxTransferBytes - run.Bytes
		if remaining := plan.Input.Limits.MaxLocalBytes - usage; remaining < budget {
			budget = remaining
		}
		remote := "https://github.com/" + r.Name + ".git"
		var observation gitmirror.Observation
		err = c.collector.Secrets.WithSecret(ctx, "github", func(secret []byte) error {
			var fetchErr error
			observation, fetchErr = observer.Observe(ctx, remote, secret, prior, budget)
			return fetchErr
		})
		run.Bytes += observation.LocalBytes
		if !c.saveRun(run) {
			return fault("ERR_AUDIT_FAILED", "falha ao auditar transferência Git")
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errors.Is(err, gitmirror.ErrBudget) || run.Bytes > plan.Input.Limits.MaxTransferBytes {
			return fault("ERR_RESOURCE_EXHAUSTED", "orçamento de transferência Git atingido")
		}
		if errors.Is(err, gitmirror.ErrCorrupt) {
			return fault("ERR_AUDIT_FAILED", "espelho Git corrompido")
		}
		if err != nil {
			d.Coverage = append(d.Coverage, c.coverage(contract.Resource{Source: "git", ID: r.ID}, "git_refs", "partial", 0, "fetch Git não concluído; nenhuma divergência inferida"))
			d.Coverage = append(d.Coverage, c.coverage(contract.Resource{Source: "git", ID: r.ID}, "code_history", "partial", 0, "grafo Git indisponível"))
			return nil
		}
		journal = gitRunObservation{Observation: observation, ObservedAt: c.setup.now().UTC().Format(time.RFC3339)}
		if err := c.store.Put("gitobservations", journalID, journal); err != nil {
			return fault("ERR_AUDIT_FAILED", "falha ao persistir observação Git")
		}
	}
	observation := journal.Observation
	now := journal.ObservedAt
	record := gitRefEvidence{Repository: r.Name, Ref: observation.Ref, OldOID: observation.OldOID, NewOID: observation.NewOID, TreeOID: observation.TreeOID, Relationship: observation.Relationship, GraphComplete: observation.GraphComplete, ObservedAt: now}
	raw, err := json.Marshal(record)
	if err != nil {
		return fault("ERR_AUDIT_FAILED", "observação Git inválida")
	}
	e, err := c.evidence("git", r.Name+":"+observation.Ref, now, raw)
	if err != nil {
		return err
	}
	upsertEvidence(d, e)
	if observation.Relationship != "unchanged" {
		event := query.Event{ID: "evt-" + storage.Hash([]byte(r.ID + ":" + observation.Ref + ":" + observation.NewOID))[:24], EntityID: "repo-" + r.ID, Type: "ref_" + observation.Relationship + "_observed", EventAt: now, ObservedAt: now, OldValue: observation.OldOID, NewValue: observation.NewOID, EvidenceID: e.ID}
		upsertEvent(d, event)
	}
	if err := c.store.Put("gitrefs", key, gitmirror.RefState{Ref: observation.Ref, OID: observation.NewOID}); err != nil {
		return fault("ERR_AUDIT_FAILED", "falha ao persistir ref Git")
	}
	run.Items++
	if !c.saveRun(run) {
		return fault("ERR_AUDIT_FAILED", "falha ao auditar ref Git")
	}
	status, reason := "complete", "ref atual e ancestralidade observadas; sem inferir rebase"
	if observation.Relationship == "unknown" || !observation.GraphComplete {
		status, reason = "partial", "grafo Git incompleto; ancestralidade não conclusiva"
	}
	gitResource := contract.Resource{Source: "git", ID: r.ID}
	d.Coverage = append(d.Coverage, c.coverage(gitResource, "git_refs", status, 1, reason))
	if err := c.collectManifestPaths(ctx, plan, r, observation.TreeOID, auth.GitHub, now, d, run); err != nil {
		return err
	}
	d.Coverage = append(d.Coverage, c.coverage(gitResource, "code_history", "partial", 1, "ref e manifestos atuais observados; cobertura final depende das filas de commits, arquivos e heads de PR"))
	return nil
}
