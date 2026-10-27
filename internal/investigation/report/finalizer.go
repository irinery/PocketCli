package report

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"pocketcli/internal/investigation/annotations"
	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/editorial"
	"pocketcli/internal/investigation/queries"
)

const reportTemplate = `<!doctype html><html lang="pt-BR"><head><meta charset="utf-8"><style>
@page{size:A4 portrait;margin:16mm}body{font:10.5pt/1.45 Arial,sans-serif;color:#17212f}h1{font-size:24pt}h2{font-size:16pt;border-bottom:1px solid #ccd4df;padding-bottom:4pt;margin-top:22pt}h3{font-size:12pt;margin-bottom:3pt}.muted{color:#586477}.demo{border:1px solid #b87910;background:#fff5e2;padding:8pt}.metadata{display:grid;grid-template-columns:1fr 1fr;gap:5pt}article{border:1px solid #dce2e9;border-radius:5pt;padding:9pt;margin:8pt 0;break-inside:avoid}pre{white-space:pre-wrap;overflow-wrap:anywhere;font:8pt/1.35 monospace}table{border-collapse:collapse;width:100%;font-size:8pt}th,td{text-align:left;vertical-align:top;border-bottom:1px solid #dce2e9;padding:5pt;overflow-wrap:anywhere}thead{display:table-header-group}a{color:#263e65}.page-break{break-before:page}.finding{break-inside:auto}.id{font:8pt monospace;color:#586477}.foot{font-size:8pt;color:#697486;margin-top:8pt}
</style></head><body><h1>Relatório da investigação</h1><p class="demo">{{if .Demo}}DEMO · dados fictícios para apresentação; não representam uma investigação real.{{else}}Relatório determinístico da investigação.{{end}}</p><div class="metadata"><div><b>Investigação:</b> {{.Snapshot.InvestigationID}}</div><div><b>Snapshot:</b> {{.Snapshot.ID}}</div><div><b>Janela:</b> {{.Snapshot.WindowStart}} — {{.Snapshot.WindowEnd}}</div><div><b>Status:</b> {{.Snapshot.Status}}</div><div><b>Cobertura:</b> {{.CoverageStatus}}</div><div><b>Gerado em:</b> {{.GeneratedAt}}</div><div><b>Dataset hash:</b> {{.Snapshot.DatasetHash}}</div><div><b>Regras:</b> {{.Snapshot.RulesetVersion}}</div><div><b>Mapeamento:</b> {{.Snapshot.MappingVersion}}</div></div>
<h2>1. Resumo determinístico</h2><p>Este relatório contém todos os registros canônicos recebidos do leitor do snapshot. Estados desconhecidos e cobertura parcial permanecem explicitados; a camada editorial não altera esses dados.</p><p><b>Achados:</b> {{.FindingCount}} · <b>Registros:</b> {{.RecordCount}} · <b>Cobertura:</b> {{.CoverageStatus}} ({{if .Snapshot.Coverage}}{{len .Snapshot.Coverage}} coleções{{else}}sem coleções informadas{{end}})</p>
<h2>2. Cobertura — {{.CoverageStatus}}</h2>{{if .Snapshot.Coverage}}<table><thead><tr><th>Fonte</th><th>Recurso</th><th>Coleção</th><th>Estado</th><th>Observado / esperado</th><th>Motivo</th></tr></thead><tbody>{{range .Snapshot.Coverage}}<tr><td>{{.Source}}</td><td>{{.ResourceID}}</td><td>{{.Collection}}</td><td>{{.Status}}</td><td>{{.Observed}} / {{intPtr .Expected}}</td><td>{{.Reason}}</td></tr>{{end}}</tbody></table>{{else}}<p>Não determinada pelo contrato recebido.</p>{{end}}
<h2>3. Registros da análise</h2>{{range .Records}}<article class="{{if eq .Kind "finding"}}finding{{end}}"><div class="id">{{.Kind}} · {{recordID .}}</div>{{if .Finding}}<h3>{{.Finding.Title}}</h3><p><b>{{.Finding.Severity}}</b> · regra {{.Finding.RuleID}} {{.Finding.RuleVersion}} · sujeito {{.Finding.SubjectID}}</p><p>{{.Finding.DeterministicText}}</p><p><b>Evidências:</b> {{range $i,$v := .Finding.EvidenceIDs}}{{if $i}}, {{end}}{{$v}}{{end}}</p><p><b>Status da evidência:</b> {{.Finding.EvidenceStatus}}. <b>Limitações:</b> {{range $i,$v := .Finding.Limitations}}{{if $i}}; {{end}}{{$v}}{{end}}</p>{{end}}{{if .Component}}<p><b>Componente:</b> {{.Component.Name}} · {{.Component.Repository}}/{{.Component.Path}} · implementação {{.Component.ImplementationStatus}} · entrega {{.Component.DeploymentStatus}} · cobertura {{.Component.Coverage}}</p>{{end}}{{if .Metric}}<p><b>Métrica:</b> {{.Metric.Name}} = {{if eq .Metric.Value.Kind "null"}}não calculado{{else}}{{.Metric.Value.Value}} {{.Metric.Unit}}{{end}} · amostra {{.Metric.SampleSize}} / {{intPtr .Metric.EligibleSize}}</p><p>{{.Metric.Definition}}. {{range .Metric.Limitations}}{{.}} {{end}}</p>{{end}}{{if .Coverage}}<p><b>Cobertura:</b> {{.Coverage.Source}} / {{.Coverage.ResourceID}} / {{.Coverage.Collection}}: {{.Coverage.Status}} ({{.Coverage.Observed}} / {{if .Coverage.Expected}}{{.Coverage.Expected}}{{else}}desconhecido{{end}}). {{.Coverage.Reason}}</p>{{end}}{{if .Evidence}}<p><b>Evidência {{.Evidence.ID}}</b> · {{.Evidence.Source}} · {{.Evidence.Locator}} · observada {{.Evidence.ObservedAt}}</p><p>{{.Evidence.Preview}}</p><p>SHA-256 {{.Evidence.SHA256}}</p>{{end}}{{if .Link}}<p><b>Vínculo:</b> {{.Link.SourceID}} — {{.Link.Relation}} → {{.Link.TargetID}} · {{.Link.Status}} · evidência {{.Link.EvidenceID}}</p>{{end}}{{if .Stage}}<p><b>Etapa:</b> {{.Stage.Stage}} · sujeito {{.Stage.SubjectID}} · início {{.Stage.StartedAt}} · fim {{.Stage.EndedAt}} · duração válida {{.Stage.DurationValid}}</p>{{end}}{{if .Estimate}}<p><b>Estimativa:</b> issue {{.Estimate.IssueID}} · {{floatPtr .Estimate.Before}} → {{floatPtr .Estimate.After}} · {{.Estimate.Timing}} · {{.Estimate.EventAt}}</p>{{end}}{{if .Rework}}<p><b>Retrabalho:</b> {{.Rework.Kind}} · {{.Rework.EvidenceStatus}} · causa {{.Rework.CauseStatus}} · {{.Rework.EventAt}}</p>{{end}}{{if .Delivery}}<p><b>Artefato de entrega:</b> {{.Delivery.ComponentID}} · ambiente {{.Delivery.Environment}} · proveniência {{.Delivery.ProvenanceStatus}} · {{.Delivery.ObservedAt}}</p>{{end}}</article>{{end}}
<h2>10. Comentários do operador</h2>{{if .Annotations}}{{range .Annotations}}<article><div class="id">{{.TargetType}} · {{.TargetID}} · revisão {{.Revision}} · {{.Status}}</div><p>{{.Body}}</p><p class="muted">{{.OperatorLabel}} · {{.UpdatedAt}}</p></article>{{end}}{{else}}<p>Nenhum comentário registrado na revisão congelada {{.AnnotationRevision}}.</p>{{end}}
{{if .Editorial}}<h2>11. Redação assistida aceita</h2><p>Camada editorial revisada por operador; não substitui os achados determinísticos acima.</p>{{range .Editorial}}<article><div class="id">{{.ModelID}} · payload {{.PayloadHash}} · aceito em {{.ReviewedAt}}</div>{{range .Sections}}<h3>{{.FindingID}}</h3><p>{{.ImprovedText}}</p><p>Evidências: {{range $i,$v := .EvidenceIDs}}{{if $i}}, {{end}}{{$v}}{{end}}</p>{{end}}</article>{{end}}{{end}}
<h2>12. Consultas SQL</h2>{{if .Executions}}{{range .Executions}}<article><div class="id">{{.CreatedAt}} · {{.Status}} · {{.QueryID}} · {{.SnapshotID}}</div><p>Hash {{.QueryHash}} · truncado {{.Truncated}}</p><pre>{{.SQL}}</pre>{{if .Error}}<p>Erro: {{.Error}}</p>{{end}}</article>{{end}}{{else}}<p>Nenhuma consulta executada neste snapshot.</p>{{end}}{{range .SavedQueries}}<article><h3>Consulta salva: {{.Name}}</h3><p>Incluída no relatório: {{.IncludeInReport}} · revisão {{.Revision}} · hash {{.QueryHash}}</p>{{if .IncludeInReport}}<pre>{{.SQL}}</pre><pre>{{printf "%+v" .Result}}</pre>{{else}}<p>Resultado excluído por decisão do operador; a execução permanece no índice acima.</p>{{end}}</article>{{end}}
<h2>14. Revisão e proveniência</h2><p>Snapshot {{.Snapshot.ID}} · dataset {{.Snapshot.DatasetHash}} · relatório revision {{.Revision}} · revisão global dos comentários {{.AnnotationRevision}} · gerado {{.GeneratedAt}} · renderer {{.RendererVersion}}.</p><p class="foot">PDF congelado. Abrir o HTML vivo ou encerrar o servidor não altera este arquivo.</p></body></html>`

