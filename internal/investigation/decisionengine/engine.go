// Package decisionengine validates and reconciles semantic judgments against
// the immutable insights contract. It never treats source text as instructions.
package decisionengine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"pocketcli/internal/investigation/insightcontract"
)

const (
	PromptVersion  = "decisionengine-prompt-v1"
	RubricVersion  = "decisionengine-rubric-v1"
	AdapterVersion = "decisionengine-v1"
)

var ErrInvalidInput = errors.New("decisionengine: invalid input")

// Analyze returns a reproducible degraded analysis when semantic processing is
// unavailable or disallowed. A configured provider is called only when policy
// explicitly permits its content mode.
func Analyze(ctx context.Context, bundle insightcontract.SourceBundle, facts insightcontract.FactsResult, cfg insightcontract.AnalysisConfig, provider insightcontract.ReasoningProvider) (insightcontract.DecisionAnalysis, error) {
	if err := validateInputs(bundle, facts, cfg); err != nil {
		return insightcontract.DecisionAnalysis{}, err
	}
	base := deterministic(bundle, facts, cfg)
	if cfg.Provider.Kind == "none" {
		base.Limitations = append(base.Limitations, "provider semântico não configurado; claims, decisões e avaliações permanecem indeterminados")
		return base, nil
	}
	if cfg.Provider.Kind != "configured" {
		return insightcontract.DecisionAnalysis{}, fmt.Errorf("%w: provider.kind deve ser none ou configured", ErrInvalidInput)
	}
	if provider == nil {
		base.Limitations = append(base.Limitations, "provider configurado indisponível; capacidade semântica não executada")
		return base, nil
	}
	if identified, ok := provider.(interface{ ProviderID() string }); ok {
		base.Provenance.Provider = identified.ProviderID()
	}
	if versioned, ok := provider.(interface{ AdapterVersion() string }); ok {
		base.Provenance.AdapterVersion = versioned.AdapterVersion()
	}
	base.Provenance.CacheKey = cacheKey(base.Provenance, provider)
	base.ID = "decision-" + base.Provenance.CacheKey[:16]
	if configured, ok := provider.(interface{ ModelName() string }); ok && cfg.Provider.Model != configured.ModelName() {
		return insightcontract.DecisionAnalysis{}, fmt.Errorf("%w: ProviderConfig.Model não coincide com o modelo injetado", ErrInvalidInput)
	}
	isLocal := false
	if locality, ok := provider.(interface{ IsLocal() bool }); ok {
		isLocal = locality.IsLocal()
	}
	if !isLocal && !cfg.Policy.ExternalProcessingAllowed {
		base.Limitations = append(base.Limitations, "provider externo bloqueado por ExternalProcessingAllowed=false")
		return base, nil
	}
	if cfg.Policy.ContentMode == "local_excerpts" {
		if !isLocal {
			base.Limitations = append(base.Limitations, "local_excerpts exige provider que declare execução local")
			return base, nil
		}
	}
	if cfg.Policy.ContentMode != "metadata_only" && cfg.Policy.ContentMode != "export_excerpts" {
		return insightcontract.DecisionAnalysis{}, fmt.Errorf("%w: policy.content_mode inválido", ErrInvalidInput)
	}
	if cfg.Policy.ContentMode == "export_excerpts" && !cfg.Policy.ExternalProcessingAllowed {
		base.Limitations = append(base.Limitations, "processamento externo bloqueado pela política")
		return base, nil
	}
	if cfg.Budget.MaxRequests < 1 || cfg.Budget.MaxInputTokens < 1 || cfg.Budget.MaxElapsedMS < 1 || cfg.Budget.MaxOutputTokens < 1 {
		base.Limitations = append(base.Limitations, "orçamento insuficiente para iniciar o provider")
		return base, nil
	}
	if cfg.Budget.MaxCost != nil {
		base.Limitations = append(base.Limitations, "limite de custo configurado, mas a interface do provider não informa preço verificável antes da chamada")
		return base, nil
	}
	request := buildRequest(bundle, facts, cfg)
	request.InputHash = base.Provenance.InputHash
	started := time.Now()
	callCtx, cancel := context.WithTimeout(ctx, time.Duration(cfg.Budget.MaxElapsedMS)*time.Millisecond)
	defer cancel()
	response, err := provider.Evaluate(callCtx, request)
	base.Usage.ElapsedMS = time.Since(started).Milliseconds()
	base.Usage.Requests = 1
	if err != nil {
		base.Limitations = append(base.Limitations, "provider indisponível ou falhou; nenhuma inferência semântica foi aceita")
		return base, nil
	}
	if err := validateResponse(response, bundle, facts, cfg); err != nil {
		base.Usage = response.Usage
		base.Usage.Requests = max64(base.Usage.Requests, 1)
		base.Usage.ElapsedMS = time.Since(started).Milliseconds()
		base.Limitations = append(base.Limitations, "resposta do provider rejeitada: "+err.Error())
		return base, nil
	}
	response.Analysis.Provenance = base.Provenance
	response.Analysis.Usage = response.Usage
	response.Analysis.Usage.Requests = max64(response.Analysis.Usage.Requests, 1)
	response.Analysis.Usage.ElapsedMS = base.Usage.ElapsedMS
	response.Analysis.Usage.CacheHits = response.Usage.CacheHits
	if response.Analysis.Status == "complete" && inputHasCoverageGaps(bundle, facts) {
		response.Analysis.Status = "partial"
		response.Analysis.Limitations = append(response.Analysis.Limitations, "fontes ou fatos têm cobertura/limitações incompletas; conclusão global rebaixada para partial")
	}
	return response.Analysis, nil
}

