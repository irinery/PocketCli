package queries

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pocketcli/internal/investigation/contract"
)

func TestLargeQueryResultReopensWithoutEmbeddingRowsInJournal(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	value := strings.Repeat("x", 2*1024*1024-1024)
	result := contract.QueryResult{SnapshotID: "snap-1", QueryHash: "hash-1", Columns: []contract.Column{{Name: "text"}}, Rows: [][]contract.Cell{{{Kind: "text", Value: value}}}, ReturnedRows: 1}
	request := contract.QueryRequest{SnapshotID: "snap-1", SQL: "SELECT text FROM v_evidence"}
	execution, err := store.Record(request, result, nil)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := os.ReadFile(filepath.Join(dir, "queries.jsonl"))
	if err != nil || len(journal) > 4096 || strings.Contains(string(journal), value[:256]) {
		t.Fatalf("resultado inteiro entrou no journal: bytes=%d err=%v", len(journal), err)
	}
	store, err = Open(dir)
	if err != nil {
		t.Fatalf("reabertura após resultado grande falhou: %v", err)
	}
	history := store.History("snap-1", "", 10)
	if len(history.Items) != 1 || history.Items[0].Result == nil || history.Items[0].Result.Rows[0][0].Value != value {
		t.Fatal("resultado da execução não foi restaurado")
	}
	saved, err := store.Create(CreateInput{Name: "consulta", QueryID: execution.QueryID}, "investigation-1", "snap-1")
	if err != nil {
		t.Fatal(err)
	}
	if saved.Result.Rows[0][0].Value != value {
		t.Fatal("consulta salva perdeu o resultado")
	}
	store, err = Open(dir)
	if err != nil {
		t.Fatalf("reabertura após salvar consulta falhou: %v", err)
	}
	reopened, ok := store.GetSaved(saved.ID)
	if !ok || reopened.Result.Rows[0][0].Value != value {
		t.Fatal("resultado da consulta salva não foi restaurado")
	}
	journal, err = os.ReadFile(filepath.Join(dir, "queries.jsonl"))
	if err != nil || len(journal) > 8192 || strings.Contains(string(journal), value[:256]) {
		t.Fatalf("resultado salvo entrou no journal: bytes=%d err=%v", len(journal), err)
	}
	for _, path := range []string{filepath.Join(dir, "results", "execution-"+execution.ID+".json"), filepath.Join(dir, "results", "saved-"+saved.ID+".json")} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0600 {
			t.Fatalf("arquivo de resultado não está privado: %s mode=%v", path, info.Mode())
		}
	}
	if err := os.Remove(filepath.Join(dir, "results", "execution-"+execution.ID+".json")); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir); err == nil {
		t.Fatal("resultado ausente foi tratado como histórico válido")
	}
}

func TestLegacySavedQueryPatchMovesResultOutOfJournal(t *testing.T) {
	dir := t.TempDir()
	id := strings.Repeat("a", 32)
	legacy := SavedQuery{ID: id, InvestigationID: "investigation-1", SnapshotID: "snap-1", Name: "antes", Revision: 1, Result: contract.QueryResult{SnapshotID: "snap-1", Rows: [][]contract.Cell{{{Kind: "text", Value: "historical result"}}}}}
	line, err := json.Marshal(map[string]any{"kind": "saved", "saved": legacy})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "queries.jsonl"), append(line, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	name := "depois"
	if _, err := store.Patch(id, PatchInput{ExpectedRevision: 1, Name: &name}); err != nil {
		t.Fatal(err)
	}
	store, err = Open(dir)
	if err != nil {
		t.Fatalf("consulta legada não reabriu após edição: %v", err)
	}
	saved, ok := store.GetSaved(id)
	if !ok || saved.Name != name || saved.Result.Rows[0][0].Value != "historical result" {
		t.Fatalf("resultado legado perdido: %+v", saved)
	}
	resultPath := filepath.Join(dir, "results", "saved-"+id+".json")
	data, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(resultPath, []byte(strings.Replace(string(data), "historical result", "historical resulx", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir); err == nil {
		t.Fatal("alteração do resultado privado não foi detectada")
	}
}

func TestRecordRejectsResultFromAnotherSnapshot(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Record(contract.QueryRequest{SnapshotID: "snap-1", SQL: "SELECT 1"}, contract.QueryResult{SnapshotID: "snap-2"}, nil)
	if err == nil || len(store.History("", "", 10).Items) != 0 {
		t.Fatal("resultado de outro snapshot entrou no histórico")
	}
}
