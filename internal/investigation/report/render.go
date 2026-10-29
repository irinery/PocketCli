package report

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"pocketcli/internal/investigation/contract"
)

// RendererVersion identifies the in-process PDF layout recorded in each
// report. The CLI report cache includes it, so a layout change regenerates
// reports that were built with an older one.
const RendererVersion = "pdf-nativo/3"

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

// section is one entry of the table of contents.
type section struct {
	key, title string
	level      int
}

// reportView adds what the pages need beyond the frozen records: readable
// names for the resources (a Linear team key or owner/repo instead of a UUID)
// and the locator of every evidence, so a finding can name its items.
type reportView struct {
	doc      model
	names    map[string]string // resource ID -> team key or owner/repo
	locators map[string]string // evidence ID -> locator
	replacer *strings.Replacer // UUID resource IDs inside free text
}

func newReportView(doc model) *reportView {
	v := &reportView{doc: doc, names: map[string]string{}, locators: map[string]string{}}
	var pairs []string
	for _, r := range doc.Snapshot.Resources {
		if r.ID == "" || r.Name == "" {
			continue
		}
		v.names[r.ID] = r.Name
		if strings.Contains(r.ID, "-") { // UUIDs are unambiguous in free text; short numeric IDs are not
			pairs = append(pairs, r.ID, r.Name)
		}
	}
	v.replacer = strings.NewReplacer(pairs...)
	for _, r := range doc.Records {
		if r.Evidence != nil {
			v.locators[r.Evidence.ID] = r.Evidence.Locator
		}
	}
	return v
}

// resource names a coverage resource: the team key, owner/repo, or the
// investigation itself for the analysis source.
func (v *reportView) resource(source, id string) string {
	if source == "analysis" {
		return "investigação"
	}
	if name := v.names[id]; name != "" {
		return name
	}
	return id
}

func (v *reportView) label(text string) string { return v.replacer.Replace(text) }

func (v *reportView) evidenceLabel(id string) string {
	loc := v.locators[id]
	if loc == "" {
		return id
	}
	if r := []rune(loc); len(r) > 40 {
		loc = string(r[:39]) + "…"
	}
	return id + " (" + loc + ")"
}

// items lists the plain identifiers (ENG-12, owner/repo#7) behind a finding.
func (v *reportView) items(evidenceIDs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, id := range evidenceIDs {
		loc := v.locators[id]
		if loc == "" || strings.Contains(loc, ":") || seen[loc] {
			continue
		}
		seen[loc] = true
		out = append(out, loc)
	}
	return out
}

// recordKinds orders section 3: what the reader needs first, evidence last as
// an appendix. Kinds not listed go between the known kinds and the evidence.
var recordKinds = []struct{ kind, title string }{
	{"finding", "Achados"}, {"metric", "Métricas"}, {"component", "Componentes"}, {"coverage", "Cobertura por coleção"},
	{"stage", "Etapas"}, {"link", "Vínculos"}, {"estimate", "Estimativas"}, {"rework", "Retrabalho"}, {"delivery", "Entregas"},
}

func (v *reportView) kindOrder() []struct{ kind, title string } {
	counts := v.counts()
	var order []struct{ kind, title string }
	known := map[string]bool{"evidence": true}
	for _, k := range recordKinds {
		known[k.kind] = true
		if counts[k.kind] > 0 {
			order = append(order, k)
		}
	}
	var others []string
	for kind := range counts {
		if !known[kind] {
			others = append(others, kind)
		}
	}
	sort.Strings(others)
	for _, kind := range others {
		order = append(order, struct{ kind, title string }{kind, kind})
	}
	if counts["evidence"] > 0 {
		order = append(order, struct{ kind, title string }{"evidence", "Evidências (apêndice)"})
	}
	return order
}

func (v *reportView) counts() map[string]int {
	counts := map[string]int{}
	for _, r := range v.doc.Records {
		counts[r.Kind]++
	}
	return counts
}

