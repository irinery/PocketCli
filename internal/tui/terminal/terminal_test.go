//go:build linux || darwin

package terminal

import (
	"errors"
	"io"
	"os"
	"reflect"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

func TestRawModeDisablesEchoAndICANON(t *testing.T) {
	original := sampleTermios()
	var applied Termios

	stubTerminalOps(t, terminalOpsStub{
		readTermios: func(fd int) (Termios, error) {
			return original, nil
		},
		writeTermios: func(fd int, state Termios) error {
			applied = state
			return nil
		},
	})

	term, err := New(10)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer term.release()

	if err := term.RawMode(); err != nil {
		t.Fatalf("RawMode() error = %v", err)
	}

	state := asSyscallTermios(applied)
	if state.Lflag&syscall.ECHO != 0 {
		t.Fatalf("ECHO still enabled after RawMode(): %#x", state.Lflag)
	}
	if state.Lflag&syscall.ICANON != 0 {
		t.Fatalf("ICANON still enabled after RawMode(): %#x", state.Lflag)
	}
}

func TestRestoreRestoresOriginalState(t *testing.T) {
	original := sampleTermios()
	var writes []Termios

	stubTerminalOps(t, terminalOpsStub{
		readTermios: func(fd int) (Termios, error) {
			return original, nil
		},
		writeTermios: func(fd int, state Termios) error {
			writes = append(writes, state)
			return nil
		},
	})

	term, err := New(11)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer term.release()

	if err := term.RawMode(); err != nil {
		t.Fatalf("RawMode() error = %v", err)
	}
	if err := term.Restore(); err != nil {
		t.Fatalf("Restore() error = %v", err)
	}
	if len(writes) != 2 {
		t.Fatalf("writeTermios() called %d times, want 2", len(writes))
	}
	if !reflect.DeepEqual(writes[1], original) {
		t.Fatalf("Restore() wrote a different state than the original one")
	}
}

func TestRestoreWithoutRawModeReturnsErrNotInRawMode(t *testing.T) {
	stubTerminalOps(t, terminalOpsStub{
		readTermios: func(fd int) (Termios, error) {
			return sampleTermios(), nil
		},
		writeTermios: func(fd int, state Termios) error {
			t.Fatal("writeTermios() should not be called")
			return nil
		},
	})

	term, err := New(12)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer term.release()

	err = term.Restore()
	if !errors.Is(err, ErrNotInRawMode) {
		t.Fatalf("Restore() error = %v, want %v", err, ErrNotInRawMode)
	}
}

func TestEnterAlternateScreenWritesExactSequence(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	defer reader.Close()

	term := &Terminal{fd: int(writer.Fd()), resize: make(chan struct{}, 1)}
	if err := term.EnterAlternateScreen(); err != nil {
		t.Fatalf("EnterAlternateScreen() error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("writer.Close() error = %v", err)
	}

	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("io.ReadAll() error = %v", err)
	}
	if string(data) != seqEnterAlt {
		t.Fatalf("EnterAlternateScreen() wrote %q, want %q", string(data), seqEnterAlt)
	}
}

func TestExitAlternateScreenWritesExactSequence(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	defer reader.Close()

	term := &Terminal{fd: int(writer.Fd()), resize: make(chan struct{}, 1)}
	if err := term.ExitAlternateScreen(); err != nil {
		t.Fatalf("ExitAlternateScreen() error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("writer.Close() error = %v", err)
	}

	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("io.ReadAll() error = %v", err)
	}
	if string(data) != seqExitAlt {
		t.Fatalf("ExitAlternateScreen() wrote %q, want %q", string(data), seqExitAlt)
	}
}

func TestSizeReturnsKnownDimensions(t *testing.T) {
	stubTerminalOps(t, terminalOpsStub{
		readTermios: func(fd int) (Termios, error) {
			return sampleTermios(), nil
		},
		readWinsize: func(fd int) (windowSize, error) {
			return windowSize{Col: 80, Row: 24}, nil
		},
	})

	term, err := New(13)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer term.release()

	cols, rows, err := term.Size()
	if err != nil {
		t.Fatalf("Size() error = %v", err)
	}
	if cols != 80 || rows != 24 {
		t.Fatalf("Size() = (%d, %d), want (80, 24)", cols, rows)
	}
}

func TestSizeReturnsErrTerminalSizeOnInvalidFD(t *testing.T) {
	stubTerminalOps(t, terminalOpsStub{
		readWinsize: func(fd int) (windowSize, error) {
			return windowSize{}, syscall.ENOTTY
		},
	})

	term := &Terminal{fd: 14, resize: make(chan struct{}, 1)}
	cols, rows, err := term.Size()
	if !errors.Is(err, ErrTerminalSize) {
		t.Fatalf("Size() error = %v, want %v", err, ErrTerminalSize)
	}
	if cols != 0 || rows != 0 {
		t.Fatalf("Size() = (%d, %d), want (0, 0)", cols, rows)
	}
}

func TestResizeChanReceivesSIGWINCH(t *testing.T) {
	stubTerminalOps(t, terminalOpsStub{
		readTermios: func(fd int) (Termios, error) {
			return sampleTermios(), nil
		},
	})

	term, err := New(15)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer term.release()

	if err := syscall.Kill(os.Getpid(), syscall.SIGWINCH); err != nil {
		t.Fatalf("syscall.Kill(SIGWINCH) error = %v", err)
	}

	select {
	case <-term.ResizeChan():
	case <-time.After(200 * time.Millisecond):
		t.Fatal("ResizeChan() did not receive SIGWINCH within 200ms")
	}
}

func TestSignalHandlerRestoresTerminalOnSIGTERM(t *testing.T) {
	original := sampleTermios()
	var (
		mu     sync.Mutex
		writes []Termios
	)

	stubTerminalOps(t, terminalOpsStub{
		readTermios: func(fd int) (Termios, error) {
			return original, nil
		},
		writeTermios: func(fd int, state Termios) error {
			mu.Lock()
			writes = append(writes, state)
			mu.Unlock()
			return nil
		},
	})

	term, err := New(16)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer term.release()

	if err := term.RawMode(); err != nil {
		t.Fatalf("RawMode() error = %v", err)
	}
	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("syscall.Kill(SIGTERM) error = %v", err)
	}

	waitFor(t, 200*time.Millisecond, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(writes) >= 2 && reflect.DeepEqual(writes[len(writes)-1], original)
	})
}

