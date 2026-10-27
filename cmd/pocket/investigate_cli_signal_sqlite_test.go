//go:build investigate_sqlite && (darwin || linux)

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
	binary := buildSignalCLI(t, root)
	profile := filepath.Join(root, "chrome-profile")
	wrapper := writeSignalChromiumWrapper(t, root, chromium, profile)
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

// The HTTP run returns before its automatic PDF finishes. SIGINT on the
// serving CLI must stop that detached Core worker and its real Chromium.
func TestCLIServeSIGINTStopsAutomaticChromium(t *testing.T) {
	chromium := os.Getenv("POCKETCLI_CHROMIUM")
	if chromium == "" {
		t.Skip("defina POCKETCLI_CHROMIUM para validar o PDF automático no shutdown")
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
	input := contract.PlanInput{SchemaVersion: contract.Version, Name: "serve-sigint", Modules: contract.Modules{RuntimeImport: true}, Period: contract.Period{Preset: "30d", Timezone: "UTC"}}
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
	binary := buildSignalCLI(t, root)
	profile := filepath.Join(root, "automatic-chrome-profile")
	wrapper := writeSignalChromiumWrapper(t, root, chromium, profile)
	cmd := exec.Command(binary, "investigate", "serve", plan.InvestigationID)
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
	urlPattern := regexp.MustCompile(`http://127\.0\.0\.1:\d+/`)
	codePattern := regexp.MustCompile(`Código local de desbloqueio: ([A-Za-z0-9_-]+)`)
	var baseURL, code string
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		printed := output.String()
		if url := urlPattern.FindString(printed); url != "" {
			baseURL = strings.TrimSuffix(url, "/")
		}
		if match := codePattern.FindStringSubmatch(printed); len(match) == 2 {
			code = match[1]
		}
		if baseURL != "" && code != "" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if baseURL == "" || code == "" {
		t.Fatalf("servidor não iniciou: %s", output.String())
	}
	client := &http.Client{Timeout: 5 * time.Second}
	unlock, err := http.NewRequest(http.MethodPost, baseURL+"/api/v1/session/unlock", strings.NewReader(`{"code":"`+code+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	unlock.Header.Set("Origin", baseURL)
	unlocked, err := client.Do(unlock)
	if err != nil {
		t.Fatal(err)
	}
	_ = unlocked.Body.Close()
	if unlocked.StatusCode != http.StatusOK || len(unlocked.Cookies()) == 0 {
		t.Fatalf("unlock: %d", unlocked.StatusCode)
	}
	cookie := unlocked.Cookies()[0]
	session, err := http.NewRequest(http.MethodGet, baseURL+"/api/v1/session", nil)
	if err != nil {
		t.Fatal(err)
	}
	session.AddCookie(cookie)
	sessionResult, err := client.Do(session)
	if err != nil {
		t.Fatal(err)
	}
	var auth struct {
		CSRF string `json:"csrf_token"`
	}
	err = json.NewDecoder(sessionResult.Body).Decode(&auth)
	_ = sessionResult.Body.Close()
	if err != nil || sessionResult.StatusCode != http.StatusOK || auth.CSRF == "" {
		t.Fatalf("sessão: %d %v", sessionResult.StatusCode, err)
	}
	requestBody, err := json.Marshal(contract.RunRequest{PlanID: plan.ID, ConfirmedPlanHash: plan.Hash, IdempotencyKey: "serve-sigint-auto"})
	if err != nil {
		t.Fatal(err)
	}
	start, err := http.NewRequest(http.MethodPost, baseURL+"/api/v1/runs", bytes.NewReader(requestBody))
	if err != nil {
		t.Fatal(err)
	}
	start.AddCookie(cookie)
	start.Header.Set("Origin", baseURL)
	start.Header.Set("X-CSRF-Token", auth.CSRF)
	started, err := client.Do(start)
	if err != nil {
		t.Fatal(err)
	}
	var run contract.Run
	err = json.NewDecoder(started.Body).Decode(&run)
	_ = started.Body.Close()
	if err != nil || started.StatusCode != http.StatusAccepted || run.ID == "" {
		t.Fatalf("run não iniciou: status=%d run=%+v erro=%v", started.StatusCode, run, err)
	}
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
			current, _ := c.GetRun(context.Background(), run.ID)
			t.Fatalf("PDF automático não abriu Chromium: run=%+v output=%s", current, output.String())
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
		if err != nil {
			t.Fatalf("servidor não encerrou após SIGINT: %v %s", err, output.String())
		}
	case <-time.After(12 * time.Second):
		_ = cmd.Process.Kill()
		<-finished
		t.Fatal("servidor manteve worker ativo após SIGINT")
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
			t.Fatalf("Chromium automático sobreviveu: %s", strings.Join(alive, "; "))
		}
		time.Sleep(50 * time.Millisecond)
	}
	finalRun, err := c.GetRun(context.Background(), run.ID)
	if err != nil || finalRun.State != "report_failed" || finalRun.SnapshotID == "" {
		t.Fatalf("run não preservou snapshot após SIGINT: %+v %v", finalRun, err)
	}
	if _, err := c.GetSnapshot(context.Background(), finalRun.SnapshotID); err != nil {
		t.Fatal(err)
	}
}

func buildSignalCLI(t *testing.T, root string) string {
	t.Helper()
	binary := filepath.Join(root, "pocket")
	build := exec.Command("go", "build", "-buildvcs=false", "-tags", "investigate_sqlite", "-o", binary, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build da CLI: %v: %s", err, out)
	}
	return binary
}

func writeSignalChromiumWrapper(t *testing.T, root, chromium, profile string) string {
	t.Helper()
	wrapper := filepath.Join(root, "chromium-wrapper.sh")
	quotedChromium := shellQuoteForSignalTest(chromium)
	quotedProfile := shellQuoteForSignalTest(profile)
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"--version\" ]; then exec " + quotedChromium + " --version; fi\n" +
		"exec " + quotedChromium + " --headless --disable-background-networking --user-data-dir=" + quotedProfile + " --remote-debugging-port=0 about:blank\n"
	if err := os.WriteFile(wrapper, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return wrapper
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