// plannedSections is the table of contents. It depends only on the model, so
// its height is the same in every layout pass.
func (v *reportView) plannedSections() []section {
	doc := v.doc
	out := []section{
		{"summary", "1. Resumo determinístico", 1},
		{"coverage", "2. Cobertura — " + doc.CoverageStatus, 1},
		{"records", "3. Registros da análise", 1},
	}
	counts := v.counts()
	for i, k := range v.kindOrder() {
		out = append(out, section{"kind:" + k.kind, fmt.Sprintf("3.%d %s (%d)", i+1, k.title, counts[k.kind]), 2})
	}
	out = append(out, section{"notes", "10. Comentários do operador", 1})
	if len(doc.Editorial) > 0 {
		out = append(out, section{"editorial", "11. Redação assistida aceita", 1})
	}
	out = append(out, section{"queries", "12. Consultas SQL", 1}, section{"provenance", "14. Revisão e proveniência", 1})
	return out
}

// layoutReport places every section on pages. Section numbers match the live
// HTML so the PDF and the browser view can be cross-referenced. The table of
// contents needs page numbers, so the layout runs until they stop moving
// (the contents has a fixed height, so the second pass already settles).
func layoutReport(ctx context.Context, doc model) (*pdfLayout, error) {
	v := newReportView(doc)
	pages := map[string]int{}
	var l *pdfLayout
	for pass := 0; pass < 4; pass++ {
		var found map[string]int
		var err error
		l, found, err = v.layoutPass(ctx, pages)
		if err != nil {
			return nil, err
		}
		if equalPages(pages, found) {
			break
		}
		pages = found
	}
	return l, ctx.Err()
}

func equalPages(a, b map[string]int) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func (v *reportView) layoutPass(ctx context.Context, tocPages map[string]int) (*pdfLayout, map[string]int, error) {
	doc := v.doc
	s := doc.Snapshot
	l := newPDFLayout()
	found := map[string]int{}
	mark := func(key string) { found[key] = len(l.pages) }

	l.heading("Relatório da investigação", 22, 0, false)
	if doc.Demo {
		l.banner(9.5, plain("DEMO · dados fictícios para apresentação; não representam uma investigação real."))
	} else {
		l.paragraph(9.5, colorMuted, plain("Relatório determinístico da investigação."))
	}
	for _, kv := range [][2]string{
		{"Investigação:", s.InvestigationID}, {"Snapshot:", s.ID}, {"Janela:", snapshotWindowLabel(s)},
		{"Status:", s.Status}, {"Cobertura:", doc.CoverageStatus}, {"Gerado em:", doc.GeneratedAt},
		{"Dataset hash:", s.DatasetHash}, {"Regras:", s.RulesetVersion}, {"Mapeamento:", s.MappingVersion},
	} {
		l.paragraph(9, colorText, bold(kv[0]), plain(kv[1]))
	}

	l.heading("Índice", 13, 10, true)
	for _, sec := range v.plannedSections() {
		page := "…"
		if n := tocPages[sec.key]; n > 0 {
			page = strconv.Itoa(n)
		}
		if sec.level == 1 {
			l.tocLine(0, fontBold, 9.5, sec.title, page)
		} else {
			l.tocLine(14, fontRegular, 9, sec.title, page)
		}
	}

	l.heading("1. Resumo determinístico", 15, 14, true)
	mark("summary")
	v.renderSummary(l)

	l.heading("2. Cobertura — "+doc.CoverageStatus, 15, 14, true)
	mark("coverage")
	v.renderCoverage(l)

	l.heading("3. Registros da análise", 15, 14, true)
	mark("records")
	if len(doc.Records) == 0 {
		l.paragraph(10, colorText, plain("Nenhum registro recebido do leitor do snapshot."))
	}
	byKind := map[string][]contract.ReportRecord{}
	for _, r := range doc.Records {
		byKind[r.Kind] = append(byKind[r.Kind], r)
	}
	n := 0
	for i, k := range v.kindOrder() {
		l.heading(fmt.Sprintf("3.%d %s (%d)", i+1, k.title, len(byKind[k.kind])), 12, 10, false)
		mark("kind:" + k.kind)
		for _, r := range byKind[k.kind] {
			if n%50 == 0 {
				if err := ctx.Err(); err != nil {
					return nil, nil, err
				}
			}
			n++
			v.renderRecord(l, r)
		}
	}

	l.heading("10. Comentários do operador", 15, 14, true)
	mark("notes")
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
		mark("editorial")
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
	mark("queries")
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
	mark("provenance")
	l.paragraph(10, colorText, plain(fmt.Sprintf("Snapshot %s · dataset %s · relatório revision %d · revisão global dos comentários %d · gerado %s · renderer %s.", s.ID, s.DatasetHash, doc.Revision, doc.AnnotationRevision, doc.GeneratedAt, doc.RendererVersion)))
	l.paragraph(8, colorMuted, plain("PDF congelado. Abrir o HTML vivo ou encerrar o servidor não altera este arquivo."))
	return l, found, ctx.Err()
}

