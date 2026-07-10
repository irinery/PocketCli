package fleet

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"pocketcli/internal/pocketpath"
	"pocketcli/internal/safety"
)

var (
	ErrEmptySelection = errors.New("ERR_FLEET_EMPTY_SELECTION")
	ErrBadSelector    = errors.New("ERR_FLEET_BAD_SELECTOR")
	ErrPlanNotFound   = errors.New("ERR_FLEET_PLAN_NOT_FOUND")
)

type Target struct {
	HostID         string `json:"host_id"`
	Hostname       string `json:"hostname"`
	Address        string `json:"address,omitempty"`
	Source         string `json:"source"`
	ApprovalStatus string `json:"approval_status"`
}

type InventoryResult struct {
	Hosts       []Target `json:"hosts"`
	OnlineCount int      `json:"online_count"`
	Sources     []string `json:"sources"`
}

type Plan struct {
	PlanID           string   `json:"plan_id"`
	Selector         string   `json:"selector"`
	Command          []string `json:"command"`
	Targets          []Target `json:"targets"`
	MaxParallel      int      `json:"max_parallel"`
	RequiresApproval bool     `json:"requires_approval"`
	EnvelopeID       string   `json:"envelope_id,omitempty"`
}

type RunResult struct {
	RunID   string          `json:"run_id"`
	PlanID  string          `json:"plan_id"`
	Results []HostRunResult `json:"results"`
}

type HostRunResult struct {
	HostID         string      `json:"host_id"`
	HostAlias      string      `json:"host_alias,omitempty"`
	CommandID      string      `json:"command_id,omitempty"`
	Status         string      `json:"status"`
	RemoteStatus   string      `json:"remote_status,omitempty"`
	ExitCode       int         `json:"exit_code,omitempty"`
	DurationMS     int         `json:"duration_ms"`
	OutputPreview  string      `json:"output_preview,omitempty"`
	StderrPreview  string      `json:"stderr_preview,omitempty"`
	Truncated      bool        `json:"truncated,omitempty"`
	PolicyDecision interface{} `json:"policy_decision,omitempty"`
}

type rawInventory struct {
	Hosts []rawHost `json:"hosts"`
}

type rawHost struct {
	ID          string          `json:"id"`
	Hostname    string          `json:"hostname"`
	TailscaleIP string          `json:"tailscale_ip"`
	Online      bool            `json:"online"`
	Source      json.RawMessage `json:"source"`
	Labels      []string        `json:"labels"`
}

func LoadInventory() (InventoryResult, error) {
	targets := []Target{}
	sources := map[string]struct{}{}
	onlineCount := 0

	if fromFile, online, err := loadInventoryFile(); err == nil {
		targets = append(targets, fromFile...)
		onlineCount += online
		for _, target := range fromFile {
			if target.Source != "" {
				sources[target.Source] = struct{}{}
			}
		}
	}
	if len(targets) == 0 {
		if saved, err := loadSavedHosts(); err == nil {
			targets = append(targets, saved...)
			if len(saved) > 0 {
				sources["saved"] = struct{}{}
			}
		}
	}

	sourceList := make([]string, 0, len(sources))
	for source := range sources {
		sourceList = append(sourceList, source)
	}
	if len(targets) == 0 {
		return InventoryResult{Hosts: []Target{}, OnlineCount: 0, Sources: sourceList}, nil
	}
	return InventoryResult{Hosts: targets, OnlineCount: onlineCount, Sources: sourceList}, nil
}

