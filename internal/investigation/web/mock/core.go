package mock

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"pocketcli/internal/investigation/contract"
)

// Core is an explicit, offline-only implementation of investigate.v1. Its
// fixtures are synthetic and every visible result is marked DEMO.
type Core struct {
	mu        sync.RWMutex
	plan      contract.Plan
	runs      map[string]contract.Run
	keys      map[string]string
	scenario  string
	runStages map[string]int
	nextRun   int
	queries   []contract.SuggestedQuery
	snapshot  contract.Snapshot
	findings  []contract.Finding
	evidence  map[string]contract.Evidence
}

func New() *Core {
	return NewScenario("complete")
}

func NewScenario(scenario string) *Core {
	if scenario == "" {
		scenario = "complete"
	}
	now := time.Date(2026, 1, 31, 23, 0, 0, 0, time.UTC).Format(time.RFC3339)
	windowStart, windowEnd := "2026-01-01T00:00:00Z", "2026-02-01T00:00:00Z"
	queries := demoQueries()
	c := &Core{runs: map[string]contract.Run{}, keys: map[string]string{}, scenario: scenario, runStages: map[string]int{}, queries: queries, evidence: map[string]contract.Evidence{}}
	c.snapshot = contract.Snapshot{ID: "snap-demo-001", InvestigationID: "investigation-demo", SchemaVersion: contract.Version, RulesetVersion: "demo-v1", MappingVersion: "demo-v1", QueryCatalogVersion: "demo-v1", AnalysisReferenceAt: now, WindowStart: windowStart, WindowEnd: windowEnd, PublishedAt: now, DatasetHash: "demo-only", Status: "complete", PlanID: "plan-demo", PlanHash: "demo-only", Modules: contract.Modules{GitHubIssues: true, CodeHistory: true}, Resources: []contract.Resource{{ID: "demo/api", Source: "github", Name: "demo/api", Access: "read"}, {ID: "team-uni", Source: "linear", Name: "team-uni", Access: "read"}}, Coverage: []contract.Coverage{{Source: "github", ResourceID: "demo/api", Collection: "pull_requests", Status: "complete", Observed: 1, CheckedAt: now}, {Source: "github", ResourceID: "demo/api", Collection: "reviews", Status: "complete", Observed: 1, CheckedAt: now}, {Source: "linear", ResourceID: "team-uni", Collection: "issues", Status: "complete", Observed: 3, Expected: int64Ptr(3), CheckedAt: now}, {Source: "github", ResourceID: "demo/api", Collection: "actions", Status: "not_selected", Reason: "Módulo CI não selecionado", CheckedAt: now}}}
	c.findings = []contract.Finding{
		{ID: "f-1", RuleID: "R-DELIVERY-001", RuleVersion: "1.0.0", Severity: "high", SubjectID: "cmp-api", Title: "Correção não comprovada no artefato observado", DeterministicText: "Há correção registrada; sua presença no artefato observado não foi comprovada.", EvidenceStatus: "direct", EvidenceIDs: []string{"ev-4", "ev-8"}, Limitations: []string{"Fixture sintética de demonstração; não representa uma empresa real."}},
		{ID: "f-2", RuleID: "R-LINK-001", RuleVersion: "1.0.0", Severity: "medium", SubjectID: "iss-3", Title: "Sem vínculo explícito localizado", DeterministicText: "Não foi localizado vínculo explícito com PR no conjunto coletado; isso não demonstra ausência de implementação.", EvidenceStatus: "direct", EvidenceIDs: []string{"ev-3"}, Limitations: []string{"Fixture sintética de demonstração; não representa uma empresa real."}},
	}
	for i := 1; i <= 8; i++ {
		id := fmt.Sprintf("ev-%d", i)
		c.evidence[id] = contract.Evidence{ID: id, Source: "fixture", Locator: "https://example.invalid/evidence/" + fmt.Sprint(i), ObservedAt: now, SHA256: strings.Repeat("0", 64), Preview: fmt.Sprintf("Evidência fictícia %d; não representa dado real.", i)}
	}
	switch scenario {
	case "large_report":
		for i := 3; i <= 237; i++ {
			id := fmt.Sprintf("f-%03d", i)
			c.findings = append(c.findings, contract.Finding{ID: id, RuleID: "R-DEMO-001", RuleVersion: "1.0.0", Severity: "info", SubjectID: "cmp-api", Title: fmt.Sprintf("Achado demonstrativo %03d", i), DeterministicText: "Registro fictício usado para verificar paginação; não representa análise real.", EvidenceStatus: "unknown", EvidenceIDs: []string{}, Limitations: []string{"Dados fictícios."}})
		}
	case "empty":
		c.findings = []contract.Finding{}
	case "partial":
		c.snapshot.Status = "partial"
		c.snapshot.Coverage[2].Status = "partial"
		c.snapshot.Coverage[2].Observed = 1
		c.snapshot.Coverage[2].Reason = "Cobertura incompleta na fixture"
	case "rate_limited":
		c.snapshot.Status = "partial"
		c.snapshot.Coverage[0].Status = "rate_limited"
		c.snapshot.Coverage[0].Reason = "Limite de requisições da fixture"
	case "unauthorized":
		c.snapshot.Status = "partial"
		c.snapshot.Coverage[0].Status = "unauthorized"
		c.snapshot.Coverage[0].Reason = "Permissão negada na fixture"
	}
	return c
}
func int64Ptr(v int64) *int64 { return &v }

