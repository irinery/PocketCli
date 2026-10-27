//go:build darwin || linux || freebsd || openbsd || netbsd

package report

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// Chromium creates helper processes. Keep them in a private process group so
// cancelling a render cannot leave helpers running after the CLI exits.
func stopProcessGroupOnCancel(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
}
