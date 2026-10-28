package insights

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"pocketcli/internal/investigation/insightcontract"
	"pocketcli/internal/investigation/storage"
)

type FactsFunc func(insightcontract.SourceBundle, insightcontract.AnalysisConfig) (insightcontract.FactsResult, error)
type DecisionsFunc func(context.Context, insightcontract.SourceBundle, insightcontract.FactsResult, insightcontract.AnalysisConfig, insightcontract.ReasoningProvider) (insightcontract.DecisionAnalysis, error)
type PresentationFunc func(insightcontract.SourceBundle, insightcontract.FactsResult, insightcontract.DecisionAnalysis, insightcontract.AnalysisConfig) (insightcontract.ExecutiveReport, error)

type Dependencies struct {
	Source       insightcontract.SourceReader
	Facts        FactsFunc
	Decisions    DecisionsFunc
	Presentation PresentationFunc
	Provider     insightcontract.ReasoningProvider
	Store        *storage.Store
	Now          func() time.Time
}

type analysisArtifacts struct {
	ID         string                           `json:"id"`
	Snapshot   string                           `json:"snapshot_id"`
	Config     insightcontract.AnalysisConfig   `json:"config"`
	ConfigHash string                           `json:"config_hash"`
	Source     insightcontract.SourceBundle     `json:"source"`
	Facts      insightcontract.FactsResult      `json:"facts"`
	Decisions  insightcontract.DecisionAnalysis `json:"decisions"`
	Report     insightcontract.ExecutiveReport  `json:"report"`
	CreatedAt  string                           `json:"created_at"`
}

type idempotencyRecord struct {
	RequestHash string `json:"request_hash"`
	RunID       string `json:"run_id"`
}

type Service struct {
	deps     Dependencies
	startMu  sync.Mutex
	mu       sync.Mutex
	stopping bool
	jobs     map[string]context.CancelFunc
	wg       sync.WaitGroup
}

func NewService(deps Dependencies) (*Service, error) {
	if deps.Source == nil || deps.Facts == nil || deps.Decisions == nil || deps.Presentation == nil || deps.Store == nil {
		return nil, errors.New("insights: source, callbacks e storage são obrigatórios")
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}
	service := &Service{deps: deps, jobs: make(map[string]context.CancelFunc)}
	if err := service.recoverInterrupted(); err != nil {
		return nil, err
	}
	return service, nil
}

