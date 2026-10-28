package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/core"
	"pocketcli/internal/investigation/secrets"
)

// showTopics are the ready-made views of a collected snapshot. Each is a read
// only query over the snapshot views, the same ones the SQL screen uses.
var showTopics = map[string]struct{ title, sql string }{
	"issues":   {"Issues", `SELECT identifier, source, state, updated_at, title FROM v_issues ORDER BY updated_at DESC, issue_id`},
	"prs":      {"Pull requests", `SELECT repository || '#' || number AS pr, state, created_at, merged_at, title FROM v_pull_requests ORDER BY created_at DESC, pr_id`},
	"findings": {"Achados", `SELECT severity, rule_id, subject_id, title FROM v_findings ORDER BY CASE severity WHEN 'critical' THEN 0 WHEN 'high' THEN 1 WHEN 'medium' THEN 2 WHEN 'low' THEN 3 ELSE 4 END, finding_id`},
	"events":   {"Eventos", `SELECT event_at, event_type, entity_id, old_value, new_value FROM v_events ORDER BY event_at DESC, event_id`},
	"coverage": {"Cobertura", `SELECT source, resource_id, collection, status, observed_count, reason FROM v_coverage ORDER BY source, resource_id, collection`},
}

type showFlags struct {
	snapshot string
	limit    int
	json     bool
	rest     []string
}

func parseShowFlags(args []string) (showFlags, error) {
	f := showFlags{limit: 20}
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--json":
			f.json = true
		case a == "--snapshot" || a == "--limit" || a == "-n":
			i++
			if i >= len(args) {
				return f, fmt.Errorf("flag %s requer valor", a)
			}
			if a == "--snapshot" {
				f.snapshot = args[i]
				break
			}
			n, err := strconv.Atoi(args[i])
			if err != nil || n < 1 || n > 1000 {
				return f, fmt.Errorf("ERR_VALIDATION: --limit deve ficar entre 1 e 1000")
			}
			f.limit = n
		case strings.HasPrefix(a, "-"):
			return f, fmt.Errorf("flag inválida: %s (veja pocket investigate --help)", a)
		default:
			f.rest = append(f.rest, a)
		}
	}
	return f, nil
}

func showInvestigation(cmd *cobra.Command, args []string) error {
	f, err := parseShowFlags(args[1:])
	if err != nil {
		return err
	}
	c, err := newInvestigationCore(secrets.NewSessionStore())
	if err != nil {
		return err
	}
	if !c.SQLAvailable() {
		return &contract.ErrorResponse{Code: "ERR_UNAVAILABLE", Message: "motor completo indisponível; compile com CGO_ENABLED=1 e -tags investigate_sqlite"}
	}
	ctx := context.Background()
	snapshots, err := c.ListSnapshots(ctx, args[0])
	if err != nil {
		return err
	}
	if len(snapshots) == 0 {
		return &contract.ErrorResponse{Code: "ERR_NOT_FOUND", Message: "investigação sem snapshot"}
	}
	snapshot := snapshots[len(snapshots)-1]
	if f.snapshot != "" {
		if snapshot, err = c.GetSnapshot(ctx, f.snapshot); err != nil {
			return err
		}
		if snapshot.InvestigationID != args[0] {
			return &contract.ErrorResponse{Code: "ERR_FORBIDDEN", Message: "snapshot de outra investigação"}
		}
	}
	out := cmd.OutOrStdout()
	p := newPalette(out)
	topic := ""
	if len(f.rest) > 0 {
		topic = f.rest[0]
	}
	switch topic {
	case "":
		return showSummary(ctx, out, p, c, snapshot, args[0], f)
	case "sql":
		if len(f.rest) != 2 {
			return fmt.Errorf("uso: show INVESTIGATION_ID sql \"SELECT …\"")
		}
		return showQuery(ctx, out, p, c, snapshot, "SQL", f.rest[1], f)
	default:
		view, ok := showTopics[topic]
		if !ok || len(f.rest) > 1 {
			return fmt.Errorf("tópico inválido: use issues, prs, findings, events, coverage ou sql")
		}
		return showQuery(ctx, out, p, c, snapshot, view.title, view.sql, f)
	}
}

