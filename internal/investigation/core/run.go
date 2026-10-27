package core

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"pocketcli/internal/investigation/collect"
	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/query"
	"pocketcli/internal/investigation/rules"
	"pocketcli/internal/investigation/storage"
)

type runKey struct{ RunID, PlanID, Hash string }

func (c *Core) StartRun(ctx context.Context, req contract.RunRequest) (contract.Run, error) {
	var plan contract.Plan
	if err := c.store.Get("plans", req.PlanID, &plan); err != nil {
		return contract.Run{}, fault("ERR_NOT_FOUND", "plano não encontrado")
	}
	if req.ConfirmedPlanHash == "" {
		return contract.Run{}, fault("ERR_CONFIRMATION_REQUIRED", "confirme o hash do plano")
	}
	if req.ConfirmedPlanHash != plan.Hash {
		return contract.Run{}, fault("ERR_PLAN_CHANGED", "plano alterado desde a confirmação")
	}
	if req.IdempotencyKey == "" {
		return contract.Run{}, fault("ERR_VALIDATION", "idempotency_key obrigatória")
	}
	if !c.query.Available() {
		return contract.Run{}, fault("ERR_UNAVAILABLE", "motor completo indisponível; compile com -tags investigate_sqlite e CGO_ENABLED=1")
	}
	if err := c.checkPlanAuth(ctx, plan); err != nil {
		return contract.Run{}, err
	}
	keyHash := storage.Hash([]byte(req.IdempotencyKey))
	var existing runKey
	if err := c.store.Get("runkeys", keyHash, &existing); err == nil {
		if existing.PlanID != req.PlanID || existing.Hash != req.ConfirmedPlanHash {
			return contract.Run{}, fault("ERR_PLAN_CHANGED", "chave de idempotência usada para outro plano")
		}
		var run contract.Run
		if err := c.store.Get("runs", existing.RunID, &run); err == nil {
			return run, nil
		} else if !os.IsNotExist(err) {
			return contract.Run{}, fault("ERR_AUDIT_FAILED", "run de idempotência corrompido")
		}
	} else if !os.IsNotExist(err) {
		return contract.Run{}, fault("ERR_AUDIT_FAILED", "chave de idempotência corrompida")
	}
	release, err := c.store.Lock(plan.InvestigationID)
	if err != nil {
		if c.store.Get("runkeys", keyHash, &existing) == nil && existing.PlanID == req.PlanID && existing.Hash == req.ConfirmedPlanHash {
			var run contract.Run
			if c.store.Get("runs", existing.RunID, &run) == nil {
				return run, nil
			}
		}
		return contract.Run{}, fault("ERR_AUDIT_FAILED", "outra escrita da investigação está ativa")
	}
	var id string
	if err := c.store.Get("runkeys", keyHash, &existing); err == nil {
		if existing.PlanID != req.PlanID || existing.Hash != req.ConfirmedPlanHash {
			release()
			return contract.Run{}, fault("ERR_PLAN_CHANGED", "chave de idempotência usada para outro plano")
		}
		var run contract.Run
		if err := c.store.Get("runs", existing.RunID, &run); err == nil {
			release()
			return run, nil
		} else if !os.IsNotExist(err) {
			release()
			return contract.Run{}, fault("ERR_AUDIT_FAILED", "run de idempotência corrompido")
		}
		id = existing.RunID
		if id == "" {
			release()
			return contract.Run{}, fault("ERR_AUDIT_FAILED", "chave de idempotência sem run")
		}
	} else if !os.IsNotExist(err) {
		release()
		return contract.Run{}, fault("ERR_AUDIT_FAILED", "chave de idempotência corrompida")
	} else {
		id, err = storage.ID()
		if err != nil {
			release()
			return contract.Run{}, err
		}
		if err := c.store.Put("runkeys", keyHash, runKey{RunID: id, PlanID: req.PlanID, Hash: req.ConfirmedPlanHash}); err != nil {
			release()
			return contract.Run{}, fault("ERR_AUDIT_FAILED", "falha ao persistir idempotência")
		}
	}
	run := contract.Run{ID: id, PlanID: plan.ID, State: "queued", Stage: "queued", Coverage: "pending"}
	if err := c.store.Put("runs", id, run); err != nil {
		release()
		return contract.Run{}, fault("ERR_AUDIT_FAILED", "falha ao criar run")
	}
	runCtx, cancel := context.WithTimeout(context.Background(), time.Duration(plan.Input.Limits.MaxSessionMS)*time.Millisecond)
	c.mu.Lock()
	c.runs[id] = cancel
	c.locks[id] = release
	c.mu.Unlock()
	go c.execute(runCtx, run, plan)
	return run, nil
}

