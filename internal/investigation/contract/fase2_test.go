package contract

import "testing"

// An organization older than its teams, a retired team and an empty legacy
// repository must not stretch the window: only resources with observed
// records bound it, and a retired team ends at its retirement.
func TestEffectiveWindowIgnoresResourcesWithoutRecords(t *testing.T) {
	end := "2026-11-03T00:00:00Z"
	legacy := Resource{ID: "1", Source: "github", Name: "org/legado", CreatedAt: "2023-03-01T00:00:00Z", LastChangeAt: "2023-04-01T00:00:00Z"}
	eng := Resource{ID: "team-eng", Source: "linear", Name: "ENG", CreatedAt: "2026-07-21T14:03:48Z"}
	qa := Resource{ID: "team-qa", Source: "linear", Name: "QA", CreatedAt: "2026-07-21T14:43:47Z", LastChangeAt: "2026-08-20T12:56:16Z"}
	coverage := []Coverage{
		{Source: "github", ResourceID: "1", Collection: "pull_requests", Status: "complete"},
		{Source: "git", ResourceID: "1", Collection: "code_history", Status: "complete", Reason: EmptyRepositoryReason},
		{Source: "linear", ResourceID: "team-eng", Collection: "issues", Status: "complete", Observed: 971},
		{Source: "linear", ResourceID: "team-qa", Collection: "issues", Status: "complete", Observed: 12},
	}
	observed := ObservedResources([]Resource{legacy, eng, qa}, coverage)
	if len(observed) != 2 || observed[0].Name != "ENG" || observed[1].Name != "QA" {
		t.Fatalf("recursos observados: %+v", observed)
	}
	from, to, lifetime := EffectiveWindow("", end, observed)
	if from != eng.CreatedAt || to != end || !lifetime {
		t.Fatalf("janela %q → %q (%v), esperado início no ENG", from, to, lifetime)
	}
	if f, tt, _ := ResourceLifetime(qa, end); f != qa.CreatedAt || tt != qa.LastChangeAt {
		t.Fatalf("team aposentado deveria terminar na aposentadoria: %q → %q", f, tt)
	}
	if got := ObservedResources([]Resource{legacy}, nil); len(got) != 1 {
		t.Fatal("sem cobertura (plano ainda não executado) todo recurso conta")
	}
}

func TestMetricBaseAndPendingQueues(t *testing.T) {
	if b := NewMetricBase(100, 300); !b.Low || b.Percent() != 33 {
		t.Fatalf("100 de 300 deveria ser baixa cobertura: %+v", b)
	}
	if b := NewMetricBase(150, 300); b.Low {
		t.Fatalf("metade não é baixa cobertura: %+v", b)
	}
	pending := PendingQueues([]Coverage{
		{ResourceID: "team-eng", Collection: "history", Status: "partial", Reason: "fila de reconciliação Linear: 871 issues pendentes"},
		{ResourceID: "team-eng", Collection: "issues", Status: "partial", Reason: "lista paginada"},
		{ResourceID: "team-cad", Collection: "history", Status: "complete", Reason: "coleção por issue consultada com paginação"},
	})
	if len(pending) != 1 || pending[0] != "team-eng/history" {
		t.Fatalf("filas pendentes: %v", pending)
	}
}