func inputHasCoverageGaps(b insightcontract.SourceBundle, f insightcontract.FactsResult) bool {
	if len(b.Limitations) > 0 || len(f.Limitations) > 0 {
		return true
	}
	for _, c := range b.Coverage {
		if c.Status != "complete" {
			return true
		}
	}
	for _, q := range b.Questions {
		if q.Status != "sufficient" && q.Status != "not_applicable" {
			return true
		}
	}
	return false
}

func cacheKey(p insightcontract.Provenance, provider insightcontract.ReasoningProvider) string {
	providerID, adapter := p.Provider, p.AdapterVersion
	if x, ok := provider.(interface{ ProviderID() string }); ok {
		providerID = x.ProviderID()
	}
	if x, ok := provider.(interface{ AdapterVersion() string }); ok {
		adapter = x.AdapterVersion()
	}
	return hash([]string{insightcontract.Version, p.InputHash, p.ConfigHash, providerID, p.Model, adapter, p.PromptVersion, p.RubricVersion, p.PolicyVersion, p.RulesetVersion})
}

func validateInputs(b insightcontract.SourceBundle, f insightcontract.FactsResult, c insightcontract.AnalysisConfig) error {
	if b.Version != insightcontract.Version || f.Version != insightcontract.Version || c.Version != insightcontract.Version {
		return fmt.Errorf("%w: versão de contrato incompatível", ErrInvalidInput)
	}
	if b.Snapshot.ID == "" || f.SnapshotID != b.Snapshot.ID {
		return fmt.Errorf("%w: facts e bundle devem pertencer ao mesmo snapshot", ErrInvalidInput)
	}
	if f.InputHash != "" && b.InputHash != "" && f.InputHash != b.InputHash {
		return fmt.Errorf("%w: hash de facts não corresponde ao bundle", ErrInvalidInput)
	}
	ids := make(map[string]struct{}, len(b.Evidence))
	for _, e := range b.Evidence {
		if e.ID == "" {
			return fmt.Errorf("%w: evidência sem ID", ErrInvalidInput)
		}
		if _, ok := ids[e.ID]; ok {
			return fmt.Errorf("%w: ID de evidência duplicado %q", ErrInvalidInput, e.ID)
		}
		ids[e.ID] = struct{}{}
		if e.IntegrityStatus == "verified" && e.ContentStatus == "available" && e.Excerpt != "" && e.ExcerptHash != "" {
			digest := sha256.Sum256([]byte(e.Excerpt))
			if hex.EncodeToString(digest[:]) != e.ExcerptHash {
				return fmt.Errorf("%w: excerpt hash inválido para evidência %q", ErrInvalidInput, e.ID)
			}
		}
	}
	entityIDs, linkIDs, findingIDs, objectiveIDs, metricIDs := map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, e := range b.Entities {
		if e.ID != "" {
			entityIDs[e.ID] = true
		}
	}
	for _, link := range b.Links {
		if link.ID != "" {
			linkIDs[link.ID] = true
		}
	}
	for _, finding := range b.LegacyFindings {
		if finding.ID != "" {
			findingIDs[finding.ID] = true
		}
	}
	for _, metric := range f.Metrics {
		if metric.ID != "" {
			metricIDs[metric.ID] = true
		}
	}
	for _, objective := range c.Objectives {
		if objective.ID != "" {
			objectiveIDs[objective.ID] = true
		}
	}
	factCaseIDs := map[string]bool{}
	for _, fact := range f.Cases {
		if fact.ID == "" || factCaseIDs[fact.ID] {
			return fmt.Errorf("%w: FactCase ID vazio/duplicado %q", ErrInvalidInput, fact.ID)
		}
		factCaseIDs[fact.ID] = true
		for _, id := range fact.EvidenceIDs {
			if _, ok := ids[id]; !ok {
				return fmt.Errorf("%w: fato %q referencia evidência ausente %q", ErrInvalidInput, fact.ID, id)
			}
		}
		for _, id := range append(append([]string{}, fact.SubjectIDs...), fact.RelatedIDs...) {
			if !entityIDs[id] {
				return fmt.Errorf("%w: fato %q referencia entidade ausente %q", ErrInvalidInput, fact.ID, id)
			}
		}
		for _, id := range fact.LinkIDs {
			if !linkIDs[id] {
				return fmt.Errorf("%w: fato %q referencia link ausente %q", ErrInvalidInput, fact.ID, id)
			}
		}
		for _, id := range fact.MetricIDs {
			if !metricIDs[id] {
				return fmt.Errorf("%w: fato %q referencia métrica ausente %q", ErrInvalidInput, fact.ID, id)
			}
		}
		for _, id := range fact.ObjectiveIDs {
			if !objectiveIDs[id] {
				return fmt.Errorf("%w: fato %q referencia objetivo ausente %q", ErrInvalidInput, fact.ID, id)
			}
		}
		for _, id := range fact.LegacyFindingIDs {
			if !findingIDs[id] {
				return fmt.Errorf("%w: fato %q referencia finding ausente %q", ErrInvalidInput, fact.ID, id)
			}
		}
	}
	if c.Policy.ContentMode != "metadata_only" && c.Policy.ContentMode != "local_excerpts" && c.Policy.ContentMode != "export_excerpts" {
		return fmt.Errorf("%w: content mode desconhecido", ErrInvalidInput)
	}
	return nil
}

