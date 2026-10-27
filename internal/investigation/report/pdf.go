package report

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"strings"
	"time"
	"unicode/utf16"
)

// Native PDF writer: A4 pages, the three standard Type 1 fonts with
// WinAnsiEncoding and Flate-compressed content streams. It needs no browser,
// font file or external tool, so the report is produced inside the process.

type pdfFont int

const (
	fontRegular pdfFont = iota
	fontBold
	fontMono
)

var pdfFontNames = [...]string{"Helvetica", "Helvetica-Bold", "Courier"}

type pdfColor struct{ r, g, b float64 }

var (
	colorText   = pdfColor{0.09, 0.13, 0.18}
	colorMuted  = pdfColor{0.35, 0.39, 0.47}
	colorRule   = pdfColor{0.80, 0.83, 0.87}
	colorDemoBg = pdfColor{1.00, 0.96, 0.89}
	colorDemoFg = pdfColor{0.72, 0.47, 0.06}
)

const (
	pdfPageWidth  = 595.28
	pdfPageHeight = 841.89
	pdfMargin     = 45.35 // 16 mm
	pdfFooterGap  = 22.0
	pdfContentW   = pdfPageWidth - 2*pdfMargin
)

type pdfPage struct {
	ops   bytes.Buffer
	lines []string // plain UTF-8 text of every drawn line, in drawing order
}

// pdfLayout places blocks top-down; y is the distance from the top edge.
type pdfLayout struct {
	pages []*pdfPage
	y     float64
}

func newPDFLayout() *pdfLayout {
	l := &pdfLayout{}
	l.newPage()
	return l
}

func (l *pdfLayout) page() *pdfPage { return l.pages[len(l.pages)-1] }

func (l *pdfLayout) newPage() {
	l.pages = append(l.pages, &pdfPage{})
	l.y = pdfMargin
}

func (l *pdfLayout) bottom() float64 { return pdfPageHeight - pdfMargin - pdfFooterGap }

// ensure starts a new page when h points do not fit below the cursor.
func (l *pdfLayout) ensure(h float64) {
	if l.y+h > l.bottom() && l.y > pdfMargin {
		l.newPage()
	}
}

func (l *pdfLayout) space(h float64) {
	l.y += h
	if l.y > l.bottom() {
		l.newPage()
	}
}

func (l *pdfLayout) drawText(x, baseline float64, font pdfFont, size float64, color pdfColor, text string) {
	if text == "" {
		return
	}
	fmt.Fprintf(&l.page().ops, "BT /F%d %.2f Tf %.3f %.3f %.3f rg %.2f %.2f Td (%s) Tj ET\n", font+1, size, color.r, color.g, color.b, x, pdfPageHeight-baseline, pdfEscape(encodeWinAnsi(text)))
}

func (l *pdfLayout) rule(y float64, color pdfColor, width float64) {
	fmt.Fprintf(&l.page().ops, "%.3f %.3f %.3f RG %.2f w %.2f %.2f m %.2f %.2f l S\n", color.r, color.g, color.b, width, pdfMargin, pdfPageHeight-y, pdfPageWidth-pdfMargin, pdfPageHeight-y)
}

func (l *pdfLayout) fillRect(x, y, w, h float64, color pdfColor) {
	fmt.Fprintf(&l.page().ops, "%.3f %.3f %.3f rg %.2f %.2f %.2f %.2f re f\n", color.r, color.g, color.b, x, pdfPageHeight-y-h, w, h)
}

// textRun is a piece of inline text in one font, e.g. a bold label.
type textRun struct {
	font pdfFont
	text string
}

func bold(s string) textRun  { return textRun{fontBold, s} }
func plain(s string) textRun { return textRun{fontRegular, s} }

type lineSegment struct {
	font pdfFont
	x    float64
	text string
}

type laidLine struct{ segments []lineSegment }

func (ln laidLine) plainText() string {
	parts := make([]string, 0, len(ln.segments))
	for _, s := range ln.segments {
		parts = append(parts, s.text)
	}
	return strings.Join(parts, " ")
}

// wrapRuns breaks inline runs into lines no wider than width. Words longer
// than a line are split by character, like CSS overflow-wrap:anywhere.
func wrapRuns(runs []textRun, size, width float64) []laidLine {
	type word struct {
		font pdfFont
		text string
	}
	var words []word
	for _, r := range runs {
		for _, w := range strings.Fields(r.text) {
			for _, piece := range splitLongWord(w, r.font, size, width) {
				words = append(words, word{r.font, piece})
			}
		}
	}
	var lines []laidLine
	var cur laidLine
	x := 0.0
	spaceW := textWidth(" ", fontRegular, size)
	for _, w := range words {
		ww := textWidth(w.text, w.font, size)
		if len(cur.segments) > 0 && x+spaceW+ww > width {
			lines = append(lines, cur)
			cur = laidLine{}
			x = 0
		}
		if n := len(cur.segments); n > 0 && cur.segments[n-1].font == w.font {
			cur.segments[n-1].text += " " + w.text
			x += spaceW + ww
			continue
		}
		if len(cur.segments) > 0 {
			x += spaceW
		}
		cur.segments = append(cur.segments, lineSegment{font: w.font, x: x, text: w.text})
		x += ww
	}
	if len(cur.segments) > 0 {
		lines = append(lines, cur)
	}
	return lines
}

