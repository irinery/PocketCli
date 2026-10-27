package gitmirror

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"os/exec"
	"strings"
)

type ChangedFile struct {
	Path    string
	Status  string // A, M, D, T; rename detection is deliberately disabled.
	TreeOID string // Parent tree for deletions, commit tree otherwise.
}

type TreeEntry struct {
	Path string
	Mode string
	Type string
	OID  string
}

// EnsureTrees upgrades an older commit-only mirror to commits plus trees.
// Blobs remain filtered. A ref that moved since Observe is never substituted.
func (m *Mirror) EnsureTrees(ctx context.Context, remote string, secret []byte, ref, head string, maxAdditionalBytes int64) (int64, error) {
	if !m.allowedRemote(remote) || !oidPattern.MatchString(head) || !strings.HasPrefix(ref, "refs/heads/") {
		return 0, ErrUnavailable
	}
	if _, err := m.command(ctx, "", nil, "check-ref-format", ref); err != nil {
		return 0, ErrUnavailable
	}
	if m.treesComplete(ctx, head) {
		return 0, nil
	}
	if maxAdditionalBytes <= 0 {
		return 0, ErrBudget
	}
	before, err := directoryBytes(m.Path)
	if err != nil {
		return 0, ErrCorrupt
	}
	destination := "refs/pocketcli/trees/" + head
	peak, fetchErr := m.fetch(ctx, remote, secret, maxAdditionalBytes, before, "fetch", "--quiet", "--no-tags", "--no-recurse-submodules", "--no-auto-maintenance", "--refetch", "--filter=blob:none", remote, "+"+ref+":"+destination)
	after, err := directoryBytes(m.Path)
	if err != nil {
		return peak, ErrCorrupt
	}
	used := peak
	if after-before > used {
		used = after - before
	}
	if fetchErr != nil {
		return used, fetchErr
	}
	if used > maxAdditionalBytes {
		return used, ErrBudget
	}
	fetched, err := m.command(ctx, "", nil, "rev-parse", destination)
	if err != nil || strings.TrimSpace(string(fetched)) != head {
		return used, ErrMoved
	}
	if !m.treesComplete(ctx, head) {
		return used, ErrCorrupt
	}
	return used, nil
}

func (m *Mirror) treesComplete(ctx context.Context, head string) bool {
	if !oidPattern.MatchString(head) {
		return false
	}
	_, status := m.commandStatus(ctx, "", nil, "rev-list", "--objects", "--missing=error", "--filter=blob:none", head)
	return status == 0
}

// ChangedFiles compares a commit to its first parent (or the empty tree for
// a root commit). A merge therefore records the change it brought to the
// first-parent line; rename continuity is not inferred.
func (m *Mirror) ChangedFiles(ctx context.Context, oid string, maxFiles int) ([]ChangedFile, bool, error) {
	if !oidPattern.MatchString(oid) || maxFiles < 1 || maxFiles > 3000 {
		return nil, false, ErrCorrupt
	}
	parentsRaw, err := m.command(ctx, "", nil, "rev-list", "--parents", "-n", "1", oid)
	if err != nil {
		return nil, false, ErrCorrupt
	}
	parents := strings.Fields(string(parentsRaw))
	if len(parents) < 1 || parents[0] != oid {
		return nil, false, ErrCorrupt
	}
	for _, parent := range parents[1:] {
		if !oidPattern.MatchString(parent) {
			return nil, false, ErrCorrupt
		}
	}
	currentTree, err := m.treeOID(ctx, oid)
	if err != nil {
		return nil, false, err
	}
	args := []string{"diff-tree", "--no-commit-id", "--name-status", "-r", "-z", "--no-renames"}
	oldTree := ""
	if len(parents) == 1 {
		args = append(args, "--root", oid)
	} else {
		oldTree, err = m.treeOID(ctx, parents[1])
		if err != nil {
			return nil, false, err
		}
		args = append(args, parents[1], oid)
	}
	command := exec.CommandContext(ctx, m.GitPath, append([]string{"--git-dir=" + m.Path}, args...)...)
	command.Dir = m.Path
	command.Env = m.environment("", nil)
	command.Stderr = io.Discard
	stdout, err := command.StdoutPipe()
	if err != nil || command.Start() != nil {
		return nil, false, ErrCorrupt
	}
	const maxDiffBytes = 16 << 20
	data, readErr := io.ReadAll(io.LimitReader(stdout, maxDiffBytes+1))
	truncated := len(data) > maxDiffBytes
	if truncated || readErr != nil {
		_ = command.Process.Kill()
	}
	waitErr := command.Wait()
	if ctx.Err() != nil {
		return nil, false, ctx.Err()
	}
	if readErr != nil || (!truncated && waitErr != nil) {
		return nil, false, ErrCorrupt
	}
	if truncated {
		data = data[:maxDiffBytes]
	}
	parts := bytes.Split(data, []byte{0})
	if len(parts) > 0 && len(parts[len(parts)-1]) == 0 {
		parts = parts[:len(parts)-1]
	} else if len(parts) > 0 {
		parts = parts[:len(parts)-1]
		truncated = true
	}
	if len(parts)%2 != 0 {
		parts = parts[:len(parts)-1]
		truncated = true
	}
	files := make([]ChangedFile, 0)
	for i := 0; i+1 < len(parts); i += 2 {
		if len(files) == maxFiles {
			return files, false, nil
		}
		status, path := string(parts[i]), string(parts[i+1])
		if (status != "A" && status != "M" && status != "D" && status != "T") || path == "" {
			truncated = true
			continue
		}
		tree := currentTree
		if status == "D" {
			tree = oldTree
		}
		files = append(files, ChangedFile{Path: path, Status: status, TreeOID: tree})
	}
	return files, !truncated, nil
}

// WalkTree reads tree metadata only. Callers decide which paths are manifests;
// the callback can stop a huge tree and mark its coverage partial.
func (m *Mirror) WalkTree(ctx context.Context, treeOID string, visit func(TreeEntry) (bool, error)) (bool, error) {
	if !oidPattern.MatchString(treeOID) {
		return false, ErrCorrupt
	}
	command := exec.CommandContext(ctx, m.GitPath, "--git-dir="+m.Path, "ls-tree", "-r", "-z", "--full-tree", treeOID)
	command.Dir = m.Path
	command.Env = m.environment("", nil)
	command.Stderr = io.Discard
	stdout, err := command.StdoutPipe()
	if err != nil || command.Start() != nil {
		return false, ErrCorrupt
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Split(scanNUL)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	for scanner.Scan() {
		meta, path, ok := bytes.Cut(scanner.Bytes(), []byte{'\t'})
		fields := strings.Fields(string(meta))
		if !ok || len(fields) != 3 || !oidPattern.MatchString(fields[2]) {
			_ = command.Process.Kill()
			_ = command.Wait()
			return false, ErrCorrupt
		}
		proceed, visitErr := visit(TreeEntry{Path: string(path), Mode: fields[0], Type: fields[1], OID: fields[2]})
		if visitErr != nil || !proceed {
			_ = command.Process.Kill()
			_ = command.Wait()
			return false, visitErr
		}
	}
	if err := scanner.Err(); err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		return false, ErrCorrupt
	}
	if err := command.Wait(); err != nil {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		return false, ErrCorrupt
	}
	return true, nil
}

func scanNUL(data []byte, atEOF bool) (int, []byte, error) {
	if i := bytes.IndexByte(data, 0); i >= 0 {
		return i + 1, data[:i], nil
	}
	if atEOF && len(data) != 0 {
		return len(data), nil, ErrCorrupt
	}
	return 0, nil, nil
}
