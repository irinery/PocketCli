package gitmirror

import (
	"bytes"
	"context"
	"io"
	"os/exec"
	"sort"
	"strings"
	"unicode/utf8"
)

type HeadRef struct {
	Name string `json:"name"`
	OID  string `json:"oid"`
}

// ListHeads observes branch names and OIDs without fetching or executing repo
// content. A bounded/truncated response is never authoritative for deletions.
func (m *Mirror) ListHeads(ctx context.Context, remote string, secret []byte, maxRefs int, maxBytes int64) ([]HeadRef, bool, int64, error) {
	if !m.allowedRemote(remote) || maxRefs < 1 || maxRefs > 10000 {
		return nil, false, 0, ErrUnavailable
	}
	if maxBytes <= 0 {
		return nil, false, 0, ErrBudget
	}
	if err := m.init(ctx); err != nil {
		return nil, false, 0, err
	}
	const hardLimit int64 = 1 << 20
	limit := maxBytes
	if limit > hardLimit {
		limit = hardLimit
	}
	command := exec.CommandContext(ctx, m.GitPath, "--git-dir="+m.Path, "ls-remote", "--heads", "--refs", remote)
	command.Dir = m.Path
	command.Env = m.environment(remote, secret)
	command.Stderr = io.Discard
	stdout, err := command.StdoutPipe()
	if err != nil || command.Start() != nil {
		return nil, false, 0, ErrUnavailable
	}
	raw, readErr := io.ReadAll(io.LimitReader(stdout, limit+1))
	truncated := int64(len(raw)) > limit
	if truncated || readErr != nil {
		_ = command.Process.Kill()
	}
	waitErr := command.Wait()
	used := int64(len(raw))
	if ctx.Err() != nil {
		return nil, false, used, ctx.Err()
	}
	if readErr != nil || (!truncated && waitErr != nil) {
		return nil, false, used, ErrUnavailable
	}
	if truncated && maxBytes <= hardLimit {
		return nil, false, used, ErrBudget
	}
	if truncated {
		raw = raw[:limit]
	}
	refs, complete := parseHeadRefs(raw, maxRefs)
	return refs, complete && !truncated, used, nil
}

func parseHeadRefs(raw []byte, maxRefs int) ([]HeadRef, bool) {
	complete := len(raw) == 0 || raw[len(raw)-1] == '\n'
	lines := bytes.Split(raw, []byte{'\n'})
	if !complete {
		lines = lines[:len(lines)-1]
	}
	seen := make(map[string]bool)
	refs := make([]HeadRef, 0)
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		oid, name, ok := bytes.Cut(line, []byte{'\t'})
		if !ok || !oidPattern.Match(oid) || !ValidHeadRef(string(name)) || seen[string(name)] {
			complete = false
			continue
		}
		seen[string(name)] = true
		if len(refs) >= maxRefs {
			complete = false
			continue
		}
		refs = append(refs, HeadRef{Name: string(name), OID: string(oid)})
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].Name < refs[j].Name })
	return refs, complete
}

func ValidHeadRef(name string) bool {
	if !strings.HasPrefix(name, "refs/heads/") || len(name) > 1024 || !utf8.ValidString(name) || strings.HasSuffix(name, ".") || strings.Contains(name, "..") || strings.Contains(name, "@{") || strings.ContainsAny(name, " ~^:?*[\\") {
		return false
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	parts := strings.Split(strings.TrimPrefix(name, "refs/heads/"), "/")
	for _, part := range parts {
		if part == "" || strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".lock") {
			return false
		}
	}
	return true
}
