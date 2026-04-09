package renderer

import (
	"errors"
	"io"
	"strconv"
	"unicode/utf8"
)

const (
	minCols  uint16 = 10
	minRows  uint16 = 3
	maxCells uint32 = 65535

	ansiEscape = "\033["
)

const (
	// ColorBlack is ANSI black.
	ColorBlack uint8 = iota
	// ColorRed is ANSI red.
	ColorRed
	// ColorGreen is ANSI green.
	ColorGreen
	// ColorYellow is ANSI yellow.
	ColorYellow
	// ColorBlue is ANSI blue.
	ColorBlue
	// ColorMagenta is ANSI magenta.
	ColorMagenta
	// ColorCyan is ANSI cyan.
	ColorCyan
	// ColorWhite is ANSI white.
	ColorWhite
	// ColorBrightBlack is ANSI bright black.
	ColorBrightBlack
	// ColorBrightRed is ANSI bright red.
	ColorBrightRed
	// ColorBrightGreen is ANSI bright green.
	ColorBrightGreen
	// ColorBrightYellow is ANSI bright yellow.
	ColorBrightYellow
	// ColorBrightBlue is ANSI bright blue.
	ColorBrightBlue
	// ColorBrightMagenta is ANSI bright magenta.
	ColorBrightMagenta
	// ColorBrightCyan is ANSI bright cyan.
	ColorBrightCyan
	// ColorBrightWhite is ANSI bright white.
	ColorBrightWhite
	// ColorReset restores the terminal default color.
	ColorReset uint8 = 255
)

var (
	// ErrTerminalTooSmall reports a renderer size below the supported minimum.
	ErrTerminalTooSmall = errors.New("renderer: terminal below minimum size")
	// ErrOutOfBounds reports an invalid 1-based cell position.
	ErrOutOfBounds = errors.New("renderer: cell position out of bounds")
	// ErrTerminalWrite reports a write failure while flushing a frame.
	ErrTerminalWrite = errors.New("renderer: write to terminal failed")
)

var defaultCell = Cell{Fg: ColorReset, Bg: ColorReset}

// Cell is the smallest renderable unit in the back/front buffers.
type Cell struct {
	Rune rune
	Fg   uint8
	Bg   uint8
}

// Renderer keeps the front/back frame buffers and writes only cell diffs.
type Renderer struct {
	out        io.Writer
	cols       uint16
	rows       uint16
	front      []Cell
	back       []Cell
	fullRedraw bool
}

// New creates a renderer bound to out using ANSI 16-color output.
func New(out io.Writer, cols, rows uint16) (*Renderer, error) {
	if !sizeSupported(cols, rows) {
		return nil, ErrTerminalTooSmall
	}

	if out == nil {
		out = io.Discard
	}

	cellCount := int(uint32(cols) * uint32(rows))
	renderer := &Renderer{
		out:   out,
		cols:  cols,
		rows:  rows,
		front: make([]Cell, cellCount),
		back:  make([]Cell, cellCount),
	}
	fillWithDefault(renderer.front)
	fillWithDefault(renderer.back)

	return renderer, nil
}

// SetCell updates a single 1-based position in the back-buffer.
func (r *Renderer) SetCell(row, col uint16, ch rune, fg, bg uint8) error {
	if !r.inBounds(row, col) {
		return ErrOutOfBounds
	}

	r.back[r.index(row, col)] = Cell{
		Rune: ch,
		Fg:   normalizeColor(fg),
		Bg:   normalizeColor(bg),
	}
	return nil
}

// WriteString writes as many runes from s as fit on the current row.
func (r *Renderer) WriteString(row, col uint16, s string, fg, bg uint8) {
	if row == 0 || col == 0 || row > r.rows || col > r.cols {
		return
	}

	currentCol := col
	for _, ch := range s {
		if currentCol > r.cols {
			return
		}
		_ = r.SetCell(row, currentCol, ch, fg, bg)
		currentCol++
	}
}

// ClearRegion resets the inclusive row range in the back-buffer.
func (r *Renderer) ClearRegion(rowStart, rowEnd uint16) {
	if r.rows == 0 {
		return
	}
	if rowStart == 0 {
		rowStart = 1
	}
	if rowStart > rowEnd || rowStart > r.rows {
		return
	}
	if rowEnd > r.rows {
		rowEnd = r.rows
	}

	for row := rowStart; row <= rowEnd; row++ {
		base := int(row-1) * int(r.cols)
		for col := 0; col < int(r.cols); col++ {
			r.back[base+col] = defaultCell
		}
	}
}