func (c *Core) PreparePlan(ctx context.Context, in contract.PlanInput) (contract.Plan, error) {
	if in.Name == "" {
		in.Name = "Investigação de demonstração"
	}
	if in.SchemaVersion == "" {
		in.SchemaVersion = contract.Version
	}
	if in.Period.Timezone == "" {
		in.Period.Timezone = "UTC"
	}
	if in.Period.Preset == "" {
		in.Period.Preset = "90d"
	}
	resolved, err := (Setup{}).ResolvePeriod(ctx, in.Period)
	if err != nil {
		return contract.Plan{}, err
	}
	canonical, err := json.Marshal(in)
	if err != nil {
		return contract.Plan{}, err
	}
	sum := sha256.Sum256(canonical)
	planHash := hex.EncodeToString(sum[:])
	resources := append([]contract.Resource(nil), c.snapshot.Resources...)
	plan := contract.Plan{InvestigationID: "investigation-demo", ID: "plan-demo", Hash: planHash, Input: in, Resources: resources, WindowStart: resolved.WindowStart, WindowEnd: resolved.WindowEnd, RequiresConfirmation: true, Warnings: []string{"DEMO: recursos, período e resultados são fictícios; o snapshot da fixture tem janela fixa."}}
	c.mu.Lock()
	c.plan = plan
	c.mu.Unlock()
	return plan, nil
}
func (c *Core) StartRun(_ context.Context, in contract.RunRequest) (contract.Run, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.plan.ID == "" || in.PlanID != c.plan.ID || in.ConfirmedPlanHash != c.plan.Hash {
		return contract.Run{}, &contract.ErrorResponse{Code: "ERR_PLAN_CHANGED", Message: "O plano precisa ser confirmado novamente.", Fields: []contract.FieldError{}}
	}
	if in.IdempotencyKey == "" {
		return contract.Run{}, &contract.ErrorResponse{Code: "ERR_VALIDATION", Message: "Idempotency key obrigatória.", Fields: []contract.FieldError{}}
	}
	keyContent := in.PlanID + ":" + in.ConfirmedPlanHash
	if old, ok := c.keys[in.IdempotencyKey]; ok && old != keyContent {
		return contract.Run{}, &contract.ErrorResponse{Code: "ERR_PLAN_CHANGED", Message: "A chave de idempotência já foi usada com outro plano.", Fields: []contract.FieldError{}}
	}
	if run, ok := c.runs[in.IdempotencyKey]; ok {
		return run, nil
	}
	c.nextRun++
	run := contract.Run{ID: fmt.Sprintf("run-demo-%03d", c.nextRun), PlanID: in.PlanID, State: "queued", Stage: "queued", Requests: 0, Bytes: 0, Items: 0, Coverage: "pending"}
	c.runs[in.IdempotencyKey] = run
	c.keys[in.IdempotencyKey] = keyContent
	c.runStages[run.ID] = 0
	return run, nil
}
func (c *Core) GetRun(_ context.Context, id string) (contract.Run, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, r := range c.runs {
		if r.ID == id {
			if r.State == "paused" || r.State == "cancelled" || r.State == "report_failed" {
				return r, nil
			}
			step := c.runStages[id]
			switch step {
			case 0:
				r.State = "collecting"
				r.Stage = "collecting"
				r.Requests = 3
				r.Bytes = 2048
				r.Items = 4
				r.Coverage = "partial"
			case 1:
				r.State = "building"
				r.Stage = "building"
				r.Items = 12
				r.Coverage = "complete"
			case 2:
				r.State = "completed"
				r.Stage = "analysis_ready"
				r.SnapshotID = c.snapshot.ID
			default:
				r.State = "completed"
				r.Stage = "analysis_ready"
				r.SnapshotID = c.snapshot.ID
			}
			if step < 3 {
				c.runStages[id] = step + 1
			}
			c.runs[key] = r
			return r, nil
		}
	}
	return contract.Run{}, notFound("run")
}
func (c *Core) transition(id, state string) (contract.Run, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, r := range c.runs {
		if r.ID == id {
			r.State = state
			if state == "paused" {
				r.Stage = "paused"
			}
			if state == "collecting" {
				r.Stage = "collecting"
				if c.runStages[id] < 1 {
					c.runStages[id] = 1
				}
			}
			if state == "cancelled" {
				r.Stage = "cancelled"
			}
			c.runs[k] = r
			return r, nil
		}
	}
	return contract.Run{}, notFound("run")
}

