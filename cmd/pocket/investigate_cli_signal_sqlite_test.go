//go:build investigate_sqlite && (darwin || linux)

package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/core"
	"pocketcli/internal/investigation/storage"
)

type lockedSignalOutput struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (o *lockedSignalOutput) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.b.Write(p)
}

func (o *lockedSignalOutput) String() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.b.String()
}

// Opt-in end-to-end SIGINT check. The Chromium wrapper keeps a real browser
// running so the signal reliably arrives while the CLI is exporting a report.
func TestCLIReportSIGINTStopsRealChromium(t *testing.T) {
	chromium := os.Getenv("POCKETCLI_CHROMIUM")
	if chromium == "" {
		t.Skip("defina POCKETCLI_CHROMIUM para validar SIGINT com Chromium real")
	}
	root := t.TempDir()
	home := filepath.Join(root, "home")
	store, err := storage.Open(filepath.Join(home, ".local", "share", "pocketcli", "investigations"))
	if err != nil {
		t.Fatal(err)
	}
	c, err := core.NewCore(core.CoreConfig{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	input := contract.PlanInput{SchemaVersion: contract.Version, Name: "sigint-pdf", Modules: contract.Modules{RuntimeImport: true}, Period: contract.Period{Preset: "30d", Timezone: "UTC"}}
	first, err := c.PreparePlan(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	observedAt := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	data := fmt.Sprintf(`{"schema_version":"pocketcli.runtime.v1","observed_at":%q,"environment":"production","components":[{"name":"api","enabled":true}]}`, observedAt)
	if _, err := c.ImportRuntime(context.Background(), first.InvestigationID, []byte(data)); err != nil {
		t.Fatal(err)
	}
	input.InvestigationID = first.InvestigationID
	plan, err := c.PreparePlan(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	run, err := c.StartRun(context.Background(), contract.RunRequest{PlanID: plan.ID, ConfirmedPlanHash: plan.Hash, IdempotencyKey: "cli-sigint"})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		run, err = c.GetRun(context.Background(), run.ID)
		if err != nil {
			t.Fatal(err)
		}
		if run.State == "report_failed" && run.SnapshotID != "" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if run.State != "report_failed" || run.SnapshotID == "" {
		t.Fatalf("snapshot de teste não ficou pronto: %+v", run)
	}
	binary := filepath.Join(root, "pocket")
	build := exec.Command("go", "build", "-buildvcs=false", "-tags", "investigate_sqlite", "-o", binary, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build da CLI: %v: %s", err, out)
	}
	profile := filepath.Join(root, "chrome-profile")
	wrapper := filepath.Join(root, "chromium-wrapper.sh")
	quotedChromium := shellQuoteForSignalTest(chromium)
	quotedProfile := shellQuoteForSignalTest(profile)
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"--version\" ]; then exec " + quotedChromium + " --version; fi\n" +
		"exec " + quotedChromium + " --headless --disable-background-networking --user-data-dir=" + quotedProfile + " --remote-debugging-port=0 about:blank\n"
	if err := os.WriteFile(wrapper, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "investigate", "report", plan.InvestigationID, "--snapshot", run.SnapshotID)
	cmd.Env = append(os.Environ(), "HOME="+home, "POCKETCLI_CHROMIUM="+wrapper)
	var output lockedSignalOutput
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pgid := 0
	defer func() {
		_ = cmd.Process.Kill()
		if pgid == 0 {
			pgid, _ = chromiumGroupForProfile(profile)
		}
		if pgid > 0 {
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
		}
	}()
	ready := filepath.Join(profile, "DevToolsActivePort")
	deadline = time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			pgid, err = chromiumGroupForProfile(profile)
			if err != nil {
				t.Fatal(err)
			}
			if pgid > 0 {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("Chromium não iniciou pela CLI: %s", output.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() { finished <- cmd.Wait() }()
	select {
	case err := <-finished:
		if err == nil || !strings.Contains(output.String(), "ERR_PDF_FAILED") {
			t.Fatalf("CLI não reportou a interrupção do PDF: err=%v output=%s", err, output.String())
		}
	case <-time.After(8 * time.Second):
		_ = cmd.Process.Kill()
		<-finished
		t.Fatal("CLI não encerrou após SIGINT")
	}
	deadline = time.Now().Add(5 * time.Second)
	for {
		alive, err := liveChromiumGroupMembers(pgid)
		if err != nil {
			t.Fatal(err)
		}
		if len(alive) == 0 {
			pgid = 0
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("Chromium sobreviveu ao SIGINT da CLI: %s", strings.Join(alive, "; "))
		}
		time.Sleep(50 * time.Millisecond)
	}
	if got, err := c.GetSnapshot(context.Background(), run.SnapshotID); err != nil || got.ID != run.SnapshotID {
		t.Fatalf("snapshot perdido após SIGINT: %+v %v", got, err)
	}
	entries, err := os.ReadDir(filepath.Join(store.Root, "reports"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".pdf") || strings.HasPrefix(entry.Name(), "report-job-") {
			t.Fatalf("artefato incompleto permaneceu após SIGINT: %s", entry.Name())
		}
	}
}

func shellQuoteForSignalTest(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func chromiumGroupForProfile(profile string) (int, error) {
	out, err := exec.Command("ps", "-Ao", "pid=,pgid=,stat=,args=").Output()
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 || !strings.Contains(line, "--user-data-dir="+profile) || strings.HasPrefix(fields[2], "Z") {
			continue
		}
		return strconv.Atoi(fields[1])
	}
	return 0, nil
}

func liveChromiumGroupMembers(pgid int) ([]string, error) {
	out, err := exec.Command("ps", "-Ao", "pgid=,stat=,args=").Output()
	if err != nil {
		return nil, err
	}
	want := strconv.Itoa(pgid)
	var alive []string
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[0] != want || strings.HasPrefix(fields[1], "Z") {
			continue
		}
		alive = append(alive, strings.Join(fields[2:], " "))
	}
	return alive, nil
}