func (s *Service) recoverInterrupted() error {
	ids, err := s.deps.Store.List("insightruns")
	if err != nil {
		return err
	}
	for _, id := range ids {
		var run insightcontract.AnalysisRun
		if err := s.deps.Store.Get("insightruns", id, &run); err != nil {
			return err
		}
		if run.State != "queued" && run.State != "running" {
			continue
		}
		run.State, run.Phase = "failed", "idle"
		run.ErrorCode = stringPointer("ERR_INTERRUPTED_NO_REPLAY")
		if err := s.deps.Store.Put("insightruns", id, run); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) StartAnalysis(ctx context.Context, req insightcontract.AnalysisRequest) (insightcontract.AnalysisRun, error) {
	if err := ctx.Err(); err != nil {
		return insightcontract.AnalysisRun{}, err
	}
	// Serialize idempotency lookup and creation within this service. Store.Lock
	// protects the same key across processes; this mutex makes concurrent calls
	// in one process observe the record created by the first caller.
	s.startMu.Lock()
	defer s.startMu.Unlock()
	if req.SnapshotID == "" || req.IdempotencyKey == "" {
		return insightcontract.AnalysisRun{}, errors.New("ERR_VALIDATION: snapshot_id e idempotency_key são obrigatórios")
	}
	if err := validateConfig(req.Config); err != nil {
		return insightcontract.AnalysisRun{}, err
	}
	configBytes, err := json.Marshal(req.Config)
	if err != nil {
		return insightcontract.AnalysisRun{}, err
	}
	configHash := storage.Hash(configBytes)
	requestBytes, _ := json.Marshal(struct {
		Snapshot   string `json:"snapshot"`
		ConfigHash string `json:"config_hash"`
	}{req.SnapshotID, configHash})
	requestHash := storage.Hash(requestBytes)
	keyID := storage.Hash([]byte(req.IdempotencyKey))
	var release func()
	for {
		release, err = s.deps.Store.Lock("insightkey-" + keyID)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			return insightcontract.AnalysisRun{}, err
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return insightcontract.AnalysisRun{}, ctx.Err()
		case <-timer.C:
		}
	}
	defer release()
	var previous idempotencyRecord
	err = s.deps.Store.Get("insightkeys", keyID, &previous)
	if err == nil {
		if previous.RequestHash != requestHash {
			return insightcontract.AnalysisRun{}, errors.New("ERR_IDEMPOTENCY_CONFLICT: chave reutilizada com request diferente")
		}
		return s.GetAnalysisRun(ctx, previous.RunID)
	}
	if !os.IsNotExist(err) {
		return insightcontract.AnalysisRun{}, err
	}
	if err := s.validateSnapshot(req.SnapshotID, req.Config.RequestedScope); err != nil {
		return insightcontract.AnalysisRun{}, err
	}
	runID, err := storage.ID()
	if err != nil {
		return insightcontract.AnalysisRun{}, err
	}
	run := insightcontract.AnalysisRun{ID: runID, SnapshotID: req.SnapshotID, ConfigHash: configHash, State: "queued", Phase: "source"}
	if err := s.deps.Store.Put("insightruns", runID, run); err != nil {
		return insightcontract.AnalysisRun{}, err
	}
	if err := s.putImmutable("insightkeys", keyID, idempotencyRecord{RequestHash: requestHash, RunID: runID}); err != nil {
		return insightcontract.AnalysisRun{}, err
	}
	jobCtx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	if s.stopping {
		s.mu.Unlock()
		cancel()
		run.State, run.Phase = "failed", "idle"
		run.ErrorCode = stringPointer("ERR_SERVICE_SHUTDOWN")
		_ = s.deps.Store.Put("insightruns", run.ID, run)
		return run, errors.New("ERR_UNAVAILABLE: serviço encerrando")
	}
	s.jobs[runID] = cancel
	s.wg.Add(1)
	s.mu.Unlock()
	go func() {
		defer s.wg.Done()
		defer cancel()
		defer func() { s.mu.Lock(); delete(s.jobs, runID); s.mu.Unlock() }()
		s.execute(jobCtx, run, req.Config)
	}()
	return run, nil
}

// Store snapshot validation is injected through the store's bound core data namespace:
// the snapshot record is written alongside each published dataset.

func (s *Service) validateSnapshot(snapshotID string, scope insightcontract.Scope) error {
	var snap struct {
		ID        string `json:"id"`
		Resources []struct {
			ID     string `json:"id"`
			Source string `json:"source"`
			Name   string `json:"name"`
		} `json:"resources"`
	}
	if err := s.deps.Store.Get("snapshots", snapshotID, &snap); err != nil || snap.ID != snapshotID {
		return errors.New("ERR_NOT_FOUND: snapshot não encontrado")
	}
	for _, requested := range scope.Resources {
		found := false
		for _, observed := range snap.Resources {
			if requested.Source == observed.Source && (requested.ID == observed.ID || requested.ID == observed.Name || requested.Name == observed.ID || requested.Name == observed.Name) {
				found = true
				break
			}
		}
		if !found {
			return errors.New("ERR_SCOPE_FORBIDDEN: recurso solicitado não pertence ao snapshot")
		}
	}
	return nil
}