func TestSizePreservesValuesBelowRuntimeMinimum(t *testing.T) {
	stubTerminalOps(t, terminalOpsStub{
		readTermios: func(fd int) (Termios, error) {
			return sampleTermios(), nil
		},
		readWinsize: func(fd int) (windowSize, error) {
			return windowSize{Col: 5, Row: 2}, nil
		},
	})

	term, err := New(17)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer term.release()

	cols, rows, err := term.Size()
	if err != nil {
		t.Fatalf("Size() error = %v", err)
	}
	if cols != 5 || rows != 2 {
		t.Fatalf("Size() = (%d, %d), want (5, 2)", cols, rows)
	}
}

func TestNewReturnsErrNotATTY(t *testing.T) {
	stubTerminalOps(t, terminalOpsStub{
		readTermios: func(fd int) (Termios, error) {
			return Termios{}, syscall.ENOTTY
		},
	})

	term, err := New(18)
	if !errors.Is(err, ErrNotATTY) {
		t.Fatalf("New() error = %v, want %v", err, ErrNotATTY)
	}
	if term != nil {
		t.Fatal("New() returned a terminal for a non-TTY fd")
	}
}

func TestRawModeReturnsErrAlreadyRaw(t *testing.T) {
	stubTerminalOps(t, terminalOpsStub{
		readTermios: func(fd int) (Termios, error) {
			return sampleTermios(), nil
		},
		writeTermios: func(fd int, state Termios) error {
			return nil
		},
	})

	term, err := New(19)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer term.release()

	if err := term.RawMode(); err != nil {
		t.Fatalf("RawMode() error = %v", err)
	}
	if err := term.RawMode(); !errors.Is(err, ErrAlreadyRaw) {
		t.Fatalf("RawMode() second call error = %v, want %v", err, ErrAlreadyRaw)
	}
}

