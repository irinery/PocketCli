// Package analysisfacts derives reproducible, offline facts from a frozen insight bundle.
package analysisfacts

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"pocketcli/internal/investigation/insightcontract"
)

const (
	statusObserved       = "observed"
	statusUnknown        = "unknown"
	statusNotCalculable  = "not_calculable"
	statusImmature       = "immature"
	coverageComplete     = "complete"
	coveragePartial      = "partial"
	coverageUnknown      = "unknown"
	coverageNotSelected  = "not_selected"
	coverageUnsupported  = "unsupported"
	minimumQuantileCount = 10
)

type linkPair struct{ from, to string }

// Analyze calculates deterministic facts only from the supplied snapshot and configuration.
// It never reads the clock, performs I/O, or invokes an inference provider.
func Analyze(bundle insightcontract.SourceBundle, cfg insightcontract.AnalysisConfig) (insightcontract.FactsResult, error) {
	start, end, reportWindowOK := parseWindow(cfg.RequestedScope.ReportWindow)
	reference, referenceOK := parseInstant(bundle.Snapshot.AnalysisReferenceAt)
	configHash, err := stableConfigHash(cfg, bundle.Snapshot.SchemaVersion, bundle.Snapshot.QueryCatalogVersion)
	if err != nil {
		return insightcontract.FactsResult{}, fmt.Errorf("hash analysis config: %w", err)
	}
	inputHash := bundle.InputHash
	if inputHash == "" {
		b, marshalErr := json.Marshal(bundle)
		if marshalErr != nil {
			return insightcontract.FactsResult{}, fmt.Errorf("hash source bundle: %w", marshalErr)
		}
		sum := sha256.Sum256(b)
		inputHash = hex.EncodeToString(sum[:])
	}
	bundle.InputHash = inputHash
	// AnalyzeConfig.RequestedScope is authoritative for the analysis identity.
	bundle.RequestedScope = cfg.RequestedScope

	result := insightcontract.FactsResult{
		Version: insightcontract.Version, SnapshotID: bundle.Snapshot.ID,
		InputHash: inputHash, ConfigHash: configHash,
		Cohorts: []insightcontract.Cohort{}, Metrics: []insightcontract.Metric{},
		Cases: []insightcontract.FactCase{}, Limitations: append([]string{}, bundle.Limitations...),
	}
	if !reportWindowOK {
		result.Limitations = appendUnique(result.Limitations, "ReportWindow inválida; coortes dependentes da janela permanecem desconhecidas.")
	}
	entities := indexEntities(bundle.Entities)
	reportCoverage := reportWindowCoverage(bundle, cfg.RequestedScope, entities)

	if reportWindowOK {
		result.Cohorts = append(result.Cohorts,
			completionTimestampCohort(bundle, cfg, start, end, reportCoverage),
			completionSnapshotCohort(bundle, cfg.RequestedScope, populationCoverage(bundle, cfg.RequestedScope, []string{"entities", "issues", "pull_requests", "snapshot"})),
			completionTransitionCohort(bundle, cfg.RequestedScope, entities, start, end, reportCoverage),
		)
	} else {
		result.Cohorts = append(result.Cohorts,
			unknownCohort("completed-at-report-window", "Entidades com completed_at na janela pedida", cfg.RequestedScope.ReportWindow),
			unknownCohort("completed-at-snapshot", "Entidades com estado concluído conhecido no snapshot", bundle.Snapshot.Window),
			unknownCohort("completion-transitions-report-window", "Transições observadas para concluído na janela pedida", cfg.RequestedScope.ReportWindow),
		)
	}

	agingCohorts, agingMetrics := aging(bundle, cfg.RequestedScope, reference, referenceOK, cfg.Policy.MinimumQuantileSample)
	result.Cohorts = append(result.Cohorts, agingCohorts...)
	result.Metrics = append(result.Metrics, agingMetrics...)
	reworkCohort, reworkMetrics, reworkCases := reworkMetrics(bundle, cfg.RequestedScope, entities, reference, referenceOK)
	result.Cohorts = append(result.Cohorts, reworkCohort)
	result.Metrics = append(result.Metrics, reworkMetrics...)
	result.Cases = append(result.Cases, reworkCases...)
	result.Cases = append(result.Cases, dependencyCases(bundle, cfg.Objectives, cfg.RequestedScope, entities, reference, referenceOK)...)
	dependencyCohort, dependencyMetrics := dependencySummary(bundle, cfg.RequestedScope, reference, referenceOK)
	result.Cohorts = append(result.Cohorts, dependencyCohort)
	result.Metrics = append(result.Metrics, dependencyMetrics...)
	result.Limitations = appendUnique(result.Limitations, "As métricas descrevem registros observados; relações não provam causa, impacto, duração ou urgência.")
	result.Limitations = appendUnique(result.Limitations, "SchemaVersion e QueryCatalogVersion do snapshot foram incluídos no ConfigHash da análise.")

	sort.Slice(result.Cohorts, func(i, j int) bool { return result.Cohorts[i].ID < result.Cohorts[j].ID })
	sort.Slice(result.Metrics, func(i, j int) bool { return result.Metrics[i].ID < result.Metrics[j].ID })
	sort.Slice(result.Cases, func(i, j int) bool { return result.Cases[i].ID < result.Cases[j].ID })
	return result, nil
}

func stableConfigHash(cfg insightcontract.AnalysisConfig, schema, catalog string) (string, error) {
	return insightcontract.ConfigHash(cfg, schema, catalog)
}

func completionTimestampCohort(bundle insightcontract.SourceBundle, cfg insightcontract.AnalysisConfig, start, end time.Time, coverage string) insightcontract.Cohort {
	ids := []string{}
	for _, e := range bundle.Entities {
		if e.Kind == "component" || e.IsContext || e.CompletedAt == nil || !entityInScope(e, cfg.RequestedScope) {
			continue
		}
		at, ok := parseInstant(*e.CompletedAt)
		if ok && inWindow(at, start, end) {
			ids = append(ids, e.ID)
		}
	}
	return makeCohort(bundle, "completed-at-report-window", "Entidades não contextuais com completed_at em [ReportWindow.Start, ReportWindow.End)", cfg.RequestedScope.ReportWindow, bundle.Snapshot.AnalysisReferenceAt, ids, int64(len(ids)), coverage, nil)
}

