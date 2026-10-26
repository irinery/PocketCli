package gitmirror

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"pocketcli/internal/pocketpath"
)

var (
	ErrBudget      = errors.New("git transfer budget exceeded")
	ErrUnavailable = errors.New("git source unavailable")
	ErrMoved       = errors.New("git ref moved during fetch")
	ErrCorrupt     = errors.New("git mirror unavailable")
)

var repoPath = regexp.MustCompile(`^/[A-Za-z0-9][A-Za-z0-9-]{0,38}/[A-Za-z0-9_.-]{1,100}\.git$`)
var oidPattern = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)

type RefState struct {
	Ref string `json:"ref"`
	OID string `json:"oid"`
}

type Observation struct {
	Ref           string
	OldOID        string
	NewOID        string
	TreeOID       string
	Relationship  string // initial, unchanged, advanced, ref_diverged, ref_changed, unknown
	GraphComplete bool
	LocalBytes    int64
}

type Mirror struct {
	Path       string
	GitPath    string
	allowLocal bool // Tests only. Production remotes are restricted to github.com HTTPS.
}

func New(path string) (*Mirror, error) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return nil, ErrUnavailable
	}
	if info, err := os.Lstat(path); err == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
		return nil, ErrCorrupt
	} else if err != nil && !os.IsNotExist(err) {
		return nil, ErrCorrupt
	}
	if err := pocketpath.EnsurePrivateDir(path); err != nil {
		return nil, ErrCorrupt
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrCorrupt
	}
	for _, name := range []string{"private-home", "disabled-hooks"} {
		if err := pocketpath.EnsurePrivateDir(filepath.Join(path, name)); err != nil {
			return nil, ErrCorrupt
		}
	}
	return &Mirror{Path: path, GitPath: gitPath}, nil
}

func (m *Mirror) Observe(ctx context.Context, remote string, secret []byte, prior RefState, maxAdditionalBytes int64) (Observation, error) {
	var out Observation
	if !m.allowedRemote(remote) {
		return out, ErrUnavailable
	}
	if maxAdditionalBytes <= 0 {
		return out, ErrBudget
	}
	if err := m.init(ctx); err != nil {
		return out, err
	}
	commandOutput, err := m.command(ctx, remote, secret, "ls-remote", "--symref", remote, "HEAD")
	if err != nil {
		return out, ErrUnavailable
	}
	ref, oid, err := parseRemoteHead(commandOutput)
	if err != nil {
		return out, ErrUnavailable
	}
	if _, err := m.command(ctx, "", nil, "check-ref-format", ref); err != nil {
		return out, ErrUnavailable
	}
	out.Ref, out.OldOID, out.NewOID = ref, prior.OID, oid
	if prior.OID != "" && !oidPattern.MatchString(prior.OID) {
		return out, ErrCorrupt
	}
	if prior.OID == oid {
		if _, err := m.command(ctx, remote, nil, "cat-file", "-e", oid+"^{commit}"); err == nil {
			tree, err := m.treeOID(ctx, oid)
			if err != nil {
				return out, err
			}
			out.TreeOID = tree
			out.Relationship = "unchanged"
			if prior.Ref != ref {
				out.Relationship = "ref_changed"
			}
			out.GraphComplete = !m.shallow(ctx)
			return out, nil
		}
	}
	if _, err := m.command(ctx, remote, nil, "cat-file", "-e", oid+"^{commit}"); err != nil {
		if maxAdditionalBytes <= 0 {
			return out, ErrBudget
		}
		destination := "refs/pocketcli/observations/" + oid
		before, err := directoryBytes(m.Path)
		if err != nil {
			return out, ErrCorrupt
		}
		peak, fetchErr := m.fetch(ctx, remote, secret, maxAdditionalBytes, before, "fetch", "--quiet", "--no-tags", "--no-recurse-submodules", "--no-auto-maintenance", "--filter=tree:0", remote, "+"+ref+":"+destination)
		after, err := directoryBytes(m.Path)
		if err != nil {
			return out, ErrCorrupt
		}
		if after > before {
			out.LocalBytes = after - before
		}
		if peak > out.LocalBytes {
			out.LocalBytes = peak
		}
		if fetchErr != nil {
			return out, fetchErr
		}
		if out.LocalBytes > maxAdditionalBytes {
			return out, ErrBudget
		}
		fetched, err := m.command(ctx, remote, nil, "rev-parse", destination)
		if err != nil || strings.TrimSpace(string(fetched)) != oid {
			return out, ErrMoved
		}
	}
	if _, err := m.command(ctx, remote, nil, "update-ref", "refs/pocketcli/observations/"+oid, oid); err != nil {
		return out, ErrCorrupt
	}
	tree, err := m.treeOID(ctx, oid)
	if err != nil {
		return out, err
	}
	out.TreeOID = tree
	out.GraphComplete = !m.shallow(ctx)
	if prior.OID == "" {
		out.Relationship = "initial"
	} else if prior.Ref != ref {
		out.Relationship = "ref_changed"
	} else {
		if _, err := m.command(ctx, remote, nil, "cat-file", "-e", prior.OID+"^{commit}"); err != nil {
			out.Relationship = "unknown"
		} else {
			_, status := m.commandStatus(ctx, remote, nil, "merge-base", "--is-ancestor", prior.OID, oid)
			switch status {
			case 0:
				out.Relationship = "advanced"
			case 1:
				if out.GraphComplete {
					out.Relationship = "ref_diverged"
				} else {
					out.Relationship = "unknown"
				}
			default:
				out.Relationship = "unknown"
			}
		}
	}
	if _, err := m.command(ctx, remote, nil, "update-ref", "refs/pocketcli/current", oid); err != nil {
		return out, ErrCorrupt
	}
	return out, nil
}