func TestRawModeReturnsErrSyscallWhenReadFails(t *testing.T) {
	var calls int

	stubTerminalOps(t, terminalOpsStub{
		readTermios: func(fd int) (Termios, error) {
			calls++
			if calls == 1 {
				return sampleTermios(), nil
			}
			return Termios{}, syscall.EBADF
		},
	})

	term, err := New(20)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer term.release()

	if err := term.RawMode(); !errors.Is(err, ErrSyscall) {
		t.Fatalf("RawMode() error = %v, want %v", err, ErrSyscall)
	}
}

func TestRestoreReturnsErrSyscallWhenWriteFails(t *testing.T) {
	original := sampleTermios()
	var calls int

	stubTerminalOps(t, terminalOpsStub{
		readTermios: func(fd int) (Termios, error) {
			return original, nil
		},
		writeTermios: func(fd int, state Termios) error {
			calls++
			if calls == 1 {
				return nil
			}
			return syscall.EBADF
		},
	})

	term, err := New(21)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer term.release()

	if err := term.RawMode(); err != nil {
		t.Fatalf("RawMode() error = %v", err)
	}
	if err := term.Restore(); !errors.Is(err, ErrSyscall) {
		t.Fatalf("Restore() error = %v, want %v", err, ErrSyscall)
	}
}

func TestEnterAlternateScreenReturnsErrTerminalWrite(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	defer reader.Close()

	term := &Terminal{fd: int(writer.Fd()), resize: make(chan struct{}, 1)}
	if err := writer.Close(); err != nil {
		t.Fatalf("writer.Close() error = %v", err)
	}
	if err := term.EnterAlternateScreen(); !errors.Is(err, ErrTerminalWrite) {
		t.Fatalf("EnterAlternateScreen() error = %v, want %v", err, ErrTerminalWrite)
	}
}

func TestExitAlternateScreenReturnsErrTerminalWrite(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	defer reader.Close()

	term := &Terminal{fd: int(writer.Fd()), resize: make(chan struct{}, 1)}
	if err := writer.Close(); err != nil {
		t.Fatalf("writer.Close() error = %v", err)
	}
	if err := term.ExitAlternateScreen(); !errors.Is(err, ErrTerminalWrite) {
		t.Fatalf("ExitAlternateScreen() error = %v, want %v", err, ErrTerminalWrite)
	}
}

type terminalOpsStub struct {
	readTermios  func(fd int) (Termios, error)
	writeTermios func(fd int, state Termios) error
	readWinsize  func(fd int) (windowSize, error)
	writeAll     func(fd int, data []byte) error
}

func stubTerminalOps(t *testing.T, stub terminalOpsStub) {
	t.Helper()

	prevReadTermios := readTermiosFunc
	prevWriteTermios := writeTermiosFunc
	prevReadWinsize := readWinsizeFunc
	prevWriteAll := writeAllFunc

	if stub.readTermios != nil {
		readTermiosFunc = stub.readTermios
	}
	if stub.writeTermios != nil {
		writeTermiosFunc = stub.writeTermios
	}
	if stub.readWinsize != nil {
		readWinsizeFunc = stub.readWinsize
	}
	if stub.writeAll != nil {
		writeAllFunc = stub.writeAll
	}

	t.Cleanup(func() {
		readTermiosFunc = prevReadTermios
		writeTermiosFunc = prevWriteTermios
		readWinsizeFunc = prevReadWinsize
		writeAllFunc = prevWriteAll
	})
}

func sampleTermios() Termios {
	var state Termios
	raw := (*syscall.Termios)(unsafe.Pointer(&state))
	raw.Iflag = syscall.ICRNL | syscall.IXON
	raw.Oflag = syscall.OPOST
	raw.Cflag = syscall.CS8 | syscall.PARENB
	raw.Lflag = syscall.ECHO | syscall.ICANON | syscall.ISIG | syscall.IEXTEN
	return state
}

func asSyscallTermios(state Termios) syscall.Termios {
	return *(*syscall.Termios)(unsafe.Pointer(&state))
}

func waitFor(t *testing.T, timeout time.Duration, predicate func() bool) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if predicate() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("condition not satisfied within %s", timeout)
}
