package decisionengine

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"pocketcli/internal/investigation/insightcontract"
)

type fixture struct {
	Config insightcontract.AnalysisConfig `json:"config"`
	Source insightcontract.SourceBundle   `json:"source"`
	Facts  insightcontract.FactsResult    `json:"facts"`
}

type goldenRelation struct {
	ID            string  `json:"id"`
	Class         string  `json:"class"`
	Proposed      string  `json:"proposed"`
	FromID        string  `json:"from_id"`
	ToID          string  `json:"to_id"`
	FromEventAt   *string `json:"from_event_at"`
	ToEventAt     *string `json:"to_event_at"`
	FromScope     string  `json:"from_scope"`
	ToScope       string  `json:"to_scope"`
	FromValidFrom *string `json:"from_valid_from"`
	FromValidTo   *string `json:"from_valid_to"`
	ToValidFrom   *string `json:"to_valid_from"`
	ToValidTo     *string `json:"to_valid_to"`
	FromValidity  string  `json:"from_validity"`
	ToValidity    string  `json:"to_validity"`
	Accept        bool    `json:"accept"`
}

type providerFunc func(context.Context, insightcontract.ReasoningRequest) (insightcontract.ReasoningResponse, error)

func (f providerFunc) Evaluate(ctx context.Context, req insightcontract.ReasoningRequest) (insightcontract.ReasoningResponse, error) {
	return f(ctx, req)
}

func loadFixture(t *testing.T) fixture {
	t.Helper()
	b, err := os.ReadFile("../insightcontract/testdata/fixture_go.json")
	if err != nil {
		t.Fatal(err)
	}
	var f fixture
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestAnalyzeNoneAbstainsAndPreservesFactCases(t *testing.T) {
	f := loadFixture(t)
	f.Config.Provider.Kind = "none"
	a, err := Analyze(context.Background(), f.Source, f.Facts, f.Config, nil)
	if err != nil {
		t.Fatal(err)
	}
	if a.Status != "partial" || len(a.Cases) != len(f.Facts.Cases) {
		t.Fatalf("status=%q cases=%d facts=%d", a.Status, len(a.Cases), len(f.Facts.Cases))
	}
	for _, c := range a.Cases {
		if c.State != "needs_evidence" || c.Impact.Level != "unknown" || c.Confidence.Level != "unknown" {
			t.Fatalf("unsupported judgment: %#v", c)
		}
	}
}

func TestProviderPolicyBlocksExternalCall(t *testing.T) {
	f := loadFixture(t)
	called := false
	p := providerFunc(func(context.Context, insightcontract.ReasoningRequest) (insightcontract.ReasoningResponse, error) {
		called = true
		return insightcontract.ReasoningResponse{}, nil
	})
	_, err := Analyze(context.Background(), f.Source, f.Facts, f.Config, p)
	if err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("provider chamado apesar de ExternalProcessingAllowed=false")
	}
}

func TestMetadataOnlyStillRequiresExternalProcessingAuthorization(t *testing.T) {
	f := loadFixture(t)
	f.Config.Policy.ContentMode = "metadata_only"
	f.Config.Policy.ExternalProcessingAllowed = false
	called := false
	p := providerFunc(func(context.Context, insightcontract.ReasoningRequest) (insightcontract.ReasoningResponse, error) {
		called = true
		return insightcontract.ReasoningResponse{}, nil
	})
	a, err := Analyze(context.Background(), f.Source, f.Facts, f.Config, p)
	if err != nil {
		t.Fatal(err)
	}
	if called || len(a.Limitations) == 0 {
		t.Fatal("metadata_only permitiu provider externo sem autorização")
	}
}

