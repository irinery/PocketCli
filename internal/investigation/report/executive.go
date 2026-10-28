package report

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"pocketcli/internal/investigation/insightcontract"
)

const executiveRendererVersion = "pdf-nativo-executivo/1"
const executivePageSize = 100

type executiveTimelineEntry struct {
	at, id, kind, text, scope string
	citations                 []insightcontract.Citation
	orderedAt                 time.Time
	ordered                   bool
}

type executiveManifest struct {
	Artifact       insightcontract.ExecutiveArtifact `json:"artifact"`
	IdempotencyKey string                            `json:"idempotency_key"`
}

// ExecutiveFinalizer stores executive and audit-detail exports separately
// from legacy ReportArtifact files.
type ExecutiveFinalizer struct {
	reader insightcontract.InsightReader
	dir    string
	mu     sync.Mutex
	items  map[string]executiveManifest
	seq    map[string]int
}

func NewExecutiveFinalizer(reader insightcontract.InsightReader, dir string) (*ExecutiveFinalizer, error) {
	if reader == nil {
		return nil, errors.New("insight reader is required")
	}
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("executive report directory is required")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return nil, err
	}
	f := &ExecutiveFinalizer{reader: reader, dir: dir, items: map[string]executiveManifest{}, seq: map[string]int{}}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		var manifest executiveManifest
		if json.Unmarshal(raw, &manifest) != nil || manifest.Artifact.ID == "" || manifest.Artifact.SnapshotID == "" || manifest.Artifact.AnalysisID == "" {
			continue
		}
		pdf := f.pdfPath(manifest.Artifact.ID)
		if st, e := os.Stat(pdf); e != nil || st.IsDir() {
			continue
		}
		f.items[manifest.Artifact.ID] = manifest
		key := sequenceKey(manifest.Artifact.SnapshotID, manifest.Artifact.AnalysisID, manifest.Artifact.Mode)
		if manifest.Artifact.Revision > f.seq[key] {
			f.seq[key] = manifest.Artifact.Revision
		}
	}
	return f, nil
}