func showQuery(ctx context.Context, out io.Writer, p palette, c *core.Core, snapshot contract.Snapshot, title, sql string, f showFlags) error {
	result, err := c.Query(ctx, contract.QueryRequest{SnapshotID: snapshot.ID, SQL: sql, MaxRows: f.limit, TimeoutMS: 5000})
	if err != nil {
		return err
	}
	if f.json {
		return writeJSON(out, result)
	}
	fmt.Fprintf(out, "%s %s %s\n", p.cyan("●"), p.bold(title), p.dim("· "+snapshot.ID))
	if len(result.Rows) == 0 {
		fmt.Fprintln(out, "  "+p.dim("nenhuma linha"))
		return nil
	}
	names := make([]string, len(result.Columns))
	for i, column := range result.Columns {
		names[i] = column.Name
	}
	rows := make([][]string, len(result.Rows))
	for i, row := range result.Rows {
		rows[i] = make([]string, len(row))
		for j, cell := range row {
			rows[i][j] = formatCell(cell)
		}
	}
	writeTable(out, p, names, rows, terminalWidth(out))
	note := fmt.Sprintf("%d linhas", result.ReturnedRows)
	if result.Truncated {
		note += fmt.Sprintf(" (cortado em %d; use --limit N, até 1000)", f.limit)
	}
	fmt.Fprintln(out, "  "+p.dim(note+" · horários em UTC"))
	return nil
}

// showSummary prints what the run actually extracted: counts per kind,
// findings by severity and the coverage of every collection.
func showSummary(ctx context.Context, out io.Writer, p palette, c *core.Core, snapshot contract.Snapshot, investigationID string, f showFlags) error {
	data, err := collectedData(ctx, c, snapshot.ID)
	if err != nil {
		return err
	}
	if f.json {
		return writeJSON(out, struct {
			Snapshot contract.Snapshot `json:"snapshot"`
			Counts   map[string]int64  `json:"counts"`
			Findings map[string]int64  `json:"findings"`
			Coverage []coverageLine    `json:"coverage"`
		}{snapshot, data.counts, data.severities, data.coverage})
	}
	window := snapshot.WindowStart + " → " + snapshot.WindowEnd
	if plan, err := c.LoadPlan(snapshot.PlanID); err == nil {
		window = formatWindow(snapshot.WindowStart, snapshot.WindowEnd, plan.Input.Period)
	}
	row := func(label, value string) { fmt.Fprintf(out, "  %s %s\n", p.dim(fmt.Sprintf("%-11s", label)), value) }
	fmt.Fprintf(out, "%s %s %s\n", p.cyan("●"), p.bold("Dados coletados"), p.dim("· "+snapshot.ID))
	row("Janela", window)
	row("Publicado", snapshot.PublishedAt)
	row("Coletado", data.countsLine(p))
	row("Achados", data.findingsLine(p))
	fmt.Fprintf(out, "  %s\n", p.dim("Cobertura"))
	for _, line := range data.coverage {
		reason := ""
		if line.Reason != "" && line.Status != "complete" {
			reason = p.dim(" — " + line.Reason)
		}
		fmt.Fprintf(out, "    %s %s %s %s%s\n", statusMark(p, line.Status), p.dim(line.Source+" "+line.Resource), line.Collection, p.dim(fmt.Sprintf("(%d)", line.Observed)), reason)
	}
	fmt.Fprintf(out, "  %s\n", p.dim("Ver os itens: ")+p.cyan("pocket investigate show "+investigationID+" issues|prs|findings|events|coverage|sql"))
	return nil
}

type coverageLine struct {
	Source, Resource, Collection, Status, Reason string
	Observed                                     int64
}

type collected struct {
	counts     map[string]int64
	severities map[string]int64
	coverage   []coverageLine
	linearN    int64
	githubN    int64
}

