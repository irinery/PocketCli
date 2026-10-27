package report

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"pocketcli/internal/investigation/contract"
)

// rendererVersion identifies the in-process PDF layout recorded in each report.
const rendererVersion = "pdf-nativo/1"

// renderReport lays out the frozen report model and encodes it as PDF bytes.
func renderReport(ctx context.Context, doc model, created time.Time) ([]byte, int, error) {
	l, err := layoutReport(ctx, doc)
	if err != nil {
		return nil, 0, err
	}
	data, err := l.encode("Relatório da investigação "+doc.Snapshot.ID, "PocketCli · snapshot "+doc.Snapshot.ID, created)
	if err != nil {
		return nil, 0, err
	}
	return data, len(l.pages), nil
}

// layoutReport places every section on pages. Section numbers match the live
// HTML so the PDF and the browser view can be cross-referenced.
func layoutReport(ctx context.Context, doc model) (*pdfLayout, error) {
	l := newPDFLayout()
	s := doc.Snapshot
	l.heading("Relatório da investigação", 22, 0, false)
	if doc.Demo {
		l.banner(9.5, plain("DEMO · dados fictícios para apresentação; não representam uma investigação real."))
	} else {
		l.paragraph(9.5, colorMuted, plain("Relatório determinístico da investigação."))
	}
	for _, kv := range [][2]string{
		{"Investigação:", s.InvestigationID}, {"Snapshot:", s.ID}, {"Janela:", s.WindowStart + " — " + s.WindowEnd},
		{"Status:", s.Status}, {"Cobertura:", doc.CoverageStatus}, {"Gerado em:", doc.GeneratedAt},
		{"Dataset hash:", s.DatasetHash}, {"Regras:", s.RulesetVersion}, {"Mapeamento:", s.MappingVersion},
	} {
		l.paragraph(9, colorText, bold(kv[0]), plain(kv[1]))
	}

	l.heading("1. Resumo determinístico", 15, 14, true)
	l.paragraph(10, colorText, plain("Este relatório contém todos os registros canônicos recebidos do leitor do snapshot. Estados desconhecidos e cobertura parcial permanecem explicitados; a camada editorial não altera esses dados."))
	collections := "sem coleções informadas"
	if len(s.Coverage) > 0 {
		collections = fmt.Sprintf("%d coleções", len(s.Coverage))
	}
	l.paragraph(10, colorText, bold("Achados:"), plain(fmt.Sprintf("%d ·", doc.FindingCount)), bold("Registros:"), plain(fmt.Sprintf("%d ·", doc.RecordCount)), bold("Cobertura:"), plain(fmt.Sprintf("%s (%s)", doc.CoverageStatus, collections)))

	l.heading("2. Cobertura — "+doc.CoverageStatus, 15, 14, true)
	if len(s.Coverage) > 0 {
		rows := make([][]string, 0, len(s.Coverage))
		for _, c := range s.Coverage {
			rows = append(rows, []string{c.Source, c.ResourceID, c.Collection, c.Status, fmt.Sprintf("%d / %s", c.Observed, formatIntPtr(c.Expected)), c.Reason})
		}
		l.table([]float64{0.11, 0.17, 0.15, 0.13, 0.14, 0.30}, []string{"Fonte", "Recurso", "Coleção", "Estado", "Observado / esperado", "Motivo"}, rows)
	} else {
		l.paragraph(10, colorText, plain("Não determinada pelo contrato recebido."))
	}

	l.heading("3. Registros da análise", 15, 14, true)
	for i, r := range doc.Records {
		if i%50 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		renderRecord(l, r)
	}

	l.heading("10. Comentários do operador", 15, 14, true)
	if len(doc.Annotations) == 0 {
		l.paragraph(10, colorText, plain(fmt.Sprintf("Nenhum comentário registrado na revisão congelada %d.", doc.AnnotationRevision)))
	}
	for _, a := range doc.Annotations {
		recordHeader(l, fmt.Sprintf("%s · %s · revisão %d · %s", a.TargetType, a.TargetID, a.Revision, a.Status))
		l.paragraph(10, colorText, plain(a.Body))
		l.paragraph(8.5, colorMuted, plain(a.OperatorLabel+" · "+a.UpdatedAt))
	}

	if len(doc.Editorial) > 0 {
		l.heading("11. Redação assistida aceita", 15, 14, true)
		l.paragraph(10, colorText, plain("Camada editorial revisada por operador; não substitui os achados determinísticos acima."))
		for _, d := range doc.Editorial {
			recordHeader(l, fmt.Sprintf("%s · payload %s · aceito em %s", d.ModelID, d.PayloadHash, d.ReviewedAt))
			for _, section := range d.Sections {
				l.heading(section.FindingID, 11, 4, false)
				l.paragraph(10, colorText, plain(section.ImprovedText))
				l.paragraph(9, colorText, plain("Evidências: "+strings.Join(section.EvidenceIDs, ", ")))
			}
		}
	}

	l.heading("12. Consultas SQL", 15, 14, true)
	if len(doc.Executions) == 0 {
		l.paragraph(10, colorText, plain("Nenhuma consulta executada neste snapshot."))
	}
	for _, e := range doc.Executions {
		recordHeader(l, fmt.Sprintf("%s · %s · %s · %s", e.CreatedAt, e.Status, e.QueryID, e.SnapshotID))
		l.paragraph(9, colorText, plain(fmt.Sprintf("Hash %s · truncado %t", e.QueryHash, e.Truncated)))
		l.preformatted(e.SQL, 8)
		if e.Error != "" {
			l.paragraph(9, colorText, plain("Erro: "+e.Error))
		}
	}
	for _, q := range doc.SavedQueries {
		recordHeader(l, "consulta salva")
		l.heading("Consulta salva: "+q.Name, 11, 2, false)
		l.paragraph(9, colorText, plain(fmt.Sprintf("Incluída no relatório: %t · revisão %d · hash %s", q.IncludeInReport, q.Revision, q.QueryHash)))
		if !q.IncludeInReport {
			l.paragraph(9, colorText, plain("Resultado excluído por decisão do operador; a execução permanece no índice acima."))
			continue
		}
		l.preformatted(q.SQL, 8)
		renderQueryResult(l, q.Result)
	}

	l.heading("14. Revisão e proveniência", 15, 14, true)
	l.paragraph(10, colorText, plain(fmt.Sprintf("Snapshot %s · dataset %s · relatório revision %d · revisão global dos comentários %d · gerado %s · renderer %s.", s.ID, s.DatasetHash, doc.Revision, doc.AnnotationRevision, doc.GeneratedAt, doc.RendererVersion)))
	l.paragraph(8, colorMuted, plain("PDF congelado. Abrir o HTML vivo ou encerrar o servidor não altera este arquivo."))
	return l, ctx.Err()
}