func TestProviderRequestHonorsMetadataOnlyAndExcerptLimit(t *testing.T) {
	f := loadFixture(t)
	f.Config.Policy.ContentMode = "metadata_only"
	f.Config.Policy.ExternalProcessingAllowed = true
	f.Source.Entities[0].Title = "SENTINEL-PRIVATE-TEXT"
	f.Facts.Cases[0].Title = "SENTINEL-PRIVATE-TEXT"
	f.Facts.Cases[0].ObservedFacts = []string{"SENTINEL-PRIVATE-TEXT"}
	var got insightcontract.ReasoningRequest
	p := providerFunc(func(_ context.Context, req insightcontract.ReasoningRequest) (insightcontract.ReasoningResponse, error) {
		got = req
		return insightcontract.ReasoningResponse{Analysis: insightcontract.DecisionAnalysis{Version: insightcontract.Version, ID: "a", SnapshotID: f.Source.Snapshot.ID, Status: "partial", Claims: []insightcontract.Claim{}, ClaimRelations: []insightcontract.ClaimRelation{}, Decisions: []insightcontract.DecisionRecord{}, Relations: []insightcontract.DecisionRelation{}, Cases: []insightcontract.DecisionCase{}, Dimensions: []insightcontract.DimensionStatus{}, Limitations: []string{}}, Usage: insightcontract.Usage{}}, nil
	})
	if _, err := Analyze(context.Background(), f.Source, f.Facts, f.Config, p); err != nil {
		t.Fatal(err)
	}
	if len(got.Evidence) == 0 {
		t.Fatal("fixture sem evidências")
	}
	for _, ev := range got.Evidence {
		if ev.Excerpt != "" || ev.ExcerptHash != "" {
			t.Fatalf("metadata_only enviou trecho: %#v", ev)
		}
	}
	encoded, _ := json.Marshal(got)
	if strings.Contains(string(encoded), "SENTINEL-PRIVATE-TEXT") {
		t.Fatal("metadata_only enviou texto livre derivado da fonte")
	}
}

func TestProviderErrorDegradesWithoutInventingJudgment(t *testing.T) {
	f := loadFixture(t)
	f.Config.Policy.ExternalProcessingAllowed = true
	p := providerFunc(func(context.Context, insightcontract.ReasoningRequest) (insightcontract.ReasoningResponse, error) {
		return insightcontract.ReasoningResponse{}, context.DeadlineExceeded
	})
	a, err := Analyze(context.Background(), f.Source, f.Facts, f.Config, p)
	if err != nil {
		t.Fatal(err)
	}
	if a.Status != "partial" || a.Usage.Requests != 1 {
		t.Fatalf("fallback inesperado: status=%s usage=%#v", a.Status, a.Usage)
	}
	for _, c := range a.Cases {
		if c.Impact.Level != "unknown" || c.Confidence.Level != "unknown" {
			t.Fatal("erro do provider virou julgamento")
		}
	}
}

func TestRejectsResponseForDifferentSnapshot(t *testing.T) {
	f := loadFixture(t)
	response := insightcontract.ReasoningResponse{Analysis: insightcontract.DecisionAnalysis{Version: insightcontract.Version, ID: "x", SnapshotID: "OTHER", Status: "partial"}}
	if err := validateResponse(response, f.Source, f.Facts, f.Config); err == nil {
		t.Fatal("esperava rejeição de snapshot divergente")
	}
}

func TestValidityUnknownCannotMasqueradeAsOpenEnded(t *testing.T) {
	start := "2026-10-01T00:00:00Z"
	if err := validateValidityRange(&start, nil, "unknown"); err == nil {
		t.Fatal("unknown com início deve ser rejeitado")
	}
	if err := validateValidityRange(&start, nil, "open_ended"); err != nil {
		t.Fatal(err)
	}
}

