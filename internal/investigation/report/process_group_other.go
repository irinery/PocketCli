//go:build !(darwin || linux || freebsd || openbsd || netbsd)

package report

import "os/exec"

func stopProcessGroupOnCancel(_ *exec.Cmd) {}
