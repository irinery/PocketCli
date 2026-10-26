package main

import (
	"context"
	"encoding/json"
	"os"
	"sort"
	"sync"

	"pocketcli/internal/investigation/annotations"
	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/editorial"
	"pocketcli/internal/investigation/queries"
	"pocketcli/internal/investigation/storage"
	"pocketcli/internal/investigation/web"
)

type reportCache struct {
	inner     web.ReportService
	store     *storage.Store
	notes     *annotations.Store
	editorial *editorial.Service
	queries   *queries.Store
	mu        sync.Mutex
}

type reportKey struct{ ArtifactID string }

func (r *reportCache) Finalize(ctx context.Context, in contract.FinalizeRequest) (contract.ReportArtifact, error) {
	var empty contract.ReportArtifact
	if in.SnapshotID == "" {
		return empty, &contract.ErrorResponse{Code: "ERR_VALIDATION", Message: "snapshot_id obrigatório"}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	lock, err := r.store.Lock("report-" + in.SnapshotID)
	if err != nil {
		return empty, &contract.ErrorResponse{Code: "ERR_AUDIT_FAILED", Message: "outra geração de PDF está ativa"}
	}
	defer lock()
	annotationRows, _ := r.notes.At(in.SnapshotID, in.AnnotationRevision)
	sort.Slice(annotationRows, func(i, j int) bool { return annotationRows[i].ID < annotationRows[j].ID })
	var drafts []editorial.Draft
	if in.IncludeApprovedEditorial {
		drafts = r.editorial.Accepted(in.SnapshotID)
		sort.Slice(drafts, func(i, j int) bool { return drafts[i].ID < drafts[j].ID })
	}
	history := r.queries.AllHistory(in.SnapshotID)
	savedQueries := r.queries.List(in.SnapshotID)
	sort.Slice(history, func(i, j int) bool { return history[i].ID < history[j].ID })
	sort.Slice(savedQueries, func(i, j int) bool { return savedQueries[i].ID < savedQueries[j].ID })
	canonical, err := json.Marshal(struct {
		Request     contract.FinalizeRequest
		Annotations []annotations.Annotation
		Editorial   []editorial.Draft
		History     []queries.Execution
		Saved       []queries.SavedQuery
	}{in, annotationRows, drafts, history, savedQueries})
	if err != nil {
		return empty, err
	}
	key := storage.Hash(canonical)
	var saved reportKey
	if err := r.store.Get("reportkeys", key, &saved); err == nil {
		if artifact, err := r.inner.Get(saved.ArtifactID); err == nil && artifact.Status == "completed" {
			return artifact, nil
		}
	} else if !os.IsNotExist(err) {
		return empty, &contract.ErrorResponse{Code: "ERR_AUDIT_FAILED", Message: "índice do PDF corrompido"}
	}
	artifact, err := r.inner.Finalize(ctx, in)
	if err != nil || artifact.Status != "completed" {
		return artifact, err
	}
	if err := r.store.Put("reportkeys", key, reportKey{ArtifactID: artifact.ID}); err != nil {
		return empty, &contract.ErrorResponse{Code: "ERR_AUDIT_FAILED", Message: "falha ao confirmar PDF"}
	}
	return artifact, nil
}

func (r *reportCache) Get(id string) (contract.ReportArtifact, error) { return r.inner.Get(id) }
func (r *reportCache) Open(id string) (*os.File, error)               { return r.inner.Open(id) }
func (r *reportCache) List(snapshot string) []contract.ReportArtifact { return r.inner.List(snapshot) }

var _ web.ReportService = (*reportCache)(nil)
