//go:build investigate_sqlite && cgo

package core

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/secrets"
	"pocketcli/internal/investigation/storage"
)

// windowBoundsGitHub serves a PR list sorted by updated_at desc whose second
// page holds only history older than any bounded window in these tests,
// including #5: open, but untouched since 2024. Without the open listing, #5
// would be reachable only through that second page.
type windowBoundsGitHub struct {
	mu          sync.Mutex
	openLists   int
	historyPage int
}

func (f *windowBoundsGitHub) RoundTrip(r *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer test-secret" {
		return nil, fmt.Errorf("credencial ausente")
	}
	header := make(http.Header)
	body := "[]"
	switch {
	case r.URL.Path == "/repos/acme/api":
		body = `{"id":42,"full_name":"acme/api"}`
	case r.URL.Path == "/repos/acme/api/pulls" && r.URL.Query().Get("state") == "open":
		f.openLists++
		// #1 is also in the bounded walk; #5 is open but untouched since 2024.
		body = `[{"id":1,"number":1,"title":"aberto recente","state":"open","created_at":"2026-10-01T00:00:00Z","updated_at":"2026-10-20T00:00:00Z","head":{"sha":"a1"}},
		{"id":5,"number":5,"title":"aberto antigo","state":"open","created_at":"2024-02-01T00:00:00Z","updated_at":"2024-03-01T00:00:00Z","head":{"sha":"a5"}}]`
	case r.URL.Path == "/repos/acme/api/pulls" && r.URL.Query().Get("page") == "2":
		f.historyPage++
		body = `[{"id":5,"number":5,"title":"aberto antigo","state":"open","created_at":"2024-02-01T00:00:00Z","updated_at":"2024-03-01T00:00:00Z","head":{"sha":"a5"}},
		{"id":4,"number":4,"title":"histórico distante","state":"closed","created_at":"2023-01-01T00:00:00Z","updated_at":"2023-02-01T00:00:00Z","head":{"sha":"a4"}}]`
	case r.URL.Path == "/repos/acme/api/pulls":
		body = `[{"id":1,"number":1,"title":"aberto recente","state":"open","created_at":"2026-10-01T00:00:00Z","updated_at":"2026-10-20T00:00:00Z","head":{"sha":"a1"}},
		{"id":2,"number":2,"title":"lacuna do ano","state":"closed","created_at":"2026-01-10T00:00:00Z","updated_at":"2026-03-01T00:00:00Z","head":{"sha":"a2"}},
		{"id":3,"number":3,"title":"fora de qualquer janela","state":"closed","created_at":"2024-01-01T00:00:00Z","updated_at":"2025-05-01T00:00:00Z","head":{"sha":"a3"}}]`
		header.Set("Link", `<https://api.github.com/repos/acme/api/pulls?state=all&sort=updated&direction=desc&per_page=100&page=2>; rel="next"`)
	}
	return &http.Response{StatusCode: http.StatusOK, Header: header, Body: io.NopCloser(strings.NewReader(body))}, nil
}