func (f *ExecutiveFinalizer) ExportExecutive(ctx context.Context, req insightcontract.ExecutiveExportRequest) (insightcontract.ExecutiveArtifact, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	if req.SnapshotID == "" || req.AnalysisID == "" || req.IdempotencyKey == "" {
		return insightcontract.ExecutiveArtifact{}, errors.New("snapshot, analysis and idempotency key are required")
	}
	if req.Mode != "executive" && req.Mode != "analysis_detail" {
		return insightcontract.ExecutiveArtifact{}, errors.New("unsupported executive export mode")
	}
	report, err := f.reader.GetExecutiveReport(ctx, req.SnapshotID, req.AnalysisID)
	if err != nil {
		return insightcontract.ExecutiveArtifact{}, err
	}
	if report.SnapshotID != req.SnapshotID || report.AnalysisID != req.AnalysisID {
		return insightcontract.ExecutiveArtifact{}, errors.New("executive report snapshot or analysis mismatch")
	}
	if err := validateExecutiveReport(report); err != nil {
		return insightcontract.ExecutiveArtifact{}, err
	}
	var details []insightcontract.CaseDetail
	if req.Mode == "analysis_detail" {
		details, err = f.readAllCases(ctx, req.SnapshotID, req.AnalysisID)
		if err != nil {
			return insightcontract.ExecutiveArtifact{}, err
		}
		if int64(len(details)) != report.TotalCases {
			return insightcontract.ExecutiveArtifact{}, errors.New("case count differs from executive report total")
		}
	}
	input := struct {
		Report insightcontract.ExecutiveReport `json:"report"`
		Cases  []insightcontract.CaseDetail    `json:"cases,omitempty"`
	}{report, details}
	analysisBytes, err := json.Marshal(input)
	if err != nil {
		return insightcontract.ExecutiveArtifact{}, err
	}
	analysisSum := sha256.Sum256(analysisBytes)
	analysisHash := hex.EncodeToString(analysisSum[:])
	// A nova chave de request não cria outra revisão quando os bytes semânticos,
	// o modo e o renderer são iguais. O conteúdo completo (inclusive os trechos
	// permitidos que chegaram pelo leitor) entra em analysisHash.
	idemBytes, _ := json.Marshal([]string{analysisHash, req.Mode, executiveRendererVersion})
	idemSum := sha256.Sum256(idemBytes)
	idem := hex.EncodeToString(idemSum[:])
	f.mu.Lock()
	for _, saved := range f.items {
		if saved.IdempotencyKey == idem {
			f.mu.Unlock()
			return saved.Artifact, nil
		}
	}
	f.mu.Unlock()

	created := time.Now().UTC()
	data, pageCount, err := renderExecutive(ctx, report, details, req.Mode, created)
	if err != nil {
		return insightcontract.ExecutiveArtifact{}, err
	}
	if pageCount < 1 || len(data) > 100*1024*1024 {
		return insightcontract.ExecutiveArtifact{}, errors.New("executive PDF is empty or too large")
	}
	pdfSum := sha256.Sum256(data)
	contentHash := hex.EncodeToString(pdfSum[:])
	key := sequenceKey(req.SnapshotID, req.AnalysisID, req.Mode)
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, saved := range f.items {
		if saved.IdempotencyKey == idem {
			return saved.Artifact, nil
		}
	}
	f.seq[key]++
	revision := f.seq[key]
	idSeed := sha256.Sum256([]byte(key + "\x00" + fmt.Sprint(revision)))
	id := "insight-" + hex.EncodeToString(idSeed[:12])
	artifact := insightcontract.ExecutiveArtifact{
		ID: id, SnapshotID: req.SnapshotID, AnalysisID: req.AnalysisID, Mode: req.Mode, Revision: revision,
		PageCount: pageCount, SelectedCaseCount: report.TotalCases, TotalCaseCount: report.TotalCases,
		ContentHash: contentHash, AnalysisHash: analysisHash, RendererVersion: executiveRendererVersion,
		CreatedAt: created.Format(time.RFC3339), DownloadPath: "/api/v1/insights/exports/" + id + "/download",
	}
	if req.Mode == "executive" {
		artifact.SelectedCaseCount = int64(len(report.Cases))
	} else {
		artifact.SelectedCaseCount = int64(len(details))
	}
	if err := os.WriteFile(f.pdfPath(id)+".tmp", data, 0600); err != nil {
		f.seq[key]--
		return insightcontract.ExecutiveArtifact{}, err
	}
	if err := os.Rename(f.pdfPath(id)+".tmp", f.pdfPath(id)); err != nil {
		_ = os.Remove(f.pdfPath(id) + ".tmp")
		f.seq[key]--
		return insightcontract.ExecutiveArtifact{}, err
	}
	manifest := executiveManifest{Artifact: artifact, IdempotencyKey: idem}
	raw, err := json.Marshal(manifest)
	if err != nil {
		_ = os.Remove(f.pdfPath(id))
		f.seq[key]--
		return insightcontract.ExecutiveArtifact{}, err
	}
	tmp := f.manifestPath(id) + ".tmp"
	if err = os.WriteFile(tmp, raw, 0600); err == nil {
		err = os.Rename(tmp, f.manifestPath(id))
	}
	if err != nil {
		_ = os.Remove(tmp)
		_ = os.Remove(f.pdfPath(id))
		f.seq[key]--
		return insightcontract.ExecutiveArtifact{}, err
	}
	f.items[id] = manifest
	return artifact, nil
}