func deterministic(b insightcontract.SourceBundle, f insightcontract.FactsResult, c insightcontract.AnalysisConfig) insightcontract.DecisionAnalysis {
	inputHash := hash(struct {
		Bundle insightcontract.SourceBundle
		Facts  insightcontract.FactsResult
	}{b, f})
	configHash := hash(c)
	key := hash([]string{insightcontract.Version, inputHash, configHash, AdapterVersion, PromptVersion, RubricVersion, b.Snapshot.RulesetVersion, c.Policy.Version, c.Provider.Model})
	status := "partial"
	if len(f.Cases) == 0 && len(b.Evidence) == 0 {
		status = "not_run"
	}
	limitations := append([]string{}, b.Limitations...)
	limitations = append(limitations, f.Limitations...)
	analysis := insightcontract.DecisionAnalysis{
		Version: insightcontract.Version, ID: "decision-" + key[:16], SnapshotID: b.Snapshot.ID, Status: status,
		Claims: []insightcontract.Claim{}, ClaimRelations: []insightcontract.ClaimRelation{}, Decisions: []insightcontract.DecisionRecord{}, Relations: []insightcontract.DecisionRelation{},
		Cases: []insightcontract.DecisionCase{}, Dimensions: []insightcontract.DimensionStatus{}, Limitations: limitations,
		Provenance: insightcontract.Provenance{InputHash: inputHash, ConfigHash: configHash, Provider: providerName(c), Model: c.Provider.Model, AdapterVersion: c.Provider.AdapterVersion, PromptVersion: PromptVersion, RubricVersion: RubricVersion, PolicyVersion: c.Policy.Version, RulesetVersion: b.Snapshot.RulesetVersion, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), CacheKey: key},
		Usage:      insightcontract.Usage{Requests: 0, InputTokens: nil, OutputTokens: nil, Cost: nil, Currency: nil, PriceVersion: nil},
	}
	for _, fact := range f.Cases {
		cites := citationsFor(fact.EvidenceIDs, b.Evidence, c.Policy)
		assessment := indeterminate("avaliação semântica não executada", "evidência insuficiente para julgar impacto, complexidade, urgência ou confiança")
		ids := append([]string{}, fact.SubjectIDs...)
		sort.Strings(ids)
		analysis.Cases = append(analysis.Cases, insightcontract.DecisionCase{ID: "decision-" + fact.ID, Title: fact.Title, State: "needs_evidence", FactCaseIDs: []string{fact.ID}, DecisionIDs: []string{}, ClaimIDs: []string{}, RelationIDs: []string{}, ObjectiveIDs: []string{}, EntityIDs: ids, Summary: "Fato determinístico preservado; interpretação semântica requer evidência elegível e provider.", Impact: assessment, Complexity: assessment, Urgency: assessment, Confidence: assessment, Alternatives: []insightcontract.Alternative{}, RecommendationBasis: "hypothesis", NextEvidence: []string{"evidência legível e rastreável", "provider semântico configurado e autorizado"}, Citations: cites})
	}
	for _, obj := range c.Objectives {
		missing := []string{}
		if obj.Target == nil || obj.Comparator == "none" {
			missing = append(missing, "meta mensurável/comparador ausente")
		}
		analysis.Dimensions = append(analysis.Dimensions, insightcontract.DimensionStatus{ObjectiveID: obj.ID, Dimension: obj.Dimension, Status: "indeterminate", Reason: "adequação ao objetivo não pode ser avaliada sem suporte semântico e critérios suficientes", MetricIDs: nonEmpty(obj.MetricID), CaseIDs: []string{}, Missing: missing})
	}
	return analysis
}

func buildRequest(b insightcontract.SourceBundle, f insightcontract.FactsResult, c insightcontract.AnalysisConfig) insightcontract.ReasoningRequest {
	evidence := append([]insightcontract.EvidenceView{}, b.Evidence...)
	for i := range evidence {
		if c.Policy.ContentMode == "metadata_only" {
			evidence[i].Excerpt = ""
			evidence[i].ExcerptHash = ""
			continue
		}
		if c.Policy.ExcerptMaxRunes > 0 {
			evidence[i].Excerpt = truncateRunes(evidence[i].Excerpt, c.Policy.ExcerptMaxRunes)
		}
	}
	entities := append([]insightcontract.Entity{}, b.Entities...)
	facts := f
	if c.Policy.ContentMode == "metadata_only" {
		// Facts and entity titles may contain copied source text even when
		// EvidenceView.Excerpt has been stripped.
		facts.Limitations = []string{}
		facts.Cohorts = append([]insightcontract.Cohort{}, f.Cohorts...)
		for i := range facts.Cohorts {
			facts.Cohorts[i].Definition = ""
			facts.Cohorts[i].Exclusions = []string{}
		}
		facts.Metrics = append([]insightcontract.Metric{}, f.Metrics...)
		for i := range facts.Metrics {
			facts.Metrics[i].Name = ""
			facts.Metrics[i].Definition = ""
			facts.Metrics[i].Limitations = []string{}
		}
		facts.Cases = append([]insightcontract.FactCase{}, f.Cases...)
		for i := range facts.Cases {
			facts.Cases[i].Title = ""
			facts.Cases[i].ObservedFacts = []string{}
			facts.Cases[i].OpenQuestions = []string{}
		}
		for i := range entities {
			entities[i].Title = ""
			entities[i].URL = nil
		}
	}
	return insightcontract.ReasoningRequest{Task: "extract", SnapshotID: b.Snapshot.ID, InputHash: b.InputHash, PromptVersion: PromptVersion, RubricVersion: RubricVersion, Entities: entities, Evidence: evidence, Facts: facts, Claims: []insightcontract.Claim{}, Decisions: []insightcontract.DecisionRecord{}, Objectives: append([]insightcontract.Objective{}, c.Objectives...), Budget: c.Budget}
}

