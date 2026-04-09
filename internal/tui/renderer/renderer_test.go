package renderer

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func TestT0301SetCellFlushWritesPositionColorAndRune(t *testing.T) {
	var out bytes.Buffer

	r, err := New(&out, 10, 3)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := r.SetCell(1, 1, 'A', ColorWhite, ColorBlack); err != nil {
		t.Fatalf("SetCell() error = %v", err)
	}
	if err := r.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}

	want := "\033[1;1H\033[37m\033[40mA"
	if got := out.String(); got != want {
		t.Fatalf("Flush() wrote %q, want %q", got, want)
	}
}

func TestT0302FlushSkipsIdenticalCell(t *testing.T) {
	var out bytes.Buffer

	r, err := New(&out, 10, 3)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := r.SetCell(1, 1, 'A', ColorWhite, ColorBlack); err != nil {
		t.Fatalf("SetCell() error = %v", err)
	}
	if err := r.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}

	out.Reset()
	if err := r.SetCell(1, 1, 'A', ColorWhite, ColorBlack); err != nil {
		t.Fatalf("SetCell() error = %v", err)
	}
	if err := r.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}

	if got := out.Len(); got != 0 {
		t.Fatalf("Flush() wrote %d bytes for an identical frame, want 0", got)
	}
}

func TestT0303FlushWritesOnlyDirtyNonAdjacentCells(t *testing.T) {
	var out bytes.Buffer

	r, err := New(&out, 10, 3)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	mustSetCell(t, r, 1, 1, 'A', ColorWhite, ColorBlack)
	mustSetCell(t, r, 2, 3, 'B', ColorRed, ColorBlue)
	mustSetCell(t, r, 3, 5, 'C', ColorReset, ColorReset)

	if err := r.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}

	want := strings.Join([]string{
		"\033[1;1H\033[37m\033[40mA",
		"\033[2;3H\033[31m\033[44mB",
		"\033[3;5H\033[39m\033[49mC",
	}, "")
	if got := out.String(); got != want {
		t.Fatalf("Flush() wrote %q, want %q", got, want)
	}
}

func TestT0304ResizeReallocatesBuffersAndForcesFullRedraw(t *testing.T) {
	var out bytes.Buffer

	r, err := New(&out, 80, 24)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	mustSetCell(t, r, 1, 1, 'A', ColorWhite, ColorBlack)
	if err := r.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}

	out.Reset()
	if err := r.Resize(40, 12); err != nil {
		t.Fatalf("Resize() error = %v", err)
	}

	if got := len(r.front); got != 40*12 {
		t.Fatalf("len(front) = %d, want %d", got, 40*12)
	}
	if got := len(r.back); got != 40*12 {
		t.Fatalf("len(back) = %d, want %d", got, 40*12)
	}

	mustSetCell(t, r, 1, 1, 'Z', ColorWhite, ColorBlack)
	if err := r.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}

	got := out.String()
	if !strings.HasPrefix(got, "\033[1;1H\033[37m\033[40mZ") {
		t.Fatalf("Flush() prefix after Resize() = %q, want redraw starting at cell 1,1", got[:min(len(got), 20)])
	}
	if len(got) <= len("\033[1;1H\033[37m\033[40mZ") {
		t.Fatalf("Flush() did not force a full redraw after Resize()")
	}
}

func TestT0305ResizeBelowMinimumMakesFlushFailWithoutWriting(t *testing.T) {
	var out bytes.Buffer

	r, err := New(&out, 10, 3)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	err = r.Resize(5, 2)
	if !errors.Is(err, ErrTerminalTooSmall) {
		t.Fatalf("Resize() error = %v, want %v", err, ErrTerminalTooSmall)
	}

	if err := r.Flush(); !errors.Is(err, ErrTerminalTooSmall) {
		t.Fatalf("Flush() error = %v, want %v", err, ErrTerminalTooSmall)
	}
	if got := out.Len(); got != 0 {
		t.Fatalf("Flush() wrote %d bytes after invalid Resize(), want 0", got)
	}
}

func TestT0306SetCellRejectsZeroBasedCoordinates(t *testing.T) {
	r, err := New(io.Discard, 10, 3)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	before := append([]Cell(nil), r.back...)
	if err := r.SetCell(0, 1, 'A', ColorWhite, ColorBlack); !errors.Is(err, ErrOutOfBounds) {
		t.Fatalf("SetCell(row=0) error = %v, want %v", err, ErrOutOfBounds)
	}
	if err := r.SetCell(1, 0, 'A', ColorWhite, ColorBlack); !errors.Is(err, ErrOutOfBounds) {
		t.Fatalf("SetCell(col=0) error = %v, want %v", err, ErrOutOfBounds)
	}
	if !bytes.Equal(marshalCells(before), marshalCells(r.back)) {
		t.Fatalf("back-buffer changed after ErrOutOfBounds")
	}
}

func TestT0307SetCellRejectsCoordinatesPastBounds(t *testing.T) {
	r, err := New(io.Discard, 10, 3)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	before := append([]Cell(nil), r.back...)
	if err := r.SetCell(4, 1, 'A', ColorWhite, ColorBlack); !errors.Is(err, ErrOutOfBounds) {
		t.Fatalf("SetCell(row>rows) error = %v, want %v", err, ErrOutOfBounds)
	}
	if err := r.SetCell(1, 11, 'A', ColorWhite, ColorBlack); !errors.Is(err, ErrOutOfBounds) {
		t.Fatalf("SetCell(col>cols) error = %v, want %v", err, ErrOutOfBounds)
	}
	if !bytes.Equal(marshalCells(before), marshalCells(r.back)) {
		t.Fatalf("back-buffer changed after ErrOutOfBounds")
	}
}