func completionSnapshotCohort(bundle insightcontract.SourceBundle, scope insightcontract.Scope, coverage string) insightcontract.Cohort {
	ids := []string{}
	for _, e := range bundle.Entities {
		if e.Kind == "component" || e.IsContext || !entityInScope(e, scope) || e.StateTemporalStatus != "known_at_reference" || !isCompletedState(e.State) {
			continue
		}
		ids = append(ids, e.ID)
	}
	exclusions := []string{}
	for _, e := range bundle.Entities {
		if e.Kind == "component" || e.IsContext || !entityInScope(e, scope) {
			continue
		}
		if isCompletedState(e.State) && e.StateTemporalStatus != "known_at_reference" {
			exclusions = appendUnique(exclusions, "estado concluído observado sem validade temporal conhecida no corte")
		}
		if e.State == "unknown" || e.StateTemporalStatus == "unknown" || e.StateTemporalStatus == "ambiguous" {
			exclusions = appendUnique(exclusions, "estado atual desconhecido/ambíguo em parte da população")
		}
	}
	if len(exclusions) > 0 && coverage == coverageComplete {
		coverage = coveragePartial
	}
	window := bundle.Snapshot.Window
	if _, _, ok := parseWindow(window); !ok {
		window = insightcontract.Window{}
	}
	return makeCohort(bundle, "completed-at-snapshot", "Entidades não contextuais em estado concluído conhecido na referência do snapshot", window, bundle.Snapshot.AnalysisReferenceAt, ids, int64(len(ids)), coverage, exclusions)
}

func completionTransitionCohort(bundle insightcontract.SourceBundle, scope insightcontract.Scope, entities map[string]insightcontract.Entity, start, end time.Time, coverage string) insightcontract.Cohort {
	byEntity := map[string]bool{}
	for _, event := range bundle.Events {
		at, ok := parseOptionalInstant(event.EventAt)
		entity, found := entities[event.EntityID]
		if !ok || !inWindow(at, start, end) || !isCompletionTransition(event) || !found || entity.Kind == "component" || entity.IsContext || !entityInScope(entity, scope) {
			continue
		}
		byEntity[event.EntityID] = true
	}
	ids := sortedKeys(byEntity)
	return makeCohort(bundle, "completion-transitions-report-window", "Entidades distintas com transição observada para concluído em [ReportWindow.Start, ReportWindow.End)", insightcontract.Window{Start: start.UTC().Format(time.RFC3339), End: end.UTC().Format(time.RFC3339)}, bundle.Snapshot.AnalysisReferenceAt, ids, int64(len(ids)), coverage, nil)
}

func makeCohort(bundle insightcontract.SourceBundle, key, definition string, window insightcontract.Window, ref string, ids []string, eligible int64, coverage string, exclusions []string) insightcontract.Cohort {
	ids = uniqueSorted(ids)
	scopeJSON, _ := json.Marshal(bundle.RequestedScope)
	var eligiblePtr *int64
	if coverage == coverageComplete {
		eligiblePtr = int64Ptr(eligible)
	}
	return insightcontract.Cohort{ID: stableID("cohort", bundle.Snapshot.ID, bundle.InputHash, bundle.Snapshot.SchemaVersion, bundle.Snapshot.QueryCatalogVersion, string(scopeJSON), key, window.Start, window.End, ref),
		Definition: definition, Window: window, ReferenceAt: ref, EntityIDs: ids, Count: int64(len(ids)), EligibleCount: eligiblePtr, CoverageStatus: coverage, Exclusions: nonNilStrings(exclusions)}
}

func unknownCohort(key, definition string, window insightcontract.Window) insightcontract.Cohort {
	return insightcontract.Cohort{ID: stableID("cohort", key, window.Start, window.End), Definition: definition, Window: window, EntityIDs: []string{}, CoverageStatus: coverageUnknown, Exclusions: []string{"janela ausente ou inválida"}}
}

func reportWindowCoverage(bundle insightcontract.SourceBundle, scope insightcontract.Scope, entities map[string]insightcontract.Entity) string {
	start, end, ok := parseWindow(scope.ReportWindow)
	if !ok {
		return coverageUnknown
	}
	resources := map[string]bool{}
	for _, e := range entities {
		if entityInScope(e, scope) && e.Source != "" && e.ResourceID != "" {
			resources[e.Source+"\x00"+e.ResourceID] = true
		}
	}
	if len(resources) == 0 {
		for _, r := range scope.Resources {
			resources[r.Source+"\x00"+r.ID] = true
		}
	}
	if len(resources) == 0 {
		return coverageUnknown
	}
	state := coverageComplete
	for key := range resources {
		parts := strings.SplitN(key, "\x00", 2)
		found := false
		resourceState := coverageUnknown
		intervals := []insightcontract.Window{}
		allComplete := true
		for _, c := range bundle.Coverage {
			if c.Key.Source != parts[0] || c.Key.ResourceID != parts[1] || c.Key.Collection != "history" {
				continue
			}
			found = true
			if c.Status == "not_selected" {
				return coverageNotSelected
			}
			if c.Status == "unsupported" {
				return coverageUnsupported
			}
			if c.Status == "unknown" {
				resourceState = coverageUnknown
				allComplete = false
				continue
			}
			if c.Status == "complete" {
				intervals = append(intervals, c.HistoryCoveredIntervals...)
			} else {
				resourceState = coveragePartial
				allComplete = false
			}
		}
		if !found {
			resourceState = coverageUnknown
		} else if resourceState != coverageUnknown && resourceState != coveragePartial && allComplete && intervalsCover(intervals, start, end) {
			resourceState = coverageComplete
		} else if resourceState != coverageUnknown {
			resourceState = coveragePartial
		}
		if resourceState == coverageUnknown || resourceState == coverageNotSelected || resourceState == coverageUnsupported {
			return resourceState
		}
		if resourceState != coverageComplete {
			state = coveragePartial
		}
	}
	return state
}

