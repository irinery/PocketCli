package contract

import "context"

const Version = "investigate.v1"

type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}
type ErrorResponse struct {
	OK        bool         `json:"ok"`
	Code      string       `json:"code"`
	Message   string       `json:"message"`
	TraceID   string       `json:"trace_id"`
	Retryable bool         `json:"retryable"`
	Fields    []FieldError `json:"fields"`
}

func (e *ErrorResponse) Error() string { return e.Code + ": " + e.Message }

type Scope struct {
	Mode         string   `json:"mode"`
	Repositories []string `json:"repositories"`
	Teams        []string `json:"teams"`
}
type Period struct {
	Preset   string `json:"preset"`
	FromDate string `json:"from_date,omitempty"`
	ToDate   string `json:"to_date,omitempty"`
	Timezone string `json:"timezone"`
}
type Modules struct {
	GitHubIssues  bool `json:"github_issues"`
	CodeHistory   bool `json:"code_history"`
	CI            bool `json:"ci"`
	Deployments   bool `json:"deployments"`
	RuntimeImport bool `json:"runtime_import"`
}
type Limits struct {
	GitHubRPH        int   `json:"github_requests_per_hour"`
	LinearRPH        int   `json:"linear_requests_per_hour"`
	MaxSessionMS     int64 `json:"max_session_ms"`
	MaxTransferBytes int64 `json:"max_transfer_bytes"`
	MaxLocalBytes    int64 `json:"max_local_bytes"`
}
type PlanInput struct {
	InvestigationID string  `json:"investigation_id,omitempty"`
	SchemaVersion   string  `json:"schema_version"`
	Name            string  `json:"name"`
	Scope           *Scope  `json:"scope,omitempty"`
	Period          Period  `json:"period"`
	GitHubEnabled   bool    `json:"github_enabled"`
	LinearEnabled   bool    `json:"linear_enabled"`
	Modules         Modules `json:"modules"`
	Limits          Limits  `json:"limits"`
}
type SetupParseRequest struct {
	Filename string `json:"filename"`
	Content  string `json:"content"`
}
type ParsedSetup struct {
	Input         PlanInput `json:"input"`
	MissingFields []string  `json:"missing_fields"`
	Warnings      []string  `json:"warnings"`
}
type ResolvedPeriod struct {
	Preset      string `json:"preset"`
	Timezone    string `json:"timezone"`
	AnchorEnd   string `json:"anchor_end"`
	WindowStart string `json:"window_start,omitempty"`
	WindowEnd   string `json:"window_end"`
}
type SetupService interface {
	Parse(context.Context, SetupParseRequest) (ParsedSetup, error)
	ResolvePeriod(context.Context, Period) (ResolvedPeriod, error)
}
type Resource struct {
	ID     string `json:"id"`
	Source string `json:"source"`
	Name   string `json:"name"`
	Access string `json:"access"`
}
type Plan struct {
	InvestigationID      string     `json:"investigation_id"`
	ID                   string     `json:"id"`
	Hash                 string     `json:"hash"`
	Input                PlanInput  `json:"input"`
	Resources            []Resource `json:"resources"`
	WindowStart          string     `json:"window_start,omitempty"`
	WindowEnd            string     `json:"window_end"`
	RequiresConfirmation bool       `json:"requires_confirmation"`
	Warnings             []string   `json:"warnings"`
}
type RunRequest struct {
	PlanID            string `json:"plan_id"`
	ConfirmedPlanHash string `json:"confirmed_plan_hash"`
	IdempotencyKey    string `json:"idempotency_key"`
}
type Run struct {
	ID         string         `json:"id"`
	PlanID     string         `json:"plan_id"`
	State      string         `json:"state"`
	Stage      string         `json:"stage"`
	Requests   int64          `json:"requests"`
	Bytes      int64          `json:"bytes"`
	Items      int64          `json:"items"`
	Coverage   string         `json:"coverage"`
	SnapshotID string         `json:"snapshot_id,omitempty"`
	LastError  *ErrorResponse `json:"last_error,omitempty"`
}
type Coverage struct {
	Source     string `json:"source"`
	ResourceID string `json:"resource_id"`
	Collection string `json:"collection"`
	Status     string `json:"status"`
	Observed   int64  `json:"observed"`
	Expected   *int64 `json:"expected"`
	Reason     string `json:"reason"`
	CheckedAt  string `json:"checked_at"`
}
type Snapshot struct {
	PlanID                string     `json:"plan_id"`
	PlanHash              string     `json:"plan_hash"`
	Resources             []Resource `json:"resources"`
	Modules               Modules    `json:"modules"`
	ObservationStartedAt  string     `json:"observation_started_at"`
	ObservationFinishedAt string     `json:"observation_finished_at"`
	ID                    string     `json:"id"`
	InvestigationID       string     `json:"investigation_id"`
	SchemaVersion         string     `json:"schema_version"`
	RulesetVersion        string     `json:"ruleset_version"`
	MappingVersion        string     `json:"mapping_version"`
	QueryCatalogVersion   string     `json:"query_catalog_version"`
	AnalysisReferenceAt   string     `json:"analysis_reference_at"`
	WindowStart           string     `json:"window_start,omitempty"`
	WindowEnd             string     `json:"window_end"`
	PublishedAt           string     `json:"published_at"`
	DatasetHash           string     `json:"dataset_hash"`
	Status                string     `json:"status"`
	Coverage              []Coverage `json:"coverage"`
}