func collectedData(ctx context.Context, c *core.Core, snapshotID string) (collected, error) {
	d := collected{counts: map[string]int64{}, severities: map[string]int64{}}
	run := func(sql string) ([][]contract.Cell, error) {
		result, err := c.Query(ctx, contract.QueryRequest{SnapshotID: snapshotID, SQL: sql, MaxRows: 1000, TimeoutMS: 5000})
		return result.Rows, err
	}
	integer := func(cell contract.Cell) int64 { n, _ := strconv.ParseInt(cell.Value, 10, 64); return n }
	rows, err := run(`SELECT source, COUNT(*) FROM v_issues GROUP BY source`)
	if err != nil {
		return d, err
	}
	for _, r := range rows {
		n := integer(r[1])
		d.counts["issues_"+r[0].Value] = n
		d.counts["issues"] += n
	}
	if rows, err = run(`SELECT (SELECT COUNT(*) FROM v_pull_requests), (SELECT COUNT(*) FROM v_events), (SELECT COUNT(*) FROM v_links), (SELECT COUNT(*) FROM v_evidence), (SELECT COUNT(*) FROM v_components)`); err != nil {
		return d, err
	}
	if len(rows) == 1 && len(rows[0]) == 5 {
		for i, name := range []string{"prs", "events", "links", "evidence", "components"} {
			d.counts[name] = integer(rows[0][i])
		}
	}
	if rows, err = run(`SELECT severity, COUNT(*) FROM v_findings GROUP BY severity`); err != nil {
		return d, err
	}
	for _, r := range rows {
		d.severities[r[0].Value] = integer(r[1])
	}
	if rows, err = run(`SELECT source, resource_id, collection, status, observed_count, reason FROM v_coverage ORDER BY source, resource_id, collection`); err != nil {
		return d, err
	}
	for _, r := range rows {
		d.coverage = append(d.coverage, coverageLine{Source: r[0].Value, Resource: r[1].Value, Collection: r[2].Value, Status: r[3].Value, Observed: integer(r[4]), Reason: r[5].Value})
	}
	return d, nil
}

func (d collected) countsLine(p palette) string {
	parts := []string{}
	add := func(n int64, label string) {
		if n > 0 {
			parts = append(parts, p.bold(strconv.FormatInt(n, 10))+" "+label)
		}
	}
	add(d.counts["issues_linear"], "issues Linear")
	add(d.counts["issues_github"], "issues GitHub")
	add(d.counts["prs"], "PRs")
	add(d.counts["events"], "eventos")
	add(d.counts["links"], "vínculos")
	add(d.counts["components"], "componentes")
	add(d.counts["evidence"], "evidências")
	if len(parts) == 0 {
		return p.dim("nenhum item na janela")
	}
	return strings.Join(parts, p.dim(" · "))
}

func (d collected) findingsLine(p palette) string {
	if len(d.severities) == 0 {
		return p.dim("nenhum")
	}
	parts := []string{}
	for _, severity := range []string{"critical", "high", "medium", "low"} {
		if n := d.severities[severity]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, severity))
		}
	}
	rest := []string{}
	for severity := range d.severities {
		switch severity {
		case "critical", "high", "medium", "low":
		default:
			rest = append(rest, severity)
		}
	}
	sort.Strings(rest)
	for _, severity := range rest {
		parts = append(parts, fmt.Sprintf("%d %s", d.severities[severity], severity))
	}
	return strings.Join(parts, p.dim(" · "))
}

// coverageLineSummary counts collections per status for the end-of-run line.
func (d collected) coverageLineSummary(p palette) string {
	counts := map[string]int{}
	for _, line := range d.coverage {
		counts[line.Status]++
	}
	parts := []string{}
	for _, status := range []string{"complete", "partial", "unsupported", "unauthorized", "unavailable", "not_selected"} {
		if n := counts[status]; n > 0 {
			parts = append(parts, statusColor(p, status, fmt.Sprintf("%d %s", n, statusLabel(status))))
		}
	}
	if len(parts) == 0 {
		return p.dim("sem coleções")
	}
	return strings.Join(parts, p.dim(" · "))
}

