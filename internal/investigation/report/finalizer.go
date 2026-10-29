package report

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	// Executive carries the decisions of a consolidated report; nil when the
	// snapshot has no finished executive analysis.
	Executive *ExecutiveBundle
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
	reader          contract.ReportReader
	annotations     AnnotationReader
	editorial       EditorialReader
	queries         QueryReader
	dir             string
	demo            bool
	rendererVersion string
	timeout         time.Duration
	executive       ExecutiveSource
	mu              sync.Mutex
	artifacts       map[string]contract.ReportArtifact
	paths           map[string]string
	revisions       map[string]int
}

// New prepares the in-process PDF renderer. It needs no browser or external
// tool; it only fails when the report directory cannot be prepared.
func New(reader contract.ReportReader, dir string, demo bool, annotationReader AnnotationReader, editorialReader EditorialReader, queryReader QueryReader) (*Finalizer, error) {
	if reader == nil {
		return nil, errors.New("report reader is required")
	}
	if dir == "" {
		return nil, errors.New("report directory is required")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("ERR_PDF_DEPENDENCY: diretório de relatórios indisponível (%v)", err)
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return nil, fmt.Errorf("ERR_PDF_DEPENDENCY: diretório de relatórios indisponível (%v)", err)
	}
	f := &Finalizer{reader: reader, annotations: annotationReader, editorial: editorialReader, queries: queryReader, dir: dir, demo: demo, rendererVersion: RendererVersion, timeout: 120 * time.Second, artifacts: map[string]contract.ReportArtifact{}, paths: map[string]string{}, revisions: map[string]int{}}
	if err := f.loadManifests(); err != nil {
		return nil, err
	}
	return f, nil
}

// SetExecutiveSource lets consolidated reports include the decision analysis.
func (f *Finalizer) SetExecutiveSource(source ExecutiveSource) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.executive = source
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
	if in.Consolidated {
		in.IncludeApprovedEditorial = true // the consolidated record carries the accepted text
	}
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
	jobCtx, cancel := context.WithTimeout(ctx, f.timeout)
	defer cancel()
	warnings := []string{}
	if in.Consolidated {
		f.mu.Lock()
		source := f.executive
		f.mu.Unlock()
		if source == nil {
			warnings = append(warnings, "Consolidado sem decisões: a análise executiva não está disponível nesta sessão.")
		} else {
			bundle, ok, bundleErr := source.Bundle(jobCtx, in.SnapshotID)
			switch {
			case bundleErr != nil:
				return contract.ReportArtifact{}, fmt.Errorf("ERR_PDF_FAILED: decisões da análise executiva ilegíveis (%v)", bundleErr)
			case !ok:
				warnings = append(warnings, "Consolidado sem decisões: este snapshot ainda não tem análise executiva. Rode uma análise e gere o consolidado de novo.")
			default:
				doc.Executive = &bundle
			}
		}
	}
	data, pageCount, err := renderReport(jobCtx, doc, time.Now())
	if err != nil {
		if ctxErr := jobCtx.Err(); ctxErr != nil {
			return contract.ReportArtifact{}, fmt.Errorf("ERR_PDF_FAILED: geração interrompida (%v)", ctxErr)
		}
		return contract.ReportArtifact{}, fmt.Errorf("ERR_PDF_FAILED: %v", err)
	}
	if pageCount < 1 || len(data) > 100*1024*1024 {
		return contract.ReportArtifact{}, errors.New("ERR_REPORT_INCOMPLETE: PDF sem páginas ou acima de 100 MiB")
	}
	tempPDF := filepath.Join(jobDir, "report.pdf")
	if err := os.WriteFile(tempPDF, data, 0600); err != nil {
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
	artifact := contract.ReportArtifact{ID: id, SnapshotID: in.SnapshotID, Revision: revision, Status: "completed", SHA256: digest, DownloadPath: "/api/v1/reports/" + id + "/download", FindingCount: int64(findingCount), PageCount: pageCount, Warnings: warnings, Consolidated: in.Consolidated}
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
