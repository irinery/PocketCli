// RFC-INV-000: referência normativa v1. Copiar antes de abrir os quatro worktrees.
// Destino: internal/investigation/insightcontract/v1.go. Não substitui investigate.v1.
package insightcontract

import "context"

const Version = "investigate.insights.v1"

type Window struct {
	Start string `json:"start"`
	End   string `json:"end"`
}
type ResourceRef struct {
	Source string `json:"source"`
	ID     string `json:"id"`
	Name   string `json:"name"`
}
type Scope struct {
	History      Window        `json:"history"`
	ReportWindow Window        `json:"report_window"`
	Resources    []ResourceRef `json:"resources"`
}
type SnapshotRef struct {
	ID                    string `json:"id"`
	SchemaVersion         string `json:"schema_version"`
	QueryCatalogVersion   string `json:"query_catalog_version"`
	DatasetHash           string `json:"dataset_hash"`
	Window                Window `json:"window"`
	AnalysisReferenceAt   string `json:"analysis_reference_at"`
	ObservationStartedAt  string `json:"observation_started_at"`
	ObservationFinishedAt string `json:"observation_finished_at"`
	RulesetVersion        string `json:"ruleset_version"`
	MappingVersion        string `json:"mapping_version"`
}
type Objective struct {
	ID                 string   `json:"id"`
	Title              string   `json:"title"`
	Dimension          string   `json:"dimension"`
	EntityIDs          []string `json:"entity_ids"`
	MetricID           string   `json:"metric_id"`
	Target             *float64 `json:"target"`
	Comparator         string   `json:"comparator"`
	Unit               string   `json:"unit"`
	Deadline           *string  `json:"deadline"`
	OwnerID            *string  `json:"owner_id"`
	BaselineValue      *float64 `json:"baseline_value"`
	BaselineSnapshotID *string  `json:"baseline_snapshot_id"`
	BaselineCohortID   *string  `json:"baseline_cohort_id"`
	EvidenceIDs        []string `json:"evidence_ids"`
}
type Policy struct {
	ID                        string `json:"id"`
	Version                   string `json:"version"`
	MinimumQuantileSample     int    `json:"minimum_quantile_sample"`
	MaximumExecutiveCases     int    `json:"maximum_executive_cases"`
	ExcerptMaxRunes           int    `json:"excerpt_max_runes"`
	ContentMode               string `json:"content_mode"`
	ExternalProcessingAllowed bool   `json:"external_processing_allowed"`
}
type Budget struct {
	MaxRequests     int      `json:"max_requests"`
	MaxInputTokens  int64    `json:"max_input_tokens"`
	MaxOutputTokens int64    `json:"max_output_tokens"`
	MaxElapsedMS    int64    `json:"max_elapsed_ms"`
	MaxCost         *float64 `json:"max_cost"`
	Currency        *string  `json:"currency"`
}
type ProviderConfig struct {
	Kind            string `json:"kind"`
	Model           string `json:"model"`
	EndpointProfile string `json:"endpoint_profile"`
	AdapterVersion  string `json:"adapter_version"`
}
type AnalysisConfig struct {
	Version        string         `json:"version"`
	ID             string         `json:"id"`
	RequestedScope Scope          `json:"requested_scope"`
	Objectives     []Objective    `json:"objectives"`
	Policy         Policy         `json:"policy"`
	Budget         Budget         `json:"budget"`
	Provider       ProviderConfig `json:"provider"`
}
type CoverageKey struct {
	Source     string `json:"source"`
	ResourceID string `json:"resource_id"`
	Collection string `json:"collection"`
}
type SourceCoverage struct {
	Key                     CoverageKey `json:"key"`
	Status                  string      `json:"status"`
	Observed                int64       `json:"observed"`
	Expected                *int64      `json:"expected"`
	HistoryObservedFrom     *string     `json:"history_observed_from"`
	HistoryObservedTo       *string     `json:"history_observed_to"`
	HistoryCoveredIntervals []Window    `json:"history_covered_intervals"`
	CheckedAt               string      `json:"checked_at"`
	Reason                  string      `json:"reason"`
	QueueIDs                []string    `json:"queue_ids"`
}
type QuestionCoverage struct {
	QuestionID          string        `json:"question_id"`
	Status              string        `json:"status"`
	RequiredCollections []CoverageKey `json:"required_collections"`
	Missing             []string      `json:"missing"`
	Reason              string        `json:"reason"`
}
type QueueProgress struct {
	ID           string   `json:"id"`
	Source       string   `json:"source"`
	ResourceID   string   `json:"resource_id"`
	Collections  []string `json:"collections"`
	ScopeHash    string   `json:"scope_hash"`
	Status       string   `json:"status"`
	Pending      *int64   `json:"pending"`
	CheckpointID *string  `json:"checkpoint_id"`
	RetryAfter   *string  `json:"retry_after"`
	ErrorCode    *string  `json:"error_code"`
}
type Entity struct {
	ID                  string   `json:"id"`
	Kind                string   `json:"kind"`
	Source              string   `json:"source"`
	ResourceID          string   `json:"resource_id"`
	Identifier          string   `json:"identifier"`
	Title               string   `json:"title"`
	URL                 *string  `json:"url"`
	TeamID              *string  `json:"team_id"`
	TeamName            *string  `json:"team_name"`
	OwnerID             *string  `json:"owner_id"`
	ProjectID           *string  `json:"project_id"`
	ComponentIDs        []string `json:"component_ids"`
	State               string   `json:"state"`
	Outcome             string   `json:"outcome"`
	StateTemporalStatus string   `json:"state_temporal_status"`
	WorkflowState       string   `json:"workflow_state"`
	CreatedAt           *string  `json:"created_at"`
	UpdatedAt           *string  `json:"updated_at"`
	StartedAt           *string  `json:"started_at"`
	CompletedAt         *string  `json:"completed_at"`
	DueAt               *string  `json:"due_at"`
	IsContext           bool     `json:"is_context"`
	EstimateCurrent     *float64 `json:"estimate_current"`
	EstimateBeforeStart *float64 `json:"estimate_before_start"`
	EstimateScaleID     *string  `json:"estimate_scale_id"`
	EvidenceIDs         []string `json:"evidence_ids"`
}
type EvidenceView struct {
	ID                  string  `json:"id"`
	Source              string  `json:"source"`
	Locator             string  `json:"locator"`
	URL                 *string `json:"url"`
	ObservedAt          string  `json:"observed_at"`
	SourceUpdatedAt     *string `json:"source_updated_at"`
	EventAt             *string `json:"event_at"`
	ContentHash         string  `json:"content_hash"`
	ContentStatus       string  `json:"content_status"`
	IntegrityStatus     string  `json:"integrity_status"`
	Excerpt             string  `json:"excerpt"`
	ExcerptHash         string  `json:"excerpt_hash"`
	Redacted            bool    `json:"redacted"`
	Truncated           bool    `json:"truncated"`
	SanitizationVersion string  `json:"sanitization_version"`
}
type Citation struct {
	EvidenceID string `json:"evidence_id"`
	Quote      string `json:"quote"`
}
type Link struct {
	ID             string   `json:"id"`
	FromID         string   `json:"from_id"`
	ToID           string   `json:"to_id"`
	Relation       string   `json:"relation"`
	Status         string   `json:"status"`
	TemporalStatus string   `json:"temporal_status"`
	ValidFrom      *string  `json:"valid_from"`
	ValidTo        *string  `json:"valid_to"`
	EvidenceIDs    []string `json:"evidence_ids"`
}
type Event struct {
	ID          string   `json:"id"`
	EntityID    string   `json:"entity_id"`
	Type        string   `json:"type"`
	EventAt     *string  `json:"event_at"`
	ObservedAt  string   `json:"observed_at"`
	ActorID     *string  `json:"actor_id"`
	OldValue    *string  `json:"old_value"`
	NewValue    *string  `json:"new_value"`
	EvidenceIDs []string `json:"evidence_ids"`
}
type Stage struct {
	ID              string   `json:"id"`
	EntityID        string   `json:"entity_id"`
	TeamID          *string  `json:"team_id"`
	Name            string   `json:"name"`
	Start           *string  `json:"start"`
	End             *string  `json:"end"`
	DurationSeconds *float64 `json:"duration_seconds"`
	DurationValid   bool     `json:"duration_valid"`
	HistoryComplete bool     `json:"history_complete"`
	EvidenceIDs     []string `json:"evidence_ids"`
}
type EstimateChange struct {
	ID          string   `json:"id"`
	EntityID    string   `json:"entity_id"`
	EventAt     *string  `json:"event_at"`
	Before      *float64 `json:"before"`
	After       *float64 `json:"after"`
	Timing      string   `json:"timing"`
	ScaleID     *string  `json:"scale_id"`
	EvidenceIDs []string `json:"evidence_ids"`
}
type DeliveryObservation struct {
	ID          string   `json:"id"`
	EntityID    string   `json:"entity_id"`
	Environment string   `json:"environment"`
	Status      string   `json:"status"`
	EventAt     *string  `json:"event_at"`
	EvidenceIDs []string `json:"evidence_ids"`
}
type SourceBundle struct {
	Version           string                `json:"version"`
	InputHash         string                `json:"input_hash"`
	Snapshot          SnapshotRef           `json:"snapshot"`
	RequestedScope    Scope                 `json:"requested_scope"`
	ObservedResources []ResourceRef         `json:"observed_resources"`
	Coverage          []SourceCoverage      `json:"coverage"`
	Questions         []QuestionCoverage    `json:"questions"`
	Queues            []QueueProgress       `json:"queues"`
	Entities          []Entity              `json:"entities"`
	Evidence          []EvidenceView        `json:"evidence"`
	Links             []Link                `json:"links"`
	Events            []Event               `json:"events"`
	Stages            []Stage               `json:"stages"`
	Estimates         []EstimateChange      `json:"estimates"`
	Deliveries        []DeliveryObservation `json:"deliveries"`
	LegacyFindings    []LegacyFinding       `json:"legacy_findings"`
	Limitations       []string              `json:"limitations"`
}
type LegacyFinding struct {
	ID          string   `json:"id"`
	RuleID      string   `json:"rule_id"`
	SubjectID   string   `json:"subject_id"`
	Severity    string   `json:"severity"`
	EvidenceIDs []string `json:"evidence_ids"`
}
type Cohort struct {
	ID             string   `json:"id"`
	Definition     string   `json:"definition"`
	Window         Window   `json:"window"`
	ReferenceAt    string   `json:"reference_at"`
	EntityIDs      []string `json:"entity_ids"`
	Count          int64    `json:"count"`
	EligibleCount  *int64   `json:"eligible_count"`
	CoverageStatus string   `json:"coverage_status"`
	Exclusions     []string `json:"exclusions"`
}
type Metric struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	State        string   `json:"state"`
	Value        *float64 `json:"value"`
	Unit         string   `json:"unit"`
	Numerator    *int64   `json:"numerator"`
	Denominator  *int64   `json:"denominator"`
	SampleSize   int64    `json:"sample_size"`
	EligibleSize *int64   `json:"eligible_size"`
	CohortID     string   `json:"cohort_id"`
	Method       string   `json:"method"`
	Definition   string   `json:"definition"`
	Limitations  []string `json:"limitations"`
}
type FactCase struct {
	ID               string   `json:"id"`
	Kind             string   `json:"kind"`
	Classification   string   `json:"classification"`
	Title            string   `json:"title"`
	SubjectIDs       []string `json:"subject_ids"`
	RelatedIDs       []string `json:"related_ids"`
	LinkIDs          []string `json:"link_ids"`
	ObjectiveIDs     []string `json:"objective_ids"`
	EvidenceIDs      []string `json:"evidence_ids"`
	MetricIDs        []string `json:"metric_ids"`
	LegacyFindingIDs []string `json:"legacy_finding_ids"`
	ObservedFacts    []string `json:"observed_facts"`
	OpenQuestions    []string `json:"open_questions"`
}
type FactsResult struct {
	Version     string     `json:"version"`
	SnapshotID  string     `json:"snapshot_id"`
	InputHash   string     `json:"input_hash"`
	ConfigHash  string     `json:"config_hash"`
	Cohorts     []Cohort   `json:"cohorts"`
	Metrics     []Metric   `json:"metrics"`
	Cases       []FactCase `json:"cases"`
	Limitations []string   `json:"limitations"`
}
type ContextScope struct {
	ResourceIDs  []string `json:"resource_ids"`
	EntityIDs    []string `json:"entity_ids"`
	ComponentIDs []string `json:"component_ids"`
	Environment  *string  `json:"environment"`
	Conditions   []string `json:"conditions"`
	Description  string   `json:"description"`
	Status       string   `json:"status"`
}
type Claim struct {
	ID             string       `json:"id"`
	Text           string       `json:"text"`
	EntityIDs      []string     `json:"entity_ids"`
	Scope          ContextScope `json:"scope"`
	Kind           string       `json:"kind"`
	Method         string       `json:"method"`
	EventAt        *string      `json:"event_at"`
	ValidFrom      *string      `json:"valid_from"`
	ValidTo        *string      `json:"valid_to"`
	ValidityStatus string       `json:"validity_status"`
	Citations      []Citation   `json:"citations"`
	Status         string       `json:"status"`
}
type ClaimRelation struct {
	ID             string     `json:"id"`
	FromClaimID    string     `json:"from_claim_id"`
	ToClaimID      string     `json:"to_claim_id"`
	Kind           string     `json:"kind"`
	Status         string     `json:"status"`
	ValidFrom      *string    `json:"valid_from"`
	ValidTo        *string    `json:"valid_to"`
	ValidityStatus string     `json:"validity_status"`
	Reason         string     `json:"reason"`
	Citations      []Citation `json:"citations"`
}
type DecisionRecord struct {
	ID              string       `json:"id"`
	Question        string       `json:"question"`
	Choice          string       `json:"choice"`
	Rationale       string       `json:"rationale"`
	Scope           ContextScope `json:"scope"`
	EntityIDs       []string     `json:"entity_ids"`
	ClaimIDs        []string     `json:"claim_ids"`
	Status          string       `json:"status"`
	Method          string       `json:"method"`
	DecidedAt       *string      `json:"decided_at"`
	ValidFrom       *string      `json:"valid_from"`
	ValidTo         *string      `json:"valid_to"`
	ValidityStatus  string       `json:"validity_status"`
	DecisionMakerID *string      `json:"decision_maker_id"`
	RecordedByID    *string      `json:"recorded_by_id"`
	ObjectiveIDs    []string     `json:"objective_ids"`
	Citations       []Citation   `json:"citations"`
}
type DecisionRelation struct {
	ID             string     `json:"id"`
	FromDecisionID string     `json:"from_decision_id"`
	ToDecisionID   string     `json:"to_decision_id"`
	Kind           string     `json:"kind"`
	Status         string     `json:"status"`
	ValidFrom      *string    `json:"valid_from"`
	ValidTo        *string    `json:"valid_to"`
	ValidityStatus string     `json:"validity_status"`
	Reason         string     `json:"reason"`
	Citations      []Citation `json:"citations"`
}
type Assessment struct {
	State       string   `json:"state"`
	Level       string   `json:"level"`
	Rationale   string   `json:"rationale"`
	EvidenceIDs []string `json:"evidence_ids"`
	Missing     []string `json:"missing"`
}
type Alternative struct {
	ID             string   `json:"id"`
	Action         string   `json:"action"`
	ExpectedEffect string   `json:"expected_effect"`
	Tradeoffs      []string `json:"tradeoffs"`
	Preconditions  []string `json:"preconditions"`
	Basis          string   `json:"basis"`
	EvidenceIDs    []string `json:"evidence_ids"`
}
type DecisionCase struct {
	ID                       string        `json:"id"`
	Title                    string        `json:"title"`
	State                    string        `json:"state"`
	FactCaseIDs              []string      `json:"fact_case_ids"`
	DecisionIDs              []string      `json:"decision_ids"`
	ClaimIDs                 []string      `json:"claim_ids"`
	RelationIDs              []string      `json:"relation_ids"`
	ObjectiveIDs             []string      `json:"objective_ids"`
	EntityIDs                []string      `json:"entity_ids"`
	Summary                  string        `json:"summary"`
	Impact                   Assessment    `json:"impact"`
	Complexity               Assessment    `json:"complexity"`
	Urgency                  Assessment    `json:"urgency"`
	Confidence               Assessment    `json:"confidence"`
	Alternatives             []Alternative `json:"alternatives"`
	RecommendedAlternativeID *string       `json:"recommended_alternative_id"`
	RecommendationBasis      string        `json:"recommendation_basis"`
	OwnerID                  *string       `json:"owner_id"`
	NextEvidence             []string      `json:"next_evidence"`
	Citations                []Citation    `json:"citations"`
}
type DimensionStatus struct {
	ObjectiveID string   `json:"objective_id"`
	Dimension   string   `json:"dimension"`
	Status      string   `json:"status"`
	Reason      string   `json:"reason"`
	MetricIDs   []string `json:"metric_ids"`
	CaseIDs     []string `json:"case_ids"`
	Missing     []string `json:"missing"`
}
type Usage struct {
	Requests     int64    `json:"requests"`
	InputTokens  *int64   `json:"input_tokens"`
	OutputTokens *int64   `json:"output_tokens"`
	ElapsedMS    int64    `json:"elapsed_ms"`
	Cost         *float64 `json:"cost"`
	Currency     *string  `json:"currency"`
	PriceVersion *string  `json:"price_version"`
	CacheHits    int64    `json:"cache_hits"`
}
type Provenance struct {
	InputHash      string `json:"input_hash"`
	ConfigHash     string `json:"config_hash"`
	Provider       string `json:"provider"`
	Model          string `json:"model"`
	AdapterVersion string `json:"adapter_version"`
	PromptVersion  string `json:"prompt_version"`
	RubricVersion  string `json:"rubric_version"`
	PolicyVersion  string `json:"policy_version"`
	RulesetVersion string `json:"ruleset_version"`
	CreatedAt      string `json:"created_at"`
	CacheKey       string `json:"cache_key"`
}
type DecisionAnalysis struct {
	Version        string             `json:"version"`
	ID             string             `json:"id"`
	SnapshotID     string             `json:"snapshot_id"`
	Status         string             `json:"status"`
	Claims         []Claim            `json:"claims"`
	ClaimRelations []ClaimRelation    `json:"claim_relations"`
	Decisions      []DecisionRecord   `json:"decisions"`
	Relations      []DecisionRelation `json:"relations"`
	Cases          []DecisionCase     `json:"cases"`
	Dimensions     []DimensionStatus  `json:"dimensions"`
	Limitations    []string           `json:"limitations"`
	Provenance     Provenance         `json:"provenance"`
	Usage          Usage              `json:"usage"`
}
type ReasoningRequest struct {
	Task          string           `json:"task"`
	SnapshotID    string           `json:"snapshot_id"`
	InputHash     string           `json:"input_hash"`
	PromptVersion string           `json:"prompt_version"`
	RubricVersion string           `json:"rubric_version"`
	Entities      []Entity         `json:"entities"`
	Evidence      []EvidenceView   `json:"evidence"`
	Facts         FactsResult      `json:"facts"`
	Claims        []Claim          `json:"claims"`
	Decisions     []DecisionRecord `json:"decisions"`
	Objectives    []Objective      `json:"objectives"`
	Budget        Budget           `json:"budget"`
}
type ReasoningResponse struct {
	Analysis DecisionAnalysis `json:"analysis"`
	Usage    Usage            `json:"usage"`
}
type ReasoningProvider interface {
	Evaluate(context.Context, ReasoningRequest) (ReasoningResponse, error)
}
type ExecutiveReport struct {
	Version           string             `json:"version"`
	SnapshotID        string             `json:"snapshot_id"`
	AnalysisID        string             `json:"analysis_id"`
	PublicationStatus string             `json:"publication_status"`
	AnalysisStatus    string             `json:"analysis_status"`
	SufficiencyStatus string             `json:"sufficiency_status"`
	Headline          string             `json:"headline"`
	Summary           string             `json:"summary"`
	RequestedScope    Scope              `json:"requested_scope"`
	Objectives        []Objective        `json:"objectives,omitempty"`
	PolicyHash        string             `json:"policy_hash,omitempty"`
	Questions         []QuestionCoverage `json:"questions"`
	Dimensions        []DimensionStatus  `json:"dimensions"`
	Metrics           []Metric           `json:"metrics"`
	Cases             []DecisionCase     `json:"cases"`
	TotalCases        int64              `json:"total_cases"`
	CaseIDs           []string           `json:"case_ids"`
	NextActions       []string           `json:"next_actions"`
	Limitations       []string           `json:"limitations"`
}
type TimelineItem struct {
	ID          string   `json:"id"`
	Kind        string   `json:"kind"`
	EntityID    string   `json:"entity_id,omitempty"`
	At          *string  `json:"at"`
	End         *string  `json:"end,omitempty"`
	ObservedAt  *string  `json:"observed_at,omitempty"`
	ActorID     *string  `json:"actor_id,omitempty"`
	OldValue    *string  `json:"old_value,omitempty"`
	NewValue    *string  `json:"new_value,omitempty"`
	Scope       string   `json:"scope,omitempty"`
	Source      string   `json:"source"`
	Summary     string   `json:"summary"`
	EvidenceIDs []string `json:"evidence_ids"`
}
type CaseDetail struct {
	SnapshotID     string             `json:"snapshot_id"`
	AnalysisID     string             `json:"analysis_id"`
	Case           DecisionCase       `json:"case"`
	Facts          []FactCase         `json:"facts"`
	Claims         []Claim            `json:"claims"`
	ClaimRelations []ClaimRelation    `json:"claim_relations"`
	Decisions      []DecisionRecord   `json:"decisions"`
	Relations      []DecisionRelation `json:"relations"`
	Entities       []Entity           `json:"entities"`
	Evidence       []EvidenceView     `json:"evidence"`
	Timeline       []TimelineItem     `json:"timeline,omitempty"`
}
type CasePage struct {
	SnapshotID string         `json:"snapshot_id"`
	AnalysisID string         `json:"analysis_id"`
	Total      int64          `json:"total"`
	Cases      []DecisionCase `json:"cases"`
	NextCursor string         `json:"next_cursor"`
}
type InsightReader interface {
	GetExecutiveReport(context.Context, string, string) (ExecutiveReport, error)
	GetInsightCase(context.Context, string, string, string) (CaseDetail, error)
	ListInsightCases(context.Context, string, string, string, int) (CasePage, error)
}
type SourceReader interface {
	ReadInsightSource(context.Context, string, AnalysisConfig) (SourceBundle, error)
}
type AnalysisRequest struct {
	SnapshotID     string         `json:"snapshot_id"`
	Config         AnalysisConfig `json:"config"`
	IdempotencyKey string         `json:"idempotency_key"`
}
type AnalysisRun struct {
	ID         string  `json:"id"`
	SnapshotID string  `json:"snapshot_id"`
	ConfigHash string  `json:"config_hash"`
	State      string  `json:"state"`
	Phase      string  `json:"phase"`
	AnalysisID *string `json:"analysis_id"`
	ErrorCode  *string `json:"error_code"`
}
type AnalysisSummary struct {
	ID             string `json:"id"`
	SnapshotID     string `json:"snapshot_id"`
	ConfigHash     string `json:"config_hash"`
	RequestedScope Scope  `json:"requested_scope"`
	Status         string `json:"status"`
	CreatedAt      string `json:"created_at"`
}
type AnalysisPage struct {
	SnapshotID string            `json:"snapshot_id"`
	Total      int64             `json:"total"`
	Items      []AnalysisSummary `json:"items"`
	NextCursor string            `json:"next_cursor"`
}
type AnalysisController interface {
	StartAnalysis(context.Context, AnalysisRequest) (AnalysisRun, error)
	GetAnalysisRun(context.Context, string) (AnalysisRun, error)
	CancelAnalysis(context.Context, string) (AnalysisRun, error)
	ListAnalyses(context.Context, string, string, int) (AnalysisPage, error)
}
type ExecutiveExportRequest struct {
	SnapshotID     string `json:"snapshot_id"`
	AnalysisID     string `json:"analysis_id"`
	Mode           string `json:"mode"`
	IdempotencyKey string `json:"idempotency_key"`
}
type ExecutiveArtifact struct {
	ID                string `json:"id"`
	SnapshotID        string `json:"snapshot_id"`
	AnalysisID        string `json:"analysis_id"`
	Mode              string `json:"mode"`
	Revision          int    `json:"revision"`
	PageCount         int    `json:"page_count"`
	SelectedCaseCount int64  `json:"selected_case_count"`
	TotalCaseCount    int64  `json:"total_case_count"`
	ContentHash       string `json:"content_hash"`
	AnalysisHash      string `json:"analysis_hash"`
	PolicyHash        string `json:"policy_hash,omitempty"`
	RendererVersion   string `json:"renderer_version"`
	CreatedAt         string `json:"created_at"`
	DownloadPath      string `json:"download_path"`
}
type ExecutiveExporter interface {
	ExportExecutive(context.Context, ExecutiveExportRequest) (ExecutiveArtifact, error)
	GetExecutiveArtifact(context.Context, string) (ExecutiveArtifact, error)
}