var severityRank = map[string]int{"critical": 0, "high": 1, "medium": 2, "low": 3, "info": 4}

func rankOf(severity string) int {
	if r, ok := severityRank[severity]; ok {
		return r
	}
	return 5
}

// renderSummary answers "what is in here" before the detail: record counts,
// findings grouped by rule and what each resource contributed.
func (v *reportView) renderSummary(l *pdfLayout) {
	doc := v.doc
	l.paragraph(10, colorText, plain("Este relatório contém todos os registros canônicos recebidos do leitor do snapshot. Estados desconhecidos e cobertura parcial permanecem explicitados; a camada editorial não altera esses dados."))
	collections := "sem coleções informadas"
	if len(doc.Snapshot.Coverage) > 0 {
		collections = fmt.Sprintf("%d coleções", len(doc.Snapshot.Coverage))
	}
	l.paragraph(10, colorText, bold("Achados:"), plain(fmt.Sprintf("%d ·", doc.FindingCount)), bold("Registros:"), plain(fmt.Sprintf("%d ·", doc.RecordCount)), bold("Cobertura:"), plain(fmt.Sprintf("%s (%s)", doc.CoverageStatus, collections)))

	if empty := v.emptyRepositories(); len(empty) > 0 {
		l.paragraph(10, colorText, bold("Repositórios vazios:"), plain(strings.Join(empty, ", ")+" — sem commits; a coleta Git está completa e não há histórico de código a analisar."))
	}

	counts := v.counts()
	var kinds []string
	for _, k := range v.kindOrder() {
		kinds = append(kinds, fmt.Sprintf("%d %s", counts[k.kind], strings.ToLower(k.title)))
	}
	if len(kinds) > 0 {
		l.paragraph(9, colorMuted, plain("Registros por tipo: "+strings.Join(kinds, " · ")+"."))
	}

	type ruleKey struct{ rule, title, severity string }
	groups := map[ruleKey]int{}
	for _, r := range doc.Records {
		if f := r.Finding; f != nil {
			groups[ruleKey{f.RuleID, f.Title, f.Severity}]++
		}
	}
	if len(groups) > 0 {
		keys := make([]ruleKey, 0, len(groups))
		for k := range groups {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			a, b := keys[i], keys[j]
			if rankOf(a.severity) != rankOf(b.severity) {
				return rankOf(a.severity) < rankOf(b.severity)
			}
			if groups[a] != groups[b] {
				return groups[a] > groups[b]
			}
			return a.rule < b.rule
		})
		rows := make([][]string, 0, len(keys))
		for _, k := range keys {
			rows = append(rows, []string{k.severity, k.rule, k.title, strconv.Itoa(groups[k])})
		}
		l.heading("Achados por regra", 11, 6, false)
		l.table([]float64{0.14, 0.2, 0.54, 0.12}, []string{"Severidade", "Regra", "Título", "Qtd"}, rows)
	}

	if resources := doc.Snapshot.Resources; len(resources) > 0 {
		type tally struct{ complete, other, items int64 }
		byID := map[string]*tally{}
		for _, c := range doc.Snapshot.Coverage {
			t := byID[c.ResourceID]
			if t == nil {
				t = &tally{}
				byID[c.ResourceID] = t
			}
			switch c.Status {
			case "complete":
				t.complete++
			case "not_selected":
			default:
				t.other++
			}
			if c.Collection == "issues" || c.Collection == "pull_requests" {
				t.items += c.Observed
			}
		}
		rows := make([][]string, 0, len(resources))
		for _, r := range resources {
			t := byID[r.ID]
			if t == nil {
				t = &tally{}
			}
			rows = append(rows, []string{r.Source, v.resource(r.Source, r.ID), r.ID, strconv.FormatInt(t.complete, 10), strconv.FormatInt(t.other, 10), strconv.FormatInt(t.items, 10)})
		}
		l.heading("Recursos analisados", 11, 6, false)
		l.table([]float64{0.07, 0.26, 0.38, 0.09, 0.09, 0.11}, []string{"Fonte", "Recurso", "ID", "Completas", "A revisar", "Issues/PRs"}, rows)
		if lives := v.lifetimeRows(); len(lives) > 0 {
			l.heading("Tempo de vida dos recursos", 11, 6, false)
			l.paragraph(9, colorMuted, plain("Período \"all\": cada repositório vai da criação à última alteração (push); no Linear, da criação da organização — ou do team, quando a investigação foi direcionada — até o fim da janela."))
			l.table([]float64{0.09, 0.31, 0.2, 0.28, 0.12}, []string{"Fonte", "Recurso", "Criado em", "Última alteração", "Dias"}, lives)
		}
	}
}