func (c *Core) execute(ctx context.Context, run contract.Run, plan contract.Plan) {
	ctx = collect.WithRunID(ctx, run.ID)
	// A CLI start/resume loads the prepared plan in a fresh process. Restore
	// its pacing before any collector request, including on resumed runs.
	c.collector.SetRPH("github", plan.Input.Limits.GitHubRPH)
	c.collector.SetRPH("linear", plan.Input.Limits.LinearRPH)
	defer func() {
		c.mu.Lock()
		if cancel := c.runs[run.ID]; cancel != nil {
			cancel()
		}
		delete(c.runs, run.ID)
		if release := c.locks[run.ID]; release != nil {
			release()
			delete(c.locks, run.ID)
		}
		c.mu.Unlock()
	}()
	d, hasBuild, err := c.loadRunBuild(run.ID, plan)
	if err != nil {
		run.State = "failed"
		run.LastError = &contract.ErrorResponse{Code: "ERR_AUDIT_FAILED", Message: "build de run indisponível"}
		c.saveRun(&run)
		return
	}
	if !hasBuild {
		started := c.setup.now().UTC().Format(time.RFC3339)
		run.State = "collecting"
		run.Stage = "collecting"
		run.Items = 0 // Reconstructed from persisted pages on resume.
		if !c.saveRun(&run) {
			return
		}
		d, err = c.collectDataset(ctx, plan, &run)
		if err == nil {
			err = ctx.Err()
		}
		if err != nil {
			var coded *contract.ErrorResponse
			if errors.As(err, &coded) {
				run.LastError = coded
			} else if ctx.Err() != nil {
				run.LastError = &contract.ErrorResponse{Code: "ERR_TIMEOUT", Message: "coleta interrompida"}
			} else {
				run.LastError = &contract.ErrorResponse{Code: "ERR_UNAVAILABLE", Message: "coleta incompleta"}
			}
			if ctx.Err() != nil || (run.LastError != nil && (run.LastError.Code == "ERR_RESOURCE_EXHAUSTED" || run.LastError.Code == "ERR_RATE_LIMITED")) {
				current, _ := c.GetRun(context.Background(), run.ID)
				if current.State == "cancelled" {
					run.State = "cancelled"
				} else {
					run.State = "paused"
				}
				run.Stage = "collecting"
			} else {
				run.State = "failed"
			}
			c.saveRun(&run)
			return
		}
		run.State = "building"
		run.Stage = "building"
		if !c.saveRun(&run) {
			return
		}
		d.Snapshot = contract.Snapshot{PlanID: plan.ID, PlanHash: plan.Hash, Resources: plan.Resources, Modules: plan.Input.Modules, ObservationStartedAt: started, ObservationFinishedAt: c.setup.now().UTC().Format(time.RFC3339), InvestigationID: plan.InvestigationID, SchemaVersion: contract.Version, RulesetVersion: rules.Version, MappingVersion: "1.17.0", QueryCatalogVersion: query.CatalogVersion, AnalysisReferenceAt: plan.WindowEnd, WindowStart: plan.WindowStart, WindowEnd: plan.WindowEnd, Coverage: d.Coverage, Status: "published"}
		canonical, marshalErr := json.Marshal(d)
		if marshalErr != nil {
			run.State = "failed"
			run.LastError = &contract.ErrorResponse{Code: "ERR_AUDIT_FAILED", Message: "dataset inválido"}
			c.saveRun(&run)
			return
		}
		d.Snapshot.DatasetHash = storage.Hash(canonical)
		d.Snapshot.ID = "snap-" + d.Snapshot.DatasetHash[:24]
		d.Snapshot.PublishedAt = c.setup.now().UTC().Format(time.RFC3339)
		if err := c.saveRunBuild(run.ID, plan, d); err != nil {
			run.State = "failed"
			run.LastError = &contract.ErrorResponse{Code: "ERR_AUDIT_FAILED", Message: "falha ao persistir build"}
			c.saveRun(&run)
			return
		}
	} else {
		run.State = "building"
		run.Stage = "building"
		if !c.saveRun(&run) {
			return
		}
	}
	if ctx.Err() != nil {
		current, _ := c.GetRun(context.Background(), run.ID)
		if current.State == "cancelled" {
			run.State = "cancelled"
		} else {
			run.State = "paused"
		}
		run.LastError = &contract.ErrorResponse{Code: "ERR_TIMEOUT", Message: "execução interrompida antes da publicação"}
		c.saveRun(&run)
		return
	}
	if err := c.ensureProjection(d); err != nil {
		run.State = "failed"
		run.LastError = &contract.ErrorResponse{Code: "ERR_AUDIT_FAILED", Message: "falha ao publicar projeção"}
		c.saveRun(&run)
		return
	}
	if err := c.putImmutable("datasets", d.Snapshot.ID, d); err != nil {
		run.State = "failed"
		run.LastError = &contract.ErrorResponse{Code: "ERR_AUDIT_FAILED", Message: "falha ao publicar dados"}
		c.saveRun(&run)
		return
	}
	if err := c.putImmutable("snapshots", d.Snapshot.ID, d.Snapshot); err != nil {
		run.State = "failed"
		run.LastError = &contract.ErrorResponse{Code: "ERR_AUDIT_FAILED", Message: "falha ao publicar snapshot"}
		c.saveRun(&run)
		return
	}
	run.SnapshotID = d.Snapshot.ID
	run.State = "analysis_ready"
	run.Stage = "analysis_ready"
	run.Coverage = coverageStatus(d.Coverage)
	if !c.saveRun(&run) {
		return
	}
	c.mu.Lock()
	finalizer := c.finalizer
	c.mu.Unlock()
	if finalizer == nil {
		run.State = "report_failed"
		run.LastError = &contract.ErrorResponse{Code: "ERR_PDF_DEPENDENCY", Message: "Finalizer PDF não instalado"}
		c.saveRun(&run)
		return
	}
	run.State = "reporting"
	run.Stage = "reporting"
	if !c.saveRun(&run) {
		return
	}
	artifact, err := finalizer.Finalize(ctx, contract.FinalizeRequest{SnapshotID: d.Snapshot.ID, AnnotationRevision: 0, IncludeApprovedEditorial: false})
	if ctx.Err() != nil {
		current, _ := c.GetRun(context.Background(), run.ID)
		if current.State == "cancelled" {
			run.State = "cancelled"
		} else {
			run.State = "report_failed"
		}
		run.LastError = &contract.ErrorResponse{Code: "ERR_PDF_FAILED", Message: "finalização PDF interrompida"}
		c.saveRun(&run)
		return
	}
	if err != nil || artifact.Status != "completed" || artifact.FindingCount != int64(len(d.Findings)) {
		run.State = "report_failed"
		run.LastError = &contract.ErrorResponse{Code: "ERR_PDF_FAILED", Message: "PDF não concluído ou incompleto"}
		c.saveRun(&run)
		return
	}
	if run.Coverage == "complete" {
		run.State = "completed"
	} else {
		run.State = "completed_partial"
	}
	run.Stage = "completed"
	c.saveRun(&run)
}

