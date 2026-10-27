//go:build darwin || linux || freebsd || openbsd || netbsd

package report

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
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