func CreatePlan(selector string, command []string, maxParallel int) (Plan, error) {
	selector = strings.TrimSpace(selector)
	if selector == "" || strings.ContainsAny(selector, "\n\r") || len([]rune(selector)) > 200 {
		return Plan{}, ErrBadSelector
	}
	if maxParallel <= 0 {
		maxParallel = 4
	}
	if maxParallel > 16 {
		maxParallel = 16
	}
	if len(command) == 0 || len(command) > 32 {
		return Plan{}, fmt.Errorf("ERR_FLEET_COMMAND_UNSAFE")
	}

	inventory, err := LoadInventory()
	if err != nil {
		return Plan{}, err
	}
	targets := SelectTargets(inventory.Hosts, selector)
	if len(targets) == 0 {
		return Plan{}, ErrEmptySelection
	}
	if len(targets) > 200 {
		targets = targets[:200]
	}

	decision, safetyErr := safety.Evaluate(safety.Request{
		Action:      "fleet",
		Command:     command,
		HostCount:   len(targets),
		Interactive: false,
	})
	requiresApproval := decision.ApprovalRequired || safetyErr == safety.ErrApprovalRequired
	var envelopeID string
	if decision.Classification == safety.ClassificationBlocked {
		return Plan{}, fmt.Errorf("ERR_FLEET_COMMAND_UNSAFE")
	}
	if requiresApproval {
		for i := range targets {
			targets[i].ApprovalStatus = "approval_required"
		}
		// CreateRunEnvelope needs interactive=true for confirm decisions.
		envelope, err := safety.CreateRunEnvelope(safety.Request{
			Action:      "fleet",
			Command:     command,
			HostCount:   len(targets),
			Interactive: true,
		})
		if err != nil {
			return Plan{}, err
		}
		envelopeID = envelope.EnvelopeID
	}

	plan := Plan{
		PlanID:           newID(),
		Selector:         selector,
		Command:          append([]string(nil), command...),
		Targets:          targets,
		MaxParallel:      maxParallel,
		RequiresApproval: requiresApproval,
		EnvelopeID:       envelopeID,
	}
	if err := SavePlan(plan); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

func SelectTargets(hosts []Target, selector string) []Target {
	selector = strings.TrimSpace(selector)
	var selected []Target
	for _, host := range hosts {
		if matchesSelector(host, selector) {
			if host.ApprovalStatus == "" {
				host.ApprovalStatus = "unknown"
			}
			selected = append(selected, host)
		}
	}
	return selected
}

func SavePlan(plan Plan) error {
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return err
	}
	path, err := planPath(plan.PlanID)
	if err != nil {
		return err
	}
	return pocketpath.AtomicWrite(path, append(data, '\n'), 0o600)
}

func LoadPlan(planID string) (Plan, error) {
	path, err := planPath(planID)
	if err != nil {
		return Plan{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Plan{}, ErrPlanNotFound
	}
	var plan Plan
	if err := json.Unmarshal(data, &plan); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

func loadInventoryFile() ([]Target, int, error) {
	dataDir, err := pocketpath.DataDir()
	if err != nil {
		return nil, 0, err
	}
	data, err := os.ReadFile(filepath.Join(dataDir, "inventory.json"))
	if err != nil {
		return nil, 0, err
	}
	var inv rawInventory
	if err := json.Unmarshal(data, &inv); err != nil {
		return nil, 0, err
	}
	targets := make([]Target, 0, len(inv.Hosts))
	onlineCount := 0
	for _, host := range inv.Hosts {
		if strings.TrimSpace(host.Hostname) == "" {
			continue
		}
		source := normalizeSource(host.Source)
		if source == "" {
			source = "saved"
		}
		if host.Online {
			onlineCount++
		}
		id := strings.TrimSpace(host.ID)
		if id == "" {
			id = "host-" + host.Hostname
		}
		targets = append(targets, Target{
			HostID:         id,
			Hostname:       host.Hostname,
			Address:        host.TailscaleIP,
			Source:         source,
			ApprovalStatus: "unknown",
		})
	}
	return targets, onlineCount, nil
}

func loadSavedHosts() ([]Target, error) {
	codeDir, err := pocketpath.CodeDir()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(codeDir, "hosts"))
	if err != nil {
		return nil, err
	}
	var targets []Target
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		host := sanitizeHost(line)
		if host == "" {
			continue
		}
		targets = append(targets, Target{HostID: "host-" + host, Hostname: host, Source: "saved", ApprovalStatus: "unknown"})
	}
	return targets, nil
}

func normalizeSource(raw json.RawMessage) string {
	var values []string
	if err := json.Unmarshal(raw, &values); err == nil && len(values) > 0 {
		return values[0]
	}
	var value string
	if err := json.Unmarshal(raw, &value); err == nil {
		return value
	}
	return ""
}

func matchesSelector(host Target, selector string) bool {
	if selector == "all" {
		return true
	}
	if strings.HasPrefix(selector, "host:") {
		query := strings.TrimPrefix(selector, "host:")
		return host.Hostname == query || host.HostID == query
	}
	if strings.HasPrefix(selector, "tag:") {
		query := strings.TrimPrefix(selector, "tag:")
		return host.Source == query
	}
	return host.Hostname == selector || host.HostID == selector
}

func sanitizeHost(value string) string {
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func planPath(planID string) (string, error) {
	if strings.TrimSpace(planID) == "" || strings.ContainsAny(planID, `/\`) || planID == "." || planID == ".." {
		return "", ErrPlanNotFound
	}
	dir, err := pocketpath.EnsureDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "fleet", "plans", planID+".json"), nil
}

func newID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	dst := make([]byte, 36)
	hex.Encode(dst[0:8], raw[0:4])
	dst[8] = '-'
	hex.Encode(dst[9:13], raw[4:6])
	dst[13] = '-'
	hex.Encode(dst[14:18], raw[6:8])
	dst[18] = '-'
	hex.Encode(dst[19:23], raw[8:10])
	dst[23] = '-'
	hex.Encode(dst[24:36], raw[10:16])
	return string(dst)
}