func populationCoverage(bundle insightcontract.SourceBundle, scope insightcontract.Scope, collections []string) string {
	resources := map[string]bool{}
	for _, resource := range scope.Resources {
		if resource.Source != "" && resource.ID != "" {
			resources[resource.Source+"\x00"+resource.ID] = true
		}
	}
	if len(resources) == 0 {
		for _, resource := range bundle.ObservedResources {
			if resource.Source != "" && resource.ID != "" {
				resources[resource.Source+"\x00"+resource.ID] = true
			}
		}
	}
	if len(resources) == 0 {
		return coverageUnknown
	}
	result := coverageComplete
	for key := range resources {
		parts := strings.SplitN(key, "\x00", 2)
		found := false
		for _, coverage := range bundle.Coverage {
			if coverage.Key.Source != parts[0] || coverage.Key.ResourceID != parts[1] || !contains(collections, coverage.Key.Collection) {
				continue
			}
			found = true
			switch coverage.Status {
			case "not_selected":
				return coverageNotSelected
			case "unsupported":
				return coverageUnsupported
			case "unknown":
				return coverageUnknown
			case "complete":
			default:
				result = coveragePartial
			}
		}
		if !found {
			return coverageUnknown
		}
	}
	return result
}

func aging(bundle insightcontract.SourceBundle, scope insightcontract.Scope, ref time.Time, refOK bool, minSample int) ([]insightcontract.Cohort, []insightcontract.Metric) {
	if minSample <= 0 {
		minSample = minimumQuantileCount
	}
	cohorts, metrics := []insightcontract.Cohort{}, []insightcontract.Metric{}
	for _, contextual := range []bool{false, true} {
		label, key := "não contextual", "open-aging-non-context"
		if contextual {
			label, key = "contextual", "open-aging-context"
		}
		openIDs := []string{}
		ages := []float64{}
		openKnown := int64(0)
		exclusions := []string{}
		for _, e := range bundle.Entities {
			if e.Kind == "component" || e.IsContext != contextual || !entityInScope(e, scope) {
				continue
			}
			if e.StateTemporalStatus != "known_at_reference" {
				if e.State == "unknown" || isOpenState(e.State) {
					exclusions = appendUnique(exclusions, "estado aberto não comprovável na âncora")
				}
				continue
			}
			if !isOpenState(e.State) {
				continue
			}
			openKnown++
			openIDs = append(openIDs, e.ID)
			if !refOK {
				exclusions = appendUnique(exclusions, "AnalysisReferenceAt ausente/inválida")
				continue
			}
			created, ok := parseOptionalInstant(e.CreatedAt)
			if !ok {
				exclusions = appendUnique(exclusions, "criação ausente/inválida")
				continue
			}
			if created.After(ref) {
				exclusions = appendUnique(exclusions, "criação posterior à âncora")
				continue
			}
			ages = append(ages, ref.Sub(created).Hours()/24)
		}
		coverage := populationCoverage(bundle, scope, []string{"entities", "issues", "pull_requests", "snapshot"})
		if !refOK {
			coverage = coverageUnknown
		}
		if len(exclusions) > 0 && coverage == coverageComplete {
			coverage = coveragePartial
		}
		refStr := bundle.Snapshot.AnalysisReferenceAt
		cohort := makeCohort(bundle, key, "Estoque aberto conhecido; idade desde CreatedAt, não tempo de trabalho (grupo "+label+")", insightcontract.Window{}, refStr, openIDs, openKnown, coverage, exclusions)
		if !refOK {
			cohort.EligibleCount = nil
		}
		cohorts = append(cohorts, cohort)
		med, p85, quantileOK := quantiles(ages, minSample)
		state, limit := statusObserved, "Idade desde criação, não tempo de trabalho; sem limiar operacional não há classificação de excesso."
		if !refOK {
			state, limit = statusNotCalculable, "AnalysisReferenceAt ausente/inválida; aging não calculável e nenhum fallback temporal foi usado."
		}
		if refOK && !quantileOK {
			state, limit = statusNotCalculable, fmt.Sprintf("Amostra n=%d menor que MinimumQuantileSample=%d; suficiência amostral não é meta de desempenho.", len(ages), minSample)
		}
		if refOK && coverage != coverageComplete {
			limit += " A cobertura do universo de estoque não é completa/verificável."
		}
		var eligible *int64
		if coverage == coverageComplete {
			eligible = int64Ptr(openKnown)
		}
		if !refOK {
			metrics = append(metrics, ageMetric(key+"-median", "Mediana de aging "+label, state, nil, "dias", nil, nil, 0, nil, cohort.ID, "nearest-rank", "Dias corridos entre CreatedAt e AnalysisReferenceAt para itens abertos conhecidos", limit))
			metrics = append(metrics, ageMetric(key+"-p85", "P85 de aging "+label, state, nil, "dias", nil, nil, 0, nil, cohort.ID, "nearest-rank", "P85 em dias corridos entre CreatedAt e AnalysisReferenceAt", limit))
			continue
		}
		metrics = append(metrics, ageMetric(key+"-median", "Mediana de aging "+label, state, floatPtrIf(quantileOK, med), "dias", nil, nil, int64(len(ages)), eligible, cohort.ID, "middle-pair average", "Dias corridos entre CreatedAt e AnalysisReferenceAt para itens abertos conhecidos; mediana é o valor central ou a média dos dois centrais", limit))
		metrics = append(metrics, ageMetric(key+"-p85", "P85 de aging "+label, state, floatPtrIf(quantileOK, p85), "dias", nil, nil, int64(len(ages)), eligible, cohort.ID, "nearest-rank", "P85 em dias corridos entre CreatedAt e AnalysisReferenceAt", limit))
	}
	return cohorts, metrics
}