func coverageStatus(items []contract.Coverage) string {
	for _, x := range items {
		if x.Status != "complete" && x.Status != "not_selected" {
			return "partial"
		}
	}
	return "complete"
}
func (c *Core) saveRun(run *contract.Run) bool {
	if err := c.store.Put("runs", run.ID, run); err != nil {
		return false
	}
	return true
}

func (c *Core) GetRun(_ context.Context, id string) (contract.Run, error) {
	var r contract.Run
	if c.store.Get("runs", id, &r) != nil {
		return r, fault("ERR_NOT_FOUND", "run não encontrado")
	}
	return r, nil
}

func (c *Core) RunStats(ctx context.Context, id string) (collect.RunStats, error) {
	if _, err := c.GetRun(ctx, id); err != nil {
		return collect.RunStats{}, err
	}
	stats := collect.RunStats{RunID: id}
	err := c.store.Get("runstats", id, &stats)
	if os.IsNotExist(err) {
		return collect.RunStats{RunID: id}, nil
	}
	if err != nil || stats.RunID != id || stats.ReuseCounter < 0 || stats.GitObjectBytesAdded < 0 ||
		stats.GitRefBytesAdded < 0 || stats.GitBranchBytesAdded < 0 || stats.GitTreeBytesAdded < 0 ||
		stats.GitRefBytesAdded > stats.GitObjectBytesAdded ||
		stats.GitBranchBytesAdded > stats.GitObjectBytesAdded-stats.GitRefBytesAdded ||
		stats.GitTreeBytesAdded != stats.GitObjectBytesAdded-stats.GitRefBytesAdded-stats.GitBranchBytesAdded {
		return collect.RunStats{}, fault("ERR_AUDIT_FAILED", "estatística de cache indisponível")
	}
	return stats, nil
}