func splitLongWord(w string, font pdfFont, size, width float64) []string {
	if textWidth(w, font, size) <= width {
		return []string{w}
	}
	var out []string
	var cur []rune
	x := 0.0
	for _, r := range w {
		rw := textWidth(string(r), font, size)
		if len(cur) > 0 && x+rw > width {
			out = append(out, string(cur))
			cur, x = nil, 0
		}
		cur = append(cur, r)
		x += rw
	}
	if len(cur) > 0 {
		out = append(out, string(cur))
	}
	return out
}

func (l *pdfLayout) drawLine(x, size float64, color pdfColor, ln laidLine) {
	baseline := l.y + size
	for _, s := range ln.segments {
		l.drawText(x+s.x, baseline, s.font, size, color, s.text)
	}
	l.page().lines = append(l.page().lines, ln.plainText())
}

func (l *pdfLayout) heading(text string, size, before float64, underline bool) {
	lines := wrapRuns([]textRun{bold(text)}, size, pdfContentW)
	lineH := size * 1.3
	l.space(before)
	// Keep the heading together with at least two lines of the next block.
	l.ensure(float64(len(lines))*lineH + 30)
	for _, ln := range lines {
		l.drawLine(pdfMargin, size, colorText, ln)
		l.y += lineH
	}
	if underline {
		l.rule(l.y+1, colorRule, 0.8)
		l.y += 4
	}
	l.y += size * 0.35
}

func (l *pdfLayout) paragraph(size float64, color pdfColor, runs ...textRun) {
	lineH := size * 1.45
	for _, ln := range wrapRuns(runs, size, pdfContentW) {
		l.ensure(lineH)
		l.drawLine(pdfMargin, size, color, ln)
		l.y += lineH
	}
	l.y += size * 0.45
}

// banner draws a paragraph over a filled box; it never splits across pages.
func (l *pdfLayout) banner(size float64, runs ...textRun) {
	pad := 6.0
	lineH := size * 1.45
	lines := wrapRuns(runs, size, pdfContentW-2*pad)
	h := float64(len(lines))*lineH + 2*pad
	l.ensure(h)
	l.fillRect(pdfMargin, l.y, pdfContentW, h, colorDemoBg)
	l.y += pad
	for _, ln := range lines {
		l.drawLine(pdfMargin+pad, size, colorDemoFg, ln)
		l.y += lineH
	}
	l.y += pad + size*0.6
}

// preformatted keeps line breaks and leading spaces, wrapping by column.
func (l *pdfLayout) preformatted(text string, size float64) {
	lineH := size * 1.35
	cols := int(pdfContentW / (0.6 * size))
	for _, src := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		runes := []rune(strings.ReplaceAll(src, "\t", "    "))
		for {
			n := len(runes)
			if n > cols {
				n = cols
			}
			l.ensure(lineH)
			chunk := string(runes[:n])
			l.drawText(pdfMargin, l.y+size, fontMono, size, colorText, chunk)
			l.page().lines = append(l.page().lines, chunk)
			l.y += lineH
			runes = runes[n:]
			if len(runes) == 0 {
				break
			}
		}
	}
	l.y += size * 0.5
}