func validateResponse(r insightcontract.ReasoningResponse, b insightcontract.SourceBundle, f insightcontract.FactsResult, c insightcontract.AnalysisConfig) error {
	a := r.Analysis
	if a.Version != insightcontract.Version || a.SnapshotID != b.Snapshot.ID {
		return errors.New("versão ou snapshot da análise não confere")
	}
	if a.Status != "complete" && a.Status != "partial" && a.Status != "not_run" {
		return errors.New("status da análise inválido")
	}
	if a.ID == "" || a.Claims == nil || a.ClaimRelations == nil || a.Decisions == nil || a.Relations == nil || a.Cases == nil || a.Dimensions == nil || a.Limitations == nil {
		return errors.New("análise sem IDs ou arrays contratuais")
	}
	if r.Usage.Requests < 0 || r.Usage.ElapsedMS < 0 || r.Usage.CacheHits < 0 || (r.Usage.Cost != nil && *r.Usage.Cost < 0) {
		return errors.New("usage contém valores inválidos")
	}
	if r.Usage.Requests > int64(c.Budget.MaxRequests) || r.Usage.ElapsedMS > c.Budget.MaxElapsedMS || (r.Usage.InputTokens != nil && *r.Usage.InputTokens > c.Budget.MaxInputTokens) || (r.Usage.OutputTokens != nil && *r.Usage.OutputTokens > c.Budget.MaxOutputTokens) {
		return errors.New("provider excedeu orçamento declarado")
	}
	allowed := make(map[string]insightcontract.EvidenceView, len(b.Evidence))
	for _, e := range b.Evidence {
		allowed[e.ID] = e
	}
	entityIDs, componentIDs := map[string]bool{}, map[string]bool{}
	for _, e := range b.Entities {
		entityIDs[e.ID] = true
		if e.Kind == "component" {
			componentIDs[e.ID] = true
		}
	}
	resourceIDs := map[string]bool{}
	for _, r := range b.RequestedScope.Resources {
		resourceIDs[r.ID] = true
	}
	factIDs, metricIDs, objectiveIDs := map[string]bool{}, map[string]bool{}, map[string]bool{}
	objectivesByID := map[string]insightcontract.Objective{}
	for _, fc := range f.Cases {
		factIDs[fc.ID] = true
	}
	for _, m := range f.Metrics {
		metricIDs[m.ID] = true
	}
	for _, o := range c.Objectives {
		objectiveIDs[o.ID] = true
		objectivesByID[o.ID] = o
	}
	for _, cl := range a.Claims {
		if cl.EntityIDs == nil || cl.Citations == nil || cl.Scope.ResourceIDs == nil || cl.Scope.EntityIDs == nil || cl.Scope.ComponentIDs == nil || cl.Scope.Conditions == nil {
			return errors.New("claim contém arrays null")
		}
		if cl.ID == "" || !validClaimStatus(cl.Status) || !validValidity(cl.ValidityStatus) || !validScope(cl.Scope.Status) {
			return errors.New("claim com campos canônicos inválidos")
		}
		if strings.TrimSpace(cl.Text) == "" {
			return errors.New("claim sem texto")
		}
		if err := validateCitations(cl.Citations, allowed, c.Policy); err != nil {
			return err
		}
		if len(cl.Citations) == 0 {
			return errors.New("claim sem citação rastreável")
		}
		if !hasReadableCitation(cl.Citations, allowed) {
			return errors.New("claim sem excerto legível que sustente o texto")
		}
		if err := validateValidityRange(cl.ValidFrom, cl.ValidTo, cl.ValidityStatus); err != nil {
			return err
		}
		if err := validateTimestamp(cl.EventAt); err != nil {
			return fmt.Errorf("claim event_at: %w", err)
		}
		if err := validateClaimScope(cl, entityIDs, componentIDs, resourceIDs); err != nil {
			return err
		}
	}
	claimIDs := make(map[string]bool)
	claimsByID := make(map[string]insightcontract.Claim)
	for _, cl := range a.Claims {
		if claimIDs[cl.ID] {
			return fmt.Errorf("claim ID duplicado %q", cl.ID)
		}
		claimIDs[cl.ID] = true
		claimsByID[cl.ID] = cl
	}
	for _, d := range a.Decisions {
		if d.EntityIDs == nil || d.ClaimIDs == nil || d.ObjectiveIDs == nil || d.Citations == nil || d.Scope.ResourceIDs == nil || d.Scope.EntityIDs == nil || d.Scope.ComponentIDs == nil || d.Scope.Conditions == nil {
			return errors.New("decision contém arrays null")
		}
		if d.ID == "" || !validDecisionStatus(d.Status) || !validValidity(d.ValidityStatus) || !validScope(d.Scope.Status) {
			return errors.New("decision com campos canônicos inválidos")
		}
		if strings.TrimSpace(d.Question) == "" || strings.TrimSpace(d.Choice) == "" {
			return errors.New("decision sem pergunta ou escolha")
		}
		if err := validateCitations(d.Citations, allowed, c.Policy); err != nil {
			return err
		}
		if len(d.Citations) == 0 {
			return errors.New("decision sem citação rastreável")
		}
		if !hasReadableCitation(d.Citations, allowed) {
			return errors.New("decision sem excerto legível que sustente a decisão")
		}
		if err := validateValidityRange(d.ValidFrom, d.ValidTo, d.ValidityStatus); err != nil {
			return err
		}
		if err := validateTimestamp(d.DecidedAt); err != nil {
			return fmt.Errorf("decision decided_at: %w", err)
		}
		if err := validateDecisionScope(d, entityIDs, componentIDs, resourceIDs); err != nil {
			return err
		}
		for _, id := range d.ClaimIDs {
			if !claimIDs[id] {
				return fmt.Errorf("decision referencia claim ausente %q", id)
			}
		}
		for _, id := range d.ObjectiveIDs {
			if !objectiveIDs[id] {
				return fmt.Errorf("decision referencia objetivo ausente %q", id)
			}
		}
	}
	claimRelationIDs := map[string]bool{}
	for _, rel := range a.ClaimRelations {
		if rel.Citations == nil {
			return errors.New("claim relation contém citations null")
		}
		if !validRelation(rel.Kind, rel.Status) || !claimIDs[rel.FromClaimID] || !claimIDs[rel.ToClaimID] {
			return errors.New("claim relation inválida ou sem claims")
		}
		if err := validateCitations(rel.Citations, allowed, c.Policy); err != nil {
			return err
		}
		if len(rel.Citations) == 0 {
			return errors.New("claim relation sem citação rastreável")
		}
		if !hasReadableCitation(rel.Citations, allowed) {
			return errors.New("claim relation sem excerto legível")
		}
		if err := validateValidityRange(rel.ValidFrom, rel.ValidTo, rel.ValidityStatus); err != nil {
			return err
		}
		if rel.Kind == "contradicts" && rel.FromClaimID > rel.ToClaimID {
			return errors.New("IDs da contradição entre claims devem estar ordenados")
		}
		if err := validateClaimRelation(rel, claimsByID); err != nil {
			return err
		}
		if rel.ID == "" || claimRelationIDs[rel.ID] {
			return errors.New("claim relation ID vazio/duplicado")
		}
		claimRelationIDs[rel.ID] = true
	}
	decisionIDs := make(map[string]bool)
	decisionsByID := make(map[string]insightcontract.DecisionRecord)
	for _, d := range a.Decisions {
		if decisionIDs[d.ID] {
			return fmt.Errorf("decision ID duplicado %q", d.ID)
		}
		decisionIDs[d.ID] = true
		decisionsByID[d.ID] = d
	}
	decisionRelationIDs := map[string]bool{}
	for _, rel := range a.Relations {
		if rel.Citations == nil {
			return errors.New("decision relation contém citations null")
		}
		if !validRelation(rel.Kind, rel.Status) || !decisionIDs[rel.FromDecisionID] || !decisionIDs[rel.ToDecisionID] {
			return errors.New("decision relation inválida ou sem decisões")
		}
		if err := validateCitations(rel.Citations, allowed, c.Policy); err != nil {
			return err
		}
		if len(rel.Citations) == 0 {
			return errors.New("decision relation sem citação rastreável")
		}
		if !hasReadableCitation(rel.Citations, allowed) {
			return errors.New("decision relation sem excerto legível")
		}
		if err := validateValidityRange(rel.ValidFrom, rel.ValidTo, rel.ValidityStatus); err != nil {
			return err
		}
		if rel.Kind == "contradicts" && rel.FromDecisionID > rel.ToDecisionID {
			return errors.New("IDs da contradição entre decisões devem estar ordenados")
		}
		if err := validateDecisionRelation(rel, decisionsByID); err != nil {
			return err
		}
		if rel.ID == "" || decisionRelationIDs[rel.ID] {
			return errors.New("decision relation ID vazio/duplicado")
		}
		if claimRelationIDs[rel.ID] {
			return fmt.Errorf("ID de relação duplicado %q", rel.ID)
		}
		decisionRelationIDs[rel.ID] = true
	}
	caseIDs := map[string]bool{}
	for _, dc := range a.Cases {
		if dc.FactCaseIDs == nil || dc.DecisionIDs == nil || dc.ClaimIDs == nil || dc.RelationIDs == nil || dc.ObjectiveIDs == nil || dc.EntityIDs == nil || dc.Alternatives == nil || dc.NextEvidence == nil || dc.Citations == nil {
			return errors.New("decision case contém arrays null")
		}
		if dc.ID == "" || caseIDs[dc.ID] {
			return errors.New("decision case ID vazio/duplicado")
		}
		caseIDs[dc.ID] = true
		for _, as := range []insightcontract.Assessment{dc.Impact, dc.Complexity, dc.Urgency} {
			if !validAssessment(as, allowed) {
				return errors.New("assessment inválida ou referencia evidência ausente")
			}
		}
		if !validConfidenceAssessment(dc.Confidence, allowed) {
			return errors.New("confidence assessment inválida")
		}
		if err := validateCitations(dc.Citations, allowed, c.Policy); err != nil {
			return err
		}
		if dc.State != "actionable" && dc.State != "monitor" && dc.State != "needs_evidence" && dc.State != "resolved" {
			return errors.New("estado de decision case inválido")
		}
		if strings.TrimSpace(dc.Title) == "" || strings.TrimSpace(dc.Summary) == "" || len(dc.FactCaseIDs)+len(dc.DecisionIDs)+len(dc.ClaimIDs) == 0 {
			return errors.New("decision case incompleto ou sem origem")
		}
		if (dc.State == "actionable" || dc.State == "resolved") && (dc.Confidence.State != "assessed" || !hasReadableCitation(dc.Citations, allowed)) {
			return errors.New("case actionable/resolved sem confiança/citação sustentada")
		}
		if dc.RecommendationBasis != "source" && dc.RecommendationBasis != "inference" && dc.RecommendationBasis != "hypothesis" {
			return errors.New("recommendation basis inválido")
		}
		for _, id := range dc.ClaimIDs {
			if !claimIDs[id] {
				return fmt.Errorf("case referencia claim ausente %q", id)
			}
		}
		for _, id := range dc.DecisionIDs {
			if !decisionIDs[id] {
				return fmt.Errorf("case referencia decisão ausente %q", id)
			}
		}
		for _, id := range dc.FactCaseIDs {
			if !factIDs[id] {
				return fmt.Errorf("case referencia fact case ausente %q", id)
			}
		}
		for _, id := range dc.RelationIDs {
			if !claimRelationIDs[id] && !decisionRelationIDs[id] {
				return fmt.Errorf("case referencia relação ausente %q", id)
			}
		}
		for _, id := range dc.ObjectiveIDs {
			if !objectiveIDs[id] {
				return fmt.Errorf("case referencia objetivo ausente %q", id)
			}
		}
		for _, id := range dc.EntityIDs {
			if !entityIDs[id] {
				return fmt.Errorf("case referencia entidade ausente %q", id)
			}
		}
		alternativeIDs := map[string]bool{}
		for _, alt := range dc.Alternatives {
			if alt.Tradeoffs == nil || alt.Preconditions == nil || alt.EvidenceIDs == nil {
				return errors.New("alternative contém arrays null")
			}
			if alt.ID == "" || (alt.Basis != "source" && alt.Basis != "inference" && alt.Basis != "hypothesis") {
				return errors.New("alternative inválida")
			}
			if strings.TrimSpace(alt.Action) == "" || strings.TrimSpace(alt.ExpectedEffect) == "" {
				return errors.New("alternative sem ação/efeito esperado")
			}
			if alternativeIDs[alt.ID] {
				return fmt.Errorf("alternative ID duplicado %q", alt.ID)
			}
			alternativeIDs[alt.ID] = true
			if alt.Basis == "source" && len(alt.EvidenceIDs) == 0 {
				return errors.New("alternative de source sem evidência")
			}
			for _, id := range alt.EvidenceIDs {
				if ev, ok := allowed[id]; !ok || !readable(ev) {
					return fmt.Errorf("alternative referencia evidência ausente %q", id)
				}
			}
		}
		if dc.RecommendedAlternativeID != nil {
			found := false
			for _, alt := range dc.Alternatives {
				if alt.ID == *dc.RecommendedAlternativeID {
					found = true
				}
			}
			if !found {
				return errors.New("recommended alternative não existe no case")
			}
		}
	}
	dimensionObjectiveIDs := map[string]bool{}
	for _, d := range a.Dimensions {
		if d.MetricIDs == nil || d.CaseIDs == nil || d.Missing == nil {
			return errors.New("dimension contém arrays null")
		}
		if d.Status != "on_track" && d.Status != "at_risk" && d.Status != "off_track" && d.Status != "indeterminate" && d.Status != "not_applicable" {
			return errors.New("dimension status inválido")
		}
		if !objectiveIDs[d.ObjectiveID] {
			return fmt.Errorf("dimension referencia objetivo ausente %q", d.ObjectiveID)
		}
		if dimensionObjectiveIDs[d.ObjectiveID] {
			return fmt.Errorf("dimension duplicada para objetivo %q", d.ObjectiveID)
		}
		dimensionObjectiveIDs[d.ObjectiveID] = true
		objective := objectivesByID[d.ObjectiveID]
		if d.Dimension != objective.Dimension || strings.TrimSpace(d.Reason) == "" {
			return errors.New("dimension incompatível com objetivo ou sem razão")
		}
		if (d.Status == "on_track" || d.Status == "at_risk" || d.Status == "off_track") && (objective.Target == nil || objective.Comparator == "none") {
			return fmt.Errorf("objetivo %q sem meta mensurável recebeu status de progresso", objective.ID)
		}
		for _, id := range d.MetricIDs {
			if !metricIDs[id] {
				return fmt.Errorf("dimension referencia métrica ausente %q", id)
			}
		}
		for _, id := range d.CaseIDs {
			if !caseIDs[id] {
				return fmt.Errorf("dimension referencia case ausente %q", id)
			}
		}
	}
	if len(dimensionObjectiveIDs) != len(c.Objectives) {
		return errors.New("deve haver exatamente um status por objetivo")
	}
	return nil
}