// collectedRows are the "what came out" lines under the run result.
func collectedRows(ctx context.Context, c *core.Core, snapshotID string, p palette) [][2]string {
	d, err := collectedData(ctx, c, snapshotID)
	if err != nil {
		return nil
	}
	return [][2]string{{"Coletado", d.countsLine(p)}, {"Achados", d.findingsLine(p)}, {"Cobertura", d.coverageLineSummary(p)}}
}

func statusLabel(status string) string {
	switch status {
	case "complete":
		return "completas"
	case "partial":
		return "parciais"
	case "unsupported":
		return "sem suporte"
	case "unauthorized":
		return "sem permissão"
	case "unavailable":
		return "indisponíveis"
	case "not_selected":
		return "não selecionadas"
	}
	return status
}

func statusColor(p palette, status, text string) string {
	switch status {
	case "complete", "completed", "done", "closed", "merged", "low":
		return p.green(text)
	case "partial", "started", "open", "In Progress", "medium", "active":
		return p.yellow(text)
	case "unsupported", "unauthorized", "unavailable", "failed", "critical", "high":
		return p.red(text)
	}
	return text
}

func statusMark(p palette, status string) string {
	switch status {
	case "complete":
		return p.green("✓")
	case "partial":
		return p.yellow("◐")
	case "not_selected":
		return p.dim("·")
	}
	return p.red("✗")
}

// formatCell shortens RFC 3339 times to "2006-01-02 15:04" and shows NULL.
func formatCell(cell contract.Cell) string {
	if cell.Kind == "null" {
		return "—"
	}
	if at, err := time.Parse(time.RFC3339, cell.Value); err == nil {
		return at.UTC().Format("2006-01-02 15:04")
	}
	return strings.ReplaceAll(cell.Value, "\n", " ")
}

func terminalWidth(out io.Writer) int {
	if file, ok := out.(*os.File); ok {
		if width, _, err := term.GetSize(int(file.Fd())); err == nil && width > 40 {
			return width
		}
	}
	return 120
}

// writeTable aligns columns, caps narrow ones and gives the last column the
// remaining width, truncating with an ellipsis. Status-like columns are
// colored by value.
func writeTable(out io.Writer, p palette, header []string, rows [][]string, width int) {
	widths := make([]int, len(header))
	for i, h := range header {
		widths[i] = utf8.RuneCountInString(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			if n := utf8.RuneCountInString(cell); n > widths[i] && i < len(widths)-1 {
				widths[i] = n
			}
		}
	}
	used := 2 + 2*(len(widths)-1)
	for i, w := range widths[:len(widths)-1] {
		if w > 36 {
			widths[i] = 36
		}
		used += widths[i]
	}
	last := len(widths) - 1
	room := width - used
	if room < 20 {
		room = 20
	}
	widths[last] = room
	line := func(cells []string, paint func(col int, text string) string) {
		var b strings.Builder
		b.WriteString("  ")
		for i := range widths {
			cell := ""
			if i < len(cells) {
				cell = truncateRunes(cells[i], widths[i])
			}
			pad := strings.Repeat(" ", widths[i]-utf8.RuneCountInString(cell))
			if i == last {
				pad = ""
			}
			b.WriteString(paint(i, cell) + pad)
			if i < last {
				b.WriteString("  ")
			}
		}
		fmt.Fprintln(out, strings.TrimRight(b.String(), " "))
	}
	line(header, func(_ int, text string) string { return p.dim(text) })
	for _, row := range rows {
		line(row, func(col int, text string) string {
			switch header[col] {
			case "state", "status", "severity":
				return statusColor(p, strings.TrimSpace(text), text)
			case "identifier", "pr", "rule_id":
				return p.bold(text)
			}
			return text
		})
	}
}

func truncateRunes(text string, max int) string {
	if utf8.RuneCountInString(text) <= max {
		return text
	}
	runes := []rune(text)
	return string(runes[:max-1]) + "…"
}