type model struct {
	Snapshot           contract.Snapshot
	CoverageStatus     string
	Records            []contract.ReportRecord
	GeneratedAt        string
	FindingCount       int
	RecordCount        int
	Revision           int
	RendererVersion    string
	Demo               bool
	Annotations        []annotations.Annotation
	AnnotationRevision int64
	Editorial          []editorial.Draft
	Executions         []queries.Execution
	SavedQueries       []queries.SavedQuery
}

func coverageLabel(items []contract.Coverage) string {
	if len(items) == 0 {
		return "não determinada"
	}
	for _, item := range items {
		if item.Status != "complete" && item.Status != "not_selected" {
			return "parcial"
		}
	}
	return "completa"
}

type AnnotationReader interface {
	At(snapshot string, cutoff int64) ([]annotations.Annotation, int64)
}
type EditorialReader interface {
	Accepted(snapshot string) []editorial.Draft
}
type QueryReader interface {
	AllHistory(snapshot string) []queries.Execution
	List(snapshot string) []queries.SavedQuery
}
type Finalizer struct {
	reader                 contract.ReportReader
	annotations            AnnotationReader
	editorial              EditorialReader
	queries                QueryReader
	dir, chromium, pdfinfo string
	demo                   bool
	rendererVersion        string
	timeout                time.Duration
	mu                     sync.Mutex
	artifacts              map[string]contract.ReportArtifact
	paths                  map[string]string
	revisions              map[string]int
}

