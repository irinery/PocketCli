package capabilities

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"pocketcli/internal/pocketpath"
)

const (
	SchemaVersion = 1
	DefaultTTL    = 60

	ModeAuto     = "auto"
	ModeViewer   = "viewer"
	ModeAgent    = "agent"
	ModeDegraded = "degraded"

	LayoutSplit   = "split"
	LayoutStack   = "stack"
	LayoutCompact = "compact"
	LayoutPlain   = "plain"
)

var ErrHomeUnavailable = errors.New("ERR_CAP_HOME_UNAVAILABLE")

type Manifest struct {
	SchemaVersion      int             `json:"schema_version"`
	GeneratedAt        string          `json:"generated_at"`
	TTLSeconds         int             `json:"ttl_seconds"`
	CacheStatus        string          `json:"cache_status"`
	ModeRequested      string          `json:"mode_requested"`
	ModeEffective      string          `json:"mode_effective"`
	Host               HostIdentity    `json:"host"`
	Terminal           TerminalProfile `json:"terminal"`
	Capabilities       CapabilitySet   `json:"capabilities"`
	DegradationReasons []string        `json:"degradation_reasons"`
}

type CapabilitySet struct {
	HasTTY       bool `json:"has_tty"`
	HasTMUX      bool `json:"has_tmux"`
	HasTailscale bool `json:"has_tailscale"`
	HasSSH       bool `json:"has_ssh"`
	HasSCP       bool `json:"has_scp"`
	HasJQ        bool `json:"has_jq"`
	HasFZF       bool `json:"has_fzf"`
	HasRG        bool `json:"has_rg"`
	HasGit       bool `json:"has_git"`
	HasGo        bool `json:"has_go"`
}

type TerminalProfile struct {
	IsInteractive bool   `json:"is_interactive"`
	IsISH         bool   `json:"is_ish"`
	IsTMUX        bool   `json:"is_tmux"`
	Cols          int    `json:"cols"`
	Rows          int    `json:"rows"`
	TUILayout     string `json:"tui_layout"`
}