func (c *Core) SetRunState(id, state string, lastErr *contract.ErrorResponse) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, run := range c.runs {
		if run.ID == id {
			run.State = state
			run.LastError = lastErr
			c.runs[key] = run
		}
	}
}
func (c *Core) PauseRun(_ context.Context, id string) (contract.Run, error) {
	return c.transition(id, "paused")
}
func (c *Core) ResumeRun(_ context.Context, id string) (contract.Run, error) {
	return c.transition(id, "collecting")
}
func (c *Core) CancelRun(_ context.Context, id string) (contract.Run, error) {
	return c.transition(id, "cancelled")
}
func (c *Core) ListSnapshots(context.Context, string) ([]contract.Snapshot, error) {
	if c.scenario == "unauthorized" {
		return nil, &contract.ErrorResponse{Code: "ERR_SOURCE_FORBIDDEN", Message: "A fonte da fixture negou acesso; não há contagem zero válida.", Fields: []contract.FieldError{}}
	}
	return []contract.Snapshot{c.snapshot}, nil
}
func (c *Core) GetSnapshot(_ context.Context, id string) (contract.Snapshot, error) {
	if c.scenario == "unauthorized" {
		return contract.Snapshot{}, &contract.ErrorResponse{Code: "ERR_SOURCE_FORBIDDEN", Message: "A fonte da fixture negou acesso; não há contagem zero válida.", Fields: []contract.FieldError{}}
	}
	if id != c.snapshot.ID {
		return contract.Snapshot{}, notFound("snapshot")
	}
	return c.snapshot, nil
}
func (c *Core) SuggestedQueries(_ context.Context, id string) ([]contract.SuggestedQuery, error) {
	if id != c.snapshot.ID {
		return nil, notFound("snapshot")
	}
	return append([]contract.SuggestedQuery(nil), c.queries...), nil
}
func (c *Core) GetFinding(_ context.Context, snapshot, id string) (contract.Finding, error) {
	if snapshot != c.snapshot.ID {
		return contract.Finding{}, notFound("snapshot")
	}
	for _, f := range c.findings {
		if f.ID == id {
			return f, nil
		}
	}
	return contract.Finding{}, notFound("finding")
}