func (m *Mirror) allowedRemote(remote string) bool {
	u, err := url.Parse(remote)
	if err != nil {
		return false
	}
	if m.allowLocal && u.Scheme == "file" && u.Host == "" && filepath.IsAbs(u.Path) {
		return true
	}
	return u.Scheme == "https" && u.Host == "github.com" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && repoPath.MatchString(u.Path)
}

func (m *Mirror) init(ctx context.Context) error {
	if _, err := os.Stat(filepath.Join(m.Path, "HEAD")); err == nil {
		data, err := m.command(ctx, "", nil, "rev-parse", "--is-bare-repository")
		if err != nil || strings.TrimSpace(string(data)) != "true" {
			return ErrCorrupt
		}
		return nil
	} else if !os.IsNotExist(err) {
		return ErrCorrupt
	}
	if _, err := m.command(ctx, "", nil, "init", "--bare", m.Path); err != nil {
		return ErrCorrupt
	}
	return nil
}

func parseRemoteHead(raw []byte) (string, string, error) {
	ref, oid := "", ""
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "ref: ") && strings.HasSuffix(line, "\tHEAD") {
			ref = strings.TrimSuffix(strings.TrimPrefix(line, "ref: "), "\tHEAD")
		}
		if strings.HasSuffix(line, "\tHEAD") {
			candidate := strings.TrimSuffix(line, "\tHEAD")
			if oidPattern.MatchString(candidate) {
				oid = candidate
			}
		}
	}
	if !strings.HasPrefix(ref, "refs/heads/") || oid == "" || strings.ContainsAny(ref, " \t\r\n") {
		return "", "", ErrUnavailable
	}
	return ref, oid, nil
}

func (m *Mirror) shallow(ctx context.Context) bool {
	data, err := m.command(ctx, "", nil, "rev-parse", "--is-shallow-repository")
	return err != nil || strings.TrimSpace(string(data)) != "false"
}

func (m *Mirror) treeOID(ctx context.Context, commit string) (string, error) {
	data, err := m.command(ctx, "", nil, "cat-file", "-p", commit)
	if err != nil {
		return "", ErrCorrupt
	}
	line, _, _ := strings.Cut(string(data), "\n")
	if !strings.HasPrefix(line, "tree ") || !oidPattern.MatchString(strings.TrimPrefix(line, "tree ")) {
		return "", ErrCorrupt
	}
	return strings.TrimPrefix(line, "tree "), nil
}

func (m *Mirror) command(ctx context.Context, remote string, secret []byte, args ...string) ([]byte, error) {
	data, status := m.commandStatus(ctx, remote, secret, args...)
	if status != 0 {
		return nil, ErrUnavailable
	}
	return data, nil
}

