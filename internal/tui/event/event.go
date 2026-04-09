package event

import (
	"context"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	eventBufferSize      = 256
	readBufferSize       = 64
	defaultEscTimeout    = 50 * time.Millisecond
	maxEscapeSequenceLen = 8
)

// EventType identifies the kind of event emitted by the loop.
type EventType uint8

const (
	// EventKey reports a decoded keyboard event.
	EventKey EventType = iota
	// EventResize reports a resize notification forwarded by the terminal layer.
	EventResize
	// EventTick reports a periodic timer tick.
	EventTick
)

// Event is a typed signal emitted by the EventLoop.
type Event struct {
	Type EventType
	Key  KeyEvent
}

// KeyEvent stores the decoded key and, for printable characters, the rune value.
type KeyEvent struct {
	Key  Key
	Rune rune
}

// Key identifies a supported keyboard input.
type Key uint16

const (
	// KeyRune carries a printable rune in KeyEvent.Rune.
	KeyRune Key = iota
	// KeyArrowUp is the up arrow key.
	KeyArrowUp
	// KeyArrowDown is the down arrow key.
	KeyArrowDown
	// KeyArrowLeft is the left arrow key.
	KeyArrowLeft
	// KeyArrowRight is the right arrow key.
	KeyArrowRight
	// KeyEnter is the enter key.
	KeyEnter
	// KeyBackspace is the backspace key.
	KeyBackspace
	// KeyTab is the tab key.
	KeyTab
	// KeyEsc is the escape key.
	KeyEsc
	// KeyCtrlC is Ctrl+C.
	KeyCtrlC
	// KeyCtrlD is Ctrl+D.
	KeyCtrlD
	// KeyCtrlL is Ctrl+L.
	KeyCtrlL
	// KeyCtrlZ is Ctrl+Z.
	KeyCtrlZ
	// KeyF1 is function key F1.
	KeyF1
	// KeyF2 is function key F2.
	KeyF2
	// KeyF3 is function key F3.
	KeyF3
	// KeyF4 is function key F4.
	KeyF4
	// KeyF5 is function key F5.
	KeyF5
	// KeyF6 is function key F6.
	KeyF6
	// KeyF7 is function key F7.
	KeyF7
	// KeyF8 is function key F8.
	KeyF8
	// KeyF9 is function key F9.
	KeyF9
	// KeyF10 is function key F10.
	KeyF10
	// KeyUnknown represents an unmapped key sequence.
	KeyUnknown
)

var knownEscapeKeys = map[string]Key{
	"\x1b[A":   KeyArrowUp,
	"\x1b[B":   KeyArrowDown,
	"\x1b[C":   KeyArrowRight,
	"\x1b[D":   KeyArrowLeft,
	"\x1bOP":   KeyF1,
	"\x1bOQ":   KeyF2,
	"\x1bOR":   KeyF3,
	"\x1bOS":   KeyF4,
	"\x1b[15~": KeyF5,
}

// EventLoop reads raw stdin bytes and publishes typed events for the TUI runtime.
type EventLoop struct {
	// DroppedEvents counts events discarded because the output channel was full.
	DroppedEvents uint64

	in           io.Reader
	resize       <-chan struct{}
	out          chan Event
	tickInterval time.Duration
	escTimeout   time.Duration

	startOnce sync.Once
}

// New creates a new EventLoop with a fixed-size event buffer.
func New(in io.Reader, resize <-chan struct{}, tickInterval time.Duration) *EventLoop {
	return &EventLoop{
		in:           in,
		resize:       resize,
		out:          make(chan Event, eventBufferSize),
		tickInterval: tickInterval,
		escTimeout:   defaultEscTimeout,
	}
}

// Start launches the internal event loop goroutine and returns immediately.
func (el *EventLoop) Start(ctx context.Context) {
	el.startOnce.Do(func() {
		go el.run(ctx)
	})
}

// Events exposes the read-only event stream produced by the loop.
func (el *EventLoop) Events() <-chan Event {
	return el.out
}

type readResult struct {
	data []byte
	err  error
}

type escapeParser struct {
	active bool
	seq    []byte
	timer  *time.Timer
	timerC <-chan time.Time
}

func (el *EventLoop) run(ctx context.Context) {
	defer close(el.out)

	done := make(chan struct{})
	defer close(done)

	results := make(chan readResult, 1)
	go el.readInput(done, results)

	resize := el.resize

	var (
		pending []byte
		parser  escapeParser
		ticker  *time.Ticker
		tickC   <-chan time.Time
	)

	if el.tickInterval > 0 {
		ticker = time.NewTicker(el.tickInterval)
		tickC = ticker.C
		defer ticker.Stop()
	}

	for {
		if len(pending) > 0 {
			next := pending[0]
			pending = pending[1:]
			el.processByte(next, &parser)
			continue
		}

		select {
		case <-ctx.Done():
			parser.clear()
			return
		case <-tickC:
			el.emit(Event{Type: EventTick})
		case _, ok := <-resize:
			if !ok {
				resize = nil
				continue
			}
			el.emit(Event{Type: EventResize})
		case <-parser.timerC:
			el.emitKey(KeyEsc)
			parser.clear()
		case result, ok := <-results:
			if !ok {
				parser.flush(el)
				return
			}
			if len(result.data) > 0 {
				pending = append(pending, result.data...)
			}
			if result.err != nil {
				parser.flush(el)
				return
			}
		}
	}
}