func (c *Core) Findings(snapshot string) ([]contract.Finding, error) {
	if snapshot != c.snapshot.ID {
		return nil, notFound("snapshot")
	}
	return append([]contract.Finding{}, c.findings...), nil
}
func (c *Core) GetEvidence(_ context.Context, snapshot, id string) (contract.Evidence, error) {
	if snapshot != c.snapshot.ID {
		return contract.Evidence{}, notFound("snapshot")
	}
	e, ok := c.evidence[id]
	if !ok {
		return contract.Evidence{}, notFound("evidence")
	}
	return e, nil
}
func (c *Core) Query(_ context.Context, in contract.QueryRequest) (contract.QueryResult, error) {
	if in.SnapshotID != c.snapshot.ID {
		return contract.QueryResult{}, notFound("snapshot")
	}
	queryID := ""
	for _, q := range c.queries {
		if strings.TrimSpace(in.SQL) == strings.TrimSpace(q.SQL) {
			queryID = q.ID
			break
		}
	}
	if queryID == "" {
		return contract.QueryResult{}, &contract.ErrorResponse{Code: "ERR_DEMO_QUERY_UNSUPPORTED", Message: "Será executada pelo motor SQL real; esta simulação não interpreta SQL livre.", Fields: []contract.FieldError{}}
	}
	result := contract.QueryResult{QueryID: "query-demo-" + strings.ToLower(queryID), SnapshotID: in.SnapshotID, QueryHash: "demo-only", Warnings: []string{"DADOS FICTÍCIOS; resultado do mock"}}
	switch queryID {
	case "Q01":
		result.Columns = []contract.Column{{Name: "component_id", DeclaredType: "TEXT"}, {Name: "name", DeclaredType: "TEXT"}, {Name: "repository", DeclaredType: "TEXT"}, {Name: "path", DeclaredType: "TEXT"}, {Name: "implementation_status", DeclaredType: "TEXT"}, {Name: "deployment_status", DeclaredType: "TEXT"}, {Name: "coverage", DeclaredType: "TEXT"}}
		result.Rows = [][]contract.Cell{{txt("cmp-api"), txt("API"), txt("demo/api"), txt("apps/api"), txt("identified"), txt("observed"), txt("complete")}, {txt("cmp-web"), txt("Web"), txt("demo/web"), txt("app"), txt("identified"), txt("unknown"), txt("complete")}}
	case "Q02":
		result.Columns = []contract.Column{{Name: "finding_id", DeclaredType: "TEXT"}, {Name: "rule_id", DeclaredType: "TEXT"}, {Name: "severity", DeclaredType: "TEXT"}, {Name: "subject_id", DeclaredType: "TEXT"}, {Name: "title", DeclaredType: "TEXT"}, {Name: "evidence_status", DeclaredType: "TEXT"}, {Name: "coverage", DeclaredType: "TEXT"}}
		for _, f := range c.findings {
			result.Rows = append(result.Rows, []contract.Cell{txt(f.ID), txt(f.RuleID), txt(f.Severity), txt(f.SubjectID), txt(f.Title), txt(f.EvidenceStatus), txt("complete")})
		}
	case "Q03":
		result.Columns = []contract.Column{{Name: "pr_id", DeclaredType: "TEXT"}, {Name: "repository", DeclaredType: "TEXT"}, {Name: "number", DeclaredType: "INTEGER"}, {Name: "wait_hours", DeclaredType: "REAL"}}
		minimum := int64(86400)
		if len(in.Parameters) > 0 {
			if in.Parameters[0].Name != "min_seconds" || in.Parameters[0].Value.Kind != "integer" {
				return contract.QueryResult{}, &contract.ErrorResponse{Code: "ERR_VALIDATION", Message: "min_seconds deve ser inteiro.", Fields: []contract.FieldError{{Field: "parameters.min_seconds", Message: "Inteiro obrigatório."}}}
			}
			if n, e := parseIntCell(in.Parameters[0].Value); e == nil {
				minimum = n
			} else {
				return contract.QueryResult{}, &contract.ErrorResponse{Code: "ERR_VALIDATION", Message: "min_seconds inválido.", Fields: []contract.FieldError{}}
			}
		}
		for _, pr := range []struct {
			id, repo string
			n        int
			sec      int64
		}{{"pr-10", "demo/api", 10, 172800}, {"pr-20", "demo/web", 20, 3600}} {
			if pr.sec >= minimum {
				result.Rows = append(result.Rows, []contract.Cell{txt(pr.id), txt(pr.repo), integer(int64(pr.n)), real(float64(pr.sec) / 3600)})
			}
		}
	case "Q04":
		result.Columns = []contract.Column{{Name: "issue_id", DeclaredType: "TEXT"}, {Name: "identifier", DeclaredType: "TEXT"}, {Name: "title", DeclaredType: "TEXT"}, {Name: "reopen_count", DeclaredType: "INTEGER"}}
		if c.scenario != "empty" {
			result.Rows = [][]contract.Cell{{txt("iss-1"), txt("UNI-1"), txt("Exportar pedidos"), integer(1)}}
		}
	case "Q05":
		result.Columns = []contract.Column{{Name: "issue_id", DeclaredType: "TEXT"}, {Name: "before_value", DeclaredType: "REAL"}, {Name: "after_value", DeclaredType: "REAL"}, {Name: "event_at", DeclaredType: "TEXT"}, {Name: "actor_id", DeclaredType: "TEXT"}}
		result.Rows = [][]contract.Cell{{txt("iss-1"), real(3), real(5), txt("2026-01-03T00:00:00Z"), txt("person-a")}}
	case "Q06":
		result.Columns = []contract.Column{{Name: "issue_id", DeclaredType: "TEXT"}, {Name: "identifier", DeclaredType: "TEXT"}, {Name: "title", DeclaredType: "TEXT"}}
		if c.scenario != "empty" {
			result.Rows = [][]contract.Cell{{txt("iss-3"), txt("UNI-3"), txt("Comportamento esperado não descrito")}}
		}
	case "Q07":
		result.Columns = []contract.Column{{Name: "source", DeclaredType: "TEXT"}, {Name: "resource_id", DeclaredType: "TEXT"}, {Name: "collection", DeclaredType: "TEXT"}, {Name: "status", DeclaredType: "TEXT"}, {Name: "observed_count", DeclaredType: "INTEGER"}, {Name: "expected_count", DeclaredType: "INTEGER"}, {Name: "reason", DeclaredType: "TEXT"}}
		for _, v := range c.snapshot.Coverage {
			expected := contract.Cell{Kind: "null"}
			if v.Expected != nil {
				expected = integer(*v.Expected)
			}
			result.Rows = append(result.Rows, []contract.Cell{txt(v.Source), txt(v.ResourceID), txt(v.Collection), txt(v.Status), integer(v.Observed), expected, txt(v.Reason)})
		}
	case "Q08":
		result.Columns = []contract.Column{{Name: "event_id", DeclaredType: "TEXT"}, {Name: "event_type", DeclaredType: "TEXT"}, {Name: "event_at", DeclaredType: "TEXT"}, {Name: "observed_at", DeclaredType: "TEXT"}, {Name: "old_value", DeclaredType: "TEXT"}, {Name: "new_value", DeclaredType: "TEXT"}}
		if len(in.Parameters) != 1 || in.Parameters[0].Name != "subject_id" || in.Parameters[0].Value.Kind != "text" || in.Parameters[0].Value.Value == "" {
			return contract.QueryResult{}, &contract.ErrorResponse{Code: "ERR_VALIDATION", Message: "Q08 exige subject_id textual.", Fields: []contract.FieldError{{Field: "parameters.subject_id", Message: "Escolha um alvo."}}}
		}
		if in.Parameters[0].Value.Value == "iss-1" {
			for _, v := range [][]string{{"evt-1", "started", "2026-01-02T00:00:00Z", "2026-01-31T23:00:00Z", "todo", "in_progress"}, {"evt-2", "estimate_changed", "2026-01-03T00:00:00Z", "2026-01-31T23:00:00Z", "3", "5"}, {"evt-3", "reopened", "2026-01-03T00:00:00Z", "2026-01-31T23:00:00Z", "done", "open"}, {"evt-4", "completed", "2026-01-04T00:00:00Z", "2026-01-31T23:00:00Z", "open", "done"}} {
				row := []contract.Cell{}
				for _, x := range v {
					row = append(row, txt(x))
				}
				result.Rows = append(result.Rows, row)
			}
		}
	case "Q09":
		result.Columns = []contract.Column{{Name: "assignee_id", DeclaredType: "TEXT"}, {Name: "completed_items", DeclaredType: "INTEGER"}}
		result.Rows = [][]contract.Cell{{txt("person-a"), integer(1)}, {txt("person-b"), integer(1)}}
	case "Q10":
		result.Columns = []contract.Column{{Name: "occurrence_id", DeclaredType: "TEXT"}, {Name: "subject_id", DeclaredType: "TEXT"}, {Name: "kind", DeclaredType: "TEXT"}, {Name: "evidence_status", DeclaredType: "TEXT"}, {Name: "cause_status", DeclaredType: "TEXT"}}
		result.Rows = [][]contract.Cell{{txt("rework-1"), txt("iss-1"), txt("reopening"), txt("direct"), txt("undetermined")}}
	default:
		return contract.QueryResult{}, &contract.ErrorResponse{Code: "ERR_DEMO_QUERY_UNSUPPORTED", Message: "Esta consulta não está no catálogo simulado.", Fields: []contract.FieldError{}}
	}
	result.ReturnedRows = len(result.Rows)
	result.ElapsedMS = 1
	max := in.MaxRows
	if max <= 0 {
		max = 1000
	}
	if len(result.Rows) > max {
		result.Rows = result.Rows[:max]
		result.ReturnedRows = max
		result.Truncated = true
	}
	return result, nil
}
func txt(v string) contract.Cell    { return contract.Cell{Kind: "text", Value: v} }
func integer(v int64) contract.Cell { return contract.Cell{Kind: "integer", Value: fmt.Sprint(v)} }
func real(v float64) contract.Cell  { return contract.Cell{Kind: "real", Value: fmt.Sprint(v)} }
func parseIntCell(c contract.Cell) (int64, error) {
	var n int64
	_, e := fmt.Sscan(c.Value, &n)
	return n, e
}

