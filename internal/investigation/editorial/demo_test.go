package editorial

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/web/mock"
)

// RFC 02 C.9 llm_invalid_output / L06-06: resposta que não é JSON é recusada
// pela validação real, sem rascunho, sem nova chamada e sem tocar no snapshot.
func TestDemoInvalidOutputIsRejectedByRealValidation(t *testing.T) {
	ctx := context.Background()
	core := mock.New()
	before, err := core.GetSnapshot(ctx, "snap-demo-001")
	if err != nil {
		t.Fatal(err)
	}
	svc, err := NewWithDemoProvider(core, nil, testSecrets{}, t.TempDir(), "invalid_output")
	if err != nil {
		t.Fatal(err)
	}
	models, err := svc.Models(ctx)
	if err != nil || len(models) != 1 || models[0].ID != "demo-editorial" {
		t.Fatalf("modelos demo: %+v %v", models, err)
	}
	preview, err := svc.Preview(ctx, Selection{SnapshotID: "snap-demo-001", FindingIDs: []string{"f-1"}, AnnotationIDs: []string{}, Purpose: "clarity", Tone: "neutral", ModelID: "demo-editorial"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Generate(ctx, GenerateInput{PreviewID: preview.ID, ApprovedPayloadHash: preview.PayloadHash})
	var coded *contract.ErrorResponse
	if !errors.As(err, &coded) || coded.Code != "ERR_LLM_OUTPUT_INVALID" {
		t.Fatalf("saída inválida deveria ser recusada: %v", err)
	}
	if drafts := svc.Drafts("snap-demo-001"); len(drafts) != 0 {
		t.Fatalf("rascunho inválido foi persistido: %+v", drafts)
	}
	if _, err := svc.Generate(ctx, GenerateInput{PreviewID: preview.ID, ApprovedPayloadHash: preview.PayloadHash}); !errors.As(err, &coded) || coded.Code != "ERR_LLM_CONSENT_REQUIRED" {
		t.Fatalf("nova tentativa deveria exigir novo consentimento: %v", err)
	}
	after, err := core.GetSnapshot(ctx, "snap-demo-001")
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("snapshot mudou: %v", err)
	}
	if _, err := NewWithDemoProvider(core, nil, testSecrets{}, t.TempDir(), "valid"); err == nil {
		t.Fatal("comportamento desconhecido deveria falhar")
	}
}
