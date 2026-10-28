// Package presentation maps frozen insight outputs into the compact report
// consumed by the executive UI and exporter. It deliberately contains no
// metric calculation or semantic judgment.
package presentation

import (
	"errors"
	"fmt"
	"strings"

	"pocketcli/internal/investigation/insightcontract"
)

// Build combines source, deterministic facts, and the completed analysis into
// a report without changing their meaning. References are validated before
// the report is exposed to readers.
func Build(source insightcontract.SourceBundle, facts insightcontract.FactsResult, decisions insightcontract.DecisionAnalysis, cfg insightcontract.AnalysisConfig) (insightcontract.ExecutiveReport, error) {
	if source.Snapshot.ID == "" || facts.SnapshotID != source.Snapshot.ID || decisions.SnapshotID != source.Snapshot.ID {
		return insightcontract.ExecutiveReport{}, errors.New("insight snapshot references do not match")
	}
	if decisions.ID == "" || decisions.Version != insightcontract.Version {
		return insightcontract.ExecutiveReport{}, errors.New("decision analysis is missing a valid identity or version")
	}
	if source.Version != insightcontract.Version || facts.Version != insightcontract.Version {
		return insightcontract.ExecutiveReport{}, errors.New("insight contract version mismatch")
	}
	if !oneOf(decisions.Status, "complete", "partial", "not_run", "failed") {
		return insightcontract.ExecutiveReport{}, fmt.Errorf("unknown analysis status %q", decisions.Status)
	}
	for _, q := range source.Questions {
		if !oneOf(q.Status, "sufficient", "partial", "insufficient", "not_applicable") {
			return insightcontract.ExecutiveReport{}, fmt.Errorf("unknown question status %q", q.Status)
		}
	}
	for _, d := range decisions.Dimensions {
		if !oneOf(d.Status, "on_track", "at_risk", "off_track", "indeterminate", "not_applicable") {
			return insightcontract.ExecutiveReport{}, fmt.Errorf("unknown dimension status %q", d.Status)
		}
	}
	if err := validateReferences(source, facts, decisions, cfg); err != nil {
		return insightcontract.ExecutiveReport{}, err
	}

	questions := append([]insightcontract.QuestionCoverage(nil), source.Questions...)
	metrics := append([]insightcontract.Metric(nil), facts.Metrics...)
	dimensions := append([]insightcontract.DimensionStatus(nil), decisions.Dimensions...)
	cases := append([]insightcontract.DecisionCase(nil), decisions.Cases...)
	caseIDs := make([]string, len(cases))
	for i := range cases {
		caseIDs[i] = cases[i].ID
	}
	return insightcontract.ExecutiveReport{
		Version: insightcontract.Version, SnapshotID: source.Snapshot.ID, AnalysisID: decisions.ID,
		PublicationStatus: "ready", AnalysisStatus: decisions.Status,
		SufficiencyStatus: sufficiency(source, decisions),
		Headline:          headline(cases), Summary: "Síntese da análise vinculada ao snapshot; consulte dimensões, casos e limitações antes de concluir.",
		RequestedScope: source.RequestedScope, Questions: questions, Dimensions: dimensions,
		Metrics: metrics, Cases: cases, TotalCases: int64(len(decisions.Cases)), CaseIDs: caseIDs,
		NextActions: []string{}, Limitations: mergeStrings(source.Limitations, facts.Limitations, decisions.Limitations),
	}, nil
}

func sufficiency(source insightcontract.SourceBundle, decisions insightcontract.DecisionAnalysis) string {
	// This is a conservative roll-up of statuses already supplied by L1/L3:
	// positive business outcomes do not influence evidence sufficiency.
	if decisions.Status == "not_run" || decisions.Status == "failed" {
		return "insufficient"
	}
	applicable, supported := false, false
	gap := false
	for _, q := range source.Questions {
		if q.Status != "not_applicable" {
			applicable = true
		}
		if q.Status == "partial" || q.Status == "insufficient" {
			gap = true
		}
	}
	for _, d := range decisions.Dimensions {
		if d.Status != "not_applicable" {
			applicable = true
		}
		if d.Status == "indeterminate" {
			gap = true
		} else if d.Status == "on_track" || d.Status == "at_risk" || d.Status == "off_track" {
			supported = true
		}
	}
	if !applicable {
		return "not_applicable"
	}
	if decisions.Status == "partial" || gap {
		if !supported {
			return "insufficient"
		}
		return "partial"
	}
	return "sufficient"
}

