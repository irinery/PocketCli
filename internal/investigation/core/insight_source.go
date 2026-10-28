package core

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/insightcontract"
	"pocketcli/internal/investigation/query"
	"pocketcli/internal/investigation/storage"
)

const insightSanitizationVersion = "pocketcli-local-v1"
const insightQuestionCatalogVersion = "l1-questions-v1"

type insightCoverageManifest struct {
	SnapshotID  string                           `json:"snapshot_id"`
	DatasetHash string                           `json:"dataset_hash"`
	Coverage    []insightcontract.SourceCoverage `json:"coverage"`
	Queues      []insightcontract.QueueProgress  `json:"queues"`
	CreatedAt   string                           `json:"created_at"`
}

// freezeInsightCoverage captures only coverage proven by the source cursors at
// publication time. The reader never consults mutable cursors or journals.
func (c *Core) freezeInsightCoverage(snapshot contract.Snapshot, plan contract.Plan, data query.Dataset) error {
	var existing insightCoverageManifest
	err := c.store.Get("insightcoverage", snapshot.ID, &existing)
	if err == nil {
		if existing.SnapshotID != snapshot.ID || existing.DatasetHash != snapshot.DatasetHash {
			return fault("ERR_AUDIT_FAILED", "manifesto de cobertura existente incompatível")
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return fault("ERR_AUDIT_FAILED", "manifesto de cobertura existente corrompido")
	}
	coverage := make([]insightcontract.SourceCoverage, 0, len(data.Coverage))
	queuesByID := make(map[string]insightcontract.QueueProgress)
	for _, item := range data.Coverage {
		entry := insightcontract.SourceCoverage{
			Key:    insightcontract.CoverageKey{Source: item.Source, ResourceID: item.ResourceID, Collection: item.Collection},
			Status: normalizeCoverageStatus(item.Status), Observed: item.Observed,
			Expected: cloneInt64(item.Expected), CheckedAt: item.CheckedAt,
			Reason: item.Reason, HistoryCoveredIntervals: []insightcontract.Window{}, QueueIDs: []string{},
		}
		resourceName := item.ResourceID
		for _, resource := range plan.Resources {
			if resource.Source == item.Source && resource.ID == item.ResourceID {
				resourceName = resource.Name
				break
			}
		}
		entry.HistoryObservedFrom, entry.HistoryObservedTo = observedEnvelope(data, item.Source, item.ResourceID, resourceName)
		for _, resource := range plan.Resources {
			if resource.ID != item.ResourceID || resource.Source != item.Source || (resource.Source != "github" && resource.Source != "linear") {
				continue
			}
			var handle cursorHandle
			var err error
			if resource.Source == "github" {
				handle, err = c.githubCursor(context.Background(), plan, resource, item.Collection)
			} else {
				handle, err = c.linearCursor(context.Background(), plan, resource, item.Collection)
			}
			if err != nil {
				continue
			}
			for _, interval := range handle.value.CoveredIntervals {
				entry.HistoryCoveredIntervals = append(entry.HistoryCoveredIntervals, insightcontract.Window{Start: interval.Start, End: interval.End})
			}
			queueID := "queue-" + storage.Hash([]byte(handle.id))[:24]
			entry.QueueIDs = append(entry.QueueIDs, queueID)
			queueStatus := "unknown"
			switch entry.Status {
			case "complete":
				queueStatus = "complete"
			case "partial":
				queueStatus = "paused"
			}
			var checkpointID *string
			if handle.value.Checkpoint != "" {
				value := "cp-" + storage.Hash([]byte(handle.id + ":" + handle.value.Checkpoint))[:24]
				checkpointID = &value
			}
			queuesByID[queueID] = insightcontract.QueueProgress{
				ID: queueID, Source: item.Source, ResourceID: item.ResourceID,
				Collections: []string{item.Collection}, ScopeHash: storage.Hash([]byte(handle.id)),
				Status: queueStatus, Pending: nil, CheckpointID: checkpointID,
			}
		}
		sort.Slice(entry.HistoryCoveredIntervals, func(i, j int) bool {
			if entry.HistoryCoveredIntervals[i].Start == entry.HistoryCoveredIntervals[j].Start {
				return entry.HistoryCoveredIntervals[i].End < entry.HistoryCoveredIntervals[j].End
			}
			return entry.HistoryCoveredIntervals[i].Start < entry.HistoryCoveredIntervals[j].Start
		})
		sort.Strings(entry.QueueIDs)
		coverage = append(coverage, entry)
	}
	sort.Slice(coverage, func(i, j int) bool { return coverageKey(coverage[i].Key) < coverageKey(coverage[j].Key) })
	queues := make([]insightcontract.QueueProgress, 0, len(queuesByID))
	for _, queue := range queuesByID {
		queues = append(queues, queue)
	}
	sort.Slice(queues, func(i, j int) bool { return queues[i].ID < queues[j].ID })
	manifest := insightCoverageManifest{SnapshotID: snapshot.ID, DatasetHash: snapshot.DatasetHash, Coverage: coverage, Queues: queues, CreatedAt: snapshot.PublishedAt}
	return c.putImmutable("insightcoverage", snapshot.ID, manifest)
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func normalizeCoverageStatus(status string) string {
	switch status {
	case "complete", "partial", "not_selected", "unsupported", "unknown":
		return status
	case "unavailable", "unauthorized", "rate_limited", "failed", "error":
		return "partial"
	default:
		return "unknown"
	}
}

func coverageKey(key insightcontract.CoverageKey) string {
	return key.Source + ":" + key.ResourceID + ":" + key.Collection
}

func observedEnvelope(data query.Dataset, source, resourceID, resourceName string) (*string, *string) {
	ids := make(map[string]bool)
	var first, last string
	add := func(value string) {
		if value == "" {
			return
		}
		if _, err := time.Parse(time.RFC3339, value); err != nil {
			return
		}
		if first == "" || value < first {
			first = value
		}
		if last == "" || value > last {
			last = value
		}
	}
	for _, issue := range data.Issues {
		matches := issue.Source == source && ((source == "linear" && issue.TeamID == resourceID) || (source == "github" && (strings.HasPrefix(strings.ToLower(issue.Identifier), strings.ToLower(resourceID)+"#") || strings.HasPrefix(strings.ToLower(issue.Identifier), strings.ToLower(resourceName)+"#"))))
		if matches {
			ids[issue.ID] = true
			add(issue.CreatedAt)
			add(issue.UpdatedAt)
			add(issue.StartedAt)
			add(issue.CompletedAt)
		}
	}
	for _, pr := range data.PullRequests {
		if source == "github" && (pr.Repository == resourceID || pr.Repository == resourceName) {
			ids[pr.ID] = true
			add(pr.CreatedAt)
			add(pr.UpdatedAt)
			add(pr.ReadyAt)
			add(pr.MergedAt)
		}
	}
	for _, event := range data.Events {
		if ids[event.EntityID] {
			add(event.EventAt)
			add(event.ObservedAt)
		}
	}
	return nonempty(first), nonempty(last)
}

// ReadInsightSource is a local-only projection over one immutable snapshot.
func (c *Core) ReadInsightSource(ctx context.Context, snapshotID string, cfg insightcontract.AnalysisConfig) (insightcontract.SourceBundle, error) {
	if err := ctx.Err(); err != nil {
		return insightcontract.SourceBundle{}, err
	}
	var snapshot contract.Snapshot
	if err := c.store.Get("snapshots", snapshotID, &snapshot); err != nil {
		return insightcontract.SourceBundle{}, fault("ERR_NOT_FOUND", "snapshot não encontrado")
	}
	var data query.Dataset
	if err := c.store.Get("datasets", snapshotID, &data); err != nil {
		return insightcontract.SourceBundle{}, fault("ERR_AUDIT_FAILED", "dataset do snapshot indisponível")
	}
	if snapshot.ID != snapshotID || len(snapshot.DatasetHash) != 64 || snapshot.ID != "snap-"+snapshot.DatasetHash[:24] || data.Snapshot.DatasetHash != snapshot.DatasetHash {
		return insightcontract.SourceBundle{}, fault("ERR_AUDIT_FAILED", "snapshot e dataset incompatíveis")
	}
	canonicalData := data
	canonicalData.Snapshot.ID = ""
	canonicalData.Snapshot.DatasetHash = ""
	canonicalData.Snapshot.PublishedAt = ""
	canonical, err := json.Marshal(canonicalData)
	if err != nil || storage.Hash(canonical) != snapshot.DatasetHash {
		return insightcontract.SourceBundle{}, fault("ERR_AUDIT_FAILED", "hash do dataset do snapshot inválido")
	}
	manifest := insightCoverageManifest{}
	manifestErr := c.store.Get("insightcoverage", snapshotID, &manifest)
	if manifestErr == nil && (manifest.SnapshotID != snapshotID || manifest.DatasetHash != snapshot.DatasetHash) {
		return insightcontract.SourceBundle{}, fault("ERR_AUDIT_FAILED", "manifesto de cobertura incompatível")
	}
	bundle := insightcontract.SourceBundle{
		Version: insightcontract.Version,
		Snapshot: insightcontract.SnapshotRef{ID: snapshot.ID, SchemaVersion: snapshot.SchemaVersion, QueryCatalogVersion: snapshot.QueryCatalogVersion, DatasetHash: snapshot.DatasetHash,
			Window: insightcontract.Window{Start: snapshot.WindowStart, End: snapshot.WindowEnd}, AnalysisReferenceAt: snapshot.AnalysisReferenceAt,
			ObservationStartedAt: snapshot.ObservationStartedAt, ObservationFinishedAt: snapshot.ObservationFinishedAt, RulesetVersion: snapshot.RulesetVersion, MappingVersion: snapshot.MappingVersion},
		RequestedScope:    cfg.RequestedScope,
		ObservedResources: make([]insightcontract.ResourceRef, 0, len(snapshot.Resources)),
		Coverage:          []insightcontract.SourceCoverage{}, Questions: []insightcontract.QuestionCoverage{}, Queues: []insightcontract.QueueProgress{},
		Entities: []insightcontract.Entity{}, Evidence: []insightcontract.EvidenceView{}, Links: []insightcontract.Link{}, Events: []insightcontract.Event{},
		Stages: []insightcontract.Stage{}, Estimates: []insightcontract.EstimateChange{}, Deliveries: []insightcontract.DeliveryObservation{}, LegacyFindings: []insightcontract.LegacyFinding{}, Limitations: []string{},
	}
	requestedResources := cfg.RequestedScope.Resources
	resourceForName := func(source, name string) (contract.Resource, bool) {
		for _, resource := range snapshot.Resources {
			if resource.Source == source && (resource.Name == name || resource.ID == name) {
				return resource, true
			}
		}
		return contract.Resource{}, false
	}
	isRequested := func(source, id, name string) bool {
		for _, requested := range requestedResources {
			if requested.Source == source && (requested.ID == id || requested.ID == name || requested.Name == id || requested.Name == name) {
				return true
			}
		}
		return false
	}
	observedResources := make(map[string]bool)
	for _, resource := range snapshot.Resources {
		key := resource.Source + ":" + resource.ID
		if isRequested(resource.Source, resource.ID, resource.Name) {
			bundle.ObservedResources = append(bundle.ObservedResources, insightcontract.ResourceRef{Source: resource.Source, ID: resource.ID, Name: resource.Name})
			observedResources[key] = true
		}
	}
	sort.Slice(bundle.ObservedResources, func(i, j int) bool {
		return resourceKey(bundle.ObservedResources[i]) < resourceKey(bundle.ObservedResources[j])
	})
	if len(requestedResources) == 0 {
		bundle.Limitations = append(bundle.Limitations, "Nenhum recurso foi selecionado na configuração; nenhum dado do snapshot foi incluído.")
	} else {
		for _, requested := range requestedResources {
			if !isRequested(requested.Source, requested.ID, requested.Name) || !resourceObserved(snapshot.Resources, requested.Source, requested.ID, requested.Name) {
				bundle.Limitations = append(bundle.Limitations, "Recurso solicitado não observado neste snapshot: "+requested.Source+":"+requested.ID)
			}
		}
	}
	if manifestErr != nil {
		if !os.IsNotExist(manifestErr) {
			return insightcontract.SourceBundle{}, fault("ERR_AUDIT_FAILED", "manifesto de cobertura corrompido")
		}
		bundle.Limitations = append(bundle.Limitations, "Snapshot legado sem manifesto de cobertura congelado; alcance histórico e filas permanecem desconhecidos.")
		for _, old := range snapshot.Coverage {
			bundle.Coverage = append(bundle.Coverage, insightcontract.SourceCoverage{Key: insightcontract.CoverageKey{Source: old.Source, ResourceID: old.ResourceID, Collection: old.Collection}, Status: "unknown", Observed: old.Observed, Expected: cloneInt64(old.Expected), HistoryCoveredIntervals: []insightcontract.Window{}, CheckedAt: old.CheckedAt, Reason: "metadata histórica não foi fixada no snapshot", QueueIDs: []string{}})
		}
	} else {
		bundle.Coverage = append(bundle.Coverage, manifest.Coverage...)
		bundle.Queues = append(bundle.Queues, manifest.Queues...)
	}
	coverageInScope := make([]insightcontract.SourceCoverage, 0, len(bundle.Coverage))
	queueIDsInScope := make(map[string]bool)
	for _, item := range bundle.Coverage {
		resourceName := ""
		if resource, ok := resourceForName(item.Key.Source, item.Key.ResourceID); ok {
			resourceName = resource.Name
		}
		if item.Key.Source != "analysis" && !isRequested(item.Key.Source, item.Key.ResourceID, resourceName) {
			continue
		}
		coverageInScope = append(coverageInScope, item)
		for _, id := range item.QueueIDs {
			queueIDsInScope[id] = true
		}
	}
	bundle.Coverage = coverageInScope
	queuesInScope := make([]insightcontract.QueueProgress, 0, len(bundle.Queues))
	for _, queue := range bundle.Queues {
		if queueIDsInScope[queue.ID] {
			queuesInScope = append(queuesInScope, queue)
		}
	}
	bundle.Queues = queuesInScope
	bundle.Questions = questionCoverage(cfg, bundle.Coverage)

	evidenceByID := make(map[string]insightcontract.EvidenceView, len(data.Evidence))
	for _, item := range data.Evidence {
		view := c.insightEvidence(item, cfg.Policy)
		bundle.Evidence = append(bundle.Evidence, view)
		evidenceByID[view.ID] = view
	}
	entityIDs := make(map[string]bool)
	allEntityIDs := make(map[string]bool, len(data.Issues)+len(data.PullRequests)+len(data.Components))
	for _, item := range data.Issues {
		allEntityIDs[item.ID] = true
	}
	for _, item := range data.PullRequests {
		allEntityIDs[item.ID] = true
	}
	for _, item := range data.Components {
		allEntityIDs[item.ID] = true
	}
	for _, component := range data.Components {
		resource, _ := resourceForName("github", component.Repository)
		if !isRequested("github", resource.ID, component.Repository) {
			continue
		}
		entity := insightcontract.Entity{ID: component.ID, Kind: "component", Source: "github", ResourceID: component.Repository, Identifier: component.Name, Title: component.Name, URL: nil, TeamID: nil, TeamName: nil, OwnerID: nil, ProjectID: nil, ComponentIDs: []string{component.ID}, State: "unknown", Outcome: "unknown", StateTemporalStatus: "unknown", WorkflowState: component.ImplementationStatus, EvidenceIDs: []string{}}
		bundle.Entities = append(bundle.Entities, entity)
		entityIDs[entity.ID] = true
	}
	for _, issue := range data.Issues {
		resourceID, resourceName := issue.TeamID, ""
		if issue.Source == "github" {
			repo := strings.SplitN(issue.Identifier, "#", 2)[0]
			resource, _ := resourceForName("github", repo)
			resourceID, resourceName = resource.ID, repo
		}
		if !isRequested(issue.Source, resourceID, resourceName) {
			continue
		}
		entity := issueEntity(issue)
		bundle.Entities = append(bundle.Entities, entity)
		entityIDs[entity.ID] = true
	}
	for _, pr := range data.PullRequests {
		resource, _ := resourceForName("github", pr.Repository)
		if !isRequested("github", resource.ID, pr.Repository) {
			continue
		}
		entity := insightcontract.Entity{ID: pr.ID, Kind: "pull_request", Source: "github", ResourceID: pr.Repository, Identifier: fmt.Sprintf("%s#%d", pr.Repository, pr.Number), Title: pr.Title,
			URL: nil, TeamID: nil, TeamName: nil, OwnerID: ptr(pr.AuthorID), ProjectID: nil, ComponentIDs: []string{}, State: normalizeEntityState(pr.State), Outcome: "unclassified", StateTemporalStatus: "current_only", WorkflowState: pr.State,
			CreatedAt: nonempty(pr.CreatedAt), UpdatedAt: nonempty(pr.UpdatedAt), StartedAt: nonempty(pr.ReadyAt), CompletedAt: nonempty(pr.MergedAt), DueAt: nil, EvidenceIDs: evidenceList(pr.EvidenceID)}
		bundle.Entities = append(bundle.Entities, entity)
		entityIDs[entity.ID] = true
	}
	for _, link := range data.Links {
		if !entityIDs[link.SourceID] || (allEntityIDs[link.TargetID] && !entityIDs[link.TargetID]) {
			continue
		}
		status := "explicit"
		if link.Relation == "mentions" || link.Relation == "declares_fix_of" || link.Status == "candidate" {
			status = "candidate"
		}
		evIDs := evidenceList(link.EvidenceID)
		validEvidence := len(evIDs) > 0 && evidenceByID[evIDs[0]].IntegrityStatus == "verified"
		if !entityIDs[link.SourceID] || !entityIDs[link.TargetID] || !validEvidence {
			status = "invalid"
		}
		bundle.Links = append(bundle.Links, insightcontract.Link{ID: link.ID, FromID: link.SourceID, ToID: link.TargetID, Relation: link.Relation, Status: status,
			TemporalStatus: temporalLinkStatus(link), ValidFrom: nonempty(link.ValidFrom), ValidTo: nonempty(link.ValidTo), EvidenceIDs: evIDs})
	}
	for _, event := range data.Events {
		if !entityIDs[event.EntityID] {
			continue
		}
		bundle.Events = append(bundle.Events, insightcontract.Event{ID: event.ID, EntityID: event.EntityID, Type: event.Type, EventAt: nonempty(event.EventAt), ObservedAt: event.ObservedAt, ActorID: nonempty(event.ActorID), OldValue: nonempty(event.OldValue), NewValue: nonempty(event.NewValue), EvidenceIDs: evidenceList(event.EvidenceID)})
	}
	for _, stage := range data.Stages {
		if !entityIDs[stage.SubjectID] {
			continue
		}
		var duration *float64
		if stage.ElapsedSeconds != nil {
			v := float64(*stage.ElapsedSeconds)
			duration = &v
		}
		bundle.Stages = append(bundle.Stages, insightcontract.Stage{ID: stage.ID, EntityID: stage.SubjectID, TeamID: nil, Name: stage.Stage, Start: nonempty(stage.StartedAt), End: nonempty(stage.EndedAt), DurationSeconds: duration, DurationValid: stage.DurationValid, HistoryComplete: stage.DurationValid, EvidenceIDs: evidenceList(stage.EvidenceID)})
	}
	for _, estimate := range data.Estimates {
		if !entityIDs[estimate.IssueID] {
			continue
		}
		bundle.Estimates = append(bundle.Estimates, insightcontract.EstimateChange{ID: estimate.ID, EntityID: estimate.IssueID, EventAt: nonempty(estimate.EventAt), Before: estimate.Before, After: estimate.After, Timing: normalizeEstimateTiming(estimate.Timing), ScaleID: nil, EvidenceIDs: evidenceList(estimate.EvidenceID)})
	}
	for _, delivery := range data.Deliveries {
		if !entityIDs[delivery.ComponentID] {
			continue
		}
		status := "unknown"
		if delivery.ProvenanceStatus == "verified" || delivery.ProvenanceStatus == "observed" {
			status = "observed"
		}
		bundle.Deliveries = append(bundle.Deliveries, insightcontract.DeliveryObservation{ID: delivery.ID, EntityID: delivery.ComponentID, Environment: delivery.Environment, Status: status, EventAt: nonempty(delivery.ObservedAt), EvidenceIDs: evidenceList(delivery.EvidenceID)})
	}
	for _, finding := range data.Findings {
		if !entityIDs[finding.SubjectID] {
			continue
		}
		bundle.LegacyFindings = append(bundle.LegacyFindings, insightcontract.LegacyFinding{ID: finding.ID, RuleID: finding.RuleID, SubjectID: finding.SubjectID, Severity: finding.Severity, EvidenceIDs: append([]string{}, finding.EvidenceIDs...)})
	}
	evidenceIDs := make(map[string]bool)
	for _, entity := range bundle.Entities {
		for _, id := range entity.EvidenceIDs {
			evidenceIDs[id] = true
		}
	}
	for _, link := range bundle.Links {
		for _, id := range link.EvidenceIDs {
			evidenceIDs[id] = true
		}
	}
	for _, event := range bundle.Events {
		for _, id := range event.EvidenceIDs {
			evidenceIDs[id] = true
		}
	}
	for _, stage := range bundle.Stages {
		for _, id := range stage.EvidenceIDs {
			evidenceIDs[id] = true
		}
	}
	for _, estimate := range bundle.Estimates {
		for _, id := range estimate.EvidenceIDs {
			evidenceIDs[id] = true
		}
	}
	for _, delivery := range bundle.Deliveries {
		for _, id := range delivery.EvidenceIDs {
			evidenceIDs[id] = true
		}
	}
	for _, finding := range bundle.LegacyFindings {
		for _, id := range finding.EvidenceIDs {
			evidenceIDs[id] = true
		}
	}
	filteredEvidence := make([]insightcontract.EvidenceView, 0, len(evidenceIDs))
	for _, evidence := range bundle.Evidence {
		if evidenceIDs[evidence.ID] {
			filteredEvidence = append(filteredEvidence, evidence)
		}
	}
	bundle.Evidence = filteredEvidence
	sortSourceBundle(&bundle)
	canonical, err = json.Marshal(struct {
		SnapshotHash           string                         `json:"snapshot_hash"`
		Config                 insightcontract.AnalysisConfig `json:"config"`
		QuestionCatalogVersion string                         `json:"question_catalog_version"`
		Bundle                 insightcontract.SourceBundle   `json:"bundle"`
	}{snapshot.DatasetHash, cfg, insightQuestionCatalogVersion, bundle})
	if err != nil {
		return insightcontract.SourceBundle{}, fault("ERR_AUDIT_FAILED", "não foi possível calcular hash da fonte")
	}
	bundle.InputHash = storage.Hash(canonical)
	return bundle, nil
}

func (c *Core) insightEvidence(item contract.Evidence, policy insightcontract.Policy) insightcontract.EvidenceView {
	view := insightcontract.EvidenceView{ID: item.ID, Source: item.Source, Locator: item.Locator, ObservedAt: item.ObservedAt, SourceUpdatedAt: nonempty(item.SourceUpdatedAt), ContentHash: item.SHA256,
		ContentStatus: "unavailable", IntegrityStatus: "missing", Excerpt: "", ExcerptHash: storage.Hash(nil), Redacted: false, Truncated: item.Truncated, SanitizationVersion: insightSanitizationVersion}
	var raw json.RawMessage
	if err := c.store.Get("objects", item.ID, &raw); err != nil {
		return view
	}
	if storage.Hash(raw) != item.SHA256 {
		view.ContentStatus = "integrity_error"
		view.IntegrityStatus = "mismatch"
		return view
	}
	view.IntegrityStatus = "verified"
	if policy.ContentMode == "metadata_only" {
		view.ContentStatus = "metadata_only"
		return view
	}
	var object struct {
		Title   string `json:"title"`
		Body    string `json:"body"`
		Message string `json:"message"`
		URL     string `json:"html_url"`
	}
	if json.Unmarshal(raw, &object) != nil {
		view.ContentStatus = "metadata_only"
		return view
	}
	text := object.Title
	if text != "" && object.Body != "" {
		text += "\n"
	}
	text += object.Body
	if text == "" {
		text = object.Message
	}
	if text == "" {
		view.ContentStatus = "metadata_only"
		return view
	}
	text, redacted := redactInsightText(text)
	view.Redacted = view.Redacted || redacted
	maxRunes := policy.ExcerptMaxRunes
	if maxRunes <= 0 {
		maxRunes = 1200
	}
	runes := []rune(text)
	if len(runes) > maxRunes {
		text = string(runes[:maxRunes])
		view.Truncated = true
	}
	view.Excerpt = strings.TrimSpace(text)
	view.ExcerptHash = storage.Hash([]byte(view.Excerpt))
	if view.Excerpt == "" {
		view.ContentStatus = "redacted_all"
	} else {
		view.ContentStatus = "available"
	}
	if object.URL != "" {
		view.URL = ptr(object.URL)
	}
	return view
}

var insightSecretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(authorization\s*[:=]\s*bearer\s+)[^\s"']+`),
	regexp.MustCompile(`(?i)(\b(?:token|api[_-]?key|password|secret)\b\s*[:=]\s*)[^\s,;"']+`),
	regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{20,}\b`),
	regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}\b`),
}

func redactInsightText(text string) (string, bool) {
	redacted := false
	for _, pattern := range insightSecretPatterns {
		if pattern.MatchString(text) {
			redacted = true
			text = pattern.ReplaceAllString(text, `${1}[REDACTED]`)
		}
	}
	return text, redacted
}

func issueEntity(issue query.Issue) insightcontract.Entity {
	state := normalizeEntityState(issue.State)
	outcome := normalizeEntityOutcome(issue.Outcome)
	components := []string{}
	if issue.ComponentID != "" {
		components = append(components, issue.ComponentID)
	}
	return insightcontract.Entity{ID: issue.ID, Kind: "issue", Source: issue.Source, ResourceID: issue.TeamID, Identifier: issue.Identifier, Title: issue.Title, URL: nil,
		TeamID: nonempty(issue.TeamID), TeamName: nil, OwnerID: nonempty(issue.AssigneeID), ProjectID: nil, ComponentIDs: components, State: state, Outcome: outcome,
		StateTemporalStatus: "current_only", WorkflowState: issue.State, CreatedAt: nonempty(issue.CreatedAt), UpdatedAt: nonempty(issue.UpdatedAt), StartedAt: nonempty(issue.StartedAt), CompletedAt: nonempty(issue.CompletedAt), DueAt: nil,
		IsContext: issue.IsContext, EstimateCurrent: issue.EstimateCurrent, EstimateBeforeStart: issue.EstimateBeforeStart, EstimateScaleID: nil, EvidenceIDs: evidenceList(issue.EvidenceID)}
}

func normalizeEntityOutcome(value string) string {
	switch strings.ToLower(value) {
	case "active", "completed", "cancelled", "duplicate", "not_planned", "unclassified", "unknown":
		return strings.ToLower(value)
	case "canceled":
		return "cancelled"
	default:
		return "unknown"
	}
}

func normalizeEntityState(value string) string {
	switch strings.ToLower(strings.ReplaceAll(value, " ", "_")) {
	case "open", "todo", "in_progress", "started", "reopened", "unstarted", "backlog", "triage", "done", "completed", "closed", "cancelled", "duplicate":
		return strings.ToLower(strings.ReplaceAll(value, " ", "_"))
	default:
		return "unknown"
	}
}

func normalizeEstimateTiming(value string) string {
	if value == "before_start" || value == "after_start" {
		return value
	}
	return "unknown"
}
func temporalLinkStatus(link contract.LinkRecord) string {
	if link.ValidFrom != "" || link.ValidTo != "" {
		return "known_interval"
	}
	if link.Status == "direct" {
		return "observed_at_reference"
	}
	return "unknown"
}
func evidenceList(id string) []string {
	if id == "" {
		return []string{}
	}
	return []string{id}
}
func nonempty(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
func ptr(value string) *string                             { return &value }
func resourceKey(value insightcontract.ResourceRef) string { return value.Source + ":" + value.ID }

func resourceObserved(resources []contract.Resource, source, id, name string) bool {
	for _, resource := range resources {
		if resource.Source == source && (resource.ID == id || resource.ID == name || resource.Name == id || resource.Name == name) {
			return true
		}
	}
	return false
}

func questionCoverage(cfg insightcontract.AnalysisConfig, coverage []insightcontract.SourceCoverage) []insightcontract.QuestionCoverage {
	questions := []string{"history_scope", "decision_lineage", "delivery_trace", "flow_timing", "quality_outcome", "cost_actual", "objective_status"}
	out := make([]insightcontract.QuestionCoverage, 0, len(questions))
	for _, id := range questions {
		required := make([]insightcontract.CoverageKey, 0)
		for _, resource := range cfg.RequestedScope.Resources {
			collections := collectionsForQuestion(id, resource.Source)
			for _, collection := range collections {
				required = append(required, insightcontract.CoverageKey{Source: resource.Source, ResourceID: resource.ID, Collection: collection})
			}
		}
		missing := make([]string, 0)
		status := "sufficient"
		if len(required) == 0 {
			status = "insufficient"
			missing = append(missing, "Nenhuma coleção necessária foi selecionada para esta pergunta.")
		}
		for _, key := range required {
			found := false
			for _, item := range coverage {
				if item.Key == key {
					found = true
					if item.Status == "not_selected" || item.Status == "unsupported" || item.Status == "unknown" {
						status = "insufficient"
						missing = append(missing, coverageKey(key)+" sem cobertura verificável")
					} else if item.Status == "partial" && status == "sufficient" {
						status = "partial"
						missing = append(missing, coverageKey(key)+" parcialmente coberta")
					}
					break
				}
			}
			if !found {
				status = "insufficient"
				missing = append(missing, coverageKey(key)+" ausente do manifesto")
			}
		}
		if id == "history_scope" {
			want := cfg.RequestedScope.History
			for _, key := range required {
				for _, item := range coverage {
					if item.Key == key && !intervalsCover(item.HistoryCoveredIntervals, want) {
						if status == "sufficient" {
							status = "partial"
						}
						missing = append(missing, "intervalo histórico solicitado não está comprovadamente coberto: "+coverageKey(key))
					}
				}
			}
			for _, resource := range cfg.RequestedScope.Resources {
				if resource.Source == "github" {
					if status == "sufficient" {
						status = "partial"
					}
					missing = append(missing, "API GitHub expõe representação atual; intervalos percorridos não comprovam reconstrução histórica de estados.")
				}
			}
		}
		if id == "objective_status" {
			hasExplicitTarget := false
			for _, objective := range cfg.Objectives {
				if objective.Target != nil || objective.Comparator != "" && objective.Comparator != "none" {
					hasExplicitTarget = true
					break
				}
			}
			if !hasExplicitTarget {
				status = "insufficient"
				missing = append(missing, "Nenhuma meta ou critério explícito foi fornecido para avaliar objetivos.")
			}
		}
		out = append(out, insightcontract.QuestionCoverage{QuestionID: id, Status: status, RequiredCollections: required, Missing: dedupe(missing), Reason: questionReason(id)})
	}
	return out
}

func collectionsForQuestion(question, source string) []string {
	switch question {
	case "history_scope":
		switch source {
		case "github":
			return []string{"issues", "pull_requests"}
		case "linear":
			return []string{"issues", "history"}
		case "git":
			return []string{"git_commits"}
		default:
			return []string{"unknown"}
		}
	case "decision_lineage":
		if source == "linear" {
			return []string{"issues", "history", "comments"}
		}
		return []string{"issues", "comments", "formal_links"}
	case "delivery_trace":
		if source == "github" {
			return []string{"pull_requests", "formal_links", "deployments"}
		}
		if source == "linear" {
			return []string{"issues", "attachments"}
		}
		return []string{"code_history", "delivery_ancestry"}
	case "flow_timing":
		if source == "linear" {
			return []string{"issues", "history"}
		}
		return []string{"pull_requests"}
	case "quality_outcome":
		if source == "github" {
			return []string{"pull_requests", "checks"}
		}
		if source == "linear" {
			return []string{"issues", "history"}
		}
		return []string{"unknown"}
	case "cost_actual":
		return []string{"cost"}
	case "objective_status":
		if source == "linear" {
			return []string{"issues"}
		}
		return []string{"unknown"}
	default:
		return []string{}
	}
}

func questionReason(question string) string {
	switch question {
	case "history_scope":
		return "Alcance histórico exige intervalos confirmados para cada recurso e coleção necessária."
	case "cost_actual":
		return "Custo real exige medidas efetivas de custo/alocação; não é inferido de permanência ou volume."
	case "objective_status":
		return "Status de objetivo exige meta ou critério explícito e medida comparável."
	default:
		return "Suficiência depende da cobertura das coleções necessárias no escopo solicitado."
	}
}

func intervalsCover(intervals []insightcontract.Window, requested insightcontract.Window) bool {
	if requested.Start == "" || requested.End == "" {
		return false
	}
	start, err1 := time.Parse(time.RFC3339, requested.Start)
	end, err2 := time.Parse(time.RFC3339, requested.End)
	if err1 != nil || err2 != nil || !start.Before(end) {
		return false
	}
	current := start
	ordered := append([]insightcontract.Window{}, intervals...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Start < ordered[j].Start })
	for _, interval := range ordered {
		from := start
		if interval.Start != "" {
			from, err1 = time.Parse(time.RFC3339, interval.Start)
			if err1 != nil {
				continue
			}
		}
		to, err := time.Parse(time.RFC3339, interval.End)
		if err != nil || to.Before(current) {
			continue
		}
		if from.After(current) {
			return false
		}
		if to.After(current) {
			current = to
		}
		if !current.Before(end) {
			return true
		}
	}
	return false
}

func sortSourceBundle(bundle *insightcontract.SourceBundle) {
	sort.Slice(bundle.Coverage, func(i, j int) bool { return coverageKey(bundle.Coverage[i].Key) < coverageKey(bundle.Coverage[j].Key) })
	sort.Slice(bundle.Queues, func(i, j int) bool { return bundle.Queues[i].ID < bundle.Queues[j].ID })
	sort.Slice(bundle.Entities, func(i, j int) bool { return bundle.Entities[i].ID < bundle.Entities[j].ID })
	sort.Slice(bundle.Evidence, func(i, j int) bool { return bundle.Evidence[i].ID < bundle.Evidence[j].ID })
	sort.Slice(bundle.Links, func(i, j int) bool { return bundle.Links[i].ID < bundle.Links[j].ID })
	sort.Slice(bundle.Events, func(i, j int) bool {
		if bundle.Events[i].EventAt == nil {
			return bundle.Events[j].EventAt != nil
		}
		if bundle.Events[j].EventAt == nil {
			return false
		}
		if *bundle.Events[i].EventAt == *bundle.Events[j].EventAt {
			return bundle.Events[i].ID < bundle.Events[j].ID
		}
		return *bundle.Events[i].EventAt < *bundle.Events[j].EventAt
	})
	sort.Slice(bundle.Stages, func(i, j int) bool {
		if bundle.Stages[i].Start == nil {
			return bundle.Stages[j].Start != nil
		}
		if bundle.Stages[j].Start == nil {
			return false
		}
		if *bundle.Stages[i].Start == *bundle.Stages[j].Start {
			return bundle.Stages[i].ID < bundle.Stages[j].ID
		}
		return *bundle.Stages[i].Start < *bundle.Stages[j].Start
	})
	sort.Slice(bundle.Estimates, func(i, j int) bool { return bundle.Estimates[i].ID < bundle.Estimates[j].ID })
	sort.Slice(bundle.Deliveries, func(i, j int) bool { return bundle.Deliveries[i].ID < bundle.Deliveries[j].ID })
	sort.Slice(bundle.LegacyFindings, func(i, j int) bool { return bundle.LegacyFindings[i].ID < bundle.LegacyFindings[j].ID })
	sort.Strings(bundle.Limitations)
}
