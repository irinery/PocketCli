package report

import (
	"context"
	"errors"
	"fmt"
	"time"

	"pocketcli/internal/investigation/insightcontract"
)

// ExecutiveBundle is the decision analysis a consolidated report carries:
// the executive report and every case with its detail and evidence.
type ExecutiveBundle struct {
	Report  insightcontract.ExecutiveReport
	Details []insightcontract.CaseDetail
}

// ExecutiveSource supplies the latest executive analysis of a snapshot.
// ok is false when the snapshot has no finished analysis.
type ExecutiveSource interface {
	LatestAnalysisID(ctx context.Context, snapshotID string) (id string, ok bool, err error)
	Bundle(ctx context.Context, snapshotID string) (bundle ExecutiveBundle, ok bool, err error)
}

type analysisLister interface {
	ListAnalyses(ctx context.Context, snapshotID, cursor string, limit int) (insightcontract.AnalysisPage, error)
}

// LatestAnalysisID names the newest finished analysis, so a cached
// consolidated PDF stops being reused once a newer analysis exists.
func (f *ExecutiveFinalizer) LatestAnalysisID(ctx context.Context, snapshotID string) (string, bool, error) {
	lister, ok := f.reader.(analysisLister)
	if !ok {
		return "", false, nil
	}
	var best insightcontract.AnalysisSummary
	cursor := ""
	for round := 0; round < 100; round++ {
		page, err := lister.ListAnalyses(ctx, snapshotID, cursor, 50)
		if err != nil {
			return "", false, err
		}
		for _, item := range page.Items {
			if item.SnapshotID != snapshotID {
				return "", false, errors.New("analysis page belongs to another snapshot")
			}
			if best.ID == "" || item.CreatedAt > best.CreatedAt || (item.CreatedAt == best.CreatedAt && item.ID > best.ID) {
				best = item
			}
		}
		if page.NextCursor == "" || page.NextCursor == cursor {
			break
		}
		cursor = page.NextCursor
	}
	return best.ID, best.ID != "", nil
}

// Bundle reads the executive report and all case details of the newest
// analysis, validated the same way the stand-alone export validates them.
func (f *ExecutiveFinalizer) Bundle(ctx context.Context, snapshotID string) (ExecutiveBundle, bool, error) {
	id, ok, err := f.LatestAnalysisID(ctx, snapshotID)
	if err != nil || !ok {
		return ExecutiveBundle{}, false, err
	}
	report, err := f.reader.GetExecutiveReport(ctx, snapshotID, id)
	if err != nil {
		return ExecutiveBundle{}, false, err
	}
	if report.SnapshotID != snapshotID || report.AnalysisID != id {
		return ExecutiveBundle{}, false, errors.New("executive report snapshot or analysis mismatch")
	}
	if err := validateExecutiveReport(report); err != nil {
		return ExecutiveBundle{}, false, err
	}
	details, err := f.readAllCases(ctx, snapshotID, id)
	if err != nil {
		return ExecutiveBundle{}, false, err
	}
	if int64(len(details)) != report.TotalCases {
		return ExecutiveBundle{}, false, fmt.Errorf("case count differs from executive report total")
	}
	return ExecutiveBundle{Report: report, Details: details}, true, nil
}

var _ ExecutiveSource = (*ExecutiveFinalizer)(nil)

// renderDecisions adds the decisions of the executive analysis to the
// consolidated report as one numbered section, in the same pages.
func (v *reportView) renderDecisions(ctx context.Context, l *pdfLayout) error {
	b := v.doc.Executive
	l.paragraph(10, colorText, plain("Decisões, casos e evidências da análise executiva mais recente deste snapshot, em modo detalhado. Cada caso aponta as evidências que o sustentam."))
	created, err := time.Parse(time.RFC3339, v.doc.GeneratedAt)
	if err != nil {
		created = time.Now().UTC()
	}
	return writeExecutive(ctx, l, b.Report, b.Details, "analysis_detail", created)
}
