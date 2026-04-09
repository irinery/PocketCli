//go:build linux || darwin

package terminal

import (
	"errors"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

const (
	resizeBufferSize = 256
	seqHideCursor    = "\033[?25l"
	seqShowCursor    = "\033[?25h"
	seqEnterAlt      = "\033[?1049h" + seqHideCursor
	seqExitAlt       = "\033[?1049l" + seqShowCursor
)

var (
	// ErrNotATTY reports that the provided file descriptor is not attached to a terminal.
	ErrNotATTY = errors.New("terminal: fd is not a TTY")
	// ErrAlreadyRaw reports that raw mode was requested while it is already active.
	ErrAlreadyRaw = errors.New("terminal: already in raw mode")
	// ErrNotInRawMode reports that restore was requested before entering raw mode.
	ErrNotInRawMode = errors.New("terminal: not in raw mode")
	// ErrTerminalWrite reports a write failure while emitting terminal escape sequences.
	ErrTerminalWrite = errors.New("terminal: write failed")
	// ErrTerminalSize reports that the terminal size could not be read.
	ErrTerminalSize = errors.New("terminal: cannot read terminal size")
	// ErrSyscall reports a syscall failure while manipulating terminal state.
	ErrSyscall = errors.New("terminal: syscall failed")

	terminalSignals = signalManager{
		terminals: make(map[*Terminal]struct{}),
	}

	readTermiosFunc  = readTermios
	writeTermiosFunc = writeTermios
	readWinsizeFunc  = readWinsize
	writeAllFunc     = writeAll
)

// Terminal wraps the low-level terminal state required by the TUI runtime.
type Terminal struct {
	fd       int
	mu       sync.Mutex
	original Termios
	inRaw    bool
	resize   chan struct{}
}

type windowSize struct {
	Row    uint16
	Col    uint16
	Xpixel uint16
	Ypixel uint16
}

// New validates the file descriptor and returns a terminal handle bound to it.
func New(fd int) (*Terminal, error) {
	if _, err := readTermiosFunc(fd); err != nil {
		return nil, ErrNotATTY
	}

	term := &Terminal{
		fd:     fd,
		resize: make(chan struct{}, resizeBufferSize),
	}
	terminalSignals.register(term)
	return term, nil
}

// RawMode switches the terminal to raw mode and saves the previous state.
func (t *Terminal) RawMode() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.inRaw {
		return ErrAlreadyRaw
	}

	original, err := readTermiosFunc(t.fd)
	if err != nil {
		return ErrSyscall
	}

	raw := original
	makeRaw(&raw)
	if err := writeTermiosFunc(t.fd, raw); err != nil {
		return ErrSyscall
	}

	t.original = original
	t.inRaw = true
	return nil
}

// Restore restores the terminal state saved by RawMode.
func (t *Terminal) Restore() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.restoreLocked()
}

// EnterAlternateScreen switches to the terminal alternate screen and hides the cursor.
func (t *Terminal) EnterAlternateScreen() error {
	if err := writeAllFunc(t.fd, []byte(seqEnterAlt)); err != nil {
		return ErrTerminalWrite
	}
	return nil
}

// ExitAlternateScreen restores the primary screen and shows the cursor.
func (t *Terminal) ExitAlternateScreen() error {
	if err := writeAllFunc(t.fd, []byte(seqExitAlt)); err != nil {
		return ErrTerminalWrite
	}
	return nil
}

// Size returns the current terminal size in columns and rows.
func (t *Terminal) Size() (cols uint16, rows uint16, err error) {
	size, err := readWinsizeFunc(t.fd)
	if err != nil {
		return 0, 0, ErrTerminalSize
	}
	return size.Col, size.Row, nil
}

// ResizeChan returns the SIGWINCH notification channel associated with the terminal.
func (t *Terminal) ResizeChan() <-chan struct{} {
	return t.resize
}

func (t *Terminal) restoreLocked() error {
	if !t.inRaw {
		return ErrNotInRawMode
	}
	if err := writeTermiosFunc(t.fd, t.original); err != nil {
		return ErrSyscall
	}
	t.inRaw = false
	return nil
}

func (t *Terminal) restoreFromSignal() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.inRaw {
		return
	}
	if err := writeTermiosFunc(t.fd, t.original); err != nil {
		return
	}
	t.inRaw = false
}

func (t *Terminal) notifyResize() {
	select {
	case t.resize <- struct{}{}:
	default:
	}
}

func (t *Terminal) release() {
	terminalSignals.unregister(t)
}

type signalManager struct {
	once      sync.Once
	mu        sync.RWMutex
	terminals map[*Terminal]struct{}
}

func (m *signalManager) register(t *Terminal) {
	m.once.Do(m.start)

	m.mu.Lock()
	m.terminals[t] = struct{}{}
	m.mu.Unlock()
}

func (m *signalManager) unregister(t *Terminal) {
	m.mu.Lock()
	delete(m.terminals, t)
	m.mu.Unlock()
}

func (m *signalManager) start() {
	signals := make(chan os.Signal, 32)
	signal.Notify(signals, syscall.SIGWINCH, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		for sig := range signals {
			for _, term := range m.snapshot() {
				switch sig {
				case syscall.SIGWINCH:
					term.notifyResize()
				case syscall.SIGINT, syscall.SIGTERM:
					term.restoreFromSignal()
				}
			}
		}
	}()
}

func (m *signalManager) snapshot() []*Terminal {
	m.mu.RLock()
	defer m.mu.RUnlock()

	terms := make([]*Terminal, 0, len(m.terminals))
	for term := range m.terminals {
		terms = append(terms, term)
	}
	return terms
}

func writeAll(fd int, data []byte) error {
	for len(data) > 0 {
		n, err := syscall.Write(fd, data)
		if err == syscall.EINTR {
			continue
		}
		if err != nil {
			return err
		}
		if n <= 0 {
			return syscall.EIO
		}
		data = data[n:]
	}
	return nil
}
