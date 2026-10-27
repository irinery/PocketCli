package core

import (
	"context"
	"testing"
	"time"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/secrets"
	"pocketcli/internal/investigation/storage"
)

func TestSourceCursorKeepsPartialCheckpointWithoutAdvancingWatermark(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ss := secrets.NewSessionStore()
	if _, err := ss.Put(context.Background(), contract.CredentialInput{Source: "github", Secret: "first-token", Persistence: "session"}); err != nil {
		t.Fatal(err)
	}
	c, err := NewCore(CoreConfig{Store: store, Secrets: ss, Clock: func() time.Time { return time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	r := contract.Resource{Source: "github", ID: "42", Name: "acme/api"}
	plan := contract.Plan{ID: "cursor-plan-1", InvestigationID: "cursor-investigation", WindowStart: "2026-01-01T00:00:00Z", WindowEnd: "2026-01-10T00:00:00Z"}
	h, err := c.githubCursor(context.Background(), plan, r, "comments")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.saveSourceCheckpoint(&h, plan.ID, "page-2"); err != nil {
		t.Fatal(err)
	}
	if err := c.finishSourceCursor(&h, plan, "partial"); err != nil {
		t.Fatal(err)
	}
	h, err = c.githubCursor(context.Background(), plan, r, "comments")
	if err != nil || h.value.Watermark != "" || len(h.value.CoveredIntervals) != 0 || h.value.Checkpoint != "page-2" {
		t.Fatalf("partial avançou watermark: %+v %v", h.value, err)
	}
	if err := c.finishSourceCursor(&h, plan, "complete"); err != nil {
		t.Fatal(err)
	}
	h, err = c.githubCursor(context.Background(), plan, r, "comments")
	if err != nil || h.value.Watermark != plan.WindowEnd || len(h.value.CoveredIntervals) != 1 || h.value.Checkpoint != "" {
		t.Fatalf("cobertura completa não persistida: %+v %v", h.value, err)
	}
	next := contract.Plan{ID: "cursor-plan-2", InvestigationID: plan.InvestigationID, WindowStart: "2026-01-05T00:00:00Z", WindowEnd: "2026-01-12T00:00:00Z"}
	if got := h.deltaStart(next); got != "2026-01-08T00:00:00Z" {
		t.Fatalf("delta sem overlap de 48h: %q", got)
	}
	if got := h.deltaStart(contract.Plan{WindowStart: "2025-12-01T00:00:00Z", WindowEnd: next.WindowEnd}); got != "2025-12-01T00:00:00Z" {
		t.Fatalf("ampliação pulou intervalo antigo: %q", got)
	}
	if err := c.finishSourceCursor(&h, next, "complete"); err != nil {
		t.Fatal(err)
	}
	h, err = c.githubCursor(context.Background(), next, r, "comments")
	if err != nil || len(h.value.CoveredIntervals) != 1 || h.value.CoveredIntervals[0].Start != plan.WindowStart || h.value.CoveredIntervals[0].End != next.WindowEnd {
		t.Fatalf("intervalos adjacentes não unidos: %+v %v", h.value, err)
	}
	if _, err := ss.Put(context.Background(), contract.CredentialInput{Source: "github", Secret: "another-token", Persistence: "session"}); err != nil {
		t.Fatal(err)
	}
	other, err := c.githubCursor(context.Background(), next, r, "comments")
	if err != nil || other.id == h.id || other.value.Watermark != "" {
		t.Fatalf("cursor reutilizado entre credenciais: %+v %v", other, err)
	}
}

func TestLinearIssueSpansScheduleHistoricalGapsAndRecentDelta(t *testing.T) {
	h := cursorHandle{value: sourceCursor{
		Watermark: "2026-01-10T00:00:00Z",
		CoveredIntervals: []sourceInterval{
			{Start: "2025-12-01T00:00:00Z", End: "2025-12-15T00:00:00Z"},
			{Start: "2026-01-01T00:00:00Z", End: "2026-01-10T00:00:00Z"},
		},
	}}
	spans := h.linearIssueSpans(contract.Plan{WindowStart: "2025-11-01T00:00:00Z", WindowEnd: "2026-01-12T00:00:00Z"})
	want := []sourceInterval{
		{Start: "2025-11-01T00:00:00Z", End: "2025-12-01T00:00:00Z"},
		{Start: "2025-12-15T00:00:00Z", End: "2026-01-01T00:00:00Z"},
		{Start: "2026-01-08T00:00:00Z"},
	}
	if len(spans) != len(want) {
		t.Fatalf("spans=%+v", spans)
	}
	for i := range want {
		if spans[i] != want[i] {
			t.Fatalf("span[%d]=%+v want=%+v", i, spans[i], want[i])
		}
	}
	if all := h.linearIssueSpans(contract.Plan{WindowEnd: "2026-01-12T00:00:00Z"}); len(all) != 1 || all[0] != (sourceInterval{}) {
		t.Fatalf("all history=%+v", all)
	}
	allCovered := cursorHandle{value: sourceCursor{Watermark: "2026-01-10T00:00:00Z", CoveredIntervals: []sourceInterval{{End: "2026-01-10T00:00:00Z"}}}}
	if delta := allCovered.linearIssueSpans(contract.Plan{WindowEnd: "2026-01-12T00:00:00Z"}); len(delta) != 1 || delta[0] != (sourceInterval{Start: "2026-01-08T00:00:00Z"}) {
		t.Fatalf("all history refresh=%+v", delta)
	}
}