func ageMetric(id, name, state string, value *float64, unit string, numerator, denominator *int64, sample int64, eligible *int64, cohortID, method, definition, limitation string) insightcontract.Metric {
	return insightcontract.Metric{ID: stableID("metric", id), Name: name, State: state, Value: value, Unit: unit, Numerator: numerator, Denominator: denominator, SampleSize: sample, EligibleSize: eligible, CohortID: cohortID, Method: method, Definition: definition, Limitations: []string{limitation}}
}

func reworkMetrics(bundle insightcontract.SourceBundle, scope insightcontract.Scope, entities map[string]insightcontract.Entity, ref time.Time, refOK bool) (insightcontract.Cohort, []insightcontract.Metric, []insightcontract.FactCase) {
	key := "rework-30d"
	if !refOK {
		cohort := makeCohort(bundle, key, "Entidades observadas para fatos de retrabalho em 30 dias", insightcontract.Window{}, bundle.Snapshot.AnalysisReferenceAt, []string{}, 0, coverageUnknown, []string{"AnalysisReferenceAt ausente/inválida"})
		cohort.EligibleCount = nil
		metric := ageMetric(key, "Ocorrências de retrabalho observadas em 30 dias", statusNotCalculable, nil, "ocorrências", nil, nil, 0, nil, cohort.ID, "event/link count", "Reaberturas e reversões/correções declaradas nos 30 dias anteriores à âncora", "AnalysisReferenceAt ausente/inválida.")
		metrics := []insightcontract.Metric{metric}
		for _, category := range []struct{ key, name string }{{"reopening", "Reaberturas observadas em 30 dias"}, {"declared_reversal", "Reversões declaradas em 30 dias"}, {"declared_correction", "Correções declaradas em 30 dias"}} {
			metrics = append(metrics, ageMetric(key+"-"+category.key, category.name, statusNotCalculable, nil, "ocorrências", nil, nil, 0, nil, cohort.ID, "event/link count", "Contagem da categoria nos 30 dias anteriores à âncora", "AnalysisReferenceAt ausente/inválida."))
		}
		return cohort, metrics, []insightcontract.FactCase{}
	}
	start := ref.Add(-30 * 24 * time.Hour)
	counts := map[string]int64{"reopening": 0, "declared_reversal": 0, "declared_correction": 0}
	seen := map[string]bool{}
	cases := []insightcontract.FactCase{}
	for _, e := range bundle.Events {
		at, ok := parseOptionalInstant(e.EventAt)
		entity, found := entities[e.EntityID]
		kind := reworkEventKind(e)
		if !ok || !inWindow(at, start, ref) || kind == "" || !found || entity.Kind == "component" || !entityInScope(entity, scope) {
			continue
		}
		recordID := e.ID
		if recordID == "" {
			recordID = e.EntityID + ":" + e.Type + ":" + at.UTC().Format(time.RFC3339)
		}
		if seen["event:"+recordID] {
			continue
		}
		seen["event:"+recordID] = true
		counts[kind]++
		cases = append(cases, reworkFactCase(bundle, key, kind, recordID, "", e.EntityID, "", e.EvidenceIDs, at.UTC().Format(time.RFC3339)))
	}
	for _, l := range bundle.Links {
		if l.Status != "explicit" || !linkAsOf(l, ref, true) || (l.Relation != "declares_revert_of" && l.Relation != "declares_fix_of") {
			continue
		}
		at, ok := parseOptionalInstant(l.ValidFrom)
		if !ok || !inWindow(at, start, ref) {
			continue
		}
		kind := "declared_reversal"
		if l.Relation == "declares_fix_of" {
			kind = "declared_correction"
		}
		id := l.ID
		if id == "" {
			id = stableID("link-key", l.FromID, l.ToID, l.Relation, at.UTC().Format(time.RFC3339))
		}
		if seen["link:"+id] {
			continue
		}
		seen["link:"+id] = true
		counts[kind]++
		cases = append(cases, reworkFactCase(bundle, key, kind, id, l.ID, l.FromID, l.ToID, l.EvidenceIDs, at.UTC().Format(time.RFC3339)))
	}
	coverage := requiredCoverageStatus(bundle, entities, start, ref, []string{"history", "links"})
	complete := coverage == coverageComplete
	entityIDs := []string{}
	for _, e := range bundle.Entities {
		if e.Kind != "component" {
			entityIDs = append(entityIDs, e.ID)
		}
	}
	state := statusImmature
	lim := "Zero observado não prova ausência de retrabalho; maturidade exige cobertura contínua verificável de history e links nos 30 dias."
	var eligible *int64
	if complete {
		state = statusObserved
		eligible = int64Ptr(int64(len(entityIDs)))
		lim = "Janela contínua coberta; oportunidade de observação não é avaliação de qualidade."
	} else if coverage == coverageUnknown || coverage == coverageNotSelected || coverage == coverageUnsupported {
		state = statusUnknown
		lim = "Cobertura necessária ausente, não selecionada ou não verificável; zero observado permanece desconhecido."
	}
	cohort := makeCohort(bundle, key, "Entidades observadas para fatos de retrabalho em 30 dias", insightcontract.Window{Start: start.UTC().Format(time.RFC3339), End: ref.UTC().Format(time.RFC3339)}, ref.UTC().Format(time.RFC3339), entityIDs, int64(len(entityIDs)), coverage, nil)
	metrics := []insightcontract.Metric{ageMetric(key, "Ocorrências de retrabalho observadas em 30 dias", state, floatPtr(float64(counts["reopening"]+counts["declared_reversal"]+counts["declared_correction"])), "ocorrências", int64Ptr(counts["reopening"]+counts["declared_reversal"]+counts["declared_correction"]), nil, int64(len(entityIDs)), eligible, cohort.ID, "event/link count", "Quantidade de registros explícitos de retrabalho na janela semiaberta [AnalysisReferenceAt-30d, AnalysisReferenceAt); sample size conta entidades presentes no bundle", lim)}
	for _, category := range []struct{ key, name string }{{"reopening", "Reaberturas observadas em 30 dias"}, {"declared_reversal", "Reversões declaradas em 30 dias"}, {"declared_correction", "Correções declaradas em 30 dias"}} {
		count := counts[category.key]
		metrics = append(metrics, ageMetric(key+"-"+category.key, category.name, state, floatPtr(float64(count)), "ocorrências", int64Ptr(count), nil, int64(len(entityIDs)), eligible, cohort.ID, "event/link count", "Contagem da categoria na janela semiaberta [AnalysisReferenceAt-30d, AnalysisReferenceAt); sample size conta entidades presentes no bundle", lim))
	}
	return cohort, metrics, cases
}

