package web

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"pocketcli/internal/investigation/insightcontract"
	"pocketcli/internal/investigation/web/mock"
)

type timelineFake struct {
	*insightFake
	calls      int
	wrongScope bool
}

func (f *timelineFake) ListDecisionTimeline(_ context.Context, snapshot, analysis, cursor string, limit int) (insightcontract.TimelinePage, error) {
	f.calls++
	if f.wrongScope {
		snapshot = "another-snapshot"
	}
	return insightcontract.TimelinePage{SnapshotID: snapshot, AnalysisID: analysis, Total: 1, Items: []insightcontract.TimelineItem{{ID: "change", RationaleBasis: "hypothesis", Confidence: "low"}}}, nil
}

func TestDecisionTimelineRouteRequiresOwnedSnapshotAndListedAnalysis(t *testing.T) {
	fake := &timelineFake{insightFake: &insightFake{}}
	client := newUnlockedClient(t, &scopeCore{Core: mock.New()}, nil, PreviewOptions{InvestigationID: "investigation-demo", InsightReader: fake, AnalysisController: fake})
	path := "/snapshots/snap-demo-001/insights/analysis-1/timeline"
	if got := client.do(http.MethodGet, path, "", ""); got.Code != http.StatusNotFound || fake.calls != 0 {
		t.Fatal("unlisted analysis reached timeline")
	}
	if got := client.do(http.MethodGet, "/snapshots/snap-demo-001/insights", "", ""); got.Code != http.StatusOK {
		t.Fatal(got.Body.String())
	}
	if got := client.do(http.MethodGet, path, "", ""); got.Code != http.StatusOK || !strings.Contains(got.Body.String(), `"rationale_basis":"hypothesis"`) {
		t.Fatalf("%d %s", got.Code, got.Body.String())
	}
	if got := client.do(http.MethodGet, path+"?limit=201", "", ""); got.Code != http.StatusBadRequest {
		t.Fatal("accepted oversized page")
	}
	if got := client.do(http.MethodGet, "/snapshots/snap-other/insights/analysis-1/timeline", "", ""); got.Code != http.StatusNotFound {
		t.Fatal("accepted another investigation's snapshot")
	}
	fake.wrongScope = true
	if got := client.do(http.MethodGet, path, "", ""); got.Code != http.StatusBadGateway {
		t.Fatal("accepted wrong response scope")
	}
}
