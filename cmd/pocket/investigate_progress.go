package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"golang.org/x/term"

	"pocketcli/internal/investigation/contract"
)

const progressBarWidth = 30

var progressStageLabels = map[string]string{
	"queued":         "na fila",
	"collecting":     "coletando",
	"building":       "montando snapshot",
	"analysis_ready": "snapshot publicado",
	"reporting":      "gerando PDF",
	"completed":      "concluído",
}

// progressLine renders a run as "[█████░░░░░]  45% coletando · 120 requests".
func progressLine(run contract.Run, p palette) string {
	percent := run.Progress
	if percent < 0 {
		percent = 0
	} else if percent > 100 {
		percent = 100
	}
	filled := percent * progressBarWidth / 100
	label := progressStageLabels[run.Stage]
	if label == "" {
		label = run.State
	}
	bar := p.green(strings.Repeat("█", filled)) + p.dim(strings.Repeat("░", progressBarWidth-filled))
	return fmt.Sprintf("[%s] %s %s %s", bar, p.bold(fmt.Sprintf("%3d%%", percent)), label, p.dim(fmt.Sprintf("· %d requests", run.Requests)))
}

// progressBar redraws one terminal line in place. Off a terminal it stays
// silent and the caller prints one plain line per state change instead.
type progressBar struct {
	out     io.Writer
	live    bool
	palette palette
	last    string
	started time.Time
}

func newProgressBar(out io.Writer) *progressBar {
	file, ok := out.(*os.File)
	return &progressBar{out: out, live: ok && term.IsTerminal(int(file.Fd())), palette: newPalette(out), started: time.Now()}
}

func (b *progressBar) update(run contract.Run) {
	if !b.live {
		return
	}
	line := progressLine(run, b.palette)
	if line == b.last {
		return
	}
	// \r returns to the line start and \x1b[K clears what a longer line left.
	fmt.Fprintf(b.out, "\r%s\x1b[K", line)
	b.last = line
}

// done ends the live line so later output starts on its own line.
func (b *progressBar) done() {
	if b.live && b.last != "" {
		fmt.Fprintln(b.out)
		b.last = ""
	}
}

// finish prints the outcome under the bar: what ended, where the data is and
// the next command. investigationID may be empty when the plan is unreadable.
func (b *progressBar) finish(run contract.Run, investigationID string, details [][2]string) {
	if !b.live {
		return
	}
	b.update(run)
	b.done()
	p := b.palette
	elapsed := time.Since(b.started).Round(time.Second)
	stats := p.dim(fmt.Sprintf("%s · %d requests · %s", elapsed, run.Requests, formatBytes(run.Bytes)))
	row := func(label, value string) { fmt.Fprintf(b.out, "  %s %s\n", p.dim(fmt.Sprintf("%-11s", label)), value) }
	switch run.State {
	case "completed", "completed_partial":
		outcome := "Investigação concluída"
		if run.State == "completed_partial" {
			outcome += " " + p.yellow("(cobertura parcial)")
		}
		fmt.Fprintf(b.out, "%s %s  %s\n", p.green("✓"), p.bold(outcome), stats)
		row("Snapshot", run.SnapshotID)
		for _, detail := range details {
			row(detail[0], detail[1])
		}
		if investigationID != "" {
			row("Ver dados", p.cyan("pocket investigate show "+investigationID))
			row("Abrir na UI", p.cyan("pocket investigate serve "+investigationID))
		}
	case "report_failed":
		fmt.Fprintf(b.out, "%s %s  %s\n", p.yellow("!"), p.bold("Snapshot pronto, PDF pendente"), stats)
		row("Snapshot", run.SnapshotID)
		for _, detail := range details {
			row(detail[0], detail[1])
		}
		if investigationID != "" {
			row("Gerar PDF", p.cyan("pocket investigate report "+investigationID))
			row("Ver dados", p.cyan("pocket investigate show "+investigationID))
		}
	case "paused":
		fmt.Fprintf(b.out, "%s %s  %s\n", p.yellow("⏸"), p.bold("Investigação pausada"), stats)
		row("Retomar", p.cyan("pocket investigate resume "+run.ID))
	default:
		fmt.Fprintf(b.out, "%s %s  %s\n", p.red("✗"), p.bold("Investigação "+run.State), stats)
	}
}
