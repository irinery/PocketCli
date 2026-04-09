package runtime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"pocketcli/internal/tui/event"
	"pocketcli/internal/tui/renderer"
)

func TestT0401ModeViewerEmitsTickEvents(t *testing.T) {
	harness := newRuntimeHarness(t, ModeViewer, 80, 24, 100*time.Millisecond, newFakeRenderer(80, 24, nil))
	defer harness.cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var ticks atomic.Int32
	done := runRuntimeAsync(ctx, harness.runtime, func(evt event.Event, draw func(func(IRenderer))) {
		if evt.Type == event.EventTick {
			ticks.Add(1)
		}
	})

	time.Sleep(320 * time.Millisecond)
	cancel()

	if err := waitRuntime(t, done, 200*time.Millisecond); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	got := ticks.Load()
	if got < 2 || got > 4 {
		t.Fatalf("tick count = %d, want between 2 and 4", got)
	}
}

func TestT0402ModeAgentIgnoresTickEvents(t *testing.T) {
	harness := newRuntimeHarness(t, ModeAgent, 80, 24, 100*time.Millisecond, newFakeRenderer(80, 24, nil))
	defer harness.cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var tickCount atomic.Int32
	var keyCount atomic.Int32
	done := runRuntimeAsync(ctx, harness.runtime, func(evt event.Event, draw func(func(IRenderer))) {
		switch evt.Type {
		case event.EventTick:
			tickCount.Add(1)
		case event.EventKey:
			keyCount.Add(1)
			cancel()
		}
	})

	time.Sleep(250 * time.Millisecond)
	harness.writeInput(t, []byte{'a'})

	if err := waitRuntime(t, done, 200*time.Millisecond); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	if got := tickCount.Load(); got != 0 {
		t.Fatalf("tick count = %d, want 0", got)
	}
	if got := keyCount.Load(); got != 1 {
		t.Fatalf("key count = %d, want 1", got)
	}
}

func TestT0403CtrlCCancelStopsRuntime(t *testing.T) {
	harness := newRuntimeHarness(t, ModeAgent, 80, 24, 0, newFakeRenderer(80, 24, nil))
	defer harness.cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := runRuntimeAsync(ctx, harness.runtime, func(evt event.Event, draw func(func(IRenderer))) {
		if evt.Type == event.EventKey && evt.Key.Key == event.KeyCtrlC {
			cancel()
		}
	})

	harness.writeInput(t, []byte{0x03})

	start := time.Now()
	if err := waitRuntime(t, done, 200*time.Millisecond); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("Run() took %v after Ctrl+C, want <= 200ms", elapsed)
	}
}

func TestT0404RejectsTerminalBelowMinimumBeforeSetup(t *testing.T) {
	recorder := newCallRecorder()
	ren := newFakeRenderer(5, 2, recorder)
	harness := newRuntimeHarness(t, ModeViewer, 5, 2, 100*time.Millisecond, ren)
	harness.term.recorder = recorder
	defer harness.cleanup()

	err := harness.runtime.Run(context.Background(), nil)
	if !errors.Is(err, ErrTerminalTooSmall) {
		t.Fatalf("Run() error = %v, want %v", err, ErrTerminalTooSmall)
	}

	if calls := recorder.snapshot(); len(calls) != 1 || calls[0] != "term.size" {
		t.Fatalf("call order = %v, want only term.size", calls)
	}
	if got := ren.flushCount(); got != 0 {
		t.Fatalf("flush count = %d, want 0", got)
	}
	if got := len(ren.writesSnapshot()); got != 0 {
		t.Fatalf("write count = %d, want 0", got)
	}
}

func TestT0405ResizeEventResizesRenderer(t *testing.T) {
	ren := newFakeRenderer(80, 24, nil)
	harness := newRuntimeHarness(t, ModeAgent, 80, 24, 0, ren)
	defer harness.cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := runRuntimeAsync(ctx, harness.runtime, func(evt event.Event, draw func(func(IRenderer))) {
		if evt.Type == event.EventResize {
			cancel()
		}
	})

	harness.term.setSize(40, 12)
	harness.term.notifyResize()

	if err := waitRuntime(t, done, 200*time.Millisecond); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	resizes := ren.resizeCallsSnapshot()
	if len(resizes) == 0 {
		t.Fatal("Resize() was not called")
	}
	got := resizes[len(resizes)-1]
	if got.cols != 40 || got.rows != 12 {
		t.Fatalf("Resize() called with %dx%d, want 40x12", got.cols, got.rows)
	}
	if got := ren.flushCount(); got == 0 {
		t.Fatal("Flush() was not called after resize")
	}
}