func (f *ExecutiveFinalizer) readAllCases(ctx context.Context, snapshotID, analysisID string) ([]insightcontract.CaseDetail, error) {
	cursor, previous := "", ""
	var expected int64 = -1
	seen := map[string]bool{}
	seenCursors := map[string]bool{}
	var out []insightcontract.CaseDetail
	for {
		page, err := f.reader.ListInsightCases(ctx, snapshotID, analysisID, cursor, executivePageSize)
		if err != nil {
			return nil, fmt.Errorf("list cases: %w", err)
		}
		if page.SnapshotID != snapshotID || page.AnalysisID != analysisID {
			return nil, errors.New("case page belongs to another snapshot or analysis")
		}
		if page.Total < 0 || len(page.Cases) > executivePageSize || (len(page.Cases) == 0 && page.NextCursor != "") {
			return nil, errors.New("case page exceeds bounds or has no progress")
		}
		if expected < 0 {
			expected = page.Total
		} else if expected != page.Total {
			return nil, errors.New("case total changed during export")
		}
		if len(page.Cases) == 0 && page.NextCursor != "" {
			return nil, errors.New("case cursor did not advance")
		}
		for _, c := range page.Cases {
			if c.ID == "" || seen[c.ID] {
				return nil, errors.New("case page contains an empty or duplicate ID")
			}
			if previous != "" && c.ID < previous {
				return nil, errors.New("case pages are not stably ordered")
			}
			seen[c.ID] = true
			previous = c.ID
			detail, err := f.reader.GetInsightCase(ctx, snapshotID, analysisID, c.ID)
			if err != nil {
				return nil, fmt.Errorf("get case %s: %w", c.ID, err)
			}
			if detail.SnapshotID != snapshotID || detail.AnalysisID != analysisID || detail.Case.ID != c.ID {
				return nil, errors.New("case detail belongs to another snapshot, analysis or case")
			}
			out = append(out, detail)
		}
		if page.NextCursor == "" {
			break
		}
		if page.NextCursor == cursor {
			return nil, errors.New("case cursor repeated or did not advance")
		}
		if page.NextCursor != "" {
			if seenCursors[page.NextCursor] {
				return nil, errors.New("case cursor repeated")
			}
			seenCursors[page.NextCursor] = true
		}
		cursor = page.NextCursor
	}
	if expected < 0 {
		expected = 0
	}
	if int64(len(out)) != expected {
		return nil, fmt.Errorf("case export incomplete: expected %d, received %d", expected, len(out))
	}
	return out, nil
}

func validateExecutiveReport(report insightcontract.ExecutiveReport) error {
	if report.Version != insightcontract.Version {
		return errors.New("executive report contract version mismatch")
	}
	if report.TotalCases < 0 || int64(len(report.Cases)) > report.TotalCases {
		return errors.New("executive case selection exceeds total")
	}
	if !oneOf(report.PublicationStatus, "ready", "exporting", "exported", "export_failed") || !oneOf(report.AnalysisStatus, "complete", "partial", "not_run", "failed") || !oneOf(report.SufficiencyStatus, "sufficient", "partial", "insufficient", "not_applicable") {
		return errors.New("executive report contains an unknown status")
	}
	if len(report.Cases) != len(report.CaseIDs) {
		return errors.New("executive cases and IDs differ in length")
	}
	seen := make(map[string]bool, len(report.Cases))
	for i, c := range report.Cases {
		if c.ID == "" || report.CaseIDs[i] != c.ID || seen[c.ID] {
			return errors.New("executive case IDs do not match selected cases")
		}
		seen[c.ID] = true
	}
	return nil
}

func oneOf(value string, allowed ...string) bool {
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
}

func (f *ExecutiveFinalizer) GetExecutiveArtifact(_ context.Context, id string) (insightcontract.ExecutiveArtifact, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	item, ok := f.items[id]
	if !ok {
		return insightcontract.ExecutiveArtifact{}, os.ErrNotExist
	}
	return item.Artifact, nil
}

// OpenExecutiveArtifact resolves only an artifact ID recorded by this exporter.
func (f *ExecutiveFinalizer) OpenExecutiveArtifact(id string) (*os.File, error) {
	f.mu.Lock()
	_, ok := f.items[id]
	f.mu.Unlock()
	if !ok {
		return nil, os.ErrNotExist
	}
	return os.Open(f.pdfPath(id))
}

func (f *ExecutiveFinalizer) pdfPath(id string) string {
	return filepath.Join(f.dir, safeName(id)+".pdf")
}
func (f *ExecutiveFinalizer) manifestPath(id string) string {
	return filepath.Join(f.dir, safeName(id)+".json")
}
func sequenceKey(snapshot, analysis, mode string) string {
	return snapshot + "\x00" + analysis + "\x00" + mode
}