func validateConfig(cfg insightcontract.AnalysisConfig) error {
	if cfg.Version != insightcontract.Version {
		return errors.New("ERR_SCHEMA_VERSION_UNSUPPORTED: config.version incompatível")
	}
	if cfg.ID == "" || cfg.RequestedScope.History.Start == "" || cfg.RequestedScope.History.End == "" || cfg.RequestedScope.ReportWindow.Start == "" || cfg.RequestedScope.ReportWindow.End == "" {
		return errors.New("ERR_VALIDATION: config sem id, history ou report_window completos")
	}
	if cfg.Policy.ContentMode != "metadata_only" && cfg.Policy.ContentMode != "local_excerpts" && cfg.Policy.ContentMode != "export_excerpts" {
		return errors.New("ERR_VALIDATION: policy.content_mode inválido")
	}
	if cfg.Provider.Kind != "none" && cfg.Provider.Kind != "configured" {
		return errors.New("ERR_VALIDATION: provider.kind inválido")
	}
	if cfg.Provider.Kind == "configured" {
		if cfg.Provider.Model == "" || cfg.Provider.AdapterVersion == "" {
			return errors.New("ERR_VALIDATION: provider configurado exige model e adapter_version")
		}
	}
	if err := validateWindow(cfg.RequestedScope.History); err != nil {
		return fmt.Errorf("ERR_VALIDATION: history: %w", err)
	}
	if err := validateWindow(cfg.RequestedScope.ReportWindow); err != nil {
		return fmt.Errorf("ERR_VALIDATION: report_window: %w", err)
	}
	seen := make(map[string]bool, len(cfg.RequestedScope.Resources))
	for _, resource := range cfg.RequestedScope.Resources {
		if resource.ID == "" || (resource.Source != "github" && resource.Source != "linear" && resource.Source != "git") {
			return errors.New("ERR_VALIDATION: recurso sem source/id válido")
		}
		key := resource.Source + ":" + resource.ID
		if seen[key] {
			return errors.New("ERR_VALIDATION: recurso duplicado no escopo")
		}
		seen[key] = true
	}
	return nil
}

func validateWindow(window insightcontract.Window) error {
	start, err := time.Parse(time.RFC3339, window.Start)
	if err != nil {
		return errors.New("start inválido")
	}
	end, err := time.Parse(time.RFC3339, window.End)
	if err != nil {
		return errors.New("end inválido")
	}
	if start.Location() != time.UTC || end.Location() != time.UTC {
		return errors.New("timestamps devem estar em UTC")
	}
	if !start.Before(end) {
		return errors.New("start deve anteceder end")
	}
	return nil
}