func TestT0406DrawFlushesAfterTick(t *testing.T) {
	var out bytes.Buffer

	ren, err := renderer.New(&out, 10, 3)
	if err != nil {
		t.Fatalf("renderer.New() error = %v", err)
	}

	harness := newRuntimeHarness(t, ModeViewer, 10, 3, 100*time.Millisecond, ren)
	defer harness.cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := runRuntimeAsync(ctx, harness.runtime, func(evt event.Event, draw func(func(IRenderer))) {
		if evt.Type != event.EventTick {
			return
		}
		draw(func(ren IRenderer) {
			if err := ren.SetCell(1, 1, 'X', renderer.ColorWhite, renderer.ColorBlack); err != nil {
				t.Fatalf("SetCell() error = %v", err)
			}
		})
		cancel()
	})

	if err := waitRuntime(t, done, 250*time.Millisecond); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	if !strings.Contains(out.String(), "\033[1;1H\033[37m\033[40mX") {
		t.Fatalf("Flush() output = %q, want rendered cell", out.String())
	}
}

func TestT0407ContextCancelRunsTeardownInOrder(t *testing.T) {
	recorder := newCallRecorder()
	ren := newFakeRenderer(80, 24, recorder)
	harness := newRuntimeHarness(t, ModeViewer, 80, 24, 0, ren)
	harness.term.recorder = recorder
	defer harness.cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	done := runRuntimeAsync(ctx, harness.runtime, nil)

	cancel()

	if err := waitRuntime(t, done, 200*time.Millisecond); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	assertSubsequence(t, recorder.snapshot(), []string{"ren.clear", "ren.flush", "term.exit_alt", "term.restore"})
}

func TestT0408HandlerPanicTriggersCleanTeardown(t *testing.T) {
	recorder := newCallRecorder()
	ren := newFakeRenderer(80, 24, recorder)
	harness := newRuntimeHarness(t, ModeViewer, 80, 24, 100*time.Millisecond, ren)
	harness.term.recorder = recorder
	defer harness.cleanup()

	err := harness.runtime.Run(context.Background(), func(evt event.Event, draw func(func(IRenderer))) {
		if evt.Type == event.EventTick {
			panic("boom")
		}
	})

	if !errors.Is(err, ErrHandlerPanic) {
		t.Fatalf("Run() error = %v, want %v", err, ErrHandlerPanic)
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("Run() error = %q, want panic message", err.Error())
	}
	assertSubsequence(t, recorder.snapshot(), []string{"ren.clear", "ren.flush", "term.exit_alt", "term.restore"})
}

func TestT0409DrawIsSerializedAgainstFlush(t *testing.T) {
	ren := newFakeRenderer(80, 24, nil)
	harness := newRuntimeHarness(t, ModeViewer, 80, 24, 5*time.Millisecond, ren)
	defer harness.cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	done := runRuntimeAsync(ctx, harness.runtime, func(evt event.Event, draw func(func(IRenderer))) {
		if evt.Type == event.EventTick {
			draw(func(ren IRenderer) {
				_ = ren.SetCell(1, 1, 'T', 255, 255)
			})
		}
	})

	var workers sync.WaitGroup
	workers.Add(1)
	go func() {
		defer workers.Done()
		for i := 0; i < 200; i++ {
			harness.runtime.Draw(func(ren IRenderer) {
				_ = ren.SetCell(1, 1, 'X', 255, 255)
			})
			time.Sleep(time.Millisecond)
		}
	}()

	time.Sleep(80 * time.Millisecond)
	cancel()
	workers.Wait()

	if err := waitRuntime(t, done, 200*time.Millisecond); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got := ren.maxConcurrent(); got > 1 {
		t.Fatalf("renderer saw %d concurrent calls, want serialized access", got)
	}
}