// CompleteReport reconciles a successful local PDF retry with a run whose
// snapshot was already published. It never starts collection or changes the
// snapshot; later editorial revisions leave completed runs unchanged.
func (c *Core) CompleteReport(ctx context.Context, artifact contract.ReportArtifact) error {
	if artifact.Status != "completed" || artifact.ID == "" || artifact.SnapshotID == "" {
		return fault("ERR_REPORT_INCOMPLETE", "PDF não concluído")
	}
	snapshot, err := c.GetSnapshot(ctx, artifact.SnapshotID)
	if err != nil {
		return err
	}
	var d query.Dataset
	if err := c.store.Get("datasets", snapshot.ID, &d); err != nil {
		return fault("ERR_AUDIT_FAILED", "dataset do PDF indisponível")
	}
	if artifact.FindingCount != int64(len(d.Findings)) {
		return fault("ERR_REPORT_INCOMPLETE", "PDF não contém todos os achados")
	}
	release, err := c.store.Lock(snapshot.InvestigationID)
	if err != nil {
		return fault("ERR_AUDIT_FAILED", "outra escrita da investigação está ativa")
	}
	defer release()
	ids, err := c.store.List("runs")
	if err != nil {
		return fault("ERR_AUDIT_FAILED", "runs indisponíveis")
	}
	for _, id := range ids {
		var run contract.Run
		if err := c.store.Get("runs", id, &run); err != nil {
			return fault("ERR_AUDIT_FAILED", "run indisponível")
		}
		if run.SnapshotID != snapshot.ID || run.State != "report_failed" {
			continue
		}
		if run.Coverage == "complete" {
			run.State = "completed"
		} else {
			run.State = "completed_partial"
		}
		run.Stage = "completed"
		run.LastError = nil
		if err := c.store.Put("runs", id, run); err != nil {
			return fault("ERR_AUDIT_FAILED", "falha ao confirmar retry do PDF")
		}
	}
	return nil
}
func (c *Core) PauseRun(ctx context.Context, id string) (contract.Run, error) {
	r, err := c.GetRun(ctx, id)
	if err != nil {
		return r, err
	}
	switch r.State {
	case "queued", "collecting", "building":
		r.State = "paused"
	case "analysis_ready", "reporting":
		if r.SnapshotID == "" {
			return r, fault("ERR_AUDIT_FAILED", "snapshot publicado ausente")
		}
		r.State = "report_failed"
		r.LastError = &contract.ErrorResponse{Code: "ERR_PDF_FAILED", Message: "finalização interrompida; gere o relatório novamente"}
	default:
		return r, fault("ERR_VALIDATION", "run não está ativo para pausa")
	}
	c.mu.Lock()
	cancel := c.runs[id]
	c.mu.Unlock()
	if !c.saveRun(&r) {
		return r, fault("ERR_AUDIT_FAILED", "falha ao auditar pausa")
	}
	if cancel != nil {
		cancel()
	}
	return r, nil
}