func TestT0308ResetColorUsesANSIResetCodes(t *testing.T) {
	var out bytes.Buffer

	r, err := New(&out, 10, 3)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	mustSetCell(t, r, 1, 1, 'A', ColorReset, ColorReset)

	if err := r.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}

	want := "\033[1;1H\033[39m\033[49mA"
	if got := out.String(); got != want {
		t.Fatalf("Flush() wrote %q, want %q", got, want)
	}
}

func TestT0309WriteStringMatchesSequentialSetCell(t *testing.T) {
	var out bytes.Buffer

	r, err := New(&out, 10, 3)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	r.WriteString(1, 1, "hello", ColorWhite, ColorBlack)

	if err := r.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}

	want := strings.Join([]string{
		"\033[1;1H\033[37m\033[40mh",
		"\033[1;2He",
		"\033[1;3Hl",
		"\033[1;4Hl",
		"\033[1;5Ho",
	}, "")
	if got := out.String(); got != want {
		t.Fatalf("Flush() wrote %q, want %q", got, want)
	}
}

func TestT0310WriteStringTruncatesSilentlyAtRightEdge(t *testing.T) {
	var out bytes.Buffer

	r, err := New(&out, 10, 3)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	r.WriteString(1, 8, "abcdef", ColorWhite, ColorBlack)

	if err := r.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}

	want := strings.Join([]string{
		"\033[1;8H\033[37m\033[40ma",
		"\033[1;9Hb",
		"\033[1;10Hc",
	}, "")
	if got := out.String(); got != want {
		t.Fatalf("Flush() wrote %q, want %q", got, want)
	}
}

func TestT0311FlushAllDirtyCellsWithinBudget(t *testing.T) {
	var out bytes.Buffer

	r, err := New(&out, 80, 24)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	for row := uint16(1); row <= 24; row++ {
		for col := uint16(1); col <= 80; col++ {
			mustSetCell(t, r, row, col, 'X', ColorWhite, ColorBlack)
		}
	}

	start := time.Now()
	if err := r.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Millisecond {
		t.Fatalf("Flush() took %v, want <= 5ms", elapsed)
	}
}

func TestT0312ControlRunesAreSanitizedToQuestionMark(t *testing.T) {
	var out bytes.Buffer

	r, err := New(&out, 10, 3)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	mustSetCell(t, r, 1, 1, '\n', ColorWhite, ColorBlack)
	mustSetCell(t, r, 1, 2, '\r', ColorWhite, ColorBlack)
	mustSetCell(t, r, 1, 3, '\033', ColorWhite, ColorBlack)

	if err := r.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}

	want := strings.Join([]string{
		"\033[1;1H\033[37m\033[40m?",
		"\033[1;2H?",
		"\033[1;3H?",
	}, "")
	if got := out.String(); got != want {
		t.Fatalf("Flush() wrote %q, want %q", got, want)
	}
}

func TestClearRegionResetsOnlySelectedRows(t *testing.T) {
	var out bytes.Buffer

	r, err := New(&out, 10, 3)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	mustSetCell(t, r, 1, 1, 'A', ColorWhite, ColorBlack)
	mustSetCell(t, r, 2, 1, 'B', ColorWhite, ColorBlack)
	mustSetCell(t, r, 3, 1, 'C', ColorWhite, ColorBlack)
	r.ClearRegion(2, 2)

	if err := r.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "\033[1;1H\033[37m\033[40mA") {
		t.Fatalf("Flush() missing row 1 content: %q", got)
	}
	if strings.Contains(got, "\033[2;1H\033[37m\033[40mB") {
		t.Fatalf("Flush() kept cleared row 2 content: %q", got)
	}
	if !strings.Contains(got, "\033[3;1HC") && !strings.Contains(got, "\033[3;1H\033[37m\033[40mC") {
		t.Fatalf("Flush() missing row 3 content: %q", got)
	}
}

func TestFlushMapsWriterFailureToErrTerminalWrite(t *testing.T) {
	r, err := New(failingWriter{}, 10, 3)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	mustSetCell(t, r, 1, 1, 'A', ColorWhite, ColorBlack)
	if err := r.Flush(); !errors.Is(err, ErrTerminalWrite) {
		t.Fatalf("Flush() error = %v, want %v", err, ErrTerminalWrite)
	}
}

func TestNewRejectsSizesBelowMinimum(t *testing.T) {
	if _, err := New(io.Discard, 9, 3); !errors.Is(err, ErrTerminalTooSmall) {
		t.Fatalf("New() error = %v, want %v", err, ErrTerminalTooSmall)
	}
	if _, err := New(io.Discard, 10, 2); !errors.Is(err, ErrTerminalTooSmall) {
		t.Fatalf("New() error = %v, want %v", err, ErrTerminalTooSmall)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("boom")
}

func mustSetCell(t *testing.T, r *Renderer, row, col uint16, ch rune, fg, bg uint8) {
	t.Helper()

	if err := r.SetCell(row, col, ch, fg, bg); err != nil {
		t.Fatalf("SetCell(%d, %d) error = %v", row, col, err)
	}
}

func marshalCells(cells []Cell) []byte {
	var buf bytes.Buffer
	for _, cell := range cells {
		buf.WriteRune(cell.Rune)
		buf.WriteByte(cell.Fg)
		buf.WriteByte(cell.Bg)
	}
	return buf.Bytes()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
