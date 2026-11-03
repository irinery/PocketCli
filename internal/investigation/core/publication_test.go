package core

import (
	"testing"
	"time"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/query"
)

// The activity window starts at the first observed event, not at the
// creation of an older organization, team or empty repository.
func TestActivityWindowStartsAtFirstEvent(t *testing.T) {
	d := query.Dataset{Snapshot: contract.Snapshot{WindowEnd: "2026-11-03T00:00:00Z"},
		Issues: []query.Issue{
			{ID: "linear-1", CreatedAt: "2026-07-22T10:00:00.000Z", CompletedAt: "2026-08-01T00:00:00Z"},
			{ID: "linear-2", CreatedAt: "2026-10-30T00:00:00Z", CanceledAt: "2026-11-02T09:00:00Z"},
			{ID: "linear-ctx", CreatedAt: "2023-01-01T00:00:00Z", IsContext: true},
			{ID: "linear-late", CreatedAt: "2026-12-01T00:00:00Z"},
		},
		Events: []query.Event{{ID: "e", EventAt: "2026-07-23T00:00:00Z"}},
	}
	from, to := activityWindow(d)
	if from != "2026-07-22T10:00:00Z" || to != "2026-11-02T09:00:00Z" {
		t.Fatalf("janela de atividade %q → %q", from, to)
	}
	if from, to := activityWindow(query.Dataset{}); from != "" || to != "" {
		t.Fatalf("sem registros não há atividade: %q → %q", from, to)
	}
}

// A pending reconciliation queue keeps the snapshot provisional; only an
// operator's reason publishes it, and the reason is kept as provenance.
func TestPendingQueueBlocksPublicationWithoutOverride(t *testing.T) {
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	pending := []contract.Coverage{{Source: "linear", ResourceID: "team-eng", Collection: "history", Status: "partial", Reason: "fila de reconciliação Linear: 200 issues pendentes"}}
	if status, override := publicationStatus(pending, "  ", now); status != contract.SnapshotProvisional || override != nil {
		t.Fatalf("fila pendente publicada sem override: %s %+v", status, override)
	}
	status, override := publicationStatus(pending, "auditoria parcial aceita", now)
	if status != contract.SnapshotPublished || override == nil || override.Reason != "auditoria parcial aceita" || override.At != "2026-11-03T12:00:00Z" || len(override.Pending) != 1 || override.Pending[0] != "team-eng/history" {
		t.Fatalf("override não registrado: %s %+v", status, override)
	}
	if status, override := publicationStatus([]contract.Coverage{{Status: "complete"}}, "", now); status != contract.SnapshotPublished || override != nil {
		t.Fatalf("sem fila deveria publicar: %s %+v", status, override)
	}
}

func TestNoteActorKeepsFirstReportedName(t *testing.T) {
	d := query.Dataset{}
	noteActor(&d, "user-1", "linear", "")
	noteActor(&d, "user-1", "linear", " Ana ")
	noteActor(&d, "user-1", "linear", "Outra")
	noteActor(&d, "", "linear", "Sem ID")
	if len(d.Snapshot.Actors) != 1 || d.Snapshot.Actors[0].Name != "Ana" {
		t.Fatalf("atores: %+v", d.Snapshot.Actors)
	}
}
