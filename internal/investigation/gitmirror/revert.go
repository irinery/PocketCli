package gitmirror

import (
	"context"
	"io"
	"os/exec"
	"strings"
)

const maxRevertMessageBytes = 64 << 10

// DeclaredRevertTarget reads only the bounded commit message and recognizes
// Git's exact revert trailer. A trailer is a declaration, not proof that the
// patch has the inverse effect. complete=false means the message was too large
// to inspect and must not be treated as a negative observation.
func (m *Mirror) DeclaredRevertTarget(ctx context.Context, oid string) (target string, complete bool, err error) {
	if !oidPattern.MatchString(oid) {
		return "", false, ErrCorrupt
	}
	command := exec.CommandContext(ctx, m.GitPath, "--git-dir="+m.Path, "show", "-s", "--format=%B", oid)
	command.Dir = m.Path
	command.Env = m.environment("", nil)
	command.Stderr = io.Discard
	stdout, err := command.StdoutPipe()
	if err != nil || command.Start() != nil {
		return "", false, ErrCorrupt
	}
	message, readErr := io.ReadAll(io.LimitReader(stdout, maxRevertMessageBytes+1))
	tooLarge := len(message) > maxRevertMessageBytes
	if tooLarge || readErr != nil {
		_ = command.Process.Kill()
	}
	waitErr := command.Wait()
	if ctx.Err() != nil {
		return "", false, ctx.Err()
	}
	if readErr != nil || (!tooLarge && waitErr != nil) {
		return "", false, ErrCorrupt
	}
	if tooLarge {
		return "", false, nil
	}
	const prefix = "This reverts commit "
	for _, line := range strings.Split(string(message), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if !strings.HasPrefix(line, prefix) || !strings.HasSuffix(line, ".") {
			continue
		}
		candidate := strings.TrimSuffix(strings.TrimPrefix(line, prefix), ".")
		if !oidPattern.MatchString(candidate) || candidate == oid {
			continue
		}
		if target != "" && target != candidate {
			return "", true, nil // ambiguous declarations
		}
		target = candidate
	}
	return target, true, nil
}
