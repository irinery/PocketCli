package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"pocketcli/internal/investigation/contract"
)

func TestProgressLineShowsPercentStageAndRequests(t *testing.T) {
	for _, test := range []struct {
		run  contract.Run
		want string
	}{
		{contract.Run{Stage: "collecting", Progress: 50, Requests: 12}, "[" + strings.Repeat("█", 15) + strings.Repeat("░", 15) + "]  50% coletando · 12 requests"},
		{contract.Run{Stage: "completed", Progress: 100}, "[" + strings.Repeat("█", 30) + "] 100% concluído · 0 requests"},
		{contract.Run{State: "paused", Progress: 140}, "[" + strings.Repeat("█", 30) + "] 100% paused · 0 requests"},
	} {
		if got := progressLine(test.run, palette{}); got != test.want {
			t.Fatalf("progressLine(%+v) = %q, esperado %q", test.run, got, test.want)
		}
	}
}

// Off a terminal (logs, CI, pipes) the bar stays silent and the state lines
// remain the only output.
func TestProgressBarIsSilentOffTerminal(t *testing.T) {
	var out bytes.Buffer
	bar := newProgressBar(&out)
	bar.update(contract.Run{Stage: "collecting", Progress: 40})
	bar.done()
	if out.Len() != 0 {
		t.Fatalf("barra escreveu fora de terminal: %q", out.String())
	}
}

func TestPlanSummaryFormatting(t *testing.T) {
	for d, want := range map[time.Duration]string{2 * time.Hour: "2h", 4 * time.Minute: "4m", 90 * time.Minute: "1h30m", 45 * time.Second: "45s"} {
		if got := formatDuration(d); got != want {
			t.Fatalf("formatDuration(%s) = %q, esperado %q", d, got, want)
		}
	}
	for n, want := range map[int64]string{512: "512 B", 16 << 20: "16.0 MiB", 5 << 30: "5.0 GiB"} {
		if got := formatBytes(n); got != want {
			t.Fatalf("formatBytes(%d) = %q, esperado %q", n, got, want)
		}
	}
	window := formatWindow("2026-10-27T09:20:00Z", "2026-10-27T21:20:00Z", contract.Period{Preset: "12h", Timezone: "America/Sao_Paulo"})
	if window != "27/10/2026 06:20 → 27/10/2026 18:20 (12h, America/Sao_Paulo)" {
		t.Fatalf("janela: %q", window)
	}
}