// snapshotWindowLabel states the window. The "all" period has no fixed
// start, so it is the span the resources lived, or the whole history when
// the sources did not report it.
func snapshotWindowLabel(s contract.Snapshot) string {
	if s.WindowStart != "" {
		return s.WindowStart + " — " + s.WindowEnd
	}
	if from, to, lifetime := contract.EffectiveWindow(s.WindowStart, s.WindowEnd, s.Resources); lifetime {
		return from + " — " + to + " (tempo de vida dos recursos)"
	}
	return "todo o histórico — " + s.WindowEnd
}

// lifetimeRows lists each resource's own lifetime. It only applies to the
// "all" period; a bounded window has nothing to add.
func (v *reportView) lifetimeRows() [][]string {
	s := v.doc.Snapshot
	if s.WindowStart != "" {
		return nil
	}
	var rows [][]string
	for _, r := range s.Resources {
		from, to, ok := contract.ResourceLifetime(r, s.WindowEnd)
		if !ok {
			continue
		}
		last := to
		if r.LastChangeAt == "" {
			last += " (fim da janela)"
		}
		days := "?"
		if a, err := time.Parse(time.RFC3339, from); err == nil {
			if b, err := time.Parse(time.RFC3339, to); err == nil && !b.Before(a) {
				days = strconv.Itoa(int(b.Sub(a).Hours() / 24))
			}
		}
		rows = append(rows, []string{r.Source, v.resource(r.Source, r.ID), from, last, days})
	}
	return rows
}

// emptyRepositories names the repositories whose Git history is empty, so a
// repository with nothing to read is listed instead of looking like a gap.
func (v *reportView) emptyRepositories() []string {
	seen := map[string]bool{}
	var names []string
	for _, c := range v.doc.Snapshot.Coverage {
		if !contract.IsEmptyRepository(c) || seen[c.ResourceID] {
			continue
		}
		seen[c.ResourceID] = true
		names = append(names, v.resource(c.Source, c.ResourceID))
	}
	sort.Strings(names)
	return names
}