// Clear resets the whole back-buffer to empty default cells.
func (r *Renderer) Clear() {
	r.ClearRegion(1, r.rows)
}

// Flush computes the frame diff, writes it to the output and commits the back-buffer.
func (r *Renderer) Flush() error {
	if !sizeSupported(r.cols, r.rows) {
		return ErrTerminalTooSmall
	}

	frame := make([]byte, 0, len(r.back)*16)
	lastFg := uint8(254)
	lastBg := uint8(254)
	dirty := false

	for idx := range r.back {
		if !r.fullRedraw && r.back[idx] == r.front[idx] {
			continue
		}

		row := uint16(idx/int(r.cols)) + 1
		col := uint16(idx%int(r.cols)) + 1
		cell := r.back[idx]

		frame = appendCursorMove(frame, row, col)
		if cell.Fg != lastFg {
			frame = appendColor(frame, cell.Fg, true)
			lastFg = cell.Fg
		}
		if cell.Bg != lastBg {
			frame = appendColor(frame, cell.Bg, false)
			lastBg = cell.Bg
		}
		frame = utf8.AppendRune(frame, sanitizeRune(cell.Rune))
		dirty = true
	}

	if !dirty {
		r.fullRedraw = false
		return nil
	}

	if err := writeAll(r.out, frame); err != nil {
		return ErrTerminalWrite
	}

	copy(r.front, r.back)
	r.fullRedraw = false
	return nil
}

// Resize reallocates the buffers and forces the next Flush to redraw the whole frame.
func (r *Renderer) Resize(cols, rows uint16) error {
	cellCount, ok := bufferLen(cols, rows)
	if !ok {
		r.cols = cols
		r.rows = rows
		r.front = nil
		r.back = nil
		r.fullRedraw = true
		return ErrTerminalTooSmall
	}

	r.cols = cols
	r.rows = rows
	r.front = make([]Cell, cellCount)
	r.back = make([]Cell, cellCount)
	fillWithDefault(r.front)
	fillWithDefault(r.back)
	r.fullRedraw = true

	if !sizeSupported(cols, rows) {
		return ErrTerminalTooSmall
	}
	return nil
}

func (r *Renderer) index(row, col uint16) int {
	return int(row-1)*int(r.cols) + int(col-1)
}

func (r *Renderer) inBounds(row, col uint16) bool {
	if row == 0 || col == 0 {
		return false
	}
	if row > r.rows || col > r.cols {
		return false
	}
	return true
}

func sizeSupported(cols, rows uint16) bool {
	_, ok := bufferLen(cols, rows)
	return ok && cols >= minCols && rows >= minRows
}

func bufferLen(cols, rows uint16) (int, bool) {
	cellCount := uint32(cols) * uint32(rows)
	if cellCount > maxCells {
		return 0, false
	}
	return int(cellCount), true
}

func fillWithDefault(cells []Cell) {
	for idx := range cells {
		cells[idx] = defaultCell
	}
}

func normalizeColor(color uint8) uint8 {
	if color <= ColorBrightWhite || color == ColorReset {
		return color
	}
	return ColorReset
}

func sanitizeRune(ch rune) rune {
	switch {
	case ch == 0:
		return ' '
	case ch == 0x1b:
		return '?'
	case ch < 0x20:
		return '?'
	case ch == 0x7f:
		return '?'
	default:
		return ch
	}
}

func appendCursorMove(dst []byte, row, col uint16) []byte {
	dst = append(dst, ansiEscape...)
	dst = strconv.AppendUint(dst, uint64(row), 10)
	dst = append(dst, ';')
	dst = strconv.AppendUint(dst, uint64(col), 10)
	dst = append(dst, 'H')
	return dst
}

func appendColor(dst []byte, color uint8, foreground bool) []byte {
	code := colorCode(color, foreground)
	dst = append(dst, ansiEscape...)
	dst = strconv.AppendInt(dst, int64(code), 10)
	dst = append(dst, 'm')
	return dst
}

func colorCode(color uint8, foreground bool) int {
	color = normalizeColor(color)
	if color == ColorReset {
		if foreground {
			return 39
		}
		return 49
	}

	if foreground {
		if color <= ColorWhite {
			return 30 + int(color)
		}
		return 90 + int(color-ColorBrightBlack)
	}

	if color <= ColorWhite {
		return 40 + int(color)
	}
	return 100 + int(color-ColorBrightBlack)
}

func writeAll(out io.Writer, data []byte) error {
	for len(data) > 0 {
		written, err := out.Write(data)
		if err != nil {
			return err
		}
		if written <= 0 {
			return io.ErrShortWrite
		}
		data = data[written:]
	}
	return nil
}