func New(reader contract.ReportReader, dir, chromium string, demo bool, annotationReader AnnotationReader, editorialReader EditorialReader, queryReader QueryReader) (*Finalizer, error) {
	if reader == nil {
		return nil, errors.New("report reader is required")
	}
	if dir == "" {
		return nil, errors.New("report directory is required")
	}
	if chromium == "" {
		chromium, _ = exec.LookPath("chromium")
		if chromium == "" {
			chromium, _ = exec.LookPath("chromium-browser")
		}
		if chromium == "" {
			chromium, _ = exec.LookPath("google-chrome")
		}
		if chromium == "" {
			for _, candidate := range []string{"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", "/Applications/Chromium.app/Contents/MacOS/Chromium"} {
				if _, e := os.Stat(candidate); e == nil {
					chromium = candidate
					break
				}
			}
		}
	}
	pdfinfo, _ := exec.LookPath("pdfinfo")
	if chromium == "" {
		return nil, errors.New("ERR_PDF_DEPENDENCY: instale Chromium ou Google Chrome localmente")
	}
	resolvedChromium, err := exec.LookPath(chromium)
	if err != nil {
		return nil, errors.New("ERR_PDF_DEPENDENCY: executável Chromium configurado não encontrado ou sem permissão")
	}
	chromium = resolvedChromium
	if pdfinfo == "" {
		return nil, errors.New("ERR_PDF_DEPENDENCY: instale Poppler para validar páginas (pdfinfo)")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return nil, err
	}
	version := "Chromium (versão desconhecida)"
	if out, e := exec.Command(chromium, "--version").Output(); e == nil {
		version = strings.TrimSpace(string(out))
	}
	f := &Finalizer{reader: reader, annotations: annotationReader, editorial: editorialReader, queries: queryReader, dir: dir, chromium: chromium, pdfinfo: pdfinfo, demo: demo, rendererVersion: version, timeout: 120 * time.Second, artifacts: map[string]contract.ReportArtifact{}, paths: map[string]string{}, revisions: map[string]int{}}
	if err := f.loadManifests(); err != nil {
		return nil, err
	}
	return f, nil
}