// WaitRunStopped waits for the in-process worker to release its investigation
// lock after cancellation. The caller supplies its own shutdown deadline.
func (c *Core) WaitRunStopped(ctx context.Context, id string) error {
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		c.mu.Lock()
		_, active := c.runs[id]
		c.mu.Unlock()
		if !active {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (c *Core) ResumeRun(ctx context.Context, id string) (contract.Run, error) {
	r, err := c.GetRun(ctx, id)
	if err != nil {
		return r, err
	}
	if r.State != "paused" {
		return r, fault("ERR_VALIDATION", "run não está pausado")
	}
	var p contract.Plan
	if c.store.Get("plans", r.PlanID, &p) != nil {
		return r, fault("ERR_NOT_FOUND", "plano não encontrado")
	}
	if err := c.checkPlanAuth(ctx, p); err != nil {
		return r, err
	}
	var release func()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		release, err = c.store.Lock(p.InvestigationID)
		if err == nil {
			break
		}
		select {
		case <-ctx.Done():
			return r, ctx.Err()
		case <-deadline.C:
			return r, fault("ERR_AUDIT_FAILED", "outra escrita da investigação está ativa")
		case <-time.After(25 * time.Millisecond):
		}
	}
	r.State = "queued"
	if !c.saveRun(&r) {
		release()
		return r, fault("ERR_AUDIT_FAILED", "falha ao retomar")
	}
	runCtx, cancel := context.WithTimeout(context.Background(), time.Duration(p.Input.Limits.MaxSessionMS)*time.Millisecond)
	c.mu.Lock()
	c.runs[id] = cancel
	c.locks[id] = release
	c.mu.Unlock()
	go c.execute(runCtx, r, p)
	return r, nil
}
func (c *Core) CancelRun(ctx context.Context, id string) (contract.Run, error) {
	r, err := c.GetRun(ctx, id)
	if err != nil {
		return r, err
	}
	switch r.State {
	case "queued", "collecting", "building", "analysis_ready", "reporting", "paused":
	default:
		return r, fault("ERR_VALIDATION", "run não está ativo")
	}
	c.mu.Lock()
	cancel := c.runs[id]
	c.mu.Unlock()
	r.State = "cancelled"
	if !c.saveRun(&r) {
		return r, fault("ERR_AUDIT_FAILED", "falha ao auditar cancelamento")
	}
	if cancel != nil {
		cancel()
	}
	return r, nil
}
func (c *Core) ListSnapshots(_ context.Context, investigationID string) ([]contract.Snapshot, error) {
	ids, err := c.store.List("snapshots")
	if err != nil {
		return nil, err
	}
	out := make([]contract.Snapshot, 0)
	for _, id := range ids {
		var s contract.Snapshot
		if err := c.store.Get("snapshots", id, &s); err != nil {
			return nil, fault("ERR_AUDIT_FAILED", "snapshot local corrompido")
		}
		if s.InvestigationID == investigationID {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].PublishedAt == out[j].PublishedAt {
			return out[i].ID < out[j].ID
		}
		return out[i].PublishedAt < out[j].PublishedAt
	})
	return out, nil
}
func (c *Core) GetSnapshot(_ context.Context, id string) (contract.Snapshot, error) {
	var s contract.Snapshot
	if c.store.Get("snapshots", id, &s) != nil {
		return s, fault("ERR_NOT_FOUND", "snapshot não encontrado")
	}
	return s, nil
}
func (c *Core) Query(ctx context.Context, req contract.QueryRequest) (contract.QueryResult, error) {
	if _, err := c.GetSnapshot(ctx, req.SnapshotID); err != nil {
		return contract.QueryResult{}, err
	}
	return c.query.Execute(ctx, req.SnapshotID, req)
}
func (c *Core) SuggestedQueries(ctx context.Context, id string) ([]contract.SuggestedQuery, error) {
	if _, err := c.GetSnapshot(ctx, id); err != nil {
		return nil, err
	}
	return query.Catalog(), nil
}

func (c *Core) dataset(id string) (query.Dataset, error) {
	var d query.Dataset
	if err := c.store.Get("datasets", id, &d); err != nil {
		return d, fault("ERR_NOT_FOUND", "snapshot não encontrado")
	}
	return d, nil
}
func (c *Core) GetEvidence(_ context.Context, snapshotID, evidenceID string) (contract.Evidence, error) {
	d, err := c.dataset(snapshotID)
	if err != nil {
		return contract.Evidence{}, err
	}
	for _, e := range d.Evidence {
		if e.ID == evidenceID {
			return e, nil
		}
	}
	return contract.Evidence{}, fault("ERR_NOT_FOUND", "evidência não encontrada")
}
func (c *Core) GetFinding(_ context.Context, snapshotID, findingID string) (contract.Finding, error) {
	d, err := c.dataset(snapshotID)
	if err != nil {
		return contract.Finding{}, err
	}
	for _, f := range d.Findings {
		if f.ID == findingID {
			return f, nil
		}
	}
	return contract.Finding{}, fault("ERR_NOT_FOUND", "achado não encontrado")
}