func windowBoundsCore(t *testing.T, transport http.RoundTripper) *Core {
	t.Helper()
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	secretStore := secrets.NewSessionStore()
	if _, err := secretStore.Put(context.Background(), contract.CredentialInput{Source: "github", Secret: "test-secret", Persistence: "session"}); err != nil {
		t.Fatal(err)
	}
	c, err := NewCore(CoreConfig{Store: store, Secrets: secretStore, HTTPClient: &http.Client{Transport: transport}, Clock: func() time.Time { return time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func runWindowBoundsPlan(t *testing.T, c *Core, input contract.PlanInput, key string) (contract.Plan, contract.Run) {
	t.Helper()
	plan, err := c.PreparePlan(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	run, err := c.StartRun(context.Background(), contract.RunRequest{PlanID: plan.ID, ConfirmedPlanHash: plan.Hash, IdempotencyKey: key})
	if err != nil {
		t.Fatal(err)
	}
	run = waitRun(t, c, run.ID)
	if run.SnapshotID == "" {
		t.Fatalf("snapshot não publicado: %+v", run)
	}
	return plan, run
}

func snapshotPRNumbers(t *testing.T, c *Core, snapshotID string) []string {
	t.Helper()
	result, err := c.Query(context.Background(), contract.QueryRequest{SnapshotID: snapshotID, SQL: "SELECT number FROM v_pull_requests ORDER BY number"})
	if err != nil {
		t.Fatal(err)
	}
	numbers := []string{}
	for _, row := range result.Rows {
		numbers = append(numbers, row[0].Value)
	}
	sort.Strings(numbers)
	return numbers
}

func pullRequestCoverage(t *testing.T, c *Core, snapshotID string) contract.Coverage {
	t.Helper()
	snapshot, err := c.GetSnapshot(context.Background(), snapshotID)
	if err != nil {
		t.Fatal(err)
	}
	for _, coverage := range snapshot.Coverage {
		if coverage.Source == "github" && coverage.Collection == "pull_requests" {
			return coverage
		}
	}
	t.Fatalf("cobertura de PRs ausente: %+v", snapshot.Coverage)
	return contract.Coverage{}
}

// RFC 01 S04-04 / C.4: a primeira coleta de uma janela limitada não percorre
// o histórico inteiro do repositório, e ampliar 90d→1y busca a lacuna sem
// voltar ao inventário completo. Itens ainda abertos e não atualizados desde
// antes da janela continuam presentes pela listagem de abertos.
func TestBoundedWindowSkipsAncientHistoryAndWidensOnlyToNewStart(t *testing.T) {
	fake := &windowBoundsGitHub{}
	c := windowBoundsCore(t, fake)
	input := contract.PlanInput{SchemaVersion: contract.Version, Name: "janela limitada", GitHubEnabled: true,
		Scope:  &contract.Scope{Mode: "selected", Repositories: []string{"acme/api"}},
		Period: contract.Period{Preset: "90d", Timezone: "UTC"}, Limits: contract.Limits{GitHubRPH: 3600000}}
	firstPlan, firstRun := runWindowBoundsPlan(t, c, input, "first")
	if got := snapshotPRNumbers(t, c, firstRun.SnapshotID); strings.Join(got, ",") != "1,5" {
		t.Fatalf("90d deveria ter o PR recente e o aberto antigo: %v", got)
	}
	if coverage := pullRequestCoverage(t, c, firstRun.SnapshotID); coverage.Status != "complete" || coverage.Observed != 4 {
		t.Fatalf("cobertura de uma página por listagem deveria ser completa e contar itens únicos: %+v", coverage)
	}
	fake.mu.Lock()
	historyPages, openLists := fake.historyPage, fake.openLists
	fake.mu.Unlock()
	if historyPages != 0 || openLists != 1 {
		t.Fatalf("primeira rodada percorreu histórico distante ou não listou abertos: página2=%d abertos=%d", historyPages, openLists)
	}

	input.InvestigationID = firstPlan.InvestigationID
	input.Period.Preset = "1y"
	_, widened := runWindowBoundsPlan(t, c, input, "widened")
	if got := snapshotPRNumbers(t, c, widened.SnapshotID); strings.Join(got, ",") != "1,2,5" {
		t.Fatalf("1y deveria incluir a lacuna sem histórico fora da janela: %v", got)
	}
	fake.mu.Lock()
	historyPages = fake.historyPage
	fake.mu.Unlock()
	if historyPages != 0 {
		t.Fatalf("ampliação voltou ao inventário completo: página2=%d", historyPages)
	}
	if first := snapshotPRNumbers(t, c, firstRun.SnapshotID); strings.Join(first, ",") != "1,5" {
		t.Fatalf("snapshot anterior mudou: %v", first)
	}
}

// Período personalizado não carrega itens abertos de fora da janela, então
// não há listagem de abertos; "todo histórico" mantém o inventário completo.
func TestCustomPeriodSkipsOpenListingAndAllHistoryWalksEverything(t *testing.T) {
	fake := &windowBoundsGitHub{}
	c := windowBoundsCore(t, fake)
	custom := contract.PlanInput{SchemaVersion: contract.Version, Name: "custom", GitHubEnabled: true,
		Scope:  &contract.Scope{Mode: "selected", Repositories: []string{"acme/api"}},
		Period: contract.Period{Preset: "custom", FromDate: "2026-01-01", ToDate: "2026-06-30", Timezone: "UTC"}, Limits: contract.Limits{GitHubRPH: 3600000}}
	_, run := runWindowBoundsPlan(t, c, custom, "custom")
	if got := snapshotPRNumbers(t, c, run.SnapshotID); strings.Join(got, ",") != "2" {
		t.Fatalf("custom deveria conter somente atividade da janela: %v", got)
	}
	fake.mu.Lock()
	if fake.openLists != 0 || fake.historyPage != 0 {
		t.Fatalf("custom listou abertos ou histórico distante: abertos=%d página2=%d", fake.openLists, fake.historyPage)
	}
	fake.mu.Unlock()

	other := &windowBoundsGitHub{}
	all := windowBoundsCore(t, other)
	everything := custom
	everything.Period = contract.Period{Preset: "all", Timezone: "UTC"}
	_, run = runWindowBoundsPlan(t, all, everything, "all")
	if got := snapshotPRNumbers(t, all, run.SnapshotID); strings.Join(got, ",") != "1,2,3,4,5" {
		t.Fatalf("todo histórico deveria percorrer a lista inteira: %v", got)
	}
	other.mu.Lock()
	defer other.mu.Unlock()
	if other.historyPage != 1 || other.openLists != 0 {
		t.Fatalf("todo histórico: página2=%d abertos=%d", other.historyPage, other.openLists)
	}
}