func headline(cases []insightcontract.DecisionCase) string {
	if len(cases) == 0 {
		return "Nenhum caso executivo selecionado"
	}
	return fmt.Sprintf("%d caso(s) para leitura executiva", len(cases))
}

func mergeStrings(groups ...[]string) []string {
	seen := map[string]bool{}
	out := make([]string, 0)
	for _, group := range groups {
		for _, value := range group {
			value = strings.TrimSpace(value)
			if value != "" && !seen[value] {
				seen[value] = true
				out = append(out, value)
			}
		}
	}
	return out
}

func oneOf(value string, allowed ...string) bool {
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
}

func validateReferences(source insightcontract.SourceBundle, facts insightcontract.FactsResult, decisions insightcontract.DecisionAnalysis, cfg insightcontract.AnalysisConfig) error {
	entities, evidence, factCases, metrics, claims, decisionIDs, caseIDs := map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, v := range source.Entities {
		entities[v.ID] = true
	}
	for _, v := range source.Evidence {
		evidence[v.ID] = true
	}
	evidenceByID := make(map[string]insightcontract.EvidenceView, len(source.Evidence))
	for _, v := range source.Evidence {
		evidenceByID[v.ID] = v
	}
	for _, v := range facts.Cases {
		factCases[v.ID] = true
	}
	for _, v := range facts.Metrics {
		metrics[v.ID] = true
	}
	for _, v := range decisions.Claims {
		claims[v.ID] = true
	}
	for _, v := range decisions.Decisions {
		decisionIDs[v.ID] = true
	}
	for _, v := range decisions.Cases {
		caseIDs[v.ID] = true
	}
	objectives := map[string]bool{}
	for _, v := range cfg.Objectives {
		objectives[v.ID] = true
	}
	check := func(kind, id string, pool map[string]bool) error {
		if id != "" && !pool[id] {
			return fmt.Errorf("%s reference %q is missing", kind, id)
		}
		return nil
	}
	checkCitation := func(kind string, citation insightcontract.Citation) error {
		if err := check(kind, citation.EvidenceID, evidence); err != nil {
			return err
		}
		ev, ok := evidenceByID[citation.EvidenceID]
		if !ok {
			return nil
		}
		if ev.IntegrityStatus == "mismatch" || ev.IntegrityStatus == "missing" {
			return fmt.Errorf("%s cites untrusted evidence %q", kind, citation.EvidenceID)
		}
		if citation.Quote != "" && !strings.Contains(ev.Excerpt, citation.Quote) {
			return fmt.Errorf("%s quote is not present in evidence %q", kind, citation.EvidenceID)
		}
		return nil
	}
	for _, o := range cfg.Objectives {
		for _, id := range o.EntityIDs {
			if err := check("objective entity", id, entities); err != nil {
				return err
			}
		}
		for _, id := range o.EvidenceIDs {
			if err := check("objective evidence", id, evidence); err != nil {
				return err
			}
		}
	}
	for _, f := range facts.Cases {
		for _, id := range f.SubjectIDs {
			if err := check("fact entity", id, entities); err != nil {
				return err
			}
		}
		for _, id := range f.RelatedIDs {
			if err := check("related entity", id, entities); err != nil {
				return err
			}
		}
		for _, id := range f.EvidenceIDs {
			if err := check("fact evidence", id, evidence); err != nil {
				return err
			}
		}
		for _, id := range f.MetricIDs {
			if err := check("fact metric", id, metrics); err != nil {
				return err
			}
		}
	}
	for _, o := range cfg.Objectives {
		if err := check("objective metric", o.MetricID, metrics); err != nil {
			return err
		}
	}
	for _, c := range decisions.Claims {
		for _, id := range c.EntityIDs {
			if err := check("claim entity", id, entities); err != nil {
				return err
			}
		}
		for _, citation := range c.Citations {
			if err := checkCitation("claim evidence", citation); err != nil {
				return err
			}
		}
	}
	for _, d := range decisions.Decisions {
		for _, id := range d.EntityIDs {
			if err := check("decision entity", id, entities); err != nil {
				return err
			}
		}
		for _, id := range d.ClaimIDs {
			if err := check("decision claim", id, claims); err != nil {
				return err
			}
		}
		for _, citation := range d.Citations {
			if err := checkCitation("decision evidence", citation); err != nil {
				return err
			}
		}
	}
	claimRelations := map[string]bool{}
	decisionRelations := map[string]bool{}
	for _, r := range decisions.ClaimRelations {
		claimRelations[r.ID] = true
	}
	for _, r := range decisions.Relations {
		decisionRelations[r.ID] = true
	}
	for _, r := range decisions.ClaimRelations {
		if err := check("claim relation source", r.FromClaimID, claims); err != nil {
			return err
		}
		if err := check("claim relation target", r.ToClaimID, claims); err != nil {
			return err
		}
		for _, citation := range r.Citations {
			if err := checkCitation("claim relation evidence", citation); err != nil {
				return err
			}
		}
	}
	for _, r := range decisions.Relations {
		if err := check("decision relation source", r.FromDecisionID, decisionIDs); err != nil {
			return err
		}
		if err := check("decision relation target", r.ToDecisionID, decisionIDs); err != nil {
			return err
		}
		for _, citation := range r.Citations {
			if err := checkCitation("decision relation evidence", citation); err != nil {
				return err
			}
		}
	}
	for _, c := range decisions.Cases {
		for _, id := range c.FactCaseIDs {
			if err := check("case fact", id, factCases); err != nil {
				return err
			}
		}
		for _, id := range c.DecisionIDs {
			if err := check("case decision", id, decisionIDs); err != nil {
				return err
			}
		}
		for _, id := range c.ClaimIDs {
			if err := check("case claim", id, claims); err != nil {
				return err
			}
		}
		for _, id := range c.RelationIDs {
			if !claimRelations[id] && !decisionRelations[id] {
				return fmt.Errorf("case relation reference %q is missing", id)
			}
		}
		for _, id := range c.ObjectiveIDs {
			if err := check("case objective", id, objectives); err != nil {
				return err
			}
		}
		for _, id := range c.EntityIDs {
			if err := check("case entity", id, entities); err != nil {
				return err
			}
		}
		for _, citation := range c.Citations {
			if err := checkCitation("case evidence", citation); err != nil {
				return err
			}
		}
		for _, a := range c.Alternatives {
			for _, id := range a.EvidenceIDs {
				if err := check("alternative evidence", id, evidence); err != nil {
					return err
				}
			}
		}
		if c.RecommendedAlternativeID != nil {
			found := false
			for _, a := range c.Alternatives {
				if a.ID == *c.RecommendedAlternativeID {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("recommended alternative %q is missing", *c.RecommendedAlternativeID)
			}
		}
	}
	for _, d := range decisions.Dimensions {
		for _, id := range d.MetricIDs {
			if err := check("dimension metric", id, metrics); err != nil {
				return err
			}
		}
		for _, id := range d.CaseIDs {
			if err := check("dimension case", id, caseIDs); err != nil {
				return err
			}
		}
		if err := check("dimension objective", d.ObjectiveID, objectives); err != nil {
			return err
		}
	}
	return nil
}

// MetricValue formats missing values explicitly and preserves the source unit.
func MetricValue(metric insightcontract.Metric) string {
	if metric.State != "observed" || metric.Value == nil {
		return "não determinado"
	}
	value := fmt.Sprintf("%g", *metric.Value)
	unit := strings.TrimSpace(metric.Unit)
	if unit == "" {
		return value
	}
	return value + " " + unit
}
