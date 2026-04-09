package runtime

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"pocketcli/internal/tui/event"
)

const (
	minCols             uint16 = 10
	minRows             uint16 = 3
	maxHandlerDuration         = 50 * time.Millisecond
	terminalTooSmallMsg        = "runtime: terminal below minimum size (10x3)"
)

var (
	// ErrTerminalTooSmall reports a terminal size below the supported minimum.
	ErrTerminalTooSmall = errors.New("runtime: terminal below minimum size (10x3)")
	// ErrHandlerPanic reports that the event handler panicked during execution.
	ErrHandlerPanic = errors.New("runtime: handler panicked")
	// ErrSetupFailed reports that runtime initialization failed before the main loop started.
	ErrSetupFailed = errors.New("runtime: terminal setup failed")
)

// Mode defines the runtime orchestration strategy.
type Mode uint8

const (
	// ModeViewer refreshes on EventTick and lets the handler redraw the frame.
	ModeViewer Mode = iota
	// ModeAgent skips timer-driven redraws and is primarily updated through AppendLine.
	ModeAgent
)

// ITerminal defines the terminal contract required by Runtime.
type ITerminal interface {
	RawMode() error
	Restore() error
	EnterAlternateScreen() error
	ExitAlternateScreen() error
	Size() (cols, rows uint16, err error)
	ResizeChan() <-chan struct{}
}

// IRenderer defines the renderer contract required by Runtime.
type IRenderer interface {
	SetCell(row, col uint16, ch rune, fg, bg uint8) error
	WriteString(row, col uint16, s string, fg, bg uint8)
	Clear()
	ClearRegion(rowStart, rowEnd uint16)
	Flush() error
	Resize(cols, rows uint16) error
}

// Handler receives events and may mutate the back-buffer via draw.
type Handler func(e event.Event, draw func(fn func(IRenderer)))

// Runtime connects terminal, event loop and renderer into a single TUI lifecycle.
type Runtime struct {
	term ITerminal
	loop *event.EventLoop
	ren  IRenderer
	mode Mode

	mu       sync.Mutex
	lines    []string
	maxLines int
	running  bool
}

// New creates a runtime bound to the provided terminal, event loop and renderer.
func New(term ITerminal, loop *event.EventLoop, ren IRenderer, mode Mode) *Runtime {
	return &Runtime{
		term: term,
		loop: loop,
		ren:  ren,
		mode: mode,
	}
}

// Run initializes the terminal, starts the event loop and blocks until shutdown.
func (r *Runtime) Run(ctx context.Context, handler Handler) error {
	if r.term == nil || r.loop == nil || r.ren == nil {
		return ErrSetupFailed
	}

	cols, rows, err := r.term.Size()
	if err != nil {
		return fmt.Errorf("%w: %v", ErrSetupFailed, err)
	}
	if !sizeSupported(cols, rows) {
		return ErrTerminalTooSmall
	}

	if err := r.term.RawMode(); err != nil {
		return fmt.Errorf("%w: %v", ErrSetupFailed, err)
	}
	rawEnabled := true

	if err := r.term.EnterAlternateScreen(); err != nil {
		if rawEnabled {
			_ = r.term.Restore()
		}
		return fmt.Errorf("%w: %v", ErrSetupFailed, err)
	}
	altEnabled := true

	loopCtx, cancelLoop := context.WithCancel(ctx)
	defer cancelLoop()

	r.mu.Lock()
	r.running = true
	r.maxLines = maxAgentLines(rows)
	r.trimLinesLocked()
	if r.mode == ModeAgent && len(r.lines) > 0 {
		r.renderAgentLocked()
		r.flushLocked()
	}
	r.mu.Unlock()

	defer func() {
		cancelLoop()

		r.mu.Lock()
		r.running = false
		r.mu.Unlock()

		r.teardown(altEnabled, rawEnabled)
	}()

	r.loop.Start(loopCtx)

	for {
		select {
		case <-ctx.Done():
			return nil
		case evt, ok := <-r.loop.Events():
			if !ok {
				return nil
			}
			if err := r.handleEvent(evt, handler); err != nil {
				return err
			}
		}
	}
}