func TestT0410AppendLineScrollsFromTop(t *testing.T) {
	ren := newFakeRenderer(10, 5, nil)
	harness := newRuntimeHarness(t, ModeAgent, 10, 5, 0, ren)
	defer harness.cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := runRuntimeAsync(ctx, harness.runtime, nil)

	time.Sleep(20 * time.Millisecond)
	for i := 1; i <= 1000; i++ {
		harness.runtime.AppendLine(fmt.Sprintf("L%04d", i))
	}

	time.Sleep(20 * time.Millisecond)

	rows := ren.renderedRows()
	want := []string{"L0997", "L0998", "L0999", "L1000"}
	for idx, line := range want {
		got := strings.TrimSpace(rows[uint16(idx+1)])
		if got != line {
			t.Fatalf("row %d = %q, want %q", idx+1, got, line)
		}
	}

	cancel()

	if err := waitRuntime(t, done, 200*time.Millisecond); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
}

type runtimeHarness struct {
	runtime *Runtime
	term    *fakeTerminal
	input   *io.PipeWriter
	reader  *io.PipeReader
}

func newRuntimeHarness(t *testing.T, mode Mode, cols, rows uint16, tickInterval time.Duration, ren IRenderer) *runtimeHarness {
	t.Helper()

	reader, writer := io.Pipe()
	term := newFakeTerminal(cols, rows, nil)
	loop := event.New(reader, term.ResizeChan(), tickInterval)

	return &runtimeHarness{
		runtime: New(term, loop, ren, mode),
		term:    term,
		input:   writer,
		reader:  reader,
	}
}

func (h *runtimeHarness) writeInput(t *testing.T, data []byte) {
	t.Helper()

	if _, err := h.input.Write(data); err != nil {
		t.Fatalf("input.Write() error = %v", err)
	}
}

func (h *runtimeHarness) cleanup() {
	_ = h.input.Close()
	_ = h.reader.Close()
}

func runRuntimeAsync(ctx context.Context, rt *Runtime, handler Handler) <-chan error {
	done := make(chan error, 1)
	go func() {
		done <- rt.Run(ctx, handler)
	}()
	return done
}

func waitRuntime(t *testing.T, done <-chan error, timeout time.Duration) error {
	t.Helper()

	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		t.Fatalf("Run() did not finish within %v", timeout)
		return nil
	}
}

type fakeTerminal struct {
	mu       sync.Mutex
	cols     uint16
	rows     uint16
	resize   chan struct{}
	recorder *callRecorder
}

func newFakeTerminal(cols, rows uint16, recorder *callRecorder) *fakeTerminal {
	return &fakeTerminal{
		cols:     cols,
		rows:     rows,
		resize:   make(chan struct{}, 16),
		recorder: recorder,
	}
}

func (t *fakeTerminal) RawMode() error {
	t.record("term.raw")
	return nil
}

func (t *fakeTerminal) Restore() error {
	t.record("term.restore")
	return nil
}

func (t *fakeTerminal) EnterAlternateScreen() error {
	t.record("term.enter_alt")
	return nil
}

func (t *fakeTerminal) ExitAlternateScreen() error {
	t.record("term.exit_alt")
	return nil
}

func (t *fakeTerminal) Size() (cols, rows uint16, err error) {
	t.record("term.size")

	t.mu.Lock()
	defer t.mu.Unlock()

	return t.cols, t.rows, nil
}

func (t *fakeTerminal) ResizeChan() <-chan struct{} {
	return t.resize
}

func (t *fakeTerminal) setSize(cols, rows uint16) {
	t.mu.Lock()
	t.cols = cols
	t.rows = rows
	t.mu.Unlock()
}

func (t *fakeTerminal) notifyResize() {
	t.resize <- struct{}{}
}

func (t *fakeTerminal) record(call string) {
	if t.recorder != nil {
		t.recorder.add(call)
	}
}

type fakeRenderer struct {
	recorder *callRecorder

	cols uint16
	rows uint16

	mu          sync.Mutex
	cells       map[cellPos]cellData
	flushes     int
	resizeCalls []resizeCall
	writes      []writeCall

	activeCalls atomic.Int32
	maxActive   atomic.Int32
}

type cellPos struct {
	row uint16
	col uint16
}

type cellData struct {
	ch rune
	fg uint8
	bg uint8
}

type resizeCall struct {
	cols uint16
	rows uint16
}

type writeCall struct {
	row uint16
	col uint16
	s   string
}

func newFakeRenderer(cols, rows uint16, recorder *callRecorder) *fakeRenderer {
	return &fakeRenderer{
		recorder: recorder,
		cols:     cols,
		rows:     rows,
		cells:    make(map[cellPos]cellData),
	}
}

