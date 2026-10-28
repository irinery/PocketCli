package core

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/insightcontract"
	"pocketcli/internal/investigation/query"
	"pocketcli/internal/investigation/storage"
)

func TestReadInsightSourceSeparatesScopeCoverageEvidenceAndLinks(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c := &Core{store: store}
	object := json.RawMessage(`{"title":"Release note","body":"Uses token=ghp_abcdefghijklmnopqrstuvwxyz123456 and is a synthetic change."}`)
	if err := store.Put("objects", "ev-issue", object); err != nil {
		t.Fatal(err)
	}
	hash := storage.Hash(object)
	snapshot := contract.Snapshot{SchemaVersion: contract.Version, QueryCatalogVersion: "q1", WindowStart: "2026-10-21T00:00:00Z", WindowEnd: "2026-10-28T00:00:00Z", AnalysisReferenceAt: "2026-10-28T00:00:00Z", Resources: []contract.Resource{{Source: "linear", ID: "TEAM", Name: "Team"}}}
	dataset := query.Dataset{Snapshot: snapshot,
		Coverage: []contract.Coverage{{Source: "linear", ResourceID: "TEAM", Collection: "issues", Status: "complete", Observed: 1, Expected: nil, CheckedAt: "2026-10-28T00:00:00Z"}},
		Issues:   []query.Issue{{ID: "ISS-1", Identifier: "TEAM-1", Source: "linear", TeamID: "TEAM", Title: "Release", State: "Backlog", Outcome: "active", EvidenceID: "ev-issue"}, {ID: "ISS-2", Identifier: "TEAM-2", Source: "linear", TeamID: "TEAM", Title: "Related", State: "Backlog", Outcome: "active", EvidenceID: "ev-issue"}, {ID: "OUT-1", Identifier: "OTHER-1", Source: "linear", TeamID: "OTHER", Title: "Outside scope", State: "Done", Outcome: "completed"}},
		Evidence: []contract.Evidence{{ID: "ev-issue", Source: "linear", Locator: "TEAM-1", ObservedAt: "2026-10-28T00:00:00Z", SHA256: hash}},
		Links:    []contract.LinkRecord{{ID: "formal", SourceID: "ISS-1", TargetID: "ISS-2", Relation: "blocked_by", Status: "direct", EvidenceID: "ev-issue"}, {ID: "mention", SourceID: "ISS-1", TargetID: "ISS-2", Relation: "mentions", Status: "direct", EvidenceID: "ev-issue"}, {ID: "fix", SourceID: "ISS-1", TargetID: "ISS-2", Relation: "declares_fix_of", Status: "direct", EvidenceID: "ev-issue"}, {ID: "orphan", SourceID: "ISS-1", TargetID: "MISSING", Relation: "blocked_by", Status: "direct", EvidenceID: "ev-issue"}},
	}
	snapshot, dataset = sealInsightDataset(t, dataset)
	if err := store.Put("snapshots", snapshot.ID, snapshot); err != nil {
		t.Fatal(err)
	}
	if err := store.Put("datasets", snapshot.ID, dataset); err != nil {
		t.Fatal(err)
	}
	manifest := insightCoverageManifest{SnapshotID: snapshot.ID, DatasetHash: snapshot.DatasetHash, Coverage: []insightcontract.SourceCoverage{{Key: insightcontract.CoverageKey{Source: "linear", ResourceID: "TEAM", Collection: "issues"}, Status: "complete", Observed: 1, Expected: nil, HistoryCoveredIntervals: []insightcontract.Window{{Start: "2026-10-21T00:00:00Z", End: "2026-10-28T00:00:00Z"}}, QueueIDs: []string{}}}, Queues: []insightcontract.QueueProgress{}}
	if err := store.Put("insightcoverage", snapshot.ID, manifest); err != nil {
		t.Fatal(err)
	}
	cfg := insightcontract.AnalysisConfig{Version: insightcontract.Version, ID: "cfg", RequestedScope: insightcontract.Scope{History: insightcontract.Window{Start: "2022-10-28T00:00:00Z", End: "2026-10-28T00:00:00Z"}, ReportWindow: insightcontract.Window{Start: "2026-10-21T00:00:00Z", End: "2026-10-28T00:00:00Z"}, Resources: []insightcontract.ResourceRef{{Source: "linear", ID: "TEAM", Name: "Team"}}}, Policy: insightcontract.Policy{ContentMode: "local_excerpts", ExcerptMaxRunes: 200}}
	bundle, err := c.ReadInsightSource(context.Background(), snapshot.ID, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Snapshot.Window.Start != snapshot.WindowStart || bundle.RequestedScope.History.Start != cfg.RequestedScope.History.Start || len(bundle.ObservedResources) != 1 {
		t.Fatalf("escopos colapsados: %+v", bundle)
	}
	if len(bundle.Coverage) != 1 || bundle.Coverage[0].Expected != nil || len(bundle.Coverage[0].HistoryCoveredIntervals) != 1 {
		t.Fatalf("cobertura perdeu null/intervalo: %+v", bundle.Coverage)
	}
	if len(bundle.Entities) != 2 {
		t.Fatalf("dados fora do escopo entraram no bundle: %+v", bundle.Entities)
	}
	var historyQuestion insightcontract.QuestionCoverage
	for _, question := range bundle.Questions {
		if question.QuestionID == "history_scope" {
			historyQuestion = question
		}
	}
	if historyQuestion.Status != "insufficient" {
		t.Fatalf("quatro anos foram considerados cobertos: %+v", historyQuestion)
	}
	if len(bundle.Evidence) != 1 || bundle.Evidence[0].IntegrityStatus != "verified" || !bundle.Evidence[0].Redacted || !strings.Contains(bundle.Evidence[0].Excerpt, "[REDACTED]") {
		t.Fatalf("evidência não sanitizada/verificada: %+v", bundle.Evidence)
	}
	statuses := map[string]string{}
	for _, link := range bundle.Links {
		statuses[link.ID] = link.Status
	}
	if statuses["formal"] != "explicit" || statuses["mention"] != "candidate" || statuses["fix"] != "candidate" || statuses["orphan"] != "invalid" {
		t.Fatalf("status de links incorreto: %v", statuses)
	}
	second, err := c.ReadInsightSource(context.Background(), snapshot.ID, cfg)
	if err != nil || second.InputHash != bundle.InputHash {
		t.Fatalf("leitura não determinística: %s != %s, err=%v", bundle.InputHash, second.InputHash, err)
	}
}

func TestReadInsightSourceLegacySnapshotHasUnknownCoverage(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	snapshot := contract.Snapshot{Coverage: []contract.Coverage{{Source: "github", ResourceID: "owner/repo", Collection: "pull_requests", Status: "complete", Observed: 4}}, Resources: []contract.Resource{{Source: "github", ID: "owner/repo", Name: "repo"}}}
	dataset := query.Dataset{Snapshot: snapshot}
	snapshot, dataset = sealInsightDataset(t, dataset)
	if err := store.Put("snapshots", snapshot.ID, snapshot); err != nil {
		t.Fatal(err)
	}
	if err := store.Put("datasets", snapshot.ID, dataset); err != nil {
		t.Fatal(err)
	}
	c := &Core{store: store}
	bundle, err := c.ReadInsightSource(context.Background(), snapshot.ID, insightcontract.AnalysisConfig{RequestedScope: insightcontract.Scope{History: insightcontract.Window{Start: "2022-01-01T00:00:00Z", End: "2026-01-01T00:00:00Z"}, Resources: []insightcontract.ResourceRef{{Source: "github", ID: "owner/repo", Name: "repo"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Coverage) != 1 || bundle.Coverage[0].Status != "unknown" || len(bundle.Limitations) == 0 {
		t.Fatalf("snapshot legado recebeu cobertura retrospectiva: %+v", bundle)
	}
}

func TestInsightEvidenceSeparatesHashAvailabilityRedactionAndTruncation(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	core := &Core{store: store}
	valid := json.RawMessage(`{"body":"abcdef"}`)
	if err := store.Put("objects", "ev-valid", valid); err != nil {
		t.Fatal(err)
	}
	policy := insightcontract.Policy{ContentMode: "local_excerpts", ExcerptMaxRunes: 3}
	view := core.insightEvidence(contract.Evidence{ID: "ev-valid", SHA256: storage.Hash(valid), Redacted: true}, policy)
	if view.IntegrityStatus != "verified" || view.ContentStatus != "available" || view.Excerpt != "abc" || !view.Truncated || view.Redacted {
		t.Fatalf("dimensões de conteúdo misturadas: %+v", view)
	}
	metadata := core.insightEvidence(contract.Evidence{ID: "ev-valid", SHA256: storage.Hash(valid)}, insightcontract.Policy{ContentMode: "metadata_only"})
	if metadata.ContentStatus != "metadata_only" || metadata.Excerpt != "" || metadata.IntegrityStatus != "verified" {
		t.Fatalf("metadata_only incorreto: %+v", metadata)
	}
	mismatch := core.insightEvidence(contract.Evidence{ID: "ev-valid", SHA256: strings.Repeat("0", 64)}, policy)
	if mismatch.ContentStatus != "integrity_error" || mismatch.IntegrityStatus != "mismatch" || mismatch.Excerpt != "" {
		t.Fatalf("digest divergente expôs conteúdo: %+v", mismatch)
	}
	missing := core.insightEvidence(contract.Evidence{ID: "ev-missing", SHA256: strings.Repeat("0", 64)}, policy)
	if missing.ContentStatus != "unavailable" || missing.IntegrityStatus != "missing" {
		t.Fatalf("objeto ausente mal classificado: %+v", missing)
	}
}

func TestIntervalsCoverRejectsGaps(t *testing.T) {
	wanted := insightcontract.Window{Start: "2022-01-01T00:00:00Z", End: "2022-01-10T00:00:00Z"}
	if intervalsCover([]insightcontract.Window{{Start: "2022-01-01T00:00:00Z", End: "2022-01-04T00:00:00Z"}, {Start: "2022-01-05T00:00:00Z", End: "2022-01-10T00:00:00Z"}}, wanted) {
		t.Fatal("intervalos com lacuna provaram cobertura contínua")
	}
	if !intervalsCover([]insightcontract.Window{{Start: "2022-01-01T00:00:00Z", End: "2022-01-04T00:00:00Z"}, {Start: "2022-01-04T00:00:00Z", End: "2022-01-10T00:00:00Z"}}, wanted) {
		t.Fatal("intervalos adjacentes não cobriram janela")
	}
}

func TestCoverageStatusesKeepPartialAndNotSelectedDistinct(t *testing.T) {
	if normalizeCoverageStatus("partial") != "partial" || normalizeCoverageStatus("not_selected") != "not_selected" {
		t.Fatal("partial e not_selected foram colapsados")
	}
	cfg := insightcontract.AnalysisConfig{RequestedScope: insightcontract.Scope{Resources: []insightcontract.ResourceRef{{Source: "linear", ID: "team"}}}}
	items := []insightcontract.SourceCoverage{{Key: insightcontract.CoverageKey{Source: "linear", ResourceID: "team", Collection: "issues"}, Status: "partial"}, {Key: insightcontract.CoverageKey{Source: "linear", ResourceID: "team", Collection: "history"}, Status: "not_selected"}}
	questions := questionCoverage(cfg, items)
	if questions[0].Status != "insufficient" || len(questions[0].Missing) == 0 {
		t.Fatalf("cobertura mista foi tratada como completa: %+v", questions[0])
	}
}

func TestCoverageCatalogDoesNotOverstateGitHubHistoryOrMissingTargets(t *testing.T) {
	window := insightcontract.Window{Start: "2022-10-28T00:00:00Z", End: "2026-10-28T00:00:00Z"}
	cfg := insightcontract.AnalysisConfig{RequestedScope: insightcontract.Scope{History: window, Resources: []insightcontract.ResourceRef{{Source: "github", ID: "42", Name: "owner/repo"}}}}
	interval := []insightcontract.Window{window}
	coverage := []insightcontract.SourceCoverage{{Key: insightcontract.CoverageKey{Source: "github", ResourceID: "42", Collection: "issues"}, Status: "complete", HistoryCoveredIntervals: interval}, {Key: insightcontract.CoverageKey{Source: "github", ResourceID: "42", Collection: "pull_requests"}, Status: "complete", HistoryCoveredIntervals: interval}}
	questions := questionCoverage(cfg, coverage)
	if questions[0].Status != "partial" || len(questions[0].Missing) == 0 {
		t.Fatalf("intervalos GitHub foram tratados como histórico de estados: %+v", questions[0])
	}
	var objective insightcontract.QuestionCoverage
	for _, item := range questions {
		if item.QuestionID == "objective_status" {
			objective = item
		}
	}
	if objective.Status != "insufficient" {
		t.Fatalf("objetivo sem meta/critério virou avaliável: %+v", objective)
	}
}

func TestCoverageSidecarFreezesIntervalsAndOpaqueCheckpoint(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	core := &Core{store: store}
	resource := contract.Resource{Source: "linear", ID: "team-1", Name: "Team"}
	plan := contract.Plan{ID: "plan-1", InvestigationID: "investigation-1", Hash: "plan-hash", WindowStart: "2026-10-21T00:00:00Z", WindowEnd: "2026-10-28T00:00:00Z", Resources: []contract.Resource{resource}, Input: contract.PlanInput{LinearEnabled: true}}
	fingerprint := "auth-fingerprint"
	queryHash := storage.Hash([]byte("linear-graphql-v1:updated-desc:first-100:issues"))
	cursorID := storage.Hash([]byte(plan.InvestigationID + ":linear:" + resource.ID + ":issues:" + queryHash + ":" + fingerprint))
	if err := store.Put("planauth", plan.ID, planAuth{Linear: fingerprint}); err != nil {
		t.Fatal(err)
	}
	cursor := sourceCursor{Source: "linear", ResourceID: resource.ID, Collection: "issues", QueryHash: queryHash, AuthScopeFingerprint: fingerprint, CoveredIntervals: []sourceInterval{{Start: "2026-10-21T00:00:00Z", End: "2026-10-28T00:00:00Z"}}, Watermark: "2026-10-28T00:00:00Z", CheckpointPlanID: plan.ID, Checkpoint: "opaque-cursor"}
	if err := store.Put("sourcecursors", cursorID, cursor); err != nil {
		t.Fatal(err)
	}
	snapshot, dataset := sealInsightDataset(t, query.Dataset{Snapshot: contract.Snapshot{Resources: []contract.Resource{resource}}, Coverage: []contract.Coverage{{Source: "linear", ResourceID: resource.ID, Collection: "issues", Status: "partial", Observed: 1}}})
	if err := core.freezeInsightCoverage(snapshot, plan, dataset); err != nil {
		t.Fatal(err)
	}
	cursor.CoveredIntervals = []sourceInterval{{Start: "2022-10-28T00:00:00Z", End: "2026-10-28T00:00:00Z"}}
	cursor.Checkpoint = "newer-secret-cursor"
	if err := store.Put("sourcecursors", cursorID, cursor); err != nil {
		t.Fatal(err)
	}
	if err := core.freezeInsightCoverage(snapshot, plan, dataset); err != nil {
		t.Fatal(err)
	}
	var manifest insightCoverageManifest
	if err := store.Get("insightcoverage", snapshot.ID, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Coverage) != 1 || len(manifest.Coverage[0].HistoryCoveredIntervals) != 1 || manifest.Coverage[0].HistoryCoveredIntervals[0].Start != "2026-10-21T00:00:00Z" {
		t.Fatalf("sidecar foi atualizado pelo cursor mutável: %+v", manifest.Coverage)
	}
	if len(manifest.Queues) != 1 || manifest.Queues[0].Pending != nil || manifest.Queues[0].CheckpointID == nil || strings.Contains(*manifest.Queues[0].CheckpointID, "opaque-cursor") || strings.Contains(*manifest.Queues[0].CheckpointID, "newer-secret-cursor") {
		t.Fatalf("fila expôs checkpoint ou converteu pending nulo: %+v", manifest.Queues)
	}
}

func TestCommonLunaFixtureUsesCanonicalSourceBundle(t *testing.T) {
	data, err := os.ReadFile("../insightcontract/testdata/fixture_go.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Config insightcontract.AnalysisConfig `json:"config"`
		Source insightcontract.SourceBundle   `json:"source"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Config.Version != insightcontract.Version || fixture.Source.Version != insightcontract.Version || fixture.Source.Snapshot.ID == "" || fixture.Source.RequestedScope.History.Start == "" {
		t.Fatalf("fixture não segue o contrato congelado: config=%+v source=%+v", fixture.Config, fixture.Source)
	}
	if fixture.Source.Coverage == nil || fixture.Source.Questions == nil || fixture.Source.Queues == nil || fixture.Source.Evidence == nil || fixture.Source.Links == nil {
		t.Fatal("coleções canônicas devem ser arrays")
	}
}

func sealInsightDataset(t *testing.T, dataset query.Dataset) (contract.Snapshot, query.Dataset) {
	t.Helper()
	dataset.Snapshot.ID = ""
	dataset.Snapshot.DatasetHash = ""
	dataset.Snapshot.PublishedAt = ""
	canonical, err := json.Marshal(dataset)
	if err != nil {
		t.Fatal(err)
	}
	dataset.Snapshot.DatasetHash = storage.Hash(canonical)
	dataset.Snapshot.ID = "snap-" + dataset.Snapshot.DatasetHash[:24]
	dataset.Snapshot.PublishedAt = "2026-10-28T00:00:00Z"
	return dataset.Snapshot, dataset
}