// coverageStatusLabel is the status in plain words for the summary line.
func coverageStatusLabel(status string) string {
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

// expectedLabel is the expected count, or "?" when the source does not say.
func expectedLabel(expected *int64) string {
	if expected == nil {
		return "?"
	}
	return strconv.FormatInt(*expected, 10)
}

// renderCoverage lists what needs attention. Complete and not-selected
// collections are counted here and stay listed in section 3.
func (v *reportView) renderCoverage(l *pdfLayout) {
	s := v.doc.Snapshot
	if len(s.Coverage) == 0 {
		l.paragraph(10, colorText, plain("Não determinada pelo contrato recebido."))
		return
	}
	counts := map[string]int{}
	var attention [][]string
	for _, c := range s.Coverage {
		counts[c.Status]++
		if c.Status == "complete" || c.Status == "not_selected" {
			continue
		}
		attention = append(attention, []string{c.Source, v.resource(c.Source, c.ResourceID), c.Collection, c.Status, fmt.Sprintf("%d / %s", c.Observed, expectedLabel(c.Expected)), v.label(c.Reason)})
	}
	var parts []string
	for _, status := range []string{"complete", "partial", "unsupported", "unauthorized", "unavailable", "not_selected"} {
		if counts[status] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", counts[status], coverageStatusLabel(status)))
			delete(counts, status)
		}
	}
	var rest []string
	for status := range counts {
		rest = append(rest, status)
	}
	sort.Strings(rest)
	for _, status := range rest {
		parts = append(parts, fmt.Sprintf("%d %s", counts[status], status))
	}
	l.paragraph(10, colorText, bold(fmt.Sprintf("%d coleções:", len(s.Coverage))), plain(strings.Join(parts, " · ")+"."))
	if empty := v.emptyRepositories(); len(empty) > 0 {
		l.paragraph(10, colorText, bold("Repositórios vazios (sem commits):"), plain(strings.Join(empty, ", ")+". Contam como coleta completa: não há o que ler em Git."))
	}
	if len(attention) == 0 {
		l.paragraph(10, colorText, plain("Todas as coleções selecionadas foram lidas por completo."))
		return
	}
	l.paragraph(9, colorMuted, plain("Coleções que exigem atenção (parciais, sem suporte, sem permissão ou indisponíveis); \"?\" em esperado significa desconhecido. As demais estão em 3.x Cobertura por coleção."))
	l.table([]float64{0.08, 0.25, 0.14, 0.1, 0.12, 0.31}, []string{"Fonte", "Recurso", "Coleção", "Estado", "Obs. / esperado", "Motivo"}, attention)
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

func (v *reportView) renderRecord(l *pdfLayout, r contract.ReportRecord) {
	recordHeader(l, r.Kind+" · "+v.label(recordID(r)))
	if f := r.Finding; f != nil {
		l.heading(f.Title, 11.5, 0, false)
		l.paragraph(9.5, colorText, bold(f.Severity), plain(fmt.Sprintf("· regra %s %s · sujeito %s", f.RuleID, f.RuleVersion, f.SubjectID)))
		if items := v.items(f.EvidenceIDs); len(items) > 0 {
			l.paragraph(9.5, colorText, bold("Itens:"), plain(strings.Join(items, ", ")))
		}
		l.paragraph(9.5, colorText, plain(f.DeterministicText))
		labels := make([]string, 0, len(f.EvidenceIDs))
		for _, id := range f.EvidenceIDs {
			labels = append(labels, v.evidenceLabel(id))
		}
		l.paragraph(9.5, colorText, bold("Evidências:"), plain(strings.Join(labels, ", ")))
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
		l.paragraph(9.5, colorText, bold("Métrica:"), plain(fmt.Sprintf("%s = %s · amostra %d / %s", v.label(m.Name), value, m.SampleSize, formatIntPtr(m.EligibleSize))))
		l.paragraph(9.5, colorText, plain(v.label(m.Definition)+". "+strings.Join(m.Limitations, " ")))
	}
	if c := r.Coverage; c != nil {
		l.paragraph(9.5, colorText, bold("Cobertura:"), plain(fmt.Sprintf("%s / %s / %s: %s (%d / %s). %s", c.Source, v.resource(c.Source, c.ResourceID), c.Collection, c.Status, c.Observed, formatIntPtr(c.Expected), v.label(c.Reason))))
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