func citationsFor(ids []string, evidence []insightcontract.EvidenceView, p insightcontract.Policy) []insightcontract.Citation {
	byID := map[string]insightcontract.EvidenceView{}
	for _, e := range evidence {
		byID[e.ID] = e
	}
	out := []insightcontract.Citation{}
	for _, id := range ids {
		e, ok := byID[id]
		if !ok || !eligible(e) {
			continue
		}
		quote := ""
		if p.ContentMode != "metadata_only" {
			quote = e.Excerpt
		}
		out = append(out, insightcontract.Citation{EvidenceID: id, Quote: quote})
	}
	return out
}
func validateCitations(cs []insightcontract.Citation, evidence map[string]insightcontract.EvidenceView, p insightcontract.Policy) error {
	for _, c := range cs {
		e, ok := evidence[c.EvidenceID]
		if !ok || !eligible(e) {
			return fmt.Errorf("citação aponta para evidência ausente ou inelegível %q", c.EvidenceID)
		}
		if c.Quote != "" && (p.ContentMode == "metadata_only" || !strings.Contains(e.Excerpt, c.Quote)) {
			return fmt.Errorf("citação contém trecho não autorizado/inexato %q", c.EvidenceID)
		}
	}
	return nil
}
func eligible(e insightcontract.EvidenceView) bool {
	return e.ContentStatus != "integrity_error" && e.IntegrityStatus != "mismatch" && e.IntegrityStatus != "missing"
}
func readable(e insightcontract.EvidenceView) bool {
	return e.ContentStatus == "available" && e.IntegrityStatus == "verified" && e.Excerpt != ""
}
func hasReadableCitation(citations []insightcontract.Citation, evidence map[string]insightcontract.EvidenceView) bool {
	for _, c := range citations {
		if e, ok := evidence[c.EvidenceID]; ok && readable(e) && c.Quote != "" {
			return true
		}
	}
	return false
}
func validAssessment(a insightcontract.Assessment, ev map[string]insightcontract.EvidenceView) bool {
	if a.EvidenceIDs == nil || a.Missing == nil {
		return false
	}
	if a.State != "assessed" && a.State != "indeterminate" && a.State != "not_applicable" {
		return false
	}
	levels := []string{"critical", "high", "medium", "low", "unknown"}
	if a.State == "indeterminate" && a.Level != "unknown" {
		return false
	}
	found := false
	for _, l := range levels {
		if a.Level == l {
			found = true
		}
	}
	if !found {
		return false
	}
	if a.State == "assessed" && (a.Rationale == "" || len(a.EvidenceIDs) == 0) {
		return false
	}
	for _, id := range a.EvidenceIDs {
		if e, ok := ev[id]; !ok || !readable(e) {
			return false
		}
	}
	return true
}
func validConfidenceAssessment(a insightcontract.Assessment, ev map[string]insightcontract.EvidenceView) bool {
	if a.Level == "critical" {
		return false
	}
	return validAssessment(a, ev)
}
func validRelation(k, s string) bool {
	kinds := map[string]bool{"supports": true, "supersedes": true, "revokes": true, "contradicts": true, "different_scope": true, "unresolved": true}
	statuses := map[string]bool{"established": true, "candidate": true, "indeterminate": true}
	return kinds[k] && statuses[s]
}
func validValidity(s string) bool {
	return s == "known_interval" || s == "open_ended" || s == "unknown" || s == "ambiguous"
}
func validateValidityRange(from, to *string, status string) error {
	if from != nil {
		if _, err := time.Parse(time.RFC3339, *from); err != nil {
			return errors.New("valid_from não é RFC3339")
		}
	}
	if to != nil {
		if _, err := time.Parse(time.RFC3339, *to); err != nil {
			return errors.New("valid_to não é RFC3339")
		}
	}
	switch status {
	case "known_interval":
		if from == nil || to == nil {
			return errors.New("known_interval exige início e fim conhecidos")
		}
		start, _ := time.Parse(time.RFC3339, *from)
		end, _ := time.Parse(time.RFC3339, *to)
		if !start.Before(end) {
			return errors.New("intervalo de validade invertido/vazio")
		}
	case "open_ended":
		if from == nil || to != nil {
			return errors.New("open_ended exige início conhecido e fim nil")
		}
	case "unknown":
		if from != nil || to != nil {
			return errors.New("validity unknown não pode carregar limites")
		}
	case "ambiguous":
	default:
		return errors.New("validity status inválido")
	}
	return nil
}