func reworkFactCase(bundle insightcontract.SourceBundle, metricKey, category, recordID, linkID, subjectID, relatedID string, evidenceIDs []string, at string) insightcontract.FactCase {
	related := []string{}
	if relatedID != "" {
		related = append(related, relatedID)
	}
	linkIDs := []string{}
	if linkID != "" {
		linkIDs = append(linkIDs, linkID)
	}
	return insightcontract.FactCase{ID: stableID("case-rework", bundle.Snapshot.ID, category, recordID), Kind: "rework", Classification: "unclassified", Title: "Registro de retrabalho: " + category, SubjectIDs: []string{subjectID}, RelatedIDs: related, LinkIDs: linkIDs, ObjectiveIDs: []string{}, EvidenceIDs: uniqueSorted(evidenceIDs), MetricIDs: []string{stableID("metric", metricKey+"-"+category), stableID("metric", metricKey)}, LegacyFindingIDs: []string{}, ObservedFacts: []string{"Registro " + recordID + " observado em " + at + "; o tipo de ocorrência foi classificado pelo evento/vínculo explícito."}, OpenQuestions: []string{"A ocorrência não comprova defeito, causa ou responsabilidade."}}
}

func requiredCoverageStatus(bundle insightcontract.SourceBundle, entities map[string]insightcontract.Entity, start, end time.Time, collections []string) string {
	resources := map[string]bool{}
	for _, e := range entities {
		if e.Source != "" && e.ResourceID != "" {
			resources[e.Source+"\x00"+e.ResourceID] = true
		}
	}
	if len(resources) == 0 {
		return coverageUnknown
	}
	result := coverageComplete
	for r := range resources {
		parts := strings.SplitN(r, "\x00", 2)
		for _, collection := range collections {
			found := false
			allComplete := true
			intervals := []insightcontract.Window{}
			for _, c := range bundle.Coverage {
				if c.Key.Source != parts[0] || c.Key.ResourceID != parts[1] || c.Key.Collection != collection {
					continue
				}
				found = true
				if c.Status == "not_selected" {
					return coverageNotSelected
				}
				if c.Status == "unsupported" {
					return coverageUnsupported
				}
				if c.Status == "unknown" {
					return coverageUnknown
				}
				if c.Status != "complete" {
					allComplete = false
				}
				intervals = append(intervals, c.HistoryCoveredIntervals...)
			}
			if !found {
				return coverageUnknown
			}
			if !allComplete || !intervalsCover(intervals, start, end) {
				result = coveragePartial
			}
		}
	}
	return result
}