// table draws a header and rows with wrapped cells; widths are fractions of
// the content width. Rows taller than the remaining space continue on the
// next page, where the header is repeated.
func (l *pdfLayout) table(widths []float64, header []string, rows [][]string) {
	const size, pad = 8.0, 3.0
	lineH := size * 1.35
	colX := make([]float64, len(widths))
	colW := make([]float64, len(widths))
	x := pdfMargin
	for i, w := range widths {
		colX[i] = x
		colW[i] = w * pdfContentW
		x += colW[i]
	}
	wrapRow := func(cells []string, font pdfFont) ([][]laidLine, int) {
		out := make([][]laidLine, len(widths))
		height := 1
		for i := range widths {
			text := ""
			if i < len(cells) {
				text = cells[i]
			}
			out[i] = wrapRuns([]textRun{{font, text}}, size, colW[i]-2*pad)
			if len(out[i]) > height {
				height = len(out[i])
			}
		}
		return out, height
	}
	drawRow := func(cells [][]laidLine, height int, font pdfFont, onBreak func()) {
		l.y += pad
		for i := 0; i < height; i++ {
			if l.y+lineH > l.bottom() {
				l.newPage()
				if onBreak != nil {
					onBreak()
				}
				l.y += pad
			}
			var parts []string
			for c := range cells {
				if i < len(cells[c]) {
					for _, s := range cells[c][i].segments {
						l.drawText(colX[c]+pad+s.x, l.y+size, font, size, colorText, s.text)
					}
					parts = append(parts, cells[c][i].plainText())
				}
			}
			l.page().lines = append(l.page().lines, strings.Join(parts, " | "))
			l.y += lineH
		}
		l.y += pad
		l.rule(l.y, colorRule, 0.5)
	}
	headCells, headHeight := wrapRow(header, fontBold)
	drawHeader := func() { drawRow(headCells, headHeight, fontBold, nil) }
	l.ensure(float64(headHeight+2)*lineH + 4*pad)
	drawHeader()
	for _, row := range rows {
		cells, height := wrapRow(row, fontRegular)
		l.ensure(lineH + 2*pad)
		if l.y == pdfMargin {
			drawHeader()
		}
		drawRow(cells, height, fontRegular, drawHeader)
	}
	l.y += 6
}

// encode serializes the pages, adding a footer with the page number to each.
func (l *pdfLayout) encode(title, footer string, created time.Time) ([]byte, error) {
	var out bytes.Buffer
	offsets := []int{0}
	obj := func(body string) {
		offsets = append(offsets, out.Len())
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", len(offsets)-1, body)
	}
	out.WriteString("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")
	pageCount := len(l.pages)
	kids := make([]string, pageCount)
	for i := range kids {
		kids[i] = fmt.Sprintf("%d 0 R", 7+2*i)
	}
	obj("<< /Type /Catalog /Pages 2 0 R >>")
	obj(fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), pageCount))
	for _, name := range pdfFontNames {
		obj(fmt.Sprintf("<< /Type /Font /Subtype /Type1 /BaseFont /%s /Encoding /WinAnsiEncoding >>", name))
	}
	date := "D:" + created.UTC().Format("20060102150405") + "Z"
	obj(fmt.Sprintf("<< /Title %s /Producer %s /CreationDate (%s) >>", pdfTextString(title), pdfTextString("PocketCli investigate "+rendererVersion), date))
	for i, p := range l.pages {
		label := fmt.Sprintf("%s · página %d de %d", footer, i+1, pageCount)
		fmt.Fprintf(&p.ops, "BT /F1 7.50 Tf %.3f %.3f %.3f rg %.2f %.2f Td (%s) Tj ET\n", colorMuted.r, colorMuted.g, colorMuted.b, pdfMargin, pdfMargin-12, pdfEscape(encodeWinAnsi(label)))
		var compressed bytes.Buffer
		zw := zlib.NewWriter(&compressed)
		if _, err := zw.Write(p.ops.Bytes()); err != nil {
			return nil, err
		}
		if err := zw.Close(); err != nil {
			return nil, err
		}
		obj(fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %.2f %.2f] /Resources << /Font << /F1 3 0 R /F2 4 0 R /F3 5 0 R >> >> /Contents %d 0 R >>", pdfPageWidth, pdfPageHeight, 8+2*i))
		obj(fmt.Sprintf("<< /Length %d /Filter /FlateDecode >>\nstream\n%s\nendstream", compressed.Len(), compressed.Bytes()))
	}
	xref := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(offsets))
	for _, off := range offsets[1:] {
		fmt.Fprintf(&out, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root 1 0 R /Info 6 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), xref)
	return out.Bytes(), nil
}

func pdfEscape(b []byte) string {
	var out strings.Builder
	for _, c := range b {
		switch c {
		case '(', ')', '\\':
			out.WriteByte('\\')
			out.WriteByte(c)
		default:
			out.WriteByte(c)
		}
	}
	return out.String()
}

// pdfTextString encodes metadata as UTF-16BE with BOM, valid for any text.
func pdfTextString(s string) string {
	var out strings.Builder
	out.WriteString("<FEFF")
	for _, u := range utf16.Encode([]rune(s)) {
		fmt.Fprintf(&out, "%04X", u)
	}
	out.WriteString(">")
	return out.String()
}

var winAnsiExtra = map[rune]byte{
	'€': 0x80, '‚': 0x82, 'ƒ': 0x83, '„': 0x84, '…': 0x85, '†': 0x86, '‡': 0x87, 'ˆ': 0x88, '‰': 0x89, 'Š': 0x8A, '‹': 0x8B, 'Œ': 0x8C, 'Ž': 0x8E,
	'‘': 0x91, '’': 0x92, '“': 0x93, '”': 0x94, '•': 0x95, '–': 0x96, '—': 0x97, '˜': 0x98, '™': 0x99, 'š': 0x9A, '›': 0x9B, 'œ': 0x9C, 'ž': 0x9E, 'Ÿ': 0x9F,
}