func (f *Finalizer) loadManifests() error {
	entries, err := os.ReadDir(f.dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(f.dir, entry.Name())
		raw, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		var a contract.ReportArtifact
		if json.Unmarshal(raw, &a) != nil || a.ID == "" || a.SnapshotID == "" || a.Revision < 1 {
			continue
		}
		pdf := filepath.Join(f.dir, fmt.Sprintf("%s-r%d.pdf", safeName(a.SnapshotID), a.Revision))
		if _, e = os.Stat(pdf); e != nil {
			continue
		}
		f.artifacts[a.ID] = a
		f.paths[a.ID] = pdf
		if a.Revision > f.revisions[a.SnapshotID] {
			f.revisions[a.SnapshotID] = a.Revision
		}
	}
	return nil
}
func (f *Finalizer) Finalize(ctx context.Context, in contract.FinalizeRequest) (contract.ReportArtifact, error) {
	snapshot, err := f.reader.GetSnapshot(ctx, in.SnapshotID)
	if err != nil {
		return contract.ReportArtifact{}, err
	}
	records := make([]contract.ReportRecord, 0)
	cursor := ""
	var expected int64 = -1
	seen := map[string]bool{}
	findingCount := 0
	lastKey := ""
	for {
		page, e := f.reader.ReadReportPage(ctx, in.SnapshotID, cursor, 200)
		if e != nil {
			return contract.ReportArtifact{}, e
		}
		if page.SnapshotID != in.SnapshotID {
			return contract.ReportArtifact{}, errors.New("ERR_REPORT_INCOMPLETE: página retornou snapshot divergente")
		}
		if expected < 0 {
			expected = page.TotalRecords
		} else if expected != page.TotalRecords {
			return contract.ReportArtifact{}, errors.New("ERR_REPORT_INCOMPLETE: total mudou durante leitura")
		}
		for _, r := range page.Records {
			if !validRecord(r) {
				return contract.ReportArtifact{}, errors.New("ERR_REPORT_INCOMPLETE: registro não possui exatamente o payload esperado")
			}
			id := recordID(r)
			if id == "" || seen[r.Kind+":"+id] {
				return contract.ReportArtifact{}, errors.New("ERR_REPORT_INCOMPLETE: registro sem ID ou duplicado")
			}
			seen[r.Kind+":"+id] = true
			key := r.Kind + ":" + id
			if lastKey != "" && key < lastKey {
				return contract.ReportArtifact{}, errors.New("ERR_REPORT_INCOMPLETE: registros fora da ordem estável")
			}
			lastKey = key
			if r.Kind == "finding" {
				findingCount++
			}
			records = append(records, r)
		}
		if page.NextCursor == "" {
			break
		}
		if page.NextCursor == cursor || len(page.Records) == 0 {
			return contract.ReportArtifact{}, errors.New("ERR_REPORT_INCOMPLETE: cursor de paginação não avançou")
		}
		cursor = page.NextCursor
	}
	if int64(len(records)) != expected {
		return contract.ReportArtifact{}, fmt.Errorf("ERR_REPORT_INCOMPLETE: esperados %d registros, recebidos %d", expected, len(records))
	}
	f.mu.Lock()
	f.revisions[in.SnapshotID]++
	revision := f.revisions[in.SnapshotID]
	f.mu.Unlock()
	jobDir, err := os.MkdirTemp(f.dir, "report-job-")
	if err != nil {
		return contract.ReportArtifact{}, err
	}
	defer os.RemoveAll(jobDir)
	_ = os.Chmod(jobDir, 0700)
	annotationRows := []annotations.Annotation{}
	annotationRevision := in.AnnotationRevision
	if f.annotations != nil {
		annotationRows, annotationRevision = f.annotations.At(in.SnapshotID, in.AnnotationRevision)
	}
	var editorialRows []editorial.Draft
	if in.IncludeApprovedEditorial && f.editorial != nil {
		editorialRows = f.editorial.Accepted(in.SnapshotID)
	}
	executions := []queries.Execution{}
	savedQueries := []queries.SavedQuery{}
	if f.queries != nil {
		executions = f.queries.AllHistory(in.SnapshotID)
		savedQueries = f.queries.List(in.SnapshotID)
	}
	doc := model{Snapshot: snapshot, CoverageStatus: coverageLabel(snapshot.Coverage), Records: records, GeneratedAt: time.Now().UTC().Format(time.RFC3339), FindingCount: findingCount, RecordCount: len(records), Revision: revision, RendererVersion: f.rendererVersion, Demo: f.demo, Annotations: annotationRows, AnnotationRevision: annotationRevision, Editorial: editorialRows, Executions: executions, SavedQueries: savedQueries}
	tmpl, err := template.New("report").Funcs(template.FuncMap{"recordID": recordID, "intPtr": formatIntPtr, "floatPtr": formatFloatPtr}).Parse(reportTemplate)
	if err != nil {
		return contract.ReportArtifact{}, err
	}
	htmlPath := filepath.Join(jobDir, "report.html")
	hf, err := os.OpenFile(htmlPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return contract.ReportArtifact{}, err
	}
	if err = tmpl.Execute(hf, doc); err != nil {
		_ = hf.Close()
		return contract.ReportArtifact{}, err
	}
	if err = hf.Close(); err != nil {
		return contract.ReportArtifact{}, err
	}
	tempPDF := filepath.Join(jobDir, "report.pdf")
	profile := filepath.Join(jobDir, "profile")
	_ = os.Mkdir(profile, 0700)
	fileURL := (&url.URL{Scheme: "file", Path: htmlPath}).String()
	jobCtx, cancel := context.WithTimeout(ctx, f.timeout)
	defer cancel()
	args := []string{"--headless", "--disable-background-networking", "--host-resolver-rules=MAP * ~NOTFOUND", "--no-pdf-header-footer", "--user-data-dir=" + profile, "--print-to-pdf=" + tempPDF, fileURL}
	cmd := exec.CommandContext(jobCtx, f.chromium, args...)
	stopProcessGroupOnCancel(cmd)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + profile}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return contract.ReportArtifact{}, fmt.Errorf("ERR_PDF_FAILED: Chromium falhou (%s)", safeOutput(string(output)))
	}
	info, err := os.Stat(tempPDF)
	if err != nil || info.Size() < 8 || info.Size() > 100*1024*1024 {
		return contract.ReportArtifact{}, errors.New("ERR_REPORT_INCOMPLETE: PDF ausente, vazio ou acima de 100 MiB")
	}
	file, err := os.Open(tempPDF)
	if err != nil {
		return contract.ReportArtifact{}, err
	}
	signature := make([]byte, 5)
	_, err = io.ReadFull(file, signature)
	_ = file.Close()
	if err != nil || string(signature) != "%PDF-" {
		return contract.ReportArtifact{}, errors.New("ERR_PDF_FAILED: assinatura PDF inválida")
	}
	pageCount := 0
	if f.pdfinfo != "" {
		out, e := exec.CommandContext(jobCtx, f.pdfinfo, tempPDF).Output()
		if e == nil {
			re := regexp.MustCompile(`(?m)^Pages:\s+(\d+)`)
			m := re.FindStringSubmatch(string(out))
			if len(m) == 2 {
				pageCount, _ = strconv.Atoi(m[1])
			}
		}
	}
	if pageCount < 1 {
		return contract.ReportArtifact{}, errors.New("ERR_REPORT_INCOMPLETE: não foi possível validar páginas do PDF; instale Poppler (pdfinfo)")
	}
	data, err := os.ReadFile(tempPDF)
	if err != nil {
		return contract.ReportArtifact{}, err
	}
	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])
	id := fmt.Sprintf("report-%s-r%d", safeName(in.SnapshotID), revision)
	name := fmt.Sprintf("%s-r%d.pdf", safeName(in.SnapshotID), revision)
	final := filepath.Join(f.dir, name)
	if err = os.Rename(tempPDF, final); err != nil {
		return contract.ReportArtifact{}, err
	}
	artifact := contract.ReportArtifact{ID: id, SnapshotID: in.SnapshotID, Revision: revision, Status: "completed", SHA256: digest, DownloadPath: "/api/v1/reports/" + id + "/download", FindingCount: int64(findingCount), PageCount: pageCount, Warnings: []string{}}
	manifest, err := json.Marshal(artifact)
	if err != nil {
		_ = os.Remove(final)
		return contract.ReportArtifact{}, err
	}
	manifestPath := filepath.Join(f.dir, fmt.Sprintf("%s-r%d.json", safeName(in.SnapshotID), revision))
	tempManifest := manifestPath + ".tmp"
	if err = os.WriteFile(tempManifest, manifest, 0600); err != nil {
		_ = os.Remove(final)
		return contract.ReportArtifact{}, err
	}
	if err = os.Rename(tempManifest, manifestPath); err != nil {
		_ = os.Remove(tempManifest)
		_ = os.Remove(final)
		return contract.ReportArtifact{}, err
	}
	f.mu.Lock()
	f.artifacts[id] = artifact
	f.paths[id] = final
	f.mu.Unlock()
	return artifact, nil
}
func recordID(r contract.ReportRecord) string {
	switch r.Kind {
	case "finding":
		if r.Finding != nil {
			return r.Finding.ID
		}
	case "metric":
		if r.Metric != nil {
			return r.Metric.ID
		}
	case "coverage":
		if r.Coverage != nil {
			return r.Coverage.Source + "/" + r.Coverage.ResourceID + "/" + r.Coverage.Collection
		}
	case "evidence":
		if r.Evidence != nil {
			return r.Evidence.ID
		}
	case "component":
		if r.Component != nil {
			return r.Component.ID
		}
	case "link":
		if r.Link != nil {
			return r.Link.ID
		}
	case "stage":
		if r.Stage != nil {
			return r.Stage.ID
		}
	case "estimate":
		if r.Estimate != nil {
			return r.Estimate.ID
		}
	case "rework":
		if r.Rework != nil {
			return r.Rework.ID
		}
	case "delivery":
		if r.Delivery != nil {
			return r.Delivery.ID
		}
	}
	return ""
}
func validRecord(r contract.ReportRecord) bool {
	count := 0
	want := ""
	if r.Component != nil {
		count++
		want = "component"
	}
	if r.Link != nil {
		count++
		want = "link"
	}
	if r.Stage != nil {
		count++
		want = "stage"
	}
	if r.Estimate != nil {
		count++
		want = "estimate"
	}
	if r.Rework != nil {
		count++
		want = "rework"
	}
	if r.Delivery != nil {
		count++
		want = "delivery"
	}
	if r.Finding != nil {
		count++
		want = "finding"
	}
	if r.Metric != nil {
		count++
		want = "metric"
	}
	if r.Coverage != nil {
		count++
		want = "coverage"
	}
	if r.Evidence != nil {
		count++
		want = "evidence"
	}
	return count == 1 && want == r.Kind
}
func safeOutput(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 300 {
		s = s[:300]
	}
	return strings.ReplaceAll(strings.ReplaceAll(s, "\n", " "), "\r", " ")
}
func safeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "snapshot"
	}
	return b.String()
}
func (f *Finalizer) Get(id string) (contract.ReportArtifact, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.artifacts[id]
	if !ok {
		return contract.ReportArtifact{}, os.ErrNotExist
	}
	return a, nil
}
func (f *Finalizer) List(snapshot string) []contract.ReportArtifact {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []contract.ReportArtifact{}
	for _, a := range f.artifacts {
		if snapshot == "" || a.SnapshotID == snapshot {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SnapshotID == out[j].SnapshotID {
			return out[i].Revision > out[j].Revision
		}
		return out[i].SnapshotID < out[j].SnapshotID
	})
	return out
}
func (f *Finalizer) Open(id string) (*os.File, error) {
	f.mu.Lock()
	path := f.paths[id]
	f.mu.Unlock()
	if path == "" {
		return nil, os.ErrNotExist
	}
	return os.Open(path)
}

func formatIntPtr(v *int64) string {
	if v == nil {
		return "desconhecido"
	}
	return strconv.FormatInt(*v, 10)
}
func formatFloatPtr(v *float64) string {
	if v == nil {
		return "desconhecido"
	}
	return strconv.FormatFloat(*v, 'f', -1, 64)
}
