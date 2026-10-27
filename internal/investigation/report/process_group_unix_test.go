//go:build darwin || linux || freebsd || openbsd || netbsd

package report

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestCancelledRendererStopsChildProcess(t *testing.T) {
	root := t.TempDir()
	ready := filepath.Join(root, "ready")
	survived := filepath.Join(root, "survived")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", `(sleep 1; printf survived > "$1") & printf ready > "$2"; wait`, "sh", survived, ready)
	stopProcessGroupOnCancel(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			_ = cmd.Wait()
			t.Fatal("processo de teste não iniciou")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	if err := cmd.Wait(); err == nil {
		t.Fatal("renderizador cancelado saiu sem erro")
	}
	time.Sleep(1200 * time.Millisecond)
	if _, err := os.Stat(survived); !os.IsNotExist(err) {
		t.Fatalf("subprocesso sobreviveu ao cancelamento: %v", err)
	}
}

// Opt-in check with the same process-group cancellation used by the PDF renderer.
func TestRealChromiumCancellationLeavesNoWorkers(t *testing.T) {
	chromium := os.Getenv("POCKETCLI_CHROMIUM")
	if chromium == "" {
		t.Skip("defina POCKETCLI_CHROMIUM para validar o cancelamento do Chromium real")
	}
	profile := filepath.Join(t.TempDir(), "profile")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, chromium, "--headless", "--disable-background-networking", "--user-data-dir="+profile, "--remote-debugging-port=0", "about:blank")
	stopProcessGroupOnCancel(cmd)
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	ready := filepath.Join(profile, "DevToolsActivePort")
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			_ = cmd.Wait()
			t.Fatalf("Chromium não iniciou o perfil de teste: %s", safeOutput(output.String()))
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	if err := cmd.Wait(); err == nil {
		t.Fatal("Chromium cancelado saiu sem erro")
	}
	deadline = time.Now().Add(5 * time.Second)
	for {
		alive, err := liveProcessGroupMembers(cmd.Process.Pid)
		if err != nil {
			t.Fatal(err)
		}
		if len(alive) == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("processos do Chromium sobreviveram ao cancelamento: %s", strings.Join(alive, "; "))
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func liveProcessGroupMembers(pgid int) ([]string, error) {
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
		alive = append(alive, fmt.Sprintf("pgid=%s stat=%s command=%s", fields[0], fields[1], strings.Join(fields[2:], " ")))
	}
	return alive, nil
}
