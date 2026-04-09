//go:build darwin

package terminal

import (
	"syscall"
	"unsafe"
)

// Termios is the Darwin terminal state used by the TUI terminal package.
type Termios syscall.Termios

func readTermios(fd int) (Termios, error) {
	var state syscall.Termios

	for {
		_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(syscall.TIOCGETA), uintptr(unsafe.Pointer(&state)))
		if errno == syscall.EINTR {
			continue
		}
		if errno != 0 {
			return Termios{}, errno
		}
		return Termios(state), nil
	}
}

func writeTermios(fd int, state Termios) error {
	raw := syscall.Termios(state)

	for {
		_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(syscall.TIOCSETA), uintptr(unsafe.Pointer(&raw)))
		if errno == syscall.EINTR {
			continue
		}
		if errno != 0 {
			return errno
		}
		return nil
	}
}

func readWinsize(fd int) (windowSize, error) {
	var size windowSize

	for {
		_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(syscall.TIOCGWINSZ), uintptr(unsafe.Pointer(&size)))
		if errno == syscall.EINTR {
			continue
		}
		if errno != 0 {
			return windowSize{}, errno
		}
		return size, nil
	}
}

func makeRaw(state *Termios) {
	raw := (*syscall.Termios)(unsafe.Pointer(state))
	raw.Iflag &^= syscall.IGNBRK | syscall.BRKINT | syscall.PARMRK | syscall.ISTRIP | syscall.INLCR | syscall.IGNCR | syscall.ICRNL | syscall.IXON
	raw.Oflag &^= syscall.OPOST
	raw.Lflag &^= syscall.ECHO | syscall.ECHONL | syscall.ICANON | syscall.ISIG | syscall.IEXTEN
	raw.Cflag &^= syscall.CSIZE | syscall.PARENB
	raw.Cflag |= syscall.CS8
	raw.Cc[syscall.VMIN] = 1
	raw.Cc[syscall.VTIME] = 0
}