type HostIdentity struct {
	Hostname string `json:"hostname"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
}

func Detect(modeRequested string) (Manifest, error) {
	if _, err := pocketpath.HomeDir(); err != nil {
		return Manifest{}, ErrHomeUnavailable
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	modeRequested = normalizeMode(modeRequested)
	caps := CapabilitySet{
		HasTTY:       stdinIsTTY(),
		HasTMUX:      commandExists(ctx, "tmux"),
		HasTailscale: commandExists(ctx, "tailscale"),
		HasSSH:       commandExists(ctx, "ssh"),
		HasSCP:       commandExists(ctx, "scp"),
		HasJQ:        commandExists(ctx, "jq"),
		HasFZF:       commandExists(ctx, "fzf"),
		HasRG:        commandExists(ctx, "rg"),
		HasGit:       commandExists(ctx, "git"),
		HasGo:        commandExists(ctx, "go"),
	}

	cols, rows := terminalSize()
	terminal := TerminalProfile{
		IsInteractive: caps.HasTTY,
		IsISH:         detectISH(),
		IsTMUX:        strings.TrimSpace(os.Getenv("TMUX")) != "",
		Cols:          cols,
		Rows:          rows,
		TUILayout:     resolveLayout(caps.HasTTY, cols),
	}

	hostname, _ := os.Hostname()
	manifest := Manifest{
		SchemaVersion: SchemaVersion,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		TTLSeconds:    DefaultTTL,
		CacheStatus:   "ok",
		ModeRequested: modeRequested,
		ModeEffective: resolveMode(modeRequested, caps),
		Host: HostIdentity{
			Hostname: truncate(hostname, 128),
			OS:       truncate(runtime.GOOS, 64),
			Arch:     truncate(runtime.GOARCH, 64),
		},
		Terminal:     terminal,
		Capabilities: caps,
	}
	manifest.DegradationReasons = degradationReasons(modeRequested, manifest)
	return manifest, nil
}

func Save(manifest Manifest) (Manifest, error) {
	dataDir, err := pocketpath.EnsureDataDir()
	if err != nil {
		manifest.CacheStatus = "write_failed"
		return manifest, err
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		manifest.CacheStatus = "write_failed"
		return manifest, err
	}
	if len(data) > 64*1024 {
		manifest.CacheStatus = "write_failed"
		return manifest, errors.New("ERR_CAP_MANIFEST_TOO_LARGE")
	}
	if err := pocketpath.AtomicWrite(filepath.Join(dataDir, "capabilities.json"), append(data, '\n'), 0o644); err != nil {
		manifest.CacheStatus = "write_failed"
		return manifest, err
	}
	return manifest, nil
}

func LoadOrDetect() Manifest {
	manifest, err := Detect(os.Getenv("POCKETCLI_MODE_REQUESTED"))
	if err != nil {
		return Manifest{
			SchemaVersion:      SchemaVersion,
			GeneratedAt:        time.Now().UTC().Format(time.RFC3339),
			TTLSeconds:         DefaultTTL,
			CacheStatus:        "recovered",
			ModeRequested:      ModeAuto,
			ModeEffective:      ModeDegraded,
			DegradationReasons: []string{err.Error()},
		}
	}
	saved, err := Save(manifest)
	if err != nil {
		return manifest
	}
	return saved
}

func HasCapability(set CapabilitySet, name string) bool {
	switch strings.TrimSpace(name) {
	case "has_tty":
		return set.HasTTY
	case "has_tmux":
		return set.HasTMUX
	case "has_tailscale":
		return set.HasTailscale
	case "has_ssh":
		return set.HasSSH
	case "has_scp":
		return set.HasSCP
	case "has_jq":
		return set.HasJQ
	case "has_fzf":
		return set.HasFZF
	case "has_rg":
		return set.HasRG
	case "has_git":
		return set.HasGit
	case "has_go":
		return set.HasGo
	default:
		return false
	}
}

func normalizeMode(mode string) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case ModeViewer, ModeAgent:
		return mode
	default:
		return ModeAuto
	}
}

func resolveMode(requested string, caps CapabilitySet) string {
	if !caps.HasTTY {
		return ModeDegraded
	}
	if requested == ModeAgent {
		if !caps.HasTMUX {
			return ModeDegraded
		}
		return ModeAgent
	}
	return ModeViewer
}

func degradationReasons(requested string, manifest Manifest) []string {
	var reasons []string
	add := func(reason string) {
		if len(reasons) >= 32 {
			return
		}
		reasons = append(reasons, truncate(reason, 80))
	}
	if !manifest.Capabilities.HasTTY {
		add("tty_missing")
	}
	if requested == ModeAgent && !manifest.Capabilities.HasTMUX {
		add("tmux_missing")
	}
	if !manifest.Capabilities.HasSSH {
		add("ssh_missing")
	}
	if !manifest.Capabilities.HasTailscale {
		add("tailscale_missing")
	}
	return reasons
}

func commandExists(ctx context.Context, name string) bool {
	done := make(chan bool, 1)
	go func() {
		_, err := exec.LookPath(name)
		done <- err == nil
	}()

	timer := time.NewTimer(500 * time.Millisecond)
	defer timer.Stop()

	select {
	case ok := <-done:
		return ok
	case <-timer.C:
		return false
	case <-ctx.Done():
		return false
	}
}

func stdinIsTTY() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func terminalSize() (int, int) {
	cols := parsePositiveInt(os.Getenv("COLUMNS"), 80)
	rows := parsePositiveInt(os.Getenv("LINES"), 24)
	if cols < 10 {
		cols = 10
	}
	if rows < 3 {
		rows = 3
	}
	if cols > 500 {
		cols = 500
	}
	if rows > 200 {
		rows = 200
	}
	return cols, rows
}

func resolveLayout(hasTTY bool, cols int) string {
	if !hasTTY {
		return LayoutPlain
	}
	if cols >= 92 {
		return LayoutSplit
	}
	if cols >= 60 {
		return LayoutStack
	}
	return LayoutCompact
}

func detectISH() bool {
	env := strings.ToLower(strings.Join([]string{
		os.Getenv("ISH_VERSION"),
		os.Getenv("TERM_PROGRAM"),
		os.Getenv("SHELL"),
	}, " "))
	return strings.Contains(env, "ish")
}

func parsePositiveInt(value string, fallback int) int {
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

func truncate(value string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= max {
		return string(runes)
	}
	return string(runes[:max])
}
