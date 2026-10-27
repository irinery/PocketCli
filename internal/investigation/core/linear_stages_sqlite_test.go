//go:build investigate_sqlite && cgo

package core

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/secrets"
	"pocketcli/internal/investigation/storage"
)

// Fluxo completo com os estados reais do team ENG do workspace Somos UNI:
// histórico Linear → intervalos por etapa em v_stage_times → métricas e
// registros do relatório; o tipo de estado "duplicate" chega ao desfecho.
func TestLinearWorkflowStagesReachSQLAndReportWithRealStateTypes(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	secretStore := secrets.NewSessionStore()
	if _, err := secretStore.Put(context.Background(), contract.CredentialInput{Source: "linear", Secret: "test-linear", Persistence: "session"}); err != nil {
		t.Fatal(err)
	}
	transport := transportFunc(func(r *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, err
		}
		var input struct {
			Query     string            `json:"query"`
			Variables map[string]string `json:"variables"`
		}
		if err := json.Unmarshal(body, &input); err != nil {
			return nil, err
		}
		response := ""
		switch {
		case strings.Contains(input.Query, "query Teams"):
			response = `{"data":{"teams":{"nodes":[{"id":"team-eng","key":"ENG","name":"Engenharia"}],"pageInfo":{"hasNextPage":false}}}}`
		case strings.Contains(input.Query, "TeamIssues"):
			response = `{"data":{"issues":{"nodes":[
			{"id":"i-7","identifier":"ENG-7","title":"Entrega","createdAt":"2026-01-02T00:00:00Z","updatedAt":"2026-01-09T00:00:00Z","startedAt":"2026-01-03T00:00:00Z","completedAt":"2026-01-09T00:00:00Z","state":{"name":"Finalizado/Merged","type":"completed"}},
			{"id":"i-8","identifier":"ENG-8","title":"Repetida","createdAt":"2026-01-04T00:00:00Z","updatedAt":"2026-01-05T00:00:00Z","state":{"name":"Duplicate","type":"duplicate"}}
			],"pageInfo":{"hasNextPage":false}}}}`
		case strings.Contains(input.Query, "IssueFields"):
			response = `{"data":{"__type":{"fields":[{"name":"history"}]}}}`
		case strings.Contains(input.Query, "IssueHistory") && input.Variables["id"] == "i-7":
			response = `{"data":{"issue":{"id":"i-7","history":{"nodes":[
			{"id":"t1","createdAt":"2026-01-03T00:00:00Z","fromState":{"name":"Aberta","type":"unstarted"},"toState":{"name":"Em Desenvolvimento","type":"started"}},
			{"id":"t2","createdAt":"2026-01-05T00:00:00Z","fromState":{"name":"Em Desenvolvimento","type":"started"},"toState":{"name":"Em revisão","type":"started"}},
			{"id":"t3","createdAt":"2026-01-06T00:00:00Z","fromState":{"name":"Em revisão","type":"started"},"toState":{"name":"QA","type":"started"}},
			{"id":"t4","createdAt":"2026-01-09T00:00:00Z","fromState":{"name":"QA","type":"started"},"toState":{"name":"Finalizado/Merged","type":"completed"}}
			],"pageInfo":{"hasNextPage":false}}}}}`
		case strings.Contains(input.Query, "IssueHistory"):
			response = `{"data":{"issue":{"id":"` + input.Variables["id"] + `","history":{"nodes":[],"pageInfo":{"hasNextPage":false}}}}}`
		case strings.Contains(input.Query, "TeamProjects"):
			response = `{"data":{"projects":{"nodes":[],"pageInfo":{"hasNextPage":false}}}}`
		default:
			response = `{"data":{}}`
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(response))}, nil
	})
	c, err := NewCore(CoreConfig{Store: store, Secrets: secretStore, HTTPClient: &http.Client{Transport: transport}, Clock: func() time.Time { return time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := c.PreparePlan(context.Background(), contract.PlanInput{SchemaVersion: contract.Version, Name: "etapas", LinearEnabled: true,
		Scope:  &contract.Scope{Mode: "selected", Teams: []string{"ENG"}},
		Period: contract.Period{Preset: "custom", FromDate: "2026-01-01", ToDate: "2026-01-30", Timezone: "UTC"}, Limits: contract.Limits{LinearRPH: 3600000}})
	if err != nil {
		t.Fatal(err)
	}
	run, err := c.StartRun(context.Background(), contract.RunRequest{PlanID: plan.ID, ConfirmedPlanHash: plan.Hash, IdempotencyKey: "stages"})
	if err != nil {
		t.Fatal(err)
	}
	run = waitRun(t, c, run.ID)
	if run.SnapshotID == "" {
		t.Fatalf("run sem snapshot: %+v", run)
	}
	snapshot, err := c.GetSnapshot(context.Background(), run.SnapshotID)
	if err != nil || snapshot.MappingVersion != MappingVersion {
		t.Fatalf("versão de mapeamento: %+v %v", snapshot.MappingVersion, err)
	}
	result, err := c.Query(context.Background(), contract.QueryRequest{SnapshotID: run.SnapshotID, SQL: "SELECT stage, elapsed_seconds, duration_valid FROM v_stage_times WHERE substr(stage, 1, 7) = 'linear:' ORDER BY started_at"})
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, row := range result.Rows {
		got = append(got, row[0].Value+"="+row[1].Value+"/"+row[2].Value)
	}
	want := "linear:Aberta=86400/1,linear:Em Desenvolvimento=172800/1,linear:Em revisão=86400/1,linear:QA=259200/1,linear:Finalizado/Merged=/0"
	if strings.Join(got, ",") != want {
		t.Fatalf("v_stage_times:\n got %s\nwant %s", strings.Join(got, ","), want)
	}
	metric, err := c.Query(context.Background(), contract.QueryRequest{SnapshotID: run.SnapshotID, SQL: "SELECT sample_size, value FROM v_metrics WHERE name = 'Permanência mediana na etapa \"QA\" do team team-eng'"})
	if err != nil || len(metric.Rows) != 1 || metric.Rows[0][0].Value != "1" || metric.Rows[0][1].Kind != "null" {
		t.Fatalf("métrica de etapa: %+v %v", metric, err)
	}
	duplicate, err := c.Query(context.Background(), contract.QueryRequest{SnapshotID: run.SnapshotID, SQL: "SELECT value FROM v_metrics WHERE name = 'Issues marcadas como duplicadas'"})
	if err != nil || len(duplicate.Rows) != 1 || duplicate.Rows[0][0].Value != "1" {
		t.Fatalf("tipo duplicate não classificado: %+v %v", duplicate, err)
	}
	var q11 contract.SuggestedQuery
	suggested, err := c.SuggestedQueries(context.Background(), run.SnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range suggested {
		if s.ID == "Q11" {
			q11 = s
		}
	}
	perStage, err := c.Query(context.Background(), contract.QueryRequest{SnapshotID: run.SnapshotID, SQL: q11.SQL})
	if err != nil || q11.SQL == "" {
		t.Fatalf("Q11 indisponível: %v", err)
	}
	summary := map[string]string{}
	for _, row := range perStage.Rows {
		summary[row[0].Value] = row[1].Value + "/" + row[2].Value + "/" + row[3].Value + "/" + row[4].Value
	}
	if summary["linear:QA"] != "1/1/0/72" || summary["linear:Finalizado/Merged"] != "1/0/1/" {
		t.Fatalf("Q11 por etapa: %+v", summary)
	}
	stages := 0
	for cursor := ""; ; {
		page, err := c.ReadReportPage(context.Background(), run.SnapshotID, cursor, 1000)
		if err != nil {
			t.Fatal(err)
		}
		for _, record := range page.Records {
			if record.Kind == "stage" && strings.HasPrefix(record.Stage.Stage, LinearStagePrefix) {
				stages++
			}
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	if stages != 5 {
		t.Fatalf("relatório deveria conter 5 intervalos Linear, veio %d", stages)
	}
}