func (c *Core) ReadReportPage(_ context.Context, snapshot, cursor string, size int) (contract.ReportPage, error) {
	if snapshot != c.snapshot.ID {
		return contract.ReportPage{}, notFound("snapshot")
	}
	if cursor != "" && !strings.HasPrefix(cursor, snapshot+":") {
		return contract.ReportPage{}, &contract.ErrorResponse{Code: "ERR_CURSOR_INVALID", Message: "Cursor pertence a outro snapshot.", Fields: []contract.FieldError{}}
	}
	if size < 1 || size > 1000 {
		size = 200
	}
	records := []contract.ReportRecord{}
	for i := 0; i < len(c.findings); i++ {
		f := c.findings[i]
		records = append(records, contract.ReportRecord{Kind: "finding", Finding: &f})
	}
	if c.scenario != "empty" {
		components := []contract.ComponentRecord{{ID: "cmp-api", Name: "API", Repository: "demo/api", Path: "apps/api", Kind: "application", ImplementationStatus: "identified", DeploymentStatus: "observed", Coverage: "complete"}, {ID: "cmp-web", Name: "Web", Repository: "demo/web", Path: "app", Kind: "application", ImplementationStatus: "identified", DeploymentStatus: "unknown", Coverage: "complete"}}
		for i := range components {
			v := components[i]
			records = append(records, contract.ReportRecord{Kind: "component", Component: &v})
		}
		links := []contract.LinkRecord{{ID: "link-1", SourceID: "iss-1", Relation: "references_pr", TargetID: "pr-10", Status: "direct", EvidenceID: "ev-1"}, {ID: "link-2", SourceID: "pr-10", Relation: "modifies_component", TargetID: "cmp-api", Status: "direct", EvidenceID: "ev-4"}}
		for i := range links {
			v := links[i]
			records = append(records, contract.ReportRecord{Kind: "link", Link: &v})
		}
		valid := int64(172800)
		short := int64(3600)
		stages := []contract.StageRecord{{ID: "stage-pr10-review", SubjectID: "pr-10", ComponentID: "cmp-api", Stage: "review_wait", StartedAt: "2026-01-02T00:00:00Z", EndedAt: "2026-01-04T00:00:00Z", ElapsedSeconds: &valid, DurationValid: true, EvidenceID: "ev-4"}, {ID: "stage-pr20-review", SubjectID: "pr-20", ComponentID: "cmp-web", Stage: "review_wait", StartedAt: "2026-01-05T00:00:00Z", EndedAt: "2026-01-05T01:00:00Z", ElapsedSeconds: &short, DurationValid: true, EvidenceID: "ev-5"}}
		for i := range stages {
			v := stages[i]
			records = append(records, contract.ReportRecord{Kind: "stage", Stage: &v})
		}
		before, after := float64(3), float64(5)
		estimate := contract.EstimateRecord{ID: "estimate-change-1", IssueID: "iss-1", ActorID: "person-a", EventAt: "2026-01-03T00:00:00Z", Before: &before, After: &after, Timing: "after_start", EvidenceID: "ev-6"}
		records = append(records, contract.ReportRecord{Kind: "estimate", Estimate: &estimate})
		rework := contract.ReworkRecord{ID: "rework-1", SubjectID: "iss-1", ComponentID: "cmp-api", Kind: "reopening", EvidenceStatus: "direct", EventAt: "2026-01-03T00:00:00Z", CauseStatus: "undetermined", EvidenceID: "ev-7"}
		records = append(records, contract.ReportRecord{Kind: "rework", Rework: &rework})
		delivery := contract.DeliveryRecord{ID: "artifact-demo", ComponentID: "cmp-api", Digest: "sha256:demo", RevisionSHA: "demo-old-sha", Environment: "production", ObservedAt: "2026-01-30T00:00:00Z", ProvenanceStatus: "declared", EvidenceID: "ev-8"}
		records = append(records, contract.ReportRecord{Kind: "delivery", Delivery: &delivery})
		metrics := []contract.Metric{{ID: "metric-1", Name: "Entregas observadas", Value: integer(2), Unit: "items", SampleSize: 2, EligibleSize: int64Ptr(3), Definition: "Issues concluídas observadas na fixture.", Limitations: []string{"Não é score de produtividade."}}, {ID: "metric-2", Name: "Espera de revisão mediana", Value: integer(88200), Unit: "seconds", SampleSize: 2, EligibleSize: int64Ptr(2), Definition: "Mediana das esperas observadas.", Limitations: []string{"Fixture sintética."}}}
		for i := range metrics {
			v := metrics[i]
			records = append(records, contract.ReportRecord{Kind: "metric", Metric: &v})
		}
		for i := range c.snapshot.Coverage {
			v := c.snapshot.Coverage[i]
			records = append(records, contract.ReportRecord{Kind: "coverage", Coverage: &v})
		}
		for _, e := range c.evidence {
			v := e
			records = append(records, contract.ReportRecord{Kind: "evidence", Evidence: &v})
		}
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].Kind == records[j].Kind {
			return recordKey(records[i]) < recordKey(records[j])
		}
		return records[i].Kind < records[j].Kind
	})
	start := 0
	if cursor != "" {
		_, err := fmt.Sscanf(strings.TrimPrefix(cursor, snapshot+":"), "%d", &start)
		if err != nil || start < 0 || start > len(records) {
			return contract.ReportPage{}, errors.New("cursor inválido")
		}
	}
	end := start + size
	if end > len(records) {
		end = len(records)
	}
	page := contract.ReportPage{SnapshotID: snapshot, Records: records[start:end], TotalRecords: int64(len(records))}
	if end < len(records) {
		page.NextCursor = fmt.Sprintf("%s:%d", snapshot, end)
	}
	return page, nil
}