// Draw serializes direct renderer mutations against Flush and AppendLine.
func (r *Runtime) Draw(fn func(IRenderer)) {
	if fn == nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	fn(r.ren)
}

// AppendLine appends a line to the agent history and refreshes the screen when running.
func (r *Runtime) AppendLine(line string) {
	if r.mode != ModeAgent {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.lines = append(r.lines, line)
	r.trimLinesLocked()

	if !r.running {
		return
	}

	r.renderAgentLocked()
	r.flushLocked()
}

func (r *Runtime) handleEvent(evt event.Event, handler Handler) error {
	switch evt.Type {
	case event.EventResize:
		return r.handleResizeEvent(evt, handler)
	case event.EventTick:
		if r.mode != ModeViewer {
			return nil
		}
		if err := r.callHandler(evt, handler); err != nil {
			return err
		}
		r.flush()
	case event.EventKey:
		if err := r.callHandler(evt, handler); err != nil {
			return err
		}
		if r.mode == ModeAgent {
			r.flush()
		}
	}

	return nil
}

func (r *Runtime) handleResizeEvent(evt event.Event, handler Handler) error {
	cols, rows, err := r.term.Size()
	if err != nil {
		log.Printf("runtime: warning: cannot read resized terminal size: %v", err)
		return nil
	}

	if !sizeSupported(cols, rows) {
		r.mu.Lock()
		r.maxLines = maxAgentLines(rows)
		r.renderTooSmallLocked()
		r.flushLocked()
		r.mu.Unlock()
		return nil
	}

	r.mu.Lock()
	if err := r.ren.Resize(cols, rows); err != nil {
		log.Printf("runtime: warning: cannot resize renderer: %v", err)
	}
	r.maxLines = maxAgentLines(rows)
	r.trimLinesLocked()
	if r.mode == ModeAgent {
		r.renderAgentLocked()
	}
	r.mu.Unlock()

	if err := r.callHandler(evt, handler); err != nil {
		return err
	}
	r.flush()

	return nil
}

func (r *Runtime) callHandler(evt event.Event, handler Handler) (err error) {
	if handler == nil {
		return nil
	}

	start := time.Now()
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("%w: %v", ErrHandlerPanic, recovered)
		}

		if elapsed := time.Since(start); elapsed > maxHandlerDuration {
			log.Printf("runtime: warning: handler took %s", elapsed)
		}
	}()

	handler(evt, r.Draw)
	return nil
}

func (r *Runtime) flush() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.flushLocked()
}

func (r *Runtime) flushLocked() {
	if err := r.ren.Flush(); err != nil {
		log.Printf("runtime: warning: flush failed: %v", err)
	}
}

func (r *Runtime) renderAgentLocked() {
	r.ren.Clear()

	if r.maxLines <= 0 {
		return
	}

	start := 0
	if len(r.lines) > r.maxLines {
		start = len(r.lines) - r.maxLines
	}

	row := uint16(1)
	for _, line := range r.lines[start:] {
		r.ren.WriteString(row, 1, line, 255, 255)
		row++
	}
}

func (r *Runtime) renderTooSmallLocked() {
	r.ren.Clear()
	r.ren.WriteString(1, 1, terminalTooSmallMsg, 255, 255)
}

func (r *Runtime) trimLinesLocked() {
	if r.maxLines <= 0 {
		return
	}
	if len(r.lines) <= r.maxLines {
		return
	}
	r.lines = append([]string(nil), r.lines[len(r.lines)-r.maxLines:]...)
}

func (r *Runtime) teardown(altEnabled, rawEnabled bool) {
	r.mu.Lock()
	r.ren.Clear()
	r.flushLocked()
	r.mu.Unlock()

	if altEnabled {
		if err := r.term.ExitAlternateScreen(); err != nil {
			log.Printf("runtime: warning: exit alternate screen failed: %v", err)
		}
	}
	if rawEnabled {
		if err := r.term.Restore(); err != nil {
			log.Printf("runtime: warning: restore terminal failed: %v", err)
		}
	}
}

func maxAgentLines(rows uint16) int {
	if rows == 0 {
		return 0
	}
	return int(rows - 1)
}

func sizeSupported(cols, rows uint16) bool {
	return cols >= minCols && rows >= minRows
}
