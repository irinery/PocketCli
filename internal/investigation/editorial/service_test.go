package editorial

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"pocketcli/internal/investigation/annotations"
	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/queries"
	"pocketcli/internal/investigation/web/mock"
)

type testSecrets struct{}

func (testSecrets) WithSecret(ctx context.Context, source string, fn func([]byte) error) error {
	return fn([]byte("local-test-key"))
}

type testProvider struct{}

func (testProvider) Models(context.Context, []byte) ([]Model, error) {
	return []Model{{ID: "claude-test"}}, nil
}

func (testProvider) Message(context.Context, []byte, string, string) (providerResult, error) {
	return providerResult{
		Text:       `{"sections":[{"finding_id":"f-1","improved_text":"Redação revisada pelo operador.","evidence_ids":["ev-4"]}]}`,
		RequestID:  "local-test-request",
		StopReason: "end_turn",
	}, nil
}

func TestPublishedAnalysisKeepsDatasetAndQueriesAfterEditorialReview(t *testing.T) {
	ctx := context.Background()
	core := mock.New()
	const snapshotID = "snap-demo-001"
	snapshotBefore, err := core.GetSnapshot(ctx, snapshotID)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := core.SuggestedQueries(ctx, snapshotID)
	if err != nil || len(catalog) == 0 {
		t.Fatalf("query catalog: %v", err)
	}
	query := contract.QueryRequest{SnapshotID: snapshotID, SQL: catalog[0].SQL}
	resultBefore, err := core.Query(ctx, query)
	if err != nil {
		t.Fatal(err)
	}
	queryStore, err := queries.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	execution, err := queryStore.Record(query, resultBefore, nil)
	if err != nil {
		t.Fatal(err)
	}
	savedBefore, err := queryStore.Create(queries.CreateInput{Name: "consulta original", QueryID: execution.QueryID, IncludeInReport: true}, snapshotBefore.InvestigationID, snapshotID)
	if err != nil {
		t.Fatal(err)
	}
	historyBefore := queryStore.AllHistory(snapshotID)

	notes, err := annotations.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var selected annotations.Annotation
	for i := 0; i < 101; i++ {
		selected, err = notes.Create(annotations.CreateInput{
			InvestigationID: snapshotBefore.InvestigationID,
			SnapshotID:      snapshotID,
			TargetType:      annotations.Finding,
			TargetID:        "f-1",
			Body:            "Comentário selecionado pelo operador",
			OperatorLabel:   "Operador local",
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if page := notes.List(snapshotID, "", 100); page.NextCursor == "" {
		t.Fatal("fixture não criou uma segunda página de comentários")
	}
	svc, err := New(core, notes, testSecrets{}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	svc.provider = testProvider{}
	if _, err = svc.Models(ctx); err != nil {
		t.Fatal(err)
	}
	preview, err := svc.Preview(ctx, Selection{
		SnapshotID: snapshotID, FindingIDs: []string{"f-1"}, AnnotationIDs: []string{selected.ID},
		Purpose: "clarity", Tone: "neutral", ModelID: "claude-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(preview.SanitizedPayload, selected.ID) {
		t.Fatal("comentário selecionado após a primeira página não entrou no preview")
	}
	draft, err := svc.Generate(ctx, GenerateInput{PreviewID: preview.ID, ApprovedPayloadHash: preview.PayloadHash})
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := svc.Review(draft.ID, draft.Revision, true)
	if err != nil || accepted.Status != "accepted" {
		t.Fatalf("review: %+v, %v", accepted, err)
	}
	if got := svc.Accepted(snapshotID); len(got) != 1 || got[0].ID != draft.ID {
		t.Fatalf("redação aceita: %+v", got)
	}
	snapshotAfter, err := core.GetSnapshot(ctx, snapshotID)
	if err != nil || !reflect.DeepEqual(snapshotBefore, snapshotAfter) {
		t.Fatalf("snapshot ou dataset_hash mudou após revisão: %v", err)
	}
	resultAfter, err := core.Query(ctx, query)
	if err != nil || !reflect.DeepEqual(resultBefore, resultAfter) {
		t.Fatalf("resultado da consulta original mudou: %v", err)
	}
	savedAfter, ok := queryStore.GetSaved(savedBefore.ID)
	if !ok || !reflect.DeepEqual(savedBefore, savedAfter) || !reflect.DeepEqual(historyBefore, queryStore.AllHistory(snapshotID)) {
		t.Fatal("consulta salva ou histórico original mudou após revisão")
	}
}

func TestPreviewRejectsAnnotationFromAnotherSnapshot(t *testing.T) {
	ctx := context.Background()
	notes, err := annotations.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := notes.Create(annotations.CreateInput{
		InvestigationID: "other", SnapshotID: "other-snapshot", TargetType: annotations.Finding,
		TargetID: "f-1", Body: "Comentário de outro snapshot", OperatorLabel: "Operador local",
	})
	if err != nil {
		t.Fatal(err)
	}
	svc, err := New(mock.New(), notes, testSecrets{}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	svc.provider = testProvider{}
	if _, err = svc.Models(ctx); err != nil {
		t.Fatal(err)
	}
	_, err = svc.Preview(ctx, Selection{
		SnapshotID: "snap-demo-001", FindingIDs: []string{"f-1"}, AnnotationIDs: []string{foreign.ID},
		Purpose: "clarity", Tone: "neutral", ModelID: "claude-test",
	})
	if err == nil || !strings.Contains(err.Error(), "ERR_VALIDATION") {
		t.Fatalf("comentário de outro snapshot foi aceito: %v", err)
	}
}