func dependencyCases(bundle insightcontract.SourceBundle, objectives []insightcontract.Objective, scope insightcontract.Scope, entities map[string]insightcontract.Entity, ref time.Time, refOK bool) []insightcontract.FactCase {
	linksByPair := map[linkPair][]insightcontract.Link{}
	for _, l := range bundle.Links {
		if l.Relation != "blocked_by" || l.Status != "explicit" || !linkAsOf(l, ref, refOK) {
			continue
		}
		linksByPair[linkPair{l.FromID, l.ToID}] = append(linksByPair[linkPair{l.FromID, l.ToID}], l)
	}
	pairs := make([]linkPair, 0, len(linksByPair))
	for p := range linksByPair {
		pairs = append(pairs, p)
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].from != pairs[j].from {
			return pairs[i].from < pairs[j].from
		}
		return pairs[i].to < pairs[j].to
	})
	legacyByPair := map[string][]string{}
	matchedLegacy := map[string]bool{}
	for _, f := range bundle.LegacyFindings {
		if f.RuleID == "R-BLOCK-001" {
			legacyByPair[f.SubjectID] = append(legacyByPair[f.SubjectID], f.ID)
		}
	}
	linkCoverage := populationCoverage(bundle, scope, []string{"links"})
	result := make([]insightcontract.FactCase, 0, len(pairs))
	for _, p := range pairs {
		from, fromOK := entities[p.from]
		to, toOK := entities[p.to]
		classification := "unclassified"
		questions := []string{}
		if !fromOK || !toOK || from.StateTemporalStatus != "known_at_reference" || to.StateTemporalStatus != "known_at_reference" {
			questions = append(questions, "estado temporal de uma ou ambas as entidades desconhecido")
		}
		if linkCoverage != coverageComplete {
			questions = append(questions, "cobertura de links incompleta/desconhecida; a lista observada não representa necessariamente todas as dependências")
		}
		if (fromOK && from.StateTemporalStatus == "known_at_reference" && isQueueState(from.State)) || (toOK && to.StateTemporalStatus == "known_at_reference" && isQueueState(to.State)) {
			classification = "planned_dependency"
		} else if fromOK && toOK && from.StateTemporalStatus == "known_at_reference" && to.StateTemporalStatus == "known_at_reference" && isOpenState(from.State) && isOpenState(to.State) {
			classification = "unclassified"
		}
		linkIDs, evidenceIDs := []string{}, []string{}
		for _, l := range linksByPair[p] {
			linkIDs = append(linkIDs, l.ID)
			evidenceIDs = append(evidenceIDs, l.EvidenceIDs...)
		}
		findingIDs := []string{}
		for _, id := range legacyByPair[p.from] {
			findingIDs = append(findingIDs, id)
			matchedLegacy[id] = true
		}
		observed := []string{fmt.Sprintf("Par distinto %s blocked_by %s; links observados=%d.", p.from, p.to, len(linksByPair[p]))}
		if fromOK {
			evidenceIDs = append(evidenceIDs, from.EvidenceIDs...)
		}
		if toOK {
			evidenceIDs = append(evidenceIDs, to.EvidenceIDs...)
		}
		if classification == "planned_dependency" {
			observed = append(observed, "A relação envolve backlog/triage e é classificada como fila, não impedimento confirmado.")
		}
		if classification == "unclassified" && fromOK && toOK && isOpenState(from.State) && isOpenState(to.State) {
			observed = append(observed, "Dependência observada entre itens ativos; não comprova duração, causa ou dano.")
			questions = append(questions, "não há compromisso explícito vencido associado; sem critério para declarar ameaça")
		}
		if fromOK && from.OwnerID == nil {
			questions = append(questions, "owner registrado ausente; responsabilidade desconhecida")
		}
		if len(legacyByPair[p.from]) > 0 && subjectPairCount(p.from, pairs) > 1 {
			questions = append(questions, "finding legado associado pelo sujeito, mas há vários pares atuais e o contrato legado não identifica o alvo")
		}
		result = append(result, insightcontract.FactCase{ID: stableID("case-dependency", bundle.Snapshot.ID, p.from, p.to), Kind: "dependency", Classification: classification, Title: "Dependência observada: " + p.from + " → " + p.to,
			SubjectIDs: []string{p.from}, RelatedIDs: []string{p.to}, LinkIDs: uniqueSorted(linkIDs), EvidenceIDs: uniqueSorted(evidenceIDs), LegacyFindingIDs: uniqueSorted(findingIDs),
			ObjectiveIDs: []string{}, MetricIDs: []string{}, ObservedFacts: observed, OpenQuestions: questions})
		if refOK && fromOK && toOK && from.StateTemporalStatus == "known_at_reference" && to.StateTemporalStatus == "known_at_reference" && isOpenState(from.State) && isOpenState(to.State) {
			objectiveIDs, deadline, owners, commitmentEvidence := expiredCommitments(objectives, from, ref)
			if deadline != "" {
				facts := []string{"Dependência observada ligada ao prazo registrado " + deadline + ", vencido na âncora " + ref.UTC().Format(time.RFC3339) + ".", "O prazo é um critério registrado; o vínculo não prova causa do atraso."}
				if len(owners) > 0 {
					facts = append(facts, "Owner registrado: "+strings.Join(owners, ", ")+"; isso não atribui responsabilidade causal.")
				} else {
					questions = append(questions, "owner explícito ausente; responsabilidade desconhecida")
				}
				result = append(result, insightcontract.FactCase{ID: stableID("case-commitment-at-risk", bundle.Snapshot.ID, p.from, p.to, deadline), Kind: "dependency", Classification: "commitment_at_risk", Title: "Compromisso vencido com dependência observada",
					SubjectIDs: []string{p.from}, RelatedIDs: []string{p.to}, LinkIDs: uniqueSorted(linkIDs), ObjectiveIDs: objectiveIDs, EvidenceIDs: uniqueSorted(append(evidenceIDs, commitmentEvidence...)), LegacyFindingIDs: []string{}, MetricIDs: []string{}, ObservedFacts: facts, OpenQuestions: nonNilStrings(questions)})
			}
		}
	}
	result = append(result, dependencyDiagnosticCases(bundle, ref, refOK)...)
	unmatched := map[string][]string{}
	for _, finding := range bundle.LegacyFindings {
		if finding.RuleID == "R-BLOCK-001" && !matchedLegacy[finding.ID] {
			unmatched[finding.SubjectID] = append(unmatched[finding.SubjectID], finding.ID)
		}
	}
	for _, subject := range sortedKeysMap(unmatched) {
		result = append(result, insightcontract.FactCase{ID: stableID("case-legacy-unmatched", bundle.Snapshot.ID, subject), Kind: "dependency", Classification: "unclassified", Title: "Achado legado sem par de dependência atual", SubjectIDs: []string{subject}, RelatedIDs: []string{}, LinkIDs: []string{}, ObjectiveIDs: []string{}, EvidenceIDs: []string{}, MetricIDs: []string{}, LegacyFindingIDs: uniqueSorted(unmatched[subject]), ObservedFacts: []string{"Achados legados R-BLOCK-001 não foram recalculados; não há par explícito elegível correspondente no bundle atual."}, OpenQuestions: []string{"A divergência pode decorrer de escopo, cobertura ou diferença entre as regras; não indica por si só falso positivo."}})
	}
	return result
}