func validateClaimRelation(rel insightcontract.ClaimRelation, claims map[string]insightcontract.Claim) error {
	from, to := claims[rel.FromClaimID], claims[rel.ToClaimID]
	switch rel.Kind {
	case "supersedes", "revokes":
		if from.EventAt == nil || to.EventAt == nil || !scopeEqualKnown(from.Scope, to.Scope) {
			return errors.New("supersedes/revokes exige cronologia e escopo conhecidos")
		}
		later, err := time.Parse(time.RFC3339, *from.EventAt)
		if err != nil {
			return errors.New("event_at posterior inválido")
		}
		earlier, err := time.Parse(time.RFC3339, *to.EventAt)
		if err != nil || !later.After(earlier) {
			return errors.New("FromClaimID precisa apontar para claim posterior")
		}
	case "contradicts":
		if !scopeEqualKnown(from.Scope, to.Scope) || !validityOverlaps(from.ValidFrom, from.ValidTo, from.ValidityStatus, to.ValidFrom, to.ValidTo, to.ValidityStatus) {
			return errors.New("contradicts exige escopo igual e validade temporal comparável")
		}
	case "different_scope":
		if !validScope(from.Scope.Status) || from.Scope.Status != "known" || to.Scope.Status != "known" || scopeEqualKnown(from.Scope, to.Scope) {
			return errors.New("different_scope exige escopos conhecidos e distintos")
		}
	case "unresolved":
		if rel.Status != "indeterminate" {
			return errors.New("unresolved deve ter status indeterminate")
		}
	}
	return nil
}