// Kind: null, text, integer, real, boolean. Value: canonical textual form.
type Cell struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}
type Parameter struct {
	Name  string `json:"name"`
	Value Cell   `json:"value"`
}
type QueryRequest struct {
	SnapshotID string      `json:"snapshot_id"`
	SQL        string      `json:"sql"`
	Parameters []Parameter `json:"parameters"`
	MaxRows    int         `json:"max_rows"`
	TimeoutMS  int         `json:"timeout_ms"`
}
type Column struct {
	Name         string `json:"name"`
	DeclaredType string `json:"declared_type"`
}
type QueryResult struct {
	QueryID      string   `json:"query_id"`
	SnapshotID   string   `json:"snapshot_id"`
	QueryHash    string   `json:"query_hash"`
	Columns      []Column `json:"columns"`
	Rows         [][]Cell `json:"rows"`
	ReturnedRows int      `json:"returned_rows"`
	Truncated    bool     `json:"truncated"`
	ElapsedMS    int64    `json:"elapsed_ms"`
	Warnings     []string `json:"warnings"`
}
type SuggestedQuery struct {
	ID          string      `json:"id"`
	Title       string      `json:"title"`
	Description string      `json:"description"`
	SQL         string      `json:"sql"`
	Parameters  []Parameter `json:"parameters"`
	Requires    []string    `json:"requires"`
}
type Evidence struct {
	ID              string `json:"id"`
	Source          string `json:"source"`
	Locator         string `json:"locator"`
	ObservedAt      string `json:"observed_at"`
	SourceUpdatedAt string `json:"source_updated_at,omitempty"`
	SHA256          string `json:"sha256"`
	Preview         string `json:"preview"`
	Redacted        bool   `json:"redacted"`
	Truncated       bool   `json:"truncated"`
}
type Finding struct {
	ID                string   `json:"id"`
	RuleID            string   `json:"rule_id"`
	RuleVersion       string   `json:"rule_version"`
	Severity          string   `json:"severity"`
	SubjectID         string   `json:"subject_id"`
	Title             string   `json:"title"`
	DeterministicText string   `json:"deterministic_text"`
	EvidenceStatus    string   `json:"evidence_status"`
	EvidenceIDs       []string `json:"evidence_ids"`
	Limitations       []string `json:"limitations"`
}
type Metric struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Value        Cell     `json:"value"`
	Unit         string   `json:"unit"`
	SampleSize   int64    `json:"sample_size"`
	EligibleSize *int64   `json:"eligible_size"`
	Definition   string   `json:"definition"`
	Limitations  []string `json:"limitations"`
}
type ComponentRecord struct {
	ID                   string `json:"component_id"`
	Name                 string `json:"name"`
	Repository           string `json:"repository"`
	Path                 string `json:"path"`
	Kind                 string `json:"kind"`
	ImplementationStatus string `json:"implementation_status"`
	DeploymentStatus     string `json:"deployment_status"`
	Coverage             string `json:"coverage"`
}
type LinkRecord struct {
	ID         string `json:"link_id"`
	SourceID   string `json:"source_id"`
	Relation   string `json:"relation"`
	TargetID   string `json:"target_id"`
	Status     string `json:"status"`
	EvidenceID string `json:"evidence_id"`
	ValidFrom  string `json:"valid_from,omitempty"`
	ValidTo    string `json:"valid_to,omitempty"`
}
type StageRecord struct {
	ID             string `json:"id"`
	SubjectID      string `json:"subject_id"`
	ComponentID    string `json:"component_id,omitempty"`
	Stage          string `json:"stage"`
	StartedAt      string `json:"started_at,omitempty"`
	EndedAt        string `json:"ended_at,omitempty"`
	ElapsedSeconds *int64 `json:"elapsed_seconds"`
	DurationValid  bool   `json:"duration_valid"`
	EvidenceID     string `json:"evidence_id"`
}
type EstimateRecord struct {
	ID         string   `json:"change_id"`
	IssueID    string   `json:"issue_id"`
	ActorID    string   `json:"actor_id,omitempty"`
	EventAt    string   `json:"event_at,omitempty"`
	Before     *float64 `json:"before_value"`
	After      *float64 `json:"after_value"`
	Timing     string   `json:"timing"`
	EvidenceID string   `json:"evidence_id"`
}
type ReworkRecord struct {
	ID             string `json:"occurrence_id"`
	SubjectID      string `json:"subject_id"`
	ComponentID    string `json:"component_id,omitempty"`
	Kind           string `json:"kind"`
	EvidenceStatus string `json:"evidence_status"`
	EventAt        string `json:"event_at,omitempty"`
	CauseStatus    string `json:"cause_status"`
	EvidenceID     string `json:"evidence_id"`
}
type DeliveryRecord struct {
	ID               string `json:"artifact_id"`
	ComponentID      string `json:"component_id"`
	Digest           string `json:"digest,omitempty"`
	RevisionSHA      string `json:"revision_sha,omitempty"`
	Environment      string `json:"environment,omitempty"`
	ObservedAt       string `json:"observed_at,omitempty"`
	ProvenanceStatus string `json:"provenance_status"`
	EvidenceID       string `json:"evidence_id"`
}
type ReportRecord struct {
	Component *ComponentRecord `json:"component,omitempty"`
	Link      *LinkRecord      `json:"link,omitempty"`
	Stage     *StageRecord     `json:"stage,omitempty"`
	Estimate  *EstimateRecord  `json:"estimate,omitempty"`
	Rework    *ReworkRecord    `json:"rework,omitempty"`
	Delivery  *DeliveryRecord  `json:"delivery,omitempty"`
	Kind      string           `json:"kind"`
	Finding   *Finding         `json:"finding,omitempty"`
	Metric    *Metric          `json:"metric,omitempty"`
	Coverage  *Coverage        `json:"coverage,omitempty"`
	Evidence  *Evidence        `json:"evidence,omitempty"`
}
type ReportPage struct {
	SnapshotID   string         `json:"snapshot_id"`
	Records      []ReportRecord `json:"records"`
	NextCursor   string         `json:"next_cursor,omitempty"`
	TotalRecords int64          `json:"total_records"`
}
type Core interface {
	PreparePlan(context.Context, PlanInput) (Plan, error)
	StartRun(context.Context, RunRequest) (Run, error)
	GetRun(context.Context, string) (Run, error)
	PauseRun(context.Context, string) (Run, error)
	ResumeRun(context.Context, string) (Run, error)
	CancelRun(context.Context, string) (Run, error)
	ListSnapshots(context.Context, string) ([]Snapshot, error)
	GetSnapshot(context.Context, string) (Snapshot, error)
	Query(context.Context, QueryRequest) (QueryResult, error)
	SuggestedQueries(context.Context, string) ([]SuggestedQuery, error)
	GetEvidence(context.Context, string, string) (Evidence, error)
	GetFinding(context.Context, string, string) (Finding, error)
	ReadReportPage(context.Context, string, string, int) (ReportPage, error)
}
type CredentialInput struct {
	Source      string `json:"source"`
	Secret      string `json:"secret"`
	Persistence string `json:"persistence"`
}
type CredentialStatus struct {
	Source             string   `json:"source"`
	Configured         bool     `json:"configured"`
	Validated          bool     `json:"validated"`
	Storage            string   `json:"storage"`
	Identity           string   `json:"identity"`
	MissingPermissions []string `json:"missing_permissions"`
}
type CredentialStore interface {
	Put(context.Context, CredentialInput) (CredentialStatus, error)
	Status(context.Context, string) (CredentialStatus, error)
	Delete(context.Context, string) error
}

// Interface apenas interna. Nunca expor ao browser a função que lê o segredo.
type SecretProvider interface {
	WithSecret(context.Context, string, func([]byte) error) error
}
type FinalizeRequest struct {
	SnapshotID               string `json:"snapshot_id"`
	AnnotationRevision       int64  `json:"annotation_revision"`
	IncludeApprovedEditorial bool   `json:"include_approved_editorial"`
}
type ReportArtifact struct {
	ID           string   `json:"id"`
	SnapshotID   string   `json:"snapshot_id"`
	Revision     int      `json:"revision"`
	Status       string   `json:"status"`
	SHA256       string   `json:"sha256"`
	DownloadPath string   `json:"download_path"`
	FindingCount int64    `json:"finding_count"`
	PageCount    int      `json:"page_count"`
	Warnings     []string `json:"warnings"`
}
type ReportReader interface {
	GetSnapshot(context.Context, string) (Snapshot, error)
	ReadReportPage(context.Context, string, string, int) (ReportPage, error)
}
type Finalizer interface {
	Finalize(context.Context, FinalizeRequest) (ReportArtifact, error)
}