func dependencyDiagnosticCases(bundle insightcontract.SourceBundle, ref time.Time, refOK bool) []insightcontract.FactCase {
	byPair := map[string][]insightcontract.Link{}
	factsByPair, questionsByPair := map[string][]string{}, map[string][]string{}
	for _, link := range bundle.Links {
		if link.Relation != "blocked_by" || (link.Status == "explicit" && linkAsOf(link, ref, refOK)) {
			continue
		}
		key := link.FromID + "\x00" + link.ToID
		reason, question := "", ""
		switch {
		case link.Status != "explicit":
			reason, question = "O registro blocked_by não tem status explicit.", "Confirme a relação na fonte; ela não entrou no agrupamento elegível."
		case !refOK:
			reason, question = "AnalysisReferenceAt ausente/inválida; a relação não pode ser cortada temporalmente.", "Temporalidade da relação permanece desconhecida."
		case link.TemporalStatus != "observed_at_reference" && link.TemporalStatus != "known_interval":
			reason, question = "TemporalStatus não permite confirmar a relação no corte.", "Histórico/temporalidade insuficiente para incluir a relação no as-of."
		case link.TemporalStatus == "known_interval" && (link.ValidFrom == nil || link.ValidTo == nil):
			reason, question = "Intervalo de validade incompleto; a relação não foi incluída no as-of.", "ValidFrom e ValidTo precisam ser conhecidos para reconstruir o intervalo."
		case (link.TemporalStatus == "known_interval" || link.TemporalStatus == "observed_at_reference") && outsideKnownInterval(link, ref):
			reason, question = "Relação explicitamente registrada fora do seu intervalo de validade na referência.", "Este registro não foi contado como dependência vigente no corte."
		default:
			reason, question = "Validade temporal inválida ou ambígua; a relação não foi incluída.", "Verifique timestamps e validade na fonte."
		}
		byPair[key] = append(byPair[key], link)
		factsByPair[key] = appendUnique(factsByPair[key], reason)
		questionsByPair[key] = appendUnique(questionsByPair[key], question)
	}
	keys := sortedKeysMap(byPair)
	result := make([]insightcontract.FactCase, 0, len(keys))
	for _, key := range keys {
		parts := strings.SplitN(key, "\x00", 2)
		links, linkIDs, evidence := byPair[key], []string{}, []string{}
		for _, link := range links {
			linkIDs = append(linkIDs, link.ID)
			evidence = append(evidence, link.EvidenceIDs...)
		}
		observed := append([]string{}, factsByPair[key]...)
		observed = append(observed, fmt.Sprintf("Registros associados ao par: %d.", len(links)))
		result = append(result, insightcontract.FactCase{ID: stableID("case-dependency-diagnostic", bundle.Snapshot.ID, key), Kind: "dependency", Classification: "unclassified", Title: "Relação blocked_by fora do conjunto elegível", SubjectIDs: []string{parts[0]}, RelatedIDs: []string{parts[1]}, LinkIDs: uniqueSorted(linkIDs), ObjectiveIDs: []string{}, EvidenceIDs: uniqueSorted(evidence), MetricIDs: []string{}, LegacyFindingIDs: []string{}, ObservedFacts: observed, OpenQuestions: questionsByPair[key]})
	}
	return result
}

func outsideKnownInterval(link insightcontract.Link, ref time.Time) bool {
	from, fromOK := parseOptionalInstant(link.ValidFrom)
	to, toOK := parseOptionalInstant(link.ValidTo)
	return fromOK && toOK && (ref.Before(from) || !ref.Before(to))
}

func subjectPairCount(subject string, pairs []linkPair) int {
	count := 0
	for _, pair := range pairs {
		if pair.from == subject {
			count++
		}
	}
	return count
}

func dependencySummary(bundle insightcontract.SourceBundle, scope insightcontract.Scope, ref time.Time, refOK bool) (insightcontract.Cohort, []insightcontract.Metric) {
	pairs, subjects := map[linkPair]bool{}, map[string]bool{}
	var rawLinks, legacyFindings int64
	for _, link := range bundle.Links {
		if link.Relation != "blocked_by" || link.Status != "explicit" || !linkAsOf(link, ref, refOK) {
			continue
		}
		rawLinks++
		pairs[linkPair{link.FromID, link.ToID}] = true
		subjects[link.FromID] = true
	}
	for _, finding := range bundle.LegacyFindings {
		if finding.RuleID == "R-BLOCK-001" {
			legacyFindings++
		}
	}
	ids := sortedKeysMap(subjects)
	coverage := populationCoverage(bundle, scope, []string{"links"})
	state := statusObserved
	if !refOK {
		state = statusUnknown
		coverage = coverageUnknown
	}
	cohort := makeCohort(bundle, "dependency-observations", "Sujeitos distintos com relação blocked_by explicitamente observada no corte", insightcontract.Window{}, bundle.Snapshot.AnalysisReferenceAt, ids, int64(len(ids)), coverage, nil)
	linkCount, pairCount, subjectCount := rawLinks, int64(len(pairs)), int64(len(subjects))
	metrics := []insightcontract.Metric{
		countMetric(bundle, cohort.ID, "blocked-by-raw-links", "Links blocked_by explícitos", linkCount, state, "links", rawLinks, "Contagem bruta de registros de link blocked_by elegíveis no as-of; links duplicados continuam na contagem."),
		countMetric(bundle, cohort.ID, "blocked-by-distinct-pairs", "Pares blocked_by distintos", pairCount, state, "pares", pairCount, "Pares distintos (FromID, ToID) com relação blocked_by elegível no as-of."),
		countMetric(bundle, cohort.ID, "blocked-by-distinct-subjects", "Sujeitos distintos com blocked_by", subjectCount, state, "sujeitos", subjectCount, "Contagem de FromID distintos em relações blocked_by explícitas e elegíveis."),
		countMetric(bundle, cohort.ID, "blocked-by-legacy-findings", "Findings legados R-BLOCK-001", legacyFindings, statusObserved, "findings", legacyFindings, "Registros legados com RuleID R-BLOCK-001; comparação não recalcula a regra."),
	}
	return cohort, metrics
}

func countMetric(bundle insightcontract.SourceBundle, cohortID, key, name string, value int64, state, unit string, sample int64, definition string) insightcontract.Metric {
	var numeric *float64
	var numerator *int64
	if state != statusUnknown {
		numeric = floatPtr(float64(value))
		numerator = int64Ptr(value)
	}
	return insightcontract.Metric{ID: stableID("metric", bundle.Snapshot.ID, key), Name: name, State: state, Value: numeric, Unit: unit, Numerator: numerator, Denominator: nil, SampleSize: sample, EligibleSize: nil, CohortID: cohortID, Method: "deterministic count", Definition: definition, Limitations: []string{"Cobertura desconhecida/parcial não permite inferir universo; contagem não representa impacto ou urgência."}}
}