func validateDecisionRelation(rel insightcontract.DecisionRelation, decisions map[string]insightcontract.DecisionRecord) error {
	from, to := decisions[rel.FromDecisionID], decisions[rel.ToDecisionID]
	switch rel.Kind {
	case "supersedes", "revokes":
		if from.DecidedAt == nil || to.DecidedAt == nil || !scopeEqualKnown(from.Scope, to.Scope) {
			return errors.New("supersedes/revokes exige cronologia e escopo conhecidos")
		}
		later, err := time.Parse(time.RFC3339, *from.DecidedAt)
		if err != nil {
			return errors.New("decided_at posterior inválido")
		}
		earlier, err := time.Parse(time.RFC3339, *to.DecidedAt)
		if err != nil || !later.After(earlier) {
			return errors.New("FromDecisionID precisa apontar para decisão posterior")
		}
	case "contradicts":
		if !scopeEqualKnown(from.Scope, to.Scope) || !validityOverlaps(from.ValidFrom, from.ValidTo, from.ValidityStatus, to.ValidFrom, to.ValidTo, to.ValidityStatus) {
			return errors.New("contradicts exige escopo igual e validade temporal comparável")
		}
	case "different_scope":
		if from.Scope.Status != "known" || to.Scope.Status != "known" || scopeEqualKnown(from.Scope, to.Scope) {
			return errors.New("different_scope exige escopos conhecidos e distintos")
		}
	case "unresolved":
		if rel.Status != "indeterminate" {
			return errors.New("unresolved deve ter status indeterminate")
		}
	}
	return nil
}

