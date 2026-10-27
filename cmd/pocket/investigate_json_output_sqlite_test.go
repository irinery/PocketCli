//go:build investigate_sqlite

package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/core"
)

type jsonRunFinalizer struct{ core *core.Core }

func (f *jsonRunFinalizer) Finalize(ctx context.Context, in contract.FinalizeRequest) (contract.ReportArtifact, error) {
	var findings int64
	for cursor := ""; ; {
		page, err := f.core.ReadReportPage(ctx, in.SnapshotID, cursor, 1000)
		if err != nil {
			return contract.ReportArtifact{}, err
		}
		for _, record := range page.Records {
			if record.Kind == "finding" {
				findings++
			}
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	return contract.ReportArtifact{SnapshotID: in.SnapshotID, Status: "completed", FindingCount: findings}, nil
}

func TestInvestigationJSONRunWritesOnlyFinalRunToStdout(t *testing.T) {
	c, err := core.NewCore(core.CoreConfig{Root: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	c.SetFinalizer(&jsonRunFinalizer{core: c})
	input := contract.PlanInput{SchemaVersion: contract.Version, Name: "json-output", Modules: contract.Modules{RuntimeImport: true}, Period: contract.Period{Preset: "30d", Timezone: "UTC"}}
	plan, err := c.PreparePlan(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	run, err := c.StartRun(context.Background(), contract.RunRequest{PlanID: plan.ID, ConfirmedPlanHash: plan.Hash, IdempotencyKey: "json-output"})
	if err != nil {
		t.Fatal(err)
	}
	stdoutReader, stdoutWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdoutReader.Close()
	stderrReader, stderrWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stderrReader.Close()
	oldStdout, oldStderr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = stdoutWriter, stderrWriter
	t.Cleanup(func() { os.Stdout, os.Stderr = oldStdout, oldStderr })
	err = waitInvestigation(&cobra.Command{}, c, run, true)
	os.Stdout, os.Stderr = oldStdout, oldStderr
	stdoutWriter.Close()
	stderrWriter.Close()
	stdout, readErr := io.ReadAll(stdoutReader)
	if readErr != nil {
		t.Fatal(readErr)
	}
	stderr, readErr := io.ReadAll(stderrReader)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if err != nil {
		t.Fatalf("run JSON falhou: %v; stderr=%s", err, stderr)
	}
	var completed contract.Run
	decoder := json.NewDecoder(strings.NewReader(string(stdout)))
	if err := decoder.Decode(&completed); err != nil {
		t.Fatalf("stdout não é Run JSON: %s: %v", stdout, err)
	}
	if completed.ID != run.ID || (completed.State != "completed" && completed.State != "completed_partial") {
		t.Fatalf("resultado inesperado: %+v", completed)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		t.Fatalf("stdout contém mais de um valor JSON: %s", stdout)
	}
	if len(stderr) == 0 {
		t.Fatal("progresso JSON ausente do stderr")
	}
	for _, line := range strings.Split(strings.TrimSpace(string(stderr)), "\n") {
		var event struct {
			Type string       `json:"type"`
			Run  contract.Run `json:"run"`
		}
		if err := json.Unmarshal([]byte(line), &event); err != nil || event.Type != "progress" || event.Run.ID != run.ID {
			t.Fatalf("progresso inválido: %q: %+v: %v", line, event, err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := c.WaitRunStopped(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
}
