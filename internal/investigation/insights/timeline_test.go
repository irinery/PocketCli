package insights

import (
	"testing"

	"pocketcli/internal/investigation/insightcontract"
)

func TestCaseTimelineSeparatesDecisionResultObservationAndUnknownDate(t *testing.T) {
	known := "2026-10-20T12:00:00Z"
	unknown := "not-a-date"
	source := insightcontract.SourceBundle{
		Entities:   []insightcontract.Entity{{ID: "issue-1", Source: "github"}, {ID: "issue-2", Source: "github"}},
		Events:     []insightcontract.Event{{ID: "observed", EntityID: "issue-1", Type: "state_changed", EventAt: &known, ObservedAt: "2026-10-21T00:00:00Z", EvidenceIDs: []string{"ev-1"}}, {ID: "other", EntityID: "issue-2", Type: "closed"}},
		Stages:     []insightcontract.Stage{{ID: "unknown-stage", EntityID: "issue-1", Name: "Backlog", Start: &unknown}},
		Deliveries: []insightcontract.DeliveryObservation{{ID: "result", EntityID: "issue-1", Environment: "prod", Status: "deployed", EventAt: &known, EvidenceIDs: []string{"ev-2"}}},
	}
	decisions := []insightcontract.DecisionRecord{{ID: "decision", Question: "Q", Choice: "A", DecidedAt: &known, Citations: []insightcontract.Citation{{EvidenceID: "ev-3"}}}}
	items := caseTimeline(source, decisions, map[string]bool{"issue-1": true})
	if len(items) != 4 || items[0].Kind != "decision" || items[1].Kind != "observation" || items[2].Kind != "result" || items[3].Kind != "stage" {
		t.Fatalf("timeline types/order/scope: %+v", items)
	}
	if items[1].Source != "github" || items[1].ObservedAt == nil || items[2].Scope != "prod" || items[3].At == nil || *items[3].At != unknown {
		t.Fatalf("timeline provenance or unknown date lost: %+v", items)
	}
}