func (el *EventLoop) readInput(done <-chan struct{}, out chan<- readResult) {
	defer close(out)

	if el.in == nil {
		return
	}

	buf := make([]byte, readBufferSize)
	for {
		n, err := el.in.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			if !sendReadResult(done, out, readResult{data: chunk}) {
				return
			}
		}
		if err != nil {
			sendReadResult(done, out, readResult{err: err})
			return
		}
	}
}

func sendReadResult(done <-chan struct{}, out chan<- readResult, result readResult) bool {
	select {
	case out <- result:
		return true
	case <-done:
		return false
	}
}

func (el *EventLoop) processByte(b byte, parser *escapeParser) {
	for {
		if !parser.active {
			if b == 0x1b {
				parser.start(el.escTimeout)
				return
			}
			el.emit(Event{Type: EventKey, Key: decodeByte(b)})
			return
		}

		replay, next := el.consumeEscapeByte(b, parser)
		if !replay {
			return
		}
		b = next
	}
}

func (el *EventLoop) consumeEscapeByte(b byte, parser *escapeParser) (bool, byte) {
	if len(parser.seq) == 1 && b != '[' && b != 'O' {
		parser.clear()
		el.emitKey(KeyEsc)
		return true, b
	}

	parser.seq = append(parser.seq, b)

	if key, ok := knownEscapeKeys[string(parser.seq)]; ok {
		parser.clear()
		el.emitKey(key)
		return false, 0
	}

	if hasKnownEscapePrefix(parser.seq) {
		parser.rearm(el.escTimeout)
		return false, 0
	}

	switch parser.seq[1] {
	case '[':
		last := parser.seq[len(parser.seq)-1]
		if isFinalCSIByte(last) || len(parser.seq) >= maxEscapeSequenceLen {
			parser.clear()
			el.emitKey(KeyUnknown)
			return false, 0
		}
		parser.rearm(el.escTimeout)
		return false, 0
	case 'O':
		if len(parser.seq) >= 3 {
			parser.clear()
			el.emitKey(KeyUnknown)
			return false, 0
		}
		parser.rearm(el.escTimeout)
		return false, 0
	default:
		parser.clear()
		el.emitKey(KeyEsc)
		return true, b
	}
}

func (el *EventLoop) emit(event Event) {
	select {
	case el.out <- event:
	default:
		atomic.AddUint64(&el.DroppedEvents, 1)
	}
}

func (el *EventLoop) emitKey(key Key) {
	el.emit(Event{
		Type: EventKey,
		Key:  KeyEvent{Key: key},
	})
}

func decodeByte(b byte) KeyEvent {
	switch b {
	case 0x0d:
		return KeyEvent{Key: KeyEnter}
	case 0x7f:
		return KeyEvent{Key: KeyBackspace}
	case 0x09:
		return KeyEvent{Key: KeyTab}
	case 0x03:
		return KeyEvent{Key: KeyCtrlC}
	case 0x04:
		return KeyEvent{Key: KeyCtrlD}
	case 0x0c:
		return KeyEvent{Key: KeyCtrlL}
	case 0x1a:
		return KeyEvent{Key: KeyCtrlZ}
	default:
		if b >= 0x20 {
			return KeyEvent{Key: KeyRune, Rune: rune(b)}
		}
		return KeyEvent{Key: KeyUnknown}
	}
}

func hasKnownEscapePrefix(seq []byte) bool {
	current := string(seq)
	for candidate := range knownEscapeKeys {
		if strings.HasPrefix(candidate, current) {
			return true
		}
	}
	return false
}

func isFinalCSIByte(b byte) bool {
	return b >= 0x40 && b <= 0x7e
}

func (p *escapeParser) start(timeout time.Duration) {
	p.active = true
	p.seq = append(p.seq[:0], 0x1b)
	p.rearm(timeout)
}

func (p *escapeParser) rearm(timeout time.Duration) {
	if p.timer == nil {
		p.timer = time.NewTimer(timeout)
		p.timerC = p.timer.C
		return
	}
	if !p.timer.Stop() {
		select {
		case <-p.timer.C:
		default:
		}
	}
	p.timer.Reset(timeout)
	p.timerC = p.timer.C
}

func (p *escapeParser) clear() {
	if p.timer != nil {
		if !p.timer.Stop() {
			select {
			case <-p.timer.C:
			default:
			}
		}
	}
	p.active = false
	p.seq = p.seq[:0]
	p.timer = nil
	p.timerC = nil
}

func (p *escapeParser) flush(el *EventLoop) {
	if !p.active {
		p.clear()
		return
	}
	el.emitKey(KeyEsc)
	p.clear()
}