func (s *Service) execute(ctx context.Context, run insightcontract.AnalysisRun, cfg insightcontract.AnalysisConfig) {
	set := func(state, phase string, code *string) bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		if ctx.Err() != nil {
			return false
		}
		run.State, run.Phase, run.ErrorCode = state, phase, code
		return s.deps.Store.Put("insightruns", run.ID, run) == nil
	}
	if !set("running", "source", nil) {
		return
	}
	source, err := s.deps.Source.ReadInsightSource(ctx, run.SnapshotID, cfg)
	if err != nil {
		s.fail(&run, ctx, "ERR_SOURCE_FAILED")
		return
	}
	if source.Snapshot.ID != run.SnapshotID || source.Version != insightcontract.Version {
		s.fail(&run, ctx, "ERR_SOURCE_INVALID")
		return
	}
	if err := s.putImmutable("insightsources", run.ID, source); err != nil {
		s.fail(&run, ctx, "ERR_PERSIST_FAILED")
		return
	}
	if !set("running", "facts", nil) {
		return
	}
	facts, err := s.deps.Facts(source, cfg)
	if err != nil || (facts.SnapshotID != "" && facts.SnapshotID != run.SnapshotID) {
		s.fail(&run, ctx, "ERR_FACTS_FAILED")
		return
	}
	if facts.SnapshotID == "" {
		facts.SnapshotID = run.SnapshotID
	}
	if facts.InputHash == "" {
		facts.InputHash = source.InputHash
	}
	if facts.ConfigHash == "" {
		facts.ConfigHash = run.ConfigHash
	}
	if facts.InputHash != source.InputHash || facts.ConfigHash != run.ConfigHash {
		s.fail(&run, ctx, "ERR_FACTS_INVALID")
		return
	}
	if err := s.putImmutable("insightfacts", run.ID, facts); err != nil {
		s.fail(&run, ctx, "ERR_PERSIST_FAILED")
		return
	}
	if !set("running", "reasoning", nil) {
		return
	}
	provider := s.deps.Provider
	if cfg.Provider.Kind == "none" || !cfg.Policy.ExternalProcessingAllowed {
		provider = nil
	}
	if cfg.Provider.Kind == "configured" && cfg.Policy.ExternalProcessingAllowed && provider == nil {
		s.fail(&run, ctx, "ERR_PROVIDER_UNAVAILABLE")
		return
	}
	decisions, err := s.deps.Decisions(ctx, source, facts, cfg, provider)
	if err != nil || (decisions.SnapshotID != "" && decisions.SnapshotID != run.SnapshotID) {
		s.fail(&run, ctx, "ERR_REASONING_FAILED")
		return
	}
	if decisions.SnapshotID == "" {
		decisions.SnapshotID = run.SnapshotID
	}
	if decisions.Provenance.InputHash != "" && decisions.Provenance.InputHash != source.InputHash {
		s.fail(&run, ctx, "ERR_REASONING_INVALID")
		return
	}
	if err := s.putImmutable("insightdecisions", run.ID, decisions); err != nil {
		s.fail(&run, ctx, "ERR_PERSIST_FAILED")
		return
	}
	if !set("running", "presentation", nil) {
		return
	}
	report, err := s.deps.Presentation(source, facts, decisions, cfg)
	if err != nil || (report.SnapshotID != "" && report.SnapshotID != run.SnapshotID) {
		s.fail(&run, ctx, "ERR_PRESENTATION_FAILED")
		return
	}
	analysisID, err := storage.ID()
	if err != nil {
		s.fail(&run, ctx, "ERR_PERSIST_FAILED")
		return
	}
	report.Version, report.SnapshotID, report.AnalysisID = insightcontract.Version, run.SnapshotID, analysisID
	if report.PublicationStatus == "" {
		report.PublicationStatus = "ready"
	}
	if report.AnalysisStatus == "" {
		report.AnalysisStatus = decisions.Status
	}
	if report.RequestedScope.History.Start == "" && report.RequestedScope.ReportWindow.Start == "" && len(report.RequestedScope.Resources) == 0 {
		report.RequestedScope = cfg.RequestedScope
	}
	if report.Cases == nil {
		report.Cases = []insightcontract.DecisionCase{}
	}
	if report.CaseIDs == nil {
		report.CaseIDs = make([]string, 0, len(report.Cases))
		for _, item := range report.Cases {
			report.CaseIDs = append(report.CaseIDs, item.ID)
		}
	}
	if report.TotalCases == 0 {
		report.TotalCases = int64(len(decisions.Cases))
	}
	decisions.ID = analysisID
	if err := validateReferences(source, facts, decisions, report); err != nil {
		s.fail(&run, ctx, "ERR_ANALYSIS_REFERENCES_INVALID")
		return
	}
	if !set("running", "persisting", nil) {
		return
	}
	artifacts := analysisArtifacts{ID: analysisID, Snapshot: run.SnapshotID, Config: cfg, ConfigHash: run.ConfigHash, Source: source, Facts: facts, Decisions: decisions, Report: report, CreatedAt: s.deps.Now().UTC().Format(time.RFC3339)}
	// Serialize cancellation against publication. Once a cancellation wins this
	// lock, no analysis is published as complete; once publication wins, cancel
	// observes a terminal run and leaves it intact.
	s.mu.Lock()
	if ctx.Err() != nil {
		run.State, run.Phase = "cancelled", "idle"
		run.ErrorCode = stringPointer("ERR_CANCELLED")
		_ = s.deps.Store.Put("insightruns", run.ID, run)
		s.mu.Unlock()
		return
	}
	persistErr := s.putImmutable("insightanalyses", analysisID, artifacts)
	if persistErr == nil {
		persistErr = s.putImmutable("insightsummary", analysisID, insightcontract.AnalysisSummary{ID: analysisID, SnapshotID: run.SnapshotID, ConfigHash: run.ConfigHash, RequestedScope: cfg.RequestedScope, Status: report.AnalysisStatus, CreatedAt: artifacts.CreatedAt})
	}
	if persistErr != nil {
		run.State, run.Phase = "failed", "idle"
		run.ErrorCode = stringPointer("ERR_PERSIST_FAILED")
	} else {
		run.AnalysisID = stringPointer(analysisID)
		run.State = "completed"
		if report.AnalysisStatus == "partial" || report.AnalysisStatus == "not_run" || report.SufficiencyStatus == "insufficient" || report.SufficiencyStatus == "partial" {
			run.State = "completed_partial"
		}
		run.Phase, run.ErrorCode = "idle", nil
	}
	_ = s.deps.Store.Put("insightruns", run.ID, run)
	s.mu.Unlock()
}