func scopeEqualKnown(a, b insightcontract.ContextScope) bool {
	if a.Status != "known" || b.Status != "known" || !equalOptionalString(a.Environment, b.Environment) {
		return false
	}
	if len(a.ResourceIDs)+len(a.EntityIDs)+len(a.ComponentIDs) == 0 || len(b.ResourceIDs)+len(b.EntityIDs)+len(b.ComponentIDs) == 0 {
		return false
	}
	qualifierKnown := a.Environment != nil && b.Environment != nil || len(a.Conditions) > 0 && len(b.Conditions) > 0 || a.Description != "" && b.Description != ""
	if !qualifierKnown {
		return false
	}
	return equalStrings(a.ResourceIDs, b.ResourceIDs) && equalStrings(a.EntityIDs, b.EntityIDs) && equalStrings(a.ComponentIDs, b.ComponentIDs) && equalStrings(a.Conditions, b.Conditions)
}

func equalOptionalString(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func validateClaimScope(c insightcontract.Claim, entities, components, resources map[string]bool) error {
	if c.Method != "source" && c.Method != "rule" && c.Method != "model" && c.Method != "human" {
		return errors.New("claim method inválido")
	}
	if err := validateContextScope(c.Scope, entities, components, resources); err != nil {
		return err
	}
	for _, id := range c.EntityIDs {
		if !entities[id] {
			return fmt.Errorf("claim amplia escopo para entidade ausente %q", id)
		}
	}
	return nil
}

func validateDecisionScope(d insightcontract.DecisionRecord, entities, components, resources map[string]bool) error {
	if d.Method != "source" && d.Method != "rule" && d.Method != "model" && d.Method != "human" {
		return errors.New("decision method inválido")
	}
	if err := validateContextScope(d.Scope, entities, components, resources); err != nil {
		return err
	}
	for _, id := range d.EntityIDs {
		if !entities[id] {
			return fmt.Errorf("decision amplia escopo para entidade ausente %q", id)
		}
	}
	return nil
}

func validateContextScope(s insightcontract.ContextScope, entities, components, resources map[string]bool) error {
	if s.Status == "known" && len(s.ResourceIDs)+len(s.EntityIDs)+len(s.ComponentIDs) == 0 && s.Environment == nil && len(s.Conditions) == 0 && s.Description == "" {
		return errors.New("ContextScope known sem escopo documentado")
	}
	for _, id := range s.ResourceIDs {
		if !resources[id] {
			return fmt.Errorf("ContextScope amplia RequestedScope com recurso %q", id)
		}
	}
	for _, id := range s.EntityIDs {
		if !entities[id] {
			return fmt.Errorf("ContextScope referencia entidade ausente %q", id)
		}
	}
	for _, id := range s.ComponentIDs {
		if !components[id] {
			return fmt.Errorf("ContextScope referencia componente ausente %q", id)
		}
	}
	return nil
}

func validateTimestamp(ts *string) error {
	if ts == nil {
		return nil
	}
	if _, err := time.Parse(time.RFC3339, *ts); err != nil {
		return errors.New("timestamp não é RFC3339")
	}
	return nil
}

func equalStrings(a, b []string) bool {
	x, y := append([]string{}, a...), append([]string{}, b...)
	sort.Strings(x)
	sort.Strings(y)
	if len(x) != len(y) {
		return false
	}
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}

func validityOverlaps(aFrom, aTo *string, aStatus string, bFrom, bTo *string, bStatus string) bool {
	if (aStatus != "known_interval" && aStatus != "open_ended") || (bStatus != "known_interval" && bStatus != "open_ended") || aFrom == nil || bFrom == nil {
		return false
	}
	parse := func(p *string) (time.Time, bool) {
		if p == nil {
			return time.Time{}, true
		}
		v, err := time.Parse(time.RFC3339, *p)
		return v, err == nil
	}
	af, ok := parse(aFrom)
	if !ok {
		return false
	}
	at, ok := parse(aTo)
	if !ok {
		return false
	}
	bf, ok := parse(bFrom)
	if !ok {
		return false
	}
	bt, ok := parse(bTo)
	if !ok {
		return false
	}
	return (aTo == nil || bf.Before(at)) && (bTo == nil || af.Before(bt))
}

func validScope(s string) bool { return s == "known" || s == "partial" || s == "unknown" }
func validClaimStatus(s string) bool {
	return s == "supported" || s == "inferred" || s == "unverified" || s == "disputed"
}
func validDecisionStatus(s string) bool {
	return s == "proposed" || s == "accepted" || s == "implemented" || s == "superseded" || s == "revoked" || s == "conflicting" || s == "unknown"
}
func indeterminate(reason, missing string) insightcontract.Assessment {
	return insightcontract.Assessment{State: "indeterminate", Level: "unknown", Rationale: reason, EvidenceIDs: []string{}, Missing: []string{missing}}
}
func providerName(c insightcontract.AnalysisConfig) string {
	if c.Provider.Kind == "configured" {
		return c.Provider.EndpointProfile
	}
	return "none"
}
func hash(v any) string {
	b, _ := json.Marshal(v)
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}
func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
func nonEmpty(s string) []string {
	if s == "" {
		return []string{}
	}
	return []string{s}
}
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