func (m *Mirror) commandStatus(ctx context.Context, remote string, secret []byte, args ...string) ([]byte, int) {
	command := exec.CommandContext(ctx, m.GitPath, append([]string{"--git-dir=" + m.Path}, args...)...)
	command.Dir = m.Path
	command.Env = m.environment(remote, secret)
	var output cappedBuffer
	command.Stdout = &output
	command.Stderr = io.Discard
	err := command.Run()
	if err == nil {
		return output.Bytes(), 0
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return nil, exit.ExitCode()
	}
	return nil, -1
}

func (m *Mirror) fetch(ctx context.Context, remote string, secret []byte, maxAdditionalBytes, before int64, args ...string) (int64, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	command := exec.CommandContext(ctx, m.GitPath, append([]string{"--git-dir=" + m.Path}, args...)...)
	command.Dir = m.Path
	command.Env = m.environment(remote, secret)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	if err := command.Start(); err != nil {
		return 0, ErrUnavailable
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	var peak int64
	updatePeak := func() error {
		current, err := directoryBytes(m.Path)
		if err != nil {
			return err
		}
		if delta := current - before; delta > peak {
			peak = delta
		}
		return nil
	}
	for {
		select {
		case err := <-done:
			if updatePeak() != nil {
				return peak, ErrCorrupt
			}
			if ctx.Err() != nil {
				return peak, ctx.Err()
			}
			if err != nil {
				return peak, ErrUnavailable
			}
			return peak, nil
		case <-ticker.C:
			if updatePeak() != nil {
				cancel()
				<-done
				return peak, ErrCorrupt
			}
			if peak > maxAdditionalBytes {
				cancel()
				<-done
				return peak, ErrBudget
			}
		case <-ctx.Done():
			cancel()
			<-done
			_ = updatePeak()
			return peak, ctx.Err()
		}
	}
}

func (m *Mirror) environment(remote string, secret []byte) []string {
	home := filepath.Join(m.Path, "private-home")
	hooks := filepath.Join(m.Path, "disabled-hooks")
	env := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "XDG_CONFIG_HOME=" + home, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null", "GIT_TERMINAL_PROMPT=0", "GIT_NO_LAZY_FETCH=1", "GIT_LFS_SKIP_SMUDGE=1", "GIT_OPTIONAL_LOCKS=0", "GIT_TEMPLATE_DIR=" + hooks, "LANG=C"}
	for _, name := range []string{"HTTPS_PROXY", "HTTP_PROXY", "NO_PROXY", "https_proxy", "http_proxy", "no_proxy"} {
		if value := os.Getenv(name); value != "" {
			env = append(env, name+"="+value)
		}
	}
	settings := [][2]string{{"core.hooksPath", hooks}, {"credential.helper", ""}, {"fetch.recurseSubmodules", "false"}, {"protocol.ext.allow", "never"}, {"protocol.file.allow", "never"}, {"http.followRedirects", "false"}, {"gc.auto", "0"}, {"fetch.writeCommitGraph", "false"}}
	if m.allowLocal {
		settings[4][1] = "always"
	}
	if len(secret) > 0 && strings.HasPrefix(remote, "https://github.com/") {
		encoded := base64.StdEncoding.EncodeToString(append([]byte("x-access-token:"), secret...))
		settings = append(settings, [2]string{"http.https://github.com/.extraheader", "AUTHORIZATION: basic " + encoded})
	}
	env = append(env, "GIT_CONFIG_COUNT="+strconv.Itoa(len(settings)))
	for i, item := range settings {
		env = append(env, "GIT_CONFIG_KEY_"+strconv.Itoa(i)+"="+item[0], "GIT_CONFIG_VALUE_"+strconv.Itoa(i)+"="+item[1])
	}
	return env
}

func directoryBytes(root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	return total, err
}

func StorageBytes(root string) (int64, error) { return directoryBytes(root) }

type cappedBuffer struct{ bytes.Buffer }

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if b.Len() < 1024*1024 {
		room := 1024*1024 - b.Len()
		if len(p) < room {
			room = len(p)
		}
		_, _ = b.Buffer.Write(p[:room])
	}
	return len(p), nil
}
