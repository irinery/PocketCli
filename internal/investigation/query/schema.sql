CREATE TABLE v_snapshot (
 id TEXT PRIMARY KEY, window_start TEXT, window_end TEXT NOT NULL,
 analysis_reference_at TEXT NOT NULL, ruleset_version TEXT NOT NULL, coverage TEXT NOT NULL
);
CREATE TABLE v_components (
 component_id TEXT PRIMARY KEY, name TEXT NOT NULL, repository TEXT NOT NULL,
 path TEXT NOT NULL, kind TEXT NOT NULL, implementation_status TEXT NOT NULL,
 deployment_status TEXT NOT NULL, coverage TEXT NOT NULL
);
CREATE TABLE v_issues (
 issue_id TEXT PRIMARY KEY, identifier TEXT NOT NULL, source TEXT NOT NULL,
 title TEXT NOT NULL, state TEXT NOT NULL, assignee_id TEXT, team_id TEXT,
 component_id TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
 started_at TEXT, completed_at TEXT, estimate_before_start REAL,
 estimate_current REAL, reopen_count INTEGER NOT NULL,
 is_context INTEGER NOT NULL, duration_valid INTEGER NOT NULL,
 cycle_seconds INTEGER, evidence_id TEXT NOT NULL
);
CREATE TABLE v_pull_requests (
 pr_id TEXT PRIMARY KEY, repository TEXT NOT NULL, number INTEGER NOT NULL,
 title TEXT NOT NULL, author_id TEXT, state TEXT NOT NULL, created_at TEXT NOT NULL,
 ready_at TEXT, first_human_review_at TEXT, merged_at TEXT, head_sha TEXT,
 merge_sha TEXT, review_wait_seconds INTEGER, evidence_id TEXT NOT NULL
);
CREATE TABLE v_links (
 link_id TEXT PRIMARY KEY, source_id TEXT NOT NULL, relation TEXT NOT NULL,
 target_id TEXT NOT NULL, status TEXT NOT NULL, evidence_id TEXT NOT NULL,
 valid_from TEXT, valid_to TEXT
);
CREATE TABLE v_events (
 event_id TEXT PRIMARY KEY, entity_id TEXT NOT NULL, event_type TEXT NOT NULL,
 event_at TEXT, observed_at TEXT NOT NULL, actor_id TEXT,
 old_value TEXT, new_value TEXT, evidence_id TEXT NOT NULL
);
CREATE TABLE v_findings (
 finding_id TEXT PRIMARY KEY, rule_id TEXT NOT NULL, rule_version TEXT NOT NULL,
 severity TEXT NOT NULL, subject_id TEXT NOT NULL, title TEXT NOT NULL,
 deterministic_text TEXT NOT NULL, evidence_status TEXT NOT NULL,
 coverage TEXT NOT NULL
);
CREATE TABLE v_finding_evidence (
 finding_id TEXT NOT NULL, evidence_id TEXT NOT NULL,
 PRIMARY KEY(finding_id,evidence_id)
);
CREATE TABLE v_coverage (
 source TEXT NOT NULL, resource_id TEXT NOT NULL, collection TEXT NOT NULL,
 status TEXT NOT NULL, observed_count INTEGER NOT NULL, expected_count INTEGER,
 reason TEXT NOT NULL, checked_at TEXT NOT NULL,
 PRIMARY KEY(source,resource_id,collection)
);
CREATE TABLE v_evidence (
 evidence_id TEXT PRIMARY KEY, source TEXT NOT NULL, locator TEXT NOT NULL,
 observed_at TEXT NOT NULL, source_updated_at TEXT, sha256 TEXT NOT NULL,
 preview TEXT NOT NULL, redacted INTEGER NOT NULL, truncated INTEGER NOT NULL
);
CREATE TABLE v_metrics (
 metric_id TEXT PRIMARY KEY, name TEXT NOT NULL, value REAL, unit TEXT NOT NULL,
 sample_size INTEGER NOT NULL, eligible_size INTEGER, definition TEXT NOT NULL,
 limitations TEXT NOT NULL
);
CREATE TABLE v_stage_times (
 subject_id TEXT NOT NULL, component_id TEXT, stage TEXT NOT NULL,
 started_at TEXT, ended_at TEXT, elapsed_seconds INTEGER,
 duration_valid INTEGER NOT NULL, evidence_id TEXT NOT NULL
);
CREATE TABLE v_estimate_changes (
 change_id TEXT PRIMARY KEY, issue_id TEXT NOT NULL, actor_id TEXT,
 event_at TEXT, before_value REAL, after_value REAL, timing TEXT NOT NULL,
 evidence_id TEXT NOT NULL
);
CREATE TABLE v_rework (
 occurrence_id TEXT PRIMARY KEY, subject_id TEXT NOT NULL, component_id TEXT,
 kind TEXT NOT NULL, evidence_status TEXT NOT NULL, event_at TEXT,
 cause_status TEXT NOT NULL, evidence_id TEXT NOT NULL
);
CREATE TABLE v_delivery_artifacts (
 artifact_id TEXT PRIMARY KEY, component_id TEXT NOT NULL, digest TEXT,
 revision_sha TEXT, environment TEXT, observed_at TEXT,
 provenance_status TEXT NOT NULL, evidence_id TEXT NOT NULL
);