func (c *Core) Findings(snapshotID string) ([]contract.Finding, error) {
	d, err := c.dataset(snapshotID)
	if err != nil {
		return nil, err
	}
	return append([]contract.Finding{}, d.Findings...), nil
}
func (c *Core) ReadReportPage(_ context.Context, snapshotID, cursor string, limit int) (contract.ReportPage, error) {
	d, err := c.dataset(snapshotID)
	if err != nil {
		return contract.ReportPage{}, err
	}
	if limit == 0 {
		limit = 200
	}
	if limit < 1 || limit > 1000 {
		return contract.ReportPage{}, fault("ERR_VALIDATION", "limit inválido")
	}
	items := make([]contract.ReportRecord, 0, len(d.Components)+len(d.Links)+len(d.Findings)+len(d.Evidence)+len(d.Coverage)+len(d.Metrics)+len(d.Stages)+len(d.Estimates)+len(d.Rework)+len(d.Deliveries))
	for _, x := range d.Components {
		v := x
		items = append(items, contract.ReportRecord{Kind: "component", Component: &v})
	}
	for _, x := range d.Links {
		v := x
		items = append(items, contract.ReportRecord{Kind: "link", Link: &v})
	}
	for _, x := range d.Findings {
		v := x
		items = append(items, contract.ReportRecord{Kind: "finding", Finding: &v})
	}
	for _, x := range d.Evidence {
		v := x
		items = append(items, contract.ReportRecord{Kind: "evidence", Evidence: &v})
	}
	for _, x := range d.Coverage {
		v := x
		items = append(items, contract.ReportRecord{Kind: "coverage", Coverage: &v})
	}
	for _, x := range d.Metrics {
		v := x
		items = append(items, contract.ReportRecord{Kind: "metric", Metric: &v})
	}
	for _, x := range d.Stages {
		v := x
		items = append(items, contract.ReportRecord{Kind: "stage", Stage: &v})
	}
	for _, x := range d.Estimates {
		v := x
		items = append(items, contract.ReportRecord{Kind: "estimate", Estimate: &v})
	}
	for _, x := range d.Rework {
		v := x
		items = append(items, contract.ReportRecord{Kind: "rework", Rework: &v})
	}
	for _, x := range d.Deliveries {
		v := x
		items = append(items, contract.ReportRecord{Kind: "delivery", Delivery: &v})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Kind == items[j].Kind {
			return reportRecordID(items[i]) < reportRecordID(items[j])
		}
		return items[i].Kind < items[j].Kind
	})
	start := 0
	if cursor != "" {
		decoded, e := base64.RawURLEncoding.DecodeString(cursor)
		if e != nil {
			return contract.ReportPage{}, fault("ERR_VALIDATION", "cursor inválido")
		}
		parts := strings.Split(string(decoded), ":")
		if len(parts) != 2 || parts[0] != snapshotID {
			return contract.ReportPage{}, fault("ERR_VALIDATION", "cursor de outro snapshot")
		}
		start, e = strconv.Atoi(parts[1])
		if e != nil || start < 0 || start > len(items) {
			return contract.ReportPage{}, fault("ERR_VALIDATION", "cursor inválido")
		}
	}
	end := start + limit
	if end > len(items) {
		end = len(items)
	}
	next := ""
	if end < len(items) {
		next = base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%d", snapshotID, end)))
	}
	return contract.ReportPage{SnapshotID: snapshotID, Records: items[start:end], NextCursor: next, TotalRecords: int64(len(items))}, nil
}

func reportRecordID(r contract.ReportRecord) string {
	switch r.Kind {
	case "component":
		return r.Component.ID
	case "link":
		return r.Link.ID
	case "finding":
		return r.Finding.ID
	case "evidence":
		return r.Evidence.ID
	case "coverage":
		return r.Coverage.Source + ":" + r.Coverage.ResourceID + ":" + r.Coverage.Collection
	case "metric":
		return r.Metric.ID
	case "stage":
		return r.Stage.ID
	case "estimate":
		return r.Estimate.ID
	case "rework":
		return r.Rework.ID
	case "delivery":
		return r.Delivery.ID
	}
	return ""
}

var _ contract.Core = (*Core)(nil)
var _ contract.ReportReader = (*Core)(nil)