func (r *fakeRenderer) SetCell(row, col uint16, ch rune, fg, bg uint8) error {
	r.enter()
	defer r.leave()

	r.record("ren.set_cell")
	r.mu.Lock()
	r.cells[cellPos{row: row, col: col}] = cellData{ch: ch, fg: fg, bg: bg}
	r.mu.Unlock()
	return nil
}

func (r *fakeRenderer) WriteString(row, col uint16, s string, fg, bg uint8) {
	r.enter()
	defer r.leave()

	r.record("ren.write_string")
	r.mu.Lock()
	defer r.mu.Unlock()

	r.writes = append(r.writes, writeCall{row: row, col: col, s: s})
	currentCol := col
	for _, ch := range s {
		if currentCol > r.cols {
			break
		}
		r.cells[cellPos{row: row, col: currentCol}] = cellData{ch: ch, fg: fg, bg: bg}
		currentCol++
	}
}

func (r *fakeRenderer) Clear() {
	r.enter()
	defer r.leave()

	r.record("ren.clear")
	r.mu.Lock()
	r.cells = make(map[cellPos]cellData)
	r.mu.Unlock()
}

func (r *fakeRenderer) ClearRegion(rowStart, rowEnd uint16) {
	r.enter()
	defer r.leave()

	r.record("ren.clear_region")
	r.mu.Lock()
	defer r.mu.Unlock()

	for pos := range r.cells {
		if pos.row >= rowStart && pos.row <= rowEnd {
			delete(r.cells, pos)
		}
	}
}

func (r *fakeRenderer) Flush() error {
	r.enter()
	defer r.leave()

	r.record("ren.flush")
	r.mu.Lock()
	r.flushes++
	r.mu.Unlock()
	return nil
}

func (r *fakeRenderer) Resize(cols, rows uint16) error {
	r.enter()
	defer r.leave()

	r.record("ren.resize")
	r.mu.Lock()
	r.cols = cols
	r.rows = rows
	r.cells = make(map[cellPos]cellData)
	r.resizeCalls = append(r.resizeCalls, resizeCall{cols: cols, rows: rows})
	r.mu.Unlock()
	return nil
}

func (r *fakeRenderer) flushCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.flushes
}

func (r *fakeRenderer) resizeCallsSnapshot() []resizeCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]resizeCall(nil), r.resizeCalls...)
}

func (r *fakeRenderer) writesSnapshot() []writeCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]writeCall(nil), r.writes...)
}

func (r *fakeRenderer) renderedRows() map[uint16]string {
	r.mu.Lock()
	defer r.mu.Unlock()

	rows := make(map[uint16]string, r.rows)
	for row := uint16(1); row <= r.rows; row++ {
		line := make([]rune, 0, r.cols)
		for col := uint16(1); col <= r.cols; col++ {
			cell, ok := r.cells[cellPos{row: row, col: col}]
			if !ok {
				line = append(line, ' ')
				continue
			}
			line = append(line, cell.ch)
		}
		rows[row] = string(line)
	}
	return rows
}

func (r *fakeRenderer) maxConcurrent() int32 {
	return r.maxActive.Load()
}

func (r *fakeRenderer) record(call string) {
	if r.recorder != nil {
		r.recorder.add(call)
	}
}

func (r *fakeRenderer) enter() {
	active := r.activeCalls.Add(1)
	for {
		current := r.maxActive.Load()
		if active <= current {
			break
		}
		if r.maxActive.CompareAndSwap(current, active) {
			break
		}
	}
}

func (r *fakeRenderer) leave() {
	r.activeCalls.Add(-1)
}

type callRecorder struct {
	mu    sync.Mutex
	calls []string
}

func newCallRecorder() *callRecorder {
	return &callRecorder{}
}

func (r *callRecorder) add(call string) {
	r.mu.Lock()
	r.calls = append(r.calls, call)
	r.mu.Unlock()
}

func (r *callRecorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.calls...)
}

func assertSubsequence(t *testing.T, calls, want []string) {
	t.Helper()

	if len(want) == 0 {
		return
	}

	idx := 0
	for _, call := range calls {
		if call == want[idx] {
			idx++
			if idx == len(want) {
				return
			}
		}
	}

	t.Fatalf("calls = %v, want subsequence %v", calls, want)
}