func expiredCommitments(objectives []insightcontract.Objective, entity insightcontract.Entity, ref time.Time) ([]string, string, []string, []string) {
	deadline, owner := "", ""
	if entity.DueAt != nil {
		if at, ok := parseInstant(*entity.DueAt); ok && !at.After(ref) {
			deadline = at.UTC().Format(time.RFC3339)
		}
	}
	if entity.OwnerID != nil {
		owner = *entity.OwnerID
	}
	objectiveIDs, owners, evidenceIDs := []string{}, []string{}, []string{}
	for _, objective := range objectives {
		if !contains(objective.EntityIDs, entity.ID) || objective.Deadline == nil {
			continue
		}
		at, ok := parseInstant(*objective.Deadline)
		if !ok || at.After(ref) {
			continue
		}
		objectiveIDs = append(objectiveIDs, objective.ID)
		evidenceIDs = append(evidenceIDs, objective.EvidenceIDs...)
		if deadline == "" || at.Before(parseInstantMust(deadline)) {
			deadline = at.UTC().Format(time.RFC3339)
		}
		if objective.OwnerID != nil {
			owners = append(owners, *objective.OwnerID)
		}
	}
	if owner != "" {
		owners = append(owners, owner)
	}
	return uniqueSorted(objectiveIDs), deadline, uniqueSorted(owners), uniqueSorted(evidenceIDs)
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
func parseInstantMust(value string) time.Time { parsed, _ := parseInstant(value); return parsed }

func linkAsOf(l insightcontract.Link, ref time.Time, refOK bool) bool {
	if !refOK {
		return false
	}
	if l.TemporalStatus != "observed_at_reference" && l.TemporalStatus != "known_interval" {
		return false
	}
	if l.ValidFrom != nil {
		from, ok := parseOptionalInstant(l.ValidFrom)
		if !ok || ref.Before(from) {
			return false
		}
	}
	if l.ValidTo != nil {
		to, ok := parseOptionalInstant(l.ValidTo)
		if !ok || !ref.Before(to) {
			return false
		}
	}
	if l.TemporalStatus == "known_interval" && (l.ValidFrom == nil || l.ValidTo == nil) {
		return false
	}
	return true
}

func intervalsCover(intervals []insightcontract.Window, start, end time.Time) bool {
	type span struct{ start, end time.Time }
	spans := []span{}
	for _, w := range intervals {
		a, b, ok := parseWindow(w)
		if ok && b.After(a) {
			spans = append(spans, span{a, b})
		}
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].start.Before(spans[j].start) })
	cursor := start
	for _, s := range spans {
		if s.end.Before(cursor) || s.end.Equal(cursor) {
			continue
		}
		if s.start.After(cursor) {
			return false
		}
		if s.end.After(cursor) {
			cursor = s.end
		}
		if !cursor.Before(end) {
			return true
		}
	}
	return false
}

func quantiles(values []float64, minimum int) (float64, float64, bool) {
	if len(values) < minimum || len(values) == 0 {
		return 0, 0, false
	}
	sorted := append([]float64{}, values...)
	sort.Float64s(sorted)
	median := sorted[len(sorted)/2]
	if len(sorted)%2 == 0 {
		median = (sorted[len(sorted)/2-1] + sorted[len(sorted)/2]) / 2
	}
	return median, sorted[int(math.Ceil(.85*float64(len(sorted))))-1], true
}

func parseWindow(w insightcontract.Window) (time.Time, time.Time, bool) {
	a, oka := parseInstant(w.Start)
	b, okb := parseInstant(w.End)
	return a, b, oka && okb && a.Before(b)
}
func parseInstant(s string) (time.Time, bool) {
	if strings.TrimSpace(s) == "" {
		return time.Time{}, false
	}
	t, e := time.Parse(time.RFC3339Nano, s)
	return t, e == nil
}
func parseOptionalInstant(s *string) (time.Time, bool) {
	if s == nil {
		return time.Time{}, false
	}
	return parseInstant(*s)
}
func inWindow(t, a, b time.Time) bool { return !t.Before(a) && t.Before(b) }
func entityInScope(e insightcontract.Entity, scope insightcontract.Scope) bool {
	if len(scope.Resources) == 0 {
		return true
	}
	for _, r := range scope.Resources {
		if e.Source == r.Source && e.ResourceID == r.ID {
			return true
		}
	}
	return false
}
func isCompletedState(s string) bool {
	switch s {
	case "done", "completed", "closed":
		return true
	}
	return false
}
func isOpenState(s string) bool {
	switch s {
	case "open", "todo", "in_progress", "started", "reopened", "unstarted", "backlog", "triage":
		return true
	}
	return false
}
func isQueueState(s string) bool { return s == "backlog" || s == "triage" }
func isCompletionTransition(e insightcontract.Event) bool {
	if e.NewValue == nil || !isCompletedState(normalizeState(*e.NewValue)) {
		return false
	}
	if e.OldValue != nil && isCompletedState(normalizeState(*e.OldValue)) {
		return false
	}
	return e.Type == "state_changed" || e.Type == "status_changed" || e.Type == "completed"
}
func normalizeState(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
func reworkEventKind(e insightcontract.Event) string {
	if e.Type == "commit_revert_declared" {
		return "declared_reversal"
	}
	if e.Type == "issue_reopened" || e.Type == "reopened" {
		return "reopening"
	}
	if e.Type == "correction_declared" || e.Type == "fix_declared" {
		return "declared_correction"
	}
	if e.OldValue != nil && e.NewValue != nil && isCompletedState(normalizeState(*e.OldValue)) && isOpenState(normalizeState(*e.NewValue)) {
		return "reopening"
	}
	return ""
}
func stableID(parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return strings.Join(parts[:1], "") + "-" + hex.EncodeToString(h[:12])
}
func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
func sortedKeysMap[T any](m map[string]T) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
func uniqueSorted(in []string) []string {
	set := map[string]bool{}
	for _, s := range in {
		if s != "" {
			set[s] = true
		}
	}
	return sortedKeys(set)
}
func appendUnique(in []string, value string) []string {
	for _, v := range in {
		if v == value {
			return in
		}
	}
	return append(in, value)
}
func nonNilStrings(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}
func indexEntities(in []insightcontract.Entity) map[string]insightcontract.Entity {
	out := make(map[string]insightcontract.Entity, len(in))
	for _, e := range in {
		out[e.ID] = e
	}
	return out
}
func int64Ptr(v int64) *int64     { return &v }
func floatPtr(v float64) *float64 { return &v }
func floatPtrIf(ok bool, v float64) *float64 {
	if !ok {
		return nil
	}
	return &v
}