func recordKey(r contract.ReportRecord) string {
	switch r.Kind {
	case "finding":
		return r.Finding.ID
	case "component":
		return r.Component.ID
	case "link":
		return r.Link.ID
	case "metric":
		return r.Metric.ID
	case "coverage":
		return r.Coverage.Source + "/" + r.Coverage.ResourceID + "/" + r.Coverage.Collection
	case "evidence":
		return r.Evidence.ID
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
func notFound(kind string) error {
	return &contract.ErrorResponse{Code: "ERR_NOT_FOUND", Message: kind + " não encontrado.", Fields: []contract.FieldError{}}
}
func demoQueries() []contract.SuggestedQuery {
	sql := []string{
		"SELECT component_id, name, repository, path, implementation_status, deployment_status, coverage FROM v_components ORDER BY repository, path, component_id;",
		"SELECT finding_id, rule_id, severity, subject_id, title, evidence_status, coverage FROM v_findings ORDER BY CASE severity WHEN 'critical' THEN 0 WHEN 'high' THEN 1 WHEN 'medium' THEN 2 WHEN 'low' THEN 3 ELSE 4 END, finding_id;",
		"SELECT pr_id, repository, number, review_wait_seconds / 3600.0 AS wait_hours FROM v_pull_requests WHERE review_wait_seconds IS NOT NULL AND review_wait_seconds >= :min_seconds ORDER BY review_wait_seconds DESC, pr_id;",
		"SELECT issue_id, identifier, title, reopen_count FROM v_issues WHERE reopen_count > 0 ORDER BY reopen_count DESC, issue_id;",
		"SELECT issue_id, before_value, after_value, event_at, actor_id FROM v_estimate_changes WHERE timing = 'after_start' ORDER BY event_at, change_id;",
		"SELECT i.issue_id, i.identifier, i.title FROM v_issues i WHERE NOT EXISTS (SELECT 1 FROM v_links l WHERE l.source_id = i.issue_id AND l.relation = 'references_pr' AND l.status = 'direct') ORDER BY i.issue_id;",
		"SELECT source, resource_id, collection, status, observed_count, expected_count, reason FROM v_coverage ORDER BY source, resource_id, collection;",
		"SELECT event_id, event_type, event_at, observed_at, old_value, new_value FROM v_events WHERE entity_id = :subject_id ORDER BY event_at, observed_at, event_id;",
		"SELECT assignee_id, COUNT(*) AS completed_items FROM v_issues WHERE completed_at IS NOT NULL AND is_context = 0 GROUP BY assignee_id ORDER BY completed_items DESC, assignee_id;",
		"SELECT occurrence_id, subject_id, kind, evidence_status, cause_status FROM v_rework ORDER BY event_at, occurrence_id;",
	}
	titles := []string{"Mapa por componente", "Achados por prioridade", "Espera por revisão", "Issues reabertas", "Mudanças de estimativa após início", "Sem vínculo explícito localizado", "Cobertura", "Timeline de um sujeito", "Volume observado por responsável", "Retrabalho registrado"}
	out := make([]contract.SuggestedQuery, 0, len(sql))
	for i, q := range sql {
		id := fmt.Sprintf("Q%02d", i+1)
		h := sha256.Sum256([]byte(q))
		_ = hex.EncodeToString(h[:])
		params := []contract.Parameter{}
		if i == 2 {
			params = []contract.Parameter{{Name: "min_seconds", Value: integer(86400)}}
		}
		if i == 7 {
			params = []contract.Parameter{{Name: "subject_id", Value: txt("")}}
		}
		out = append(out, contract.SuggestedQuery{ID: id, Title: titles[i], Description: "Consulta demonstrativa; os dados são fictícios.", SQL: q, Parameters: params, Requires: []string{}})
	}
	return out
}