func TestMetadataOnlyCannotSubstantiateSemanticClaimFromHashAlone(t *testing.T) {
	f := loadFixture(t)
	evidence := f.Source.Evidence[0]
	evidence.ContentStatus = "metadata_only"
	evidence.Excerpt = ""
	evidence.ExcerptHash = ""
	f.Source.Evidence[0] = evidence
	claim := insightcontract.Claim{ID: "claim-1", Text: "A fonte determina uma decisão", EntityIDs: []string{}, Scope: insightcontract.ContextScope{ResourceIDs: []string{}, EntityIDs: []string{}, ComponentIDs: []string{}, Conditions: []string{}, Status: "unknown"}, Kind: "decision", Method: "model", ValidityStatus: "unknown", Citations: []insightcontract.Citation{{EvidenceID: evidence.ID, Quote: ""}}, Status: "unverified"}
	a := insightcontract.DecisionAnalysis{Version: insightcontract.Version, ID: "a", SnapshotID: f.Source.Snapshot.ID, Status: "partial", Claims: []insightcontract.Claim{claim}, ClaimRelations: []insightcontract.ClaimRelation{}, Decisions: []insightcontract.DecisionRecord{}, Relations: []insightcontract.DecisionRelation{}, Cases: []insightcontract.DecisionCase{}, Dimensions: []insightcontract.DimensionStatus{}, Limitations: []string{}}
	if err := validateResponse(insightcontract.ReasoningResponse{Analysis: a}, f.Source, f.Facts, f.Config); err == nil {
		t.Fatal("aceitou claim sustentada apenas por ID/hash")
	}
}

func TestAnalyzeRejectsMissingEvidenceReference(t *testing.T) {
	f := loadFixture(t)
	if len(f.Facts.Cases) == 0 {
		t.Fatal("fixture sem fatos")
	}
	f.Facts.Cases[0].EvidenceIDs = append(f.Facts.Cases[0].EvidenceIDs, "EVIDENCE-NOT-IN-BUNDLE")
	if _, err := Analyze(context.Background(), f.Source, f.Facts, f.Config, nil); err == nil {
		t.Fatal("esperava rejeição de evidence ID ausente")
	}
}

func TestCacheKeyChangesWithContentAndRubric(t *testing.T) {
	f := loadFixture(t)
	a := deterministic(f.Source, f.Facts, f.Config)
	f.Source.Evidence[0].Excerpt += " alteração"
	b := deterministic(f.Source, f.Facts, f.Config)
	if a.Provenance.CacheKey == b.Provenance.CacheKey {
		t.Fatal("mudança de conteúdo não invalidou cache")
	}
	f.Config.Policy.Version += "-nova"
	c := deterministic(f.Source, f.Facts, f.Config)
	if b.Provenance.CacheKey == c.Provenance.CacheKey {
		t.Fatal("mudança de política não invalidou cache")
	}
}

func TestCanonicalRelationKinds(t *testing.T) {
	for _, kind := range []string{"supports", "supersedes", "revokes", "contradicts", "different_scope", "unresolved"} {
		if !validRelation(kind, "established") {
			t.Errorf("enum canônico rejeitado: %s", kind)
		}
	}
	if validRelation("invented", "established") {
		t.Fatal("aceitou enum fora do contrato")
	}
}

func TestClaimRelationRequiresTemporalAndScopeSupport(t *testing.T) {
	dateA, dateB := "2026-01-01T00:00:00Z", "2026-02-01T00:00:00Z"
	prod := "production"
	scope := insightcontract.ContextScope{ResourceIDs: []string{"repo-1"}, EntityIDs: []string{"service-1"}, Environment: &prod, Description: "service-1 em production", Status: "known"}
	claims := map[string]insightcontract.Claim{
		"A": {ID: "A", EventAt: &dateA, ValidFrom: &dateA, ValidityStatus: "open_ended", Scope: scope},
		"B": {ID: "B", EventAt: &dateB, ValidFrom: &dateB, ValidityStatus: "open_ended", Scope: scope},
	}
	if err := validateClaimRelation(insightcontract.ClaimRelation{FromClaimID: "B", ToClaimID: "A", Kind: "supersedes"}, claims); err != nil {
		t.Fatalf("relação posterior explícita deveria passar: %v", err)
	}
	if err := validateClaimRelation(insightcontract.ClaimRelation{FromClaimID: "A", ToClaimID: "B", Kind: "supersedes"}, claims); err == nil {
		t.Fatal("aceitou direção de supersedes invertida")
	}
	if err := validateClaimRelation(insightcontract.ClaimRelation{FromClaimID: "A", ToClaimID: "B", Kind: "contradicts"}, claims); err != nil {
		t.Fatalf("valididades abertas se sobrepõem: %v", err)
	}
	unknownEnvironment := scope
	unknownEnvironment.Environment = nil
	unknownEnvironment.Description = ""
	claims["A"] = insightcontract.Claim{ID: "A", EventAt: &dateA, ValidFrom: &dateA, ValidityStatus: "open_ended", Scope: unknownEnvironment}
	claims["B"] = insightcontract.Claim{ID: "B", EventAt: &dateB, ValidFrom: &dateB, ValidityStatus: "open_ended", Scope: unknownEnvironment}
	if err := validateClaimRelation(insightcontract.ClaimRelation{FromClaimID: "A", ToClaimID: "B", Kind: "contradicts"}, claims); err == nil {
		t.Fatal("escopo sem ambiente documentado foi tratado como igual")
	}
}

