package ssh

import (
	"fmt"
	"os"
	"os/exec"
)

func Open(host string) error {
	cmd := exec.Command("ssh", host)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ssh failed: %w", err)
	}
	return nil
}

func Exec(host string, remoteCommand string) error {
	cmd := exec.Command("ssh", host, remoteCommand)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("remote exec failed: %w", err)
	}
	return nil
}
