package core

import (
	"context"
	"strings"
	"testing"

	"pocketcli/internal/investigation/collect"
	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/storage"
)

func TestRunStatsReadOnlyAndPersistedOutsideContract(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	core, err := NewCore(CoreConfig{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	const runID = "run-cache-test"
	if err := store.Put("runs", runID, contract.Run{ID: runID, State: "completed"}); err != nil {
		t.Fatal(err)
	}
	stats, err := core.RunStats(context.Background(), runID)
	if err != nil || stats.RunID != runID || stats.ReuseCounter != 0 {
		t.Fatalf("run sem cache: %+v err=%v", stats, err)
	}
	if err := collect.RecordReuse(store, runID); err != nil {
		t.Fatal(err)
	}
	if err := collect.RecordGitObjectBytes(store, runID, collect.GitRefObjects, 256); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewCore(CoreConfig{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	stats, err = reopened.RunStats(context.Background(), runID)
	if err != nil || stats.ReuseCounter != 1 || stats.GitObjectBytesAdded != 256 || stats.GitRefBytesAdded != 256 {
		t.Fatalf("estatística após reinício: %+v err=%v", stats, err)
	}
	var unchanged contract.Run
	if err := store.Get("runs", runID, &unchanged); err != nil || unchanged.ID != runID || unchanged.State != "completed" {
		t.Fatalf("contrato Run alterado: %+v err=%v", unchanged, err)
	}
	if err := store.Put("runstats", runID, collect.RunStats{RunID: "other", ReuseCounter: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.RunStats(context.Background(), runID); err == nil || !strings.Contains(err.Error(), "ERR_AUDIT_FAILED") {
		t.Fatalf("estatística corrompida foi aceita: %v", err)
	}
	if err := store.Put("runstats", runID, collect.RunStats{RunID: runID, GitObjectBytesAdded: -1}); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.RunStats(context.Background(), runID); err == nil || !strings.Contains(err.Error(), "ERR_AUDIT_FAILED") {
		t.Fatalf("contagem Git negativa aceita: %v", err)
	}
}