func TestSyntheticGoldenBenchmarkReportsPrecisionCoverageAndAbstention(t *testing.T) {
	data, err := os.ReadFile("testdata/golden_relations.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden struct {
		Version   string           `json:"version"`
		Synthetic bool             `json:"synthetic"`
		Cases     []goldenRelation `json:"cases"`
	}
	if err := json.Unmarshal(data, &golden); err != nil {
		t.Fatal(err)
	}
	if golden.Version != "decisionengine-golden-relations-v1" || !golden.Synthetic || len(golden.Cases) == 0 {
		t.Fatal("dataset golden inválido")
	}
	type metric struct{ total, positive, accepted, truePositive, falsePositive, abstained int }
	byClass := map[string]*metric{}
	for _, tc := range golden.Cases {
		m := byClass[tc.Class]
		if m == nil {
			m = &metric{}
			byClass[tc.Class] = m
		}
		m.total++
		if tc.Accept {
			m.positive++
		}
		envFrom, envTo := tc.FromScope, tc.ToScope
		scopeFrom := insightcontract.ContextScope{ResourceIDs: []string{"resource-1"}, EntityIDs: []string{"service-1"}, Environment: &envFrom, Description: "service production setting", Status: "known"}
		scopeTo := insightcontract.ContextScope{ResourceIDs: []string{"resource-1"}, EntityIDs: []string{"service-1"}, Environment: &envTo, Description: "service production setting", Status: "known"}
		claims := map[string]insightcontract.Claim{
			tc.FromID: {ID: tc.FromID, EventAt: tc.FromEventAt, ValidFrom: tc.FromValidFrom, ValidTo: tc.FromValidTo, ValidityStatus: tc.FromValidity, Scope: scopeFrom},
			tc.ToID:   {ID: tc.ToID, EventAt: tc.ToEventAt, ValidFrom: tc.ToValidFrom, ValidTo: tc.ToValidTo, ValidityStatus: tc.ToValidity, Scope: scopeTo},
		}
		err := validateClaimRelation(insightcontract.ClaimRelation{ID: tc.ID, FromClaimID: tc.FromID, ToClaimID: tc.ToID, Kind: tc.Proposed}, claims)
		accepted := err == nil
		if accepted {
			m.accepted++
			if tc.Accept && tc.Proposed == tc.Class {
				m.truePositive++
			} else {
				m.falsePositive++
			}
		} else {
			m.abstained++
		}
		if accepted != tc.Accept {
			t.Errorf("%s: aceite=%t esperado=%t; err=%v", tc.ID, accepted, tc.Accept, err)
		}
	}
	for _, class := range []string{"supersedes", "contradicts", "different_scope"} {
		m := byClass[class]
		if m == nil || m.total == 0 || m.positive == 0 {
			t.Fatalf("denominador ausente para classe %s", class)
		}
		precisionDenom := m.truePositive + m.falsePositive
		coverage := float64(m.truePositive) / float64(m.positive)
		abstention := float64(m.abstained) / float64(m.total)
		t.Logf("dataset=%s class=%s total=%d positives=%d accepted=%d precision=%d/%d coverage=%.2f abstention=%d/%d (%.2f)", golden.Version, class, m.total, m.positive, m.accepted, m.truePositive, precisionDenom, coverage, m.abstained, m.total, abstention)
		if m.falsePositive != 0 || coverage != 1 || abstention != .5 {
			t.Errorf("métrica inesperada para %s: %#v", class, m)
		}
	}
}