// putImmutable persists a private artifact once. A repeated identical write is
// idempotent; a conflicting write fails closed instead of replacing history.
func (s *Service) putImmutable(kind, id string, value interface{}) error {
	release, err := s.deps.Store.Lock("insight-artifact-" + storage.Hash([]byte(kind+":"+id)))
	if err != nil {
		return err
	}
	defer release()
	expected, err := json.Marshal(value)
	if err != nil {
		return err
	}
	var existing json.RawMessage
	err = s.deps.Store.Get(kind, id, &existing)
	if err == nil {
		var compact bytes.Buffer
		if json.Compact(&compact, existing) != nil || !bytes.Equal(compact.Bytes(), expected) {
			return errors.New("ERR_AUDIT_FAILED: artefato imutável incompatível")
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return err
	}
	return s.deps.Store.Put(kind, id, value)
}

func (s *Service) fail(run *insightcontract.AnalysisRun, ctx context.Context, code string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var current insightcontract.AnalysisRun
	if err := s.deps.Store.Get("insightruns", run.ID, &current); err == nil && isTerminal(current.State) {
		return
	}
	run.Phase = "idle"
	if ctx.Err() != nil {
		run.State = "cancelled"
		code = "ERR_CANCELLED"
	} else {
		run.State = "failed"
	}
	run.ErrorCode = stringPointer(code)
	_ = s.deps.Store.Put("insightruns", run.ID, *run)
}

func (s *Service) GetAnalysisRun(ctx context.Context, runID string) (insightcontract.AnalysisRun, error) {
	if err := ctx.Err(); err != nil {
		return insightcontract.AnalysisRun{}, err
	}
	var run insightcontract.AnalysisRun
	if err := s.deps.Store.Get("insightruns", runID, &run); err != nil {
		return run, errors.New("ERR_NOT_FOUND: análise não encontrada")
	}
	return run, nil
}

func (s *Service) CancelAnalysis(ctx context.Context, runID string) (insightcontract.AnalysisRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	run, err := s.GetAnalysisRun(ctx, runID)
	if err != nil {
		return run, err
	}
	if isTerminal(run.State) {
		return run, nil
	}
	cancel := s.jobs[runID]
	if cancel != nil {
		cancel()
	}
	run.State, run.Phase = "cancelled", "idle"
	run.ErrorCode = stringPointer("ERR_CANCELLED")
	if err := s.deps.Store.Put("insightruns", runID, run); err != nil {
		return run, err
	}
	return run, nil
}

func (s *Service) ListAnalyses(ctx context.Context, snapshotID, cursor string, limit int) (insightcontract.AnalysisPage, error) {
	if err := ctx.Err(); err != nil {
		return insightcontract.AnalysisPage{}, err
	}
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 200 {
		return insightcontract.AnalysisPage{}, errors.New("ERR_VALIDATION: limit inválido")
	}
	ids, err := s.deps.Store.List("insightsummary")
	if err != nil {
		return insightcontract.AnalysisPage{}, err
	}
	items := make([]insightcontract.AnalysisSummary, 0)
	for _, id := range ids {
		var item insightcontract.AnalysisSummary
		if err := s.deps.Store.Get("insightsummary", id, &item); err != nil {
			return insightcontract.AnalysisPage{}, err
		}
		if item.SnapshotID == snapshotID {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt == items[j].CreatedAt {
			return items[i].ID < items[j].ID
		}
		return items[i].CreatedAt < items[j].CreatedAt
	})
	start, err := decodeCursor(cursor, snapshotID)
	if err != nil {
		return insightcontract.AnalysisPage{}, err
	}
	if start > len(items) {
		return insightcontract.AnalysisPage{}, errors.New("ERR_VALIDATION: cursor inválido")
	}
	end := start + limit
	if end > len(items) {
		end = len(items)
	}
	next := ""
	if end < len(items) {
		next = encodeCursor(snapshotID, end)
	}
	return insightcontract.AnalysisPage{SnapshotID: snapshotID, Total: int64(len(items)), Items: items[start:end], NextCursor: next}, nil
}

func (s *Service) GetExecutiveReport(ctx context.Context, snapshotID, analysisID string) (insightcontract.ExecutiveReport, error) {
	a, err := s.loadArtifacts(ctx, snapshotID, analysisID)
	if err != nil {
		return insightcontract.ExecutiveReport{}, err
	}
	return a.Report, nil
}

func (s *Service) ListInsightCases(ctx context.Context, snapshotID, analysisID, cursor string, limit int) (insightcontract.CasePage, error) {
	a, err := s.loadArtifacts(ctx, snapshotID, analysisID)
	if err != nil {
		return insightcontract.CasePage{}, err
	}
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 200 {
		return insightcontract.CasePage{}, errors.New("ERR_VALIDATION: limit inválido")
	}
	cases := append([]insightcontract.DecisionCase{}, a.Decisions.Cases...)
	sort.Slice(cases, func(i, j int) bool { return cases[i].ID < cases[j].ID })
	start, err := decodeCursor(cursor, snapshotID+":"+analysisID)
	if err != nil {
		return insightcontract.CasePage{}, err
	}
	if start > len(cases) {
		return insightcontract.CasePage{}, errors.New("ERR_VALIDATION: cursor inválido")
	}
	end := start + limit
	if end > len(cases) {
		end = len(cases)
	}
	next := ""
	if end < len(cases) {
		next = encodeCursor(snapshotID+":"+analysisID, end)
	}
	return insightcontract.CasePage{SnapshotID: snapshotID, AnalysisID: analysisID, Total: int64(len(cases)), Cases: cases[start:end], NextCursor: next}, nil
}

func (s *Service) GetInsightCase(ctx context.Context, snapshotID, analysisID, caseID string) (insightcontract.CaseDetail, error) {
	a, err := s.loadArtifacts(ctx, snapshotID, analysisID)
	if err != nil {
		return insightcontract.CaseDetail{}, err
	}
	var selected *insightcontract.DecisionCase
	for i := range a.Decisions.Cases {
		if a.Decisions.Cases[i].ID == caseID {
			selected = &a.Decisions.Cases[i]
			break
		}
	}
	if selected == nil {
		return insightcontract.CaseDetail{}, errors.New("ERR_NOT_FOUND: caso não encontrado")
	}
	detail := insightcontract.CaseDetail{SnapshotID: snapshotID, AnalysisID: analysisID, Case: *selected, Facts: []insightcontract.FactCase{}, Claims: []insightcontract.Claim{}, ClaimRelations: []insightcontract.ClaimRelation{}, Decisions: []insightcontract.DecisionRecord{}, Relations: []insightcontract.DecisionRelation{}, Entities: []insightcontract.Entity{}, Evidence: []insightcontract.EvidenceView{}}
	factIDs, claimIDs, decisionIDs, entityIDs, evidenceIDs := toSet(selected.FactCaseIDs), toSet(selected.ClaimIDs), toSet(selected.DecisionIDs), toSet(selected.EntityIDs), make(map[string]bool)
	for _, citation := range selected.Citations {
		evidenceIDs[citation.EvidenceID] = true
	}
	for _, fact := range a.Facts.Cases {
		if factIDs[fact.ID] {
			detail.Facts = append(detail.Facts, fact)
			for _, id := range fact.EvidenceIDs {
				evidenceIDs[id] = true
			}
		}
	}
	for _, claim := range a.Decisions.Claims {
		if claimIDs[claim.ID] {
			detail.Claims = append(detail.Claims, claim)
			for _, citation := range claim.Citations {
				evidenceIDs[citation.EvidenceID] = true
			}
		}
	}
	for _, decision := range a.Decisions.Decisions {
		if decisionIDs[decision.ID] {
			detail.Decisions = append(detail.Decisions, decision)
			for _, citation := range decision.Citations {
				evidenceIDs[citation.EvidenceID] = true
			}
		}
	}
	for _, relation := range a.Decisions.ClaimRelations {
		if claimIDs[relation.FromClaimID] || claimIDs[relation.ToClaimID] {
			detail.ClaimRelations = append(detail.ClaimRelations, relation)
		}
	}
	for _, relation := range a.Decisions.Relations {
		if decisionIDs[relation.FromDecisionID] || decisionIDs[relation.ToDecisionID] {
			detail.Relations = append(detail.Relations, relation)
		}
	}
	for _, entity := range a.Source.Entities {
		if entityIDs[entity.ID] {
			detail.Entities = append(detail.Entities, entity)
			for _, id := range entity.EvidenceIDs {
				evidenceIDs[id] = true
			}
		}
	}
	for _, evidence := range a.Source.Evidence {
		if evidenceIDs[evidence.ID] {
			detail.Evidence = append(detail.Evidence, evidence)
		}
	}
	return detail, nil
}

func (s *Service) loadArtifacts(ctx context.Context, snapshotID, analysisID string) (analysisArtifacts, error) {
	if err := ctx.Err(); err != nil {
		return analysisArtifacts{}, err
	}
	var a analysisArtifacts
	if err := s.deps.Store.Get("insightanalyses", analysisID, &a); err != nil || a.Snapshot != snapshotID || a.ID != analysisID {
		return a, errors.New("ERR_NOT_FOUND: análise não encontrada neste snapshot")
	}
	return a, nil
}

func (s *Service) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	s.stopping = true
	for _, cancel := range s.jobs {
		cancel()
	}
	s.mu.Unlock()
	done := make(chan struct{})
	go func() { s.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func isTerminal(state string) bool {
	return state == "completed" || state == "completed_partial" || state == "failed" || state == "cancelled"
}

func validateReferences(source insightcontract.SourceBundle, facts insightcontract.FactsResult, decisions insightcontract.DecisionAnalysis, report insightcontract.ExecutiveReport) error {
	entities, evidence, links, findings := map[string]bool{}, map[string]insightcontract.EvidenceView{}, map[string]bool{}, map[string]bool{}
	for _, entity := range source.Entities {
		if entity.ID == "" || entities[entity.ID] {
			return errors.New("duplicate/empty source entity id")
		}
		entities[entity.ID] = true
	}
	for _, item := range source.Evidence {
		if item.ID == "" || evidence[item.ID].ID != "" {
			return errors.New("duplicate/empty source evidence id")
		}
		evidence[item.ID] = item
	}
	for _, link := range source.Links {
		if link.ID == "" || links[link.ID] {
			return errors.New("duplicate/empty source link id")
		}
		links[link.ID] = true
	}
	for _, item := range source.LegacyFindings {
		if item.ID == "" || findings[item.ID] {
			return errors.New("duplicate/empty legacy finding id")
		}
		findings[item.ID] = true
	}
	checkEvidence := func(id string) bool {
		return id != "" && evidence[id].ID != "" && evidence[id].IntegrityStatus == "verified"
	}
	for _, item := range facts.Cases {
		for _, id := range item.SubjectIDs {
			if !entities[id] {
				return fmt.Errorf("fact %s references missing entity %s", item.ID, id)
			}
		}
		for _, id := range item.RelatedIDs {
			if !entities[id] {
				return fmt.Errorf("fact %s references missing related entity %s", item.ID, id)
			}
		}
		for _, id := range item.LinkIDs {
			if !links[id] {
				return fmt.Errorf("fact %s references missing link %s", item.ID, id)
			}
		}
		for _, id := range item.EvidenceIDs {
			if !checkEvidence(id) {
				return fmt.Errorf("fact %s references unavailable evidence %s", item.ID, id)
			}
		}
		for _, id := range item.LegacyFindingIDs {
			if !findings[id] {
				return fmt.Errorf("fact %s references missing finding %s", item.ID, id)
			}
		}
	}
	factIDs, claimIDs, decisionIDs, relationIDs := map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, item := range facts.Cases {
		factIDs[item.ID] = true
	}
	for _, item := range decisions.Claims {
		if item.ID == "" || claimIDs[item.ID] {
			return errors.New("duplicate/empty claim id")
		}
		claimIDs[item.ID] = true
		for _, id := range item.EntityIDs {
			if !entities[id] {
				return fmt.Errorf("claim %s references missing entity %s", item.ID, id)
			}
		}
		for _, citation := range item.Citations {
			if !validCitation(citation, evidence) {
				return fmt.Errorf("claim %s has invalid citation", item.ID)
			}
		}
	}
	for _, item := range decisions.Decisions {
		if item.ID == "" || decisionIDs[item.ID] {
			return errors.New("duplicate/empty decision id")
		}
		decisionIDs[item.ID] = true
		for _, id := range item.EntityIDs {
			if !entities[id] {
				return fmt.Errorf("decision %s references missing entity %s", item.ID, id)
			}
		}
		for _, id := range item.ClaimIDs {
			if !claimIDs[id] {
				return fmt.Errorf("decision %s references missing claim %s", item.ID, id)
			}
		}
		for _, citation := range item.Citations {
			if !validCitation(citation, evidence) {
				return fmt.Errorf("decision %s has invalid citation", item.ID)
			}
		}
	}
	for _, item := range decisions.ClaimRelations {
		if !claimIDs[item.FromClaimID] || !claimIDs[item.ToClaimID] {
			return fmt.Errorf("claim relation %s has missing endpoint", item.ID)
		}
		relationIDs[item.ID] = true
	}
	for _, item := range decisions.Relations {
		if !decisionIDs[item.FromDecisionID] || !decisionIDs[item.ToDecisionID] {
			return fmt.Errorf("decision relation %s has missing endpoint", item.ID)
		}
		relationIDs[item.ID] = true
		for _, citation := range item.Citations {
			if !validCitation(citation, evidence) {
				return fmt.Errorf("decision relation %s has invalid citation", item.ID)
			}
		}
	}
	decisionCaseIDs := map[string]bool{}
	for _, item := range decisions.Cases {
		if item.ID == "" || decisionCaseIDs[item.ID] {
			return errors.New("duplicate/empty decision case id")
		}
		decisionCaseIDs[item.ID] = true
		for _, id := range item.FactCaseIDs {
			if !factIDs[id] {
				return fmt.Errorf("case %s references missing fact %s", item.ID, id)
			}
		}
		for _, id := range item.DecisionIDs {
			if !decisionIDs[id] {
				return fmt.Errorf("case %s references missing decision %s", item.ID, id)
			}
		}
		for _, id := range item.ClaimIDs {
			if !claimIDs[id] {
				return fmt.Errorf("case %s references missing claim %s", item.ID, id)
			}
		}
		for _, id := range item.RelationIDs {
			if !relationIDs[id] {
				return fmt.Errorf("case %s references missing relation %s", item.ID, id)
			}
		}
		for _, id := range item.EntityIDs {
			if !entities[id] {
				return fmt.Errorf("case %s references missing entity %s", item.ID, id)
			}
		}
		for _, citation := range item.Citations {
			if !validCitation(citation, evidence) {
				return fmt.Errorf("case %s has invalid citation", item.ID)
			}
		}
	}
	if report.TotalCases != int64(len(decisions.Cases)) {
		return errors.New("report total_cases diverges from analysis cases")
	}
	if len(report.CaseIDs) != len(report.Cases) {
		return errors.New("report case_ids and cases length differ")
	}
	for i, item := range report.Cases {
		if !decisionCaseIDs[item.ID] || report.CaseIDs[i] != item.ID {
			return fmt.Errorf("report contains invalid or out-of-order case %s", item.ID)
		}
	}
	return nil
}

func validCitation(c insightcontract.Citation, evidence map[string]insightcontract.EvidenceView) bool {
	item, ok := evidence[c.EvidenceID]
	return ok && item.IntegrityStatus == "verified" && c.Quote != "" && strings.Contains(item.Excerpt, c.Quote)
}

func stringPointer(value string) *string { return &value }
func toSet(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		out[value] = true
	}
	return out
}
func encodeCursor(scope string, index int) string {
	raw := fmt.Sprintf("%s:%d", scope, index)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}
func decodeCursor(cursor, scope string) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, errors.New("ERR_VALIDATION: cursor inválido")
	}
	value := string(raw)
	separator := strings.LastIndexByte(value, ':')
	if separator < 0 || value[:separator] != scope {
		return 0, errors.New("ERR_VALIDATION: cursor de outro snapshot/análise")
	}
	n, err := strconv.Atoi(value[separator+1:])
	if err != nil || n < 0 {
		return 0, errors.New("ERR_VALIDATION: cursor inválido")
	}
	return n, nil
}

var _ insightcontract.AnalysisController = (*Service)(nil)
var _ insightcontract.InsightReader = (*Service)(nil)