var winAnsiFallback = map[rune]string{'→': "->", '←': "<-", '≥': ">=", '≤': "<=", '≠': "!=", '✓': "v", ' ': " "}

// encodeWinAnsi maps text to the standard fonts' encoding. Characters the
// encoding cannot represent become an ASCII fallback or '?'.
func encodeWinAnsi(s string) []byte {
	out := make([]byte, 0, len(s))
	for _, r := range s {
		switch {
		case r == '\t' || r == '\n' || r == '\r':
			out = append(out, ' ')
		case r >= 0x20 && r < 0x7f:
			out = append(out, byte(r))
		case r >= 0xa1 && r <= 0xff:
			out = append(out, byte(r))
		default:
			if b, ok := winAnsiExtra[r]; ok {
				out = append(out, b)
			} else if f, ok := winAnsiFallback[r]; ok {
				out = append(out, f...)
			} else {
				out = append(out, '?')
			}
		}
	}
	return out
}

// Helvetica and Helvetica-Bold advance widths (1/1000 em) for ASCII 32..126,
// from the Adobe core font metrics.
var helveticaWidths = [95]int{
	278, 278, 355, 556, 556, 889, 667, 191, 333, 333, 389, 584, 278, 333, 278, 278,
	556, 556, 556, 556, 556, 556, 556, 556, 556, 556, 278, 278, 584, 584, 584, 556,
	1015, 667, 667, 722, 722, 667, 611, 778, 722, 278, 500, 667, 556, 833, 722, 778,
	667, 778, 722, 667, 611, 722, 667, 944, 667, 667, 611, 278, 278, 278, 469, 556,
	333, 556, 556, 500, 556, 556, 278, 556, 556, 222, 222, 500, 222, 833, 556, 556,
	556, 556, 333, 500, 278, 556, 500, 722, 500, 500, 500, 334, 260, 334, 584,
}

var helveticaBoldWidths = [95]int{
	278, 333, 474, 556, 556, 889, 722, 238, 333, 333, 389, 584, 278, 333, 278, 278,
	556, 556, 556, 556, 556, 556, 556, 556, 556, 556, 333, 333, 584, 584, 584, 611,
	975, 722, 722, 722, 722, 667, 611, 778, 722, 278, 556, 722, 611, 833, 722, 778,
	667, 778, 722, 667, 611, 722, 667, 944, 667, 667, 611, 333, 278, 333, 584, 556,
	333, 556, 611, 556, 611, 556, 333, 611, 611, 278, 278, 556, 278, 889, 611, 611,
	611, 611, 389, 556, 333, 611, 556, 778, 556, 556, 500, 389, 280, 389, 584,
}

// Accented Latin-1 letters share the width of their base letter.
var latin1Base = func() map[byte]byte {
	m := map[byte]byte{}
	groups := map[byte]string{'A': "ÀÁÂÃÄÅ", 'C': "Ç", 'E': "ÈÉÊË", 'D': "Ð", 'N': "Ñ", 'O': "ÒÓÔÕÖØ", 'U': "ÙÚÛÜ", 'Y': "Ý",
		'a': "àáâãäå", 'c': "ç", 'e': "èéêë", 'n': "ñ", 'o': "òóôõöø", 'u': "ùúûü", 'y': "ýÿ", 'I': "ÌÍÎÏ"}
	for base, letters := range groups {
		for _, r := range letters {
			m[byte(r)] = base
		}
	}
	return m
}()

var winAnsiWidths = map[byte]int{0x80: 556, 0x85: 1000, 0x91: 222, 0x92: 222, 0x93: 333, 0x94: 333, 0x95: 350, 0x96: 556, 0x97: 1000, 0xa0: 278, 0xaa: 370, 0xb0: 400, 0xb7: 278, 0xba: 365, 0xab: 556, 0xbb: 556, 0xc6: 1000, 0xe6: 889, 0xdf: 611, 0xd7: 584, 0xf7: 584, 0xec: 278, 0xed: 278, 0xee: 278, 0xef: 278}

func charWidth(c byte, font pdfFont) int {
	if font == fontMono {
		return 600
	}
	table := &helveticaWidths
	if font == fontBold {
		table = &helveticaBoldWidths
	}
	if c >= 32 && c < 127 {
		return table[c-32]
	}
	if base, ok := latin1Base[c]; ok {
		return table[base-32]
	}
	if w, ok := winAnsiWidths[c]; ok {
		return w
	}
	return 556
}

func textWidth(s string, font pdfFont, size float64) float64 {
	total := 0
	for _, c := range encodeWinAnsi(s) {
		total += charWidth(c, font)
	}
	return float64(total) * size / 1000
}