func renderExecutive(ctx context.Context, report insightcontract.ExecutiveReport, details []insightcontract.CaseDetail, mode string, created time.Time) ([]byte, int, error) {
	l, err := layoutExecutive(ctx, report, details, mode, created)
	if err != nil {
		return nil, 0, err
	}
	data, err := l.encode("Visão executiva "+report.SnapshotID, "PocketCli · análise "+report.AnalysisID, created)
	return data, len(l.pages), err
}

func layoutExecutive(ctx context.Context, report insightcontract.ExecutiveReport, details []insightcontract.CaseDetail, mode string, created time.Time) (*pdfLayout, error) {
	l := newPDFLayout()
	l.heading("Visão executiva", 22, 0, false)
	l.paragraph(9.5, colorMuted, plain("PocketCli · investigação executiva"))
	modeLabel := map[string]string{"executive": "Executivo", "analysis_detail": "Detalhado"}[mode]
	for _, row := range [][2]string{{"Snapshot:", report.SnapshotID}, {"ID da análise:", report.AnalysisID}, {"Gerado:", created.Format(time.RFC3339)}, {"Apresentação:", executiveRendererVersion}, {"Modo:", modeLabel}, {"Publicação:", label(report.PublicationStatus)}, {"Estado da análise:", label(report.AnalysisStatus)}, {"Suficiência:", label(report.SufficiencyStatus)}} {
		l.paragraph(9, colorText, bold(row[0]), plain(row[1]))
	}
	l.heading(report.Headline, 15, 8, true)
	l.paragraph(10, colorText, plain(report.Summary))
	if report.SufficiencyStatus == "partial" || report.SufficiencyStatus == "insufficient" {
		l.banner(9.5, plain("Aviso: a análise não tem suficiência plena. A exportação concluída comprova a geração do arquivo, não a sustentação de todas as conclusões."))
	} else if report.AnalysisStatus == "partial" {
		l.banner(9.5, plain("Aviso: a análise foi concluída parcialmente. A suficiência é um estado separado e não elimina este limite."))
	} else if report.SufficiencyStatus == "not_applicable" {
		l.banner(9.5, plain("Não há dimensão aplicável para classificar como suficiente. A exportação concluída comprova a geração do arquivo, não suficiência."))
	}
	l.heading("Objetivos e dimensões", 14, 10, true)
	for _, q := range report.Questions {
		l.paragraph(9.5, colorText, bold(label(q.QuestionID)), plain("· "+label(q.Status)+" · "+q.Reason))
		for _, missing := range q.Missing {
			l.paragraph(8.5, colorMuted, plain("Falta: "+missing))
		}
	}
	for _, d := range report.Dimensions {
		l.paragraph(9.5, colorText, bold(label(d.Dimension)+" · "+label(d.Status)), plain("· "+d.Reason))
		if len(d.MetricIDs) > 0 {
			l.paragraph(8.2, colorMuted, plain("Métricas: "+strings.Join(d.MetricIDs, ", ")))
		}
		if len(d.CaseIDs) > 0 {
			l.paragraph(8.2, colorMuted, plain("Casos relacionados: "+strings.Join(d.CaseIDs, ", ")))
		}
		for _, missing := range d.Missing {
			l.paragraph(8.5, colorMuted, plain("Falta: "+missing))
		}
	}
	if len(report.Metrics) > 0 {
		l.heading("Métricas", 14, 8, true)
		for _, m := range report.Metrics {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			value := "não determinado"
			if m.State == "observed" && m.Value != nil {
				value = fmt.Sprintf("%g", *m.Value)
			}
			if m.Unit != "" {
				value += " " + m.Unit
			}
			denom := fmt.Sprintf(" · amostra %d", m.SampleSize)
			if m.EligibleSize != nil {
				denom += fmt.Sprintf(" de %d elegíveis", *m.EligibleSize)
			}
			if m.Numerator != nil {
				denom += fmt.Sprintf(" · numerador %d", *m.Numerator)
			}
			if m.Denominator != nil {
				denom += fmt.Sprintf(" · denominador %d", *m.Denominator)
			}
			l.paragraph(9, colorText, bold(m.Name+":"), plain(value+" · "+m.Definition+denom+" · coorte "+m.CohortID+" · método "+m.Method))
			for _, limitation := range m.Limitations {
				l.paragraph(8.2, colorMuted, plain("Limitação: "+limitation))
			}
		}
	}
	l.heading("Casos para leitura", 14, 8, true)
	cases := report.Cases
	if mode == "analysis_detail" {
		cases = make([]insightcontract.DecisionCase, len(details))
		for i := range details {
			cases[i] = details[i].Case
		}
	}
	selectedLabel := "selecionados"
	if mode == "analysis_detail" {
		selectedLabel = "incluídos no detalhe"
	}
	l.paragraph(9, colorMuted, plain(fmt.Sprintf("%d de %d casos %s.", len(cases), report.TotalCases, selectedLabel)))
	if mode == "executive" && int64(len(cases)) < report.TotalCases {
		l.banner(8.8, plain("Esta seleção é parcial. Consulte a lista completa de casos na interface local e gere a exportação detalhada para percorrer toda a análise."))
	}
	for _, c := range cases {
		renderDecisionCase(l, c)
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	if mode == "analysis_detail" {
		l.heading("Detalhe e evidências", 14, 8, true)
		for _, d := range details {
			renderCaseDetail(l, d)
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
	}
	if len(report.Limitations) > 0 {
		l.heading("Limitações e dados ausentes", 14, 8, true)
		for _, v := range report.Limitations {
			l.paragraph(9, colorText, plain("• "+v))
		}
	}
	return l, nil
}

func renderDecisionCase(l *pdfLayout, c insightcontract.DecisionCase) {
	recordHeader(l, c.ID+" · "+label(c.State))
	l.heading(c.Title, 11.5, 0, false)
	l.paragraph(9.5, colorText, plain(c.Summary))
	if len(c.ObjectiveIDs) > 0 {
		l.paragraph(8.2, colorMuted, plain("Objetivos relacionados: "+strings.Join(c.ObjectiveIDs, ", ")))
	}
	if len(c.EntityIDs) > 0 {
		l.paragraph(8.2, colorMuted, plain("Entidades: "+strings.Join(c.EntityIDs, ", ")))
	}
	for _, row := range []struct {
		name  string
		value insightcontract.Assessment
	}{{"Impacto", c.Impact}, {"Complexidade", c.Complexity}, {"Urgência", c.Urgency}, {"Confiança", c.Confidence}} {
		a := row.value
		l.paragraph(9, colorText, bold(row.name+":"), plain(label(a.State)+" · "+label(a.Level)+" · "+a.Rationale))
		if len(a.EvidenceIDs) > 0 {
			l.paragraph(8.2, colorMuted, plain("Evidências: "+strings.Join(a.EvidenceIDs, ", ")))
		}
		for _, missing := range a.Missing {
			l.paragraph(8.5, colorMuted, plain("Falta: "+missing))
		}
	}
	if c.RecommendedAlternativeID != nil {
		recommendation := *c.RecommendedAlternativeID
		for _, alternative := range c.Alternatives {
			if alternative.ID == recommendation {
				recommendation = alternative.Action
				break
			}
		}
		l.paragraph(9, colorText, bold("Recomendação:"), plain(recommendation+" · "+label(c.RecommendationBasis)))
	}
	for _, alternative := range c.Alternatives {
		l.paragraph(8.8, colorText, bold("Alternativa:"), plain(alternative.Action+" · base "+label(alternative.Basis)))
		if len(alternative.EvidenceIDs) > 0 {
			l.paragraph(8.2, colorMuted, plain("Evidências: "+strings.Join(alternative.EvidenceIDs, ", ")))
		}
		if alternative.ExpectedEffect != "" {
			l.paragraph(8.5, colorMuted, plain("Efeito esperado: "+alternative.ExpectedEffect))
		}
		for _, tradeoff := range alternative.Tradeoffs {
			l.paragraph(8.2, colorMuted, plain("Trade-off: "+tradeoff))
		}
		for _, precondition := range alternative.Preconditions {
			l.paragraph(8.2, colorMuted, plain("Pré-condição: "+precondition))
		}
	}
	for _, evidence := range c.NextEvidence {
		l.paragraph(8.5, colorMuted, plain("Próxima evidência: "+evidence))
	}
	for _, citation := range c.Citations {
		l.paragraph(8, colorMuted, plain("Evidência "+citation.EvidenceID+": "+citation.Quote))
	}
}

func renderCaseDetail(l *pdfLayout, d insightcontract.CaseDetail) {
	recordHeader(l, "Caso "+d.Case.ID)
	for _, row := range []struct {
		label string
		ids   []string
	}{
		{"Fatos", d.Case.FactCaseIDs}, {"Afirmações", d.Case.ClaimIDs}, {"Decisões", d.Case.DecisionIDs}, {"Relações", d.Case.RelationIDs}, {"Objetivos", d.Case.ObjectiveIDs},
	} {
		if len(row.ids) > 0 {
			l.paragraph(8.2, colorMuted, bold(row.label+":"), plain(strings.Join(row.ids, ", ")))
		}
	}
	for _, f := range d.Facts {
		l.paragraph(8.8, colorText, bold("Fato "+f.ID+" · "+label(f.Kind)+" / "+label(f.Classification)+":"), plain(f.Title))
		for _, fact := range f.ObservedFacts {
			l.paragraph(8.2, colorMuted, plain("Observado: "+fact))
		}
		for _, question := range f.OpenQuestions {
			l.paragraph(8.2, colorMuted, plain("Em aberto: "+question))
		}
	}
	var timeline []executiveTimelineEntry
	for _, c := range d.Claims {
		at := ""
		if c.EventAt != nil {
			at = *c.EventAt
		}
		timeline = append(timeline, newTimelineEntry(at, c.ID, "Afirmação · "+label(c.Kind)+" · "+label(c.Status), c.Text, c.Scope.Description, c.Citations))
	}
	for _, decision := range d.Decisions {
		at := ""
		if decision.DecidedAt != nil {
			at = *decision.DecidedAt
		}
		timeline = append(timeline, newTimelineEntry(at, decision.ID, "Decisão · "+label(decision.Status), decision.Question+" → "+decision.Choice+" · "+decision.Rationale, decision.Scope.Description, decision.Citations))
	}
	sort.SliceStable(timeline, func(i, j int) bool {
		if !timeline[i].ordered {
			return false
		}
		if !timeline[j].ordered {
			return true
		}
		return timeline[i].orderedAt.Before(timeline[j].orderedAt)
	})
	if len(timeline) > 0 {
		l.heading("Timeline disponível no contrato", 11, 5, true)
	}
	for _, item := range timeline {
		at := item.at
		if !item.ordered {
			at = "data/ordem desconhecida"
			if item.at != "" {
				at += " (" + item.at + ")"
			}
		}
		l.paragraph(9, colorText, bold(at+" · "+item.id+" · "+item.kind+":"), plain(item.text))
		if item.scope != "" {
			l.paragraph(8.2, colorMuted, plain("Escopo: "+item.scope))
		}
		for _, cite := range item.citations {
			l.paragraph(8.5, colorMuted, plain("Evidência "+cite.EvidenceID+": "+cite.Quote))
		}
	}
	for _, relation := range d.ClaimRelations {
		l.paragraph(8.5, colorMuted, plain(fmt.Sprintf("Relação entre afirmações %s: %s → %s · %s · %s", label(relation.Kind), relation.FromClaimID, relation.ToClaimID, label(relation.Status), relation.Reason)))
	}
	for _, relation := range d.Relations {
		l.paragraph(8.5, colorMuted, plain(fmt.Sprintf("Relação %s: %s → %s · %s", label(relation.Kind), relation.FromDecisionID, relation.ToDecisionID, label(relation.Status))))
	}
	for _, e := range d.Evidence {
		l.paragraph(8.5, colorText, bold("Evidência "+e.ID+" · "+e.Source+" · "+e.Locator), plain("· observada "+label(e.ObservedAt)+" · evento "+optional(e.EventAt)+" · fonte atualizada "+optional(e.SourceUpdatedAt)+" · "+label(e.ContentStatus)+" / "+label(e.IntegrityStatus)))
		l.paragraph(7.8, colorMuted, plain("Hash de conteúdo: "+label(e.ContentHash)+" · hash do trecho: "+label(e.ExcerptHash)+" · sanitização: "+label(e.SanitizationVersion)))
		if e.Excerpt != "" {
			l.paragraph(8.5, colorText, plain(e.Excerpt))
		}
		if e.Redacted {
			l.paragraph(8, colorMuted, plain("Trecho contém redação."))
		}
		if e.Truncated {
			l.paragraph(8, colorMuted, plain("Trecho truncado."))
		}
		if e.URL != nil && verifiedExternalURL(*e.URL) {
			l.paragraph(8.2, colorMuted, plain("Fonte: "+*e.URL))
		}
	}
	for _, entity := range d.Entities {
		l.paragraph(8.2, colorMuted, plain(fmt.Sprintf("Entidade %s · %s · %s · %s · estado temporal %s", entity.ID, label(entity.Kind), entity.Identifier, entity.Title, label(entity.StateTemporalStatus))))
	}
}

func newTimelineEntry(at, id, kind, text, scope string, citations []insightcontract.Citation) executiveTimelineEntry {
	entry := executiveTimelineEntry{at: at, id: id, kind: kind, text: text, scope: scope, citations: citations}
	if at != "" {
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
			if parsed, err := time.Parse(layout, at); err == nil {
				entry.orderedAt, entry.ordered = parsed, true
				break
			}
		}
	}
	return entry
}

func optional(value *string) string {
	if value == nil || strings.TrimSpace(*value) == "" {
		return "não determinado"
	}
	return *value
}

func label(s string) string {
	if s == "" {
		return "não determinado"
	}
	if translated, ok := labelsPT[s]; ok {
		return translated
	}
	words := strings.ReplaceAll(s, "_", " ")
	return words
}

var labelsPT = map[string]string{
	"ready": "pronta", "exporting": "em exportação", "exported": "exportada", "export_failed": "falha na exportação",
	"complete": "concluída", "partial": "parcial", "not_run": "não executada", "failed": "falhou", "sufficient": "suficiente", "insufficient": "insuficiente", "not_applicable": "não aplicável",
	"on_track": "no esperado", "at_risk": "em risco", "off_track": "fora do esperado", "indeterminate": "indeterminada", "assessed": "avaliado", "critical": "crítico", "high": "alto", "medium": "médio", "low": "baixo", "unknown": "desconhecido",
	"actionable": "acionável", "monitor": "monitorar", "needs_evidence": "precisa de evidência", "resolved": "resolvido", "source": "fonte", "inference": "inferência", "hypothesis": "hipótese",
	"history_scope": "Alcance histórico", "decision_lineage": "Linha de decisões", "delivery_trace": "Trilha de entrega", "flow_timing": "Tempo de fluxo", "quality_outcome": "Resultado de qualidade", "cost_actual": "Custo real", "objective_status": "Estado do objetivo",
	"direction": "Direção", "delivery": "Entrega", "flow": "Fluxo", "quality": "Qualidade", "cost": "Custo", "capacity": "Capacidade", "solution": "Solução", "dependency": "dependência", "scope": "escopo", "estimate": "estimativa", "outcome": "resultado", "rework": "retrabalho",
	"planned_dependency": "dependência planejada", "active_impediment": "impedimento ativo", "commitment_at_risk": "compromisso em risco", "shared_blocker": "bloqueio compartilhado", "cycle": "ciclo", "unclassified": "não classificado",
	"supported": "sustentada", "inferred": "inferida", "unverified": "não verificada", "disputed": "contestada", "proposed": "proposta", "accepted": "aceita", "implemented": "implementada", "superseded": "superada", "revoked": "revogada", "conflicting": "conflitante",
	"available": "disponível", "metadata_only": "somente metadados", "unavailable": "indisponível", "redacted_all": "conteúdo todo redigido", "integrity_error": "erro de integridade", "verified": "verificada", "mismatch": "divergente", "missing": "ausente",
}

func verifiedExternalURL(raw string) bool {
	u, err := url.ParseRequestURI(strings.TrimSpace(raw))
	return err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil
}

var _ insightcontract.ExecutiveExporter = (*ExecutiveFinalizer)(nil)

// Keep stable rendering for deterministic field order in callers and tests.