// text returns every drawn line, page by page, for assertions and diagnostics.
func (l *pdfLayout) text() string {
	var out strings.Builder
	for _, p := range l.pages {
		for _, line := range p.lines {
			out.WriteString(line)
			out.WriteByte('\n')
		}
	}
	return out.String()
}

// recordHeader separates records with a rule and a small monospace id line.
func recordHeader(l *pdfLayout, id string) {
	l.ensure(48)
	l.space(4)
	l.rule(l.y, colorRule, 0.5)
	l.y += 5
	l.paragraph(7.5, colorMuted, textRun{fontMono, id})
}

func renderRecord(l *pdfLayout, r contract.ReportRecord) {
	recordHeader(l, r.Kind+" · "+recordID(r))
	if f := r.Finding; f != nil {
		l.heading(f.Title, 11.5, 0, false)
		l.paragraph(9.5, colorText, bold(f.Severity), plain(fmt.Sprintf("· regra %s %s · sujeito %s", f.RuleID, f.RuleVersion, f.SubjectID)))
		l.paragraph(9.5, colorText, plain(f.DeterministicText))
		l.paragraph(9.5, colorText, bold("Evidências:"), plain(strings.Join(f.EvidenceIDs, ", ")))
		l.paragraph(9.5, colorText, bold("Status da evidência:"), plain(f.EvidenceStatus+"."), bold("Limitações:"), plain(strings.Join(f.Limitations, "; ")))
	}
	if c := r.Component; c != nil {
		l.paragraph(9.5, colorText, bold("Componente:"), plain(fmt.Sprintf("%s · %s/%s · implementação %s · entrega %s · cobertura %s", c.Name, c.Repository, c.Path, c.ImplementationStatus, c.DeploymentStatus, c.Coverage)))
	}
	if m := r.Metric; m != nil {
		value := "não calculado"
		if m.Value.Kind != "null" {
			value = m.Value.Value + " " + m.Unit
		}
		l.paragraph(9.5, colorText, bold("Métrica:"), plain(fmt.Sprintf("%s = %s · amostra %d / %s", m.Name, value, m.SampleSize, formatIntPtr(m.EligibleSize))))
		l.paragraph(9.5, colorText, plain(m.Definition+". "+strings.Join(m.Limitations, " ")))
	}
	if c := r.Coverage; c != nil {
		l.paragraph(9.5, colorText, bold("Cobertura:"), plain(fmt.Sprintf("%s / %s / %s: %s (%d / %s). %s", c.Source, c.ResourceID, c.Collection, c.Status, c.Observed, formatIntPtr(c.Expected), c.Reason)))
	}
	if e := r.Evidence; e != nil {
		l.paragraph(9.5, colorText, bold("Evidência "+e.ID), plain(fmt.Sprintf("· %s · %s · observada %s", e.Source, e.Locator, e.ObservedAt)))
		l.paragraph(9.5, colorText, plain(e.Preview))
		l.paragraph(8.5, colorMuted, textRun{fontMono, "SHA-256 " + e.SHA256})
	}
	if k := r.Link; k != nil {
		l.paragraph(9.5, colorText, bold("Vínculo:"), plain(fmt.Sprintf("%s — %s → %s · %s · evidência %s", k.SourceID, k.Relation, k.TargetID, k.Status, k.EvidenceID)))
	}
	if st := r.Stage; st != nil {
		l.paragraph(9.5, colorText, bold("Etapa:"), plain(fmt.Sprintf("%s · sujeito %s · início %s · fim %s · duração válida %t", st.Stage, st.SubjectID, st.StartedAt, st.EndedAt, st.DurationValid)))
	}
	if e := r.Estimate; e != nil {
		l.paragraph(9.5, colorText, bold("Estimativa:"), plain(fmt.Sprintf("issue %s · %s → %s · %s · %s", e.IssueID, formatFloatPtr(e.Before), formatFloatPtr(e.After), e.Timing, e.EventAt)))
	}
	if w := r.Rework; w != nil {
		l.paragraph(9.5, colorText, bold("Retrabalho:"), plain(fmt.Sprintf("%s · %s · causa %s · %s", w.Kind, w.EvidenceStatus, w.CauseStatus, w.EventAt)))
	}
	if d := r.Delivery; d != nil {
		l.paragraph(9.5, colorText, bold("Artefato de entrega:"), plain(fmt.Sprintf("%s · ambiente %s · proveniência %s · %s", d.ComponentID, d.Environment, d.ProvenanceStatus, d.ObservedAt)))
	}
}

func renderQueryResult(l *pdfLayout, result contract.QueryResult) {
	if len(result.Columns) == 0 {
		l.paragraph(9, colorText, plain("Resultado sem colunas."))
		return
	}
	widths := make([]float64, len(result.Columns))
	header := make([]string, len(result.Columns))
	for i, c := range result.Columns {
		widths[i] = 1 / float64(len(result.Columns))
		header[i] = c.Name
	}
	rows := make([][]string, 0, len(result.Rows))
	for _, row := range result.Rows {
		cells := make([]string, len(row))
		for i, cell := range row {
			cells[i] = cell.Value
			if cell.Kind == "null" {
				cells[i] = "NULL"
			}
		}
		rows = append(rows, cells)
	}
	l.table(widths, header, rows)
	l.paragraph(8.5, colorMuted, plain(fmt.Sprintf("%s linhas · truncado %t · %d ms", strconv.Itoa(result.ReturnedRows), result.Truncated, result.ElapsedMS)))
}
