package report_test

import (
	"bytes"
	"compress/zlib"
	"context"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"testing"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/report"
	"pocketcli/internal/investigation/web/mock"
)

// The in-process renderer produces a structurally valid PDF for every demo
// scenario, with no browser, Poppler or network involved.
func TestPDFWithFixture(t *testing.T) {
	for _, test := range []struct {
		scenario string
		findings int64
	}{{"complete", 2}, {"partial", 2}, {"large_report", 237}} {
		t.Run(test.scenario, func(t *testing.T) {
			reader := mock.NewScenario(test.scenario)
			f, err := report.New(reader, t.TempDir(), true, nil, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			artifact, err := f.Finalize(context.Background(), contract.FinalizeRequest{SnapshotID: "snap-demo-001"})
			if err != nil {
				t.Fatal(err)
			}
			if artifact.Status != "completed" || artifact.FindingCount != test.findings || artifact.PageCount < 1 {
				t.Fatalf("artefato incompleto: %+v", artifact)
			}
			file, err := f.Open(artifact.ID)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			data, err := io.ReadAll(file)
			if err != nil {
				t.Fatal(err)
			}
			pages, content := checkPDFStructure(t, data)
			if pages != artifact.PageCount {
				t.Fatalf("PageCount=%d, PDF tem %d páginas", artifact.PageCount, pages)
			}
			if test.scenario == "large_report" && pages < 2 {
				t.Fatalf("relatório grande coube em %d página", pages)
			}
			// Accented text is written in WinAnsi, e.g. "ó" as byte 0xF3.
			for _, want := range [][]byte{[]byte("Relat\xf3rio da investiga\xe7\xe3o"), []byte("snap-demo-001"), []byte(fmt.Sprintf("de %d", pages))} {
				if !bytes.Contains(content, want) {
					t.Fatalf("conteúdo sem %q", want)
				}
			}
		})
	}
}

func TestCancelledContextStopsPDFGeneration(t *testing.T) {
	f, err := report.New(mock.NewScenario("large_report"), t.TempDir(), true, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.Finalize(ctx, contract.FinalizeRequest{SnapshotID: "snap-demo-001"}); err == nil {
		t.Fatal("geração com contexto cancelado deveria falhar")
	}
	if got := f.List("snap-demo-001"); len(got) != 0 {
		t.Fatalf("PDF cancelado foi publicado: %+v", got)
	}
}

// checkPDFStructure validates the xref table, the page tree and every
// content stream, returning the page count and the decompressed content.
func checkPDFStructure(t *testing.T, data []byte) (int, []byte) {
	t.Helper()
	if !bytes.HasPrefix(data, []byte("%PDF-1.4\n")) || !bytes.HasSuffix(data, []byte("%%EOF\n")) {
		t.Fatal("cabeçalho ou final PDF inválido")
	}
	m := regexp.MustCompile(`startxref\n(\d+)\n%%EOF\n$`).FindSubmatch(data)
	if m == nil {
		t.Fatal("startxref ausente")
	}
	xref, _ := strconv.Atoi(string(m[1]))
	head := regexp.MustCompile(`^xref\n0 (\d+)\n`).FindSubmatch(data[xref:])
	if head == nil {
		t.Fatalf("tabela xref ausente no offset %d", xref)
	}
	size, _ := strconv.Atoi(string(head[1]))
	entries := data[xref+len(head[0]):]
	for i := 1; i < size; i++ {
		entry := entries[i*20 : i*20+20]
		offset, err := strconv.Atoi(string(entry[:10]))
		if err != nil || !bytes.HasPrefix(data[offset:], []byte(fmt.Sprintf("%d 0 obj\n", i))) {
			t.Fatalf("xref do objeto %d aponta para offset inválido %q", i, entry)
		}
	}
	count := regexp.MustCompile(`/Type /Pages /Kids \[[^\]]*\] /Count (\d+)`).FindSubmatch(data)
	if count == nil {
		t.Fatal("árvore de páginas ausente")
	}
	pages, _ := strconv.Atoi(string(count[1]))
	if objects := bytes.Count(data, []byte("<< /Type /Page /Parent 2 0 R")); objects != pages || pages < 1 {
		t.Fatalf("/Count=%d, objetos de página=%d", pages, objects)
	}
	var content bytes.Buffer
	streams := regexp.MustCompile(`<< /Length (\d+) /Filter /FlateDecode >>\nstream\n`)
	for _, loc := range streams.FindAllSubmatchIndex(data, -1) {
		length, _ := strconv.Atoi(string(data[loc[2]:loc[3]]))
		body := data[loc[1] : loc[1]+length]
		if !bytes.HasPrefix(data[loc[1]+length:], []byte("\nendstream")) {
			t.Fatal("/Length não corresponde ao stream")
		}
		zr, err := zlib.NewReader(bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.Copy(&content, zr); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(streams.FindAllIndex(data, -1)); n != pages {
		t.Fatalf("%d streams para %d páginas", n, pages)
	}
	return pages, content.Bytes()
}
