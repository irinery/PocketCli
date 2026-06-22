package insights

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"pocketcli/internal/capabilities"
	"pocketcli/internal/ledger"
	"pocketcli/internal/pocketpath"
)

type Request struct {
	Scope             string `json:"scope"`
	HostID            string `json:"host_id,omitempty"`
	ProjectPath       string `json:"project_path,omitempty"`
	TimeWindowMinutes int    `json:"time_window_minutes"`
}

type Insight struct {
	ID                string     `json:"id"`
	Kind              string     `json:"kind"`
	Severity          string     `json:"severity"`
	Confidence        int        `json:"confidence"`
	Title             string     `json:"title"`
	Summary           string     `json:"summary"`
	Evidence          []Evidence `json:"evidence"`
	RecommendedAction string     `json:"recommended_action,omitempty"`
	Status            string     `json:"status"`
}

type Evidence struct {
	Source  string `json:"source"`
	Ref     string `json:"ref"`
	Preview string `json:"preview"`
}

type List struct {
	Insights []Insight `json:"insights"`
	Summary  Summary   `json:"summary"`
}

type Summary struct {
	Total         int  `json:"total"`
	Critical      int  `json:"critical"`
	Warning       int  `json:"warning"`
	Partial       bool `json:"partial"`
	SkippedEvents int  `json:"skipped_events"`
}

type statusState struct {
	Statuses map[string]string `json:"statuses"`
}

func Compute(request Request) (List, error) {
	if request.Scope == "" {
		request.Scope = "active"
	}
	switch request.Scope {
	case "active", "all", "host", "project":
	default:
		return List{}, fmt.Errorf("ERR_INSIGHT_BAD_SCOPE")
	}
	if request.TimeWindowMinutes <= 0 {
		request.TimeWindowMinutes = 1440
	}
	if request.TimeWindowMinutes > 10080 {
		request.TimeWindowMinutes = 10080
	}

	var (
		result  List
		events  []ledger.Event
		partial bool
	)
	store, err := ledger.NewStore()
	if err != nil {
		partial = true
	} else {
		search, err := store.Search(ledger.SearchFilter{
			HostID: request.HostID,
			Since:  time.Now().UTC().Add(-time.Duration(request.TimeWindowMinutes) * time.Minute).Format(time.RFC3339),
			Limit:  500,
		})
		if err != nil {
			partial = true
		} else {
			events = search.Events
			partial = search.Partial
		}
	}

	manifest := capabilities.LoadOrDetect()
	insights := make([]Insight, 0)
	insights = append(insights, missingCapabilityInsights(manifest)...)
	insights = append(insights, hostStabilityInsights(events)...)
	insights = append(insights, backendFallbackInsights(events)...)
	insights = append(insights, contextPartialInsights(events)...)

	state := loadState()
	deduped := dedupe(insights)
	for i := range deduped {
		if status := state.Statuses[deduped[i].ID]; status != "" {
			deduped[i].Status = status
		}
		if deduped[i].Status == "" {
			deduped[i].Status = "active"
		}
	}
	if request.Scope == "active" {
		filtered := deduped[:0]
		for _, insight := range deduped {
			if insight.Status == "active" {
				filtered = append(filtered, insight)
			}
		}
		deduped = filtered
	}

	sort.SliceStable(deduped, func(i, j int) bool {
		if severityRank(deduped[i].Severity) == severityRank(deduped[j].Severity) {
			return deduped[i].Confidence > deduped[j].Confidence
		}
		return severityRank(deduped[i].Severity) > severityRank(deduped[j].Severity)
	})
	if len(deduped) > 100 {
		deduped = deduped[:100]
	}

	result.Insights = deduped
	result.Summary.Partial = partial
	result.Summary.Total = len(deduped)
	for _, insight := range deduped {
		switch insight.Severity {
		case "critical":
			result.Summary.Critical++
		case "warning":
			result.Summary.Warning++
		}
	}
	return result, nil
}

func missingCapabilityInsights(manifest capabilities.Manifest) []Insight {
	var out []Insight
	if manifest.ModeRequested == capabilities.ModeAgent && !manifest.Capabilities.HasTMUX {
		out = append(out, newInsight("missing_capability", "warning", 80, "tmux ausente para modo agent", "Modo agent foi solicitado, mas tmux nao esta disponivel.", "install_tmux_or_use_viewer", Evidence{
			Source:  "capabilities",
			Ref:     "has_tmux",
			Preview: "has_tmux=false mode_requested=agent",
		}))
	}
	if !manifest.Capabilities.HasSSH {
		out = append(out, newInsight("missing_capability", "critical", 90, "ssh indisponivel", "Acoes SSH e fleet ficam indisponiveis sem cliente ssh.", "install_openssh_client", Evidence{
			Source:  "capabilities",
			Ref:     "has_ssh",
			Preview: "has_ssh=false",
		}))
	}
	return out
}

func hostStabilityInsights(events []ledger.Event) []Insight {
	type counters struct {
		timeouts []ledger.Event
		failures []ledger.Event
	}
	byHost := map[string]*counters{}
	now := time.Now().UTC()
	for _, event := range events {
		if event.Type != ledger.EventSSHProbe && event.Type != ledger.EventSSHExec {
			continue
		}
		host := strings.TrimSpace(event.HostID)
		if host == "" {
			continue
		}
		ts, err := time.Parse(time.RFC3339, event.Timestamp)
		if err != nil {
			continue
		}
		c := byHost[host]
		if c == nil {
			c = &counters{}
			byHost[host] = c
		}
		if event.Status == "timeout" && now.Sub(ts) <= 10*time.Minute {
			c.timeouts = append(c.timeouts, event)
		}
		if (event.Status == "error" || event.Status == "timeout") && now.Sub(ts) <= 30*time.Minute {
			c.failures = append(c.failures, event)
		}
	}

	var out []Insight
	for host, c := range byHost {
		if len(c.failures) >= 5 {
			out = append(out, newInsight("host_unstable", "critical", 90, "host instavel: "+host, fmt.Sprintf("%s teve %d falhas em 30 minutos.", host, len(c.failures)), "check_host_network_or_ssh", evidenceFromEvents(c.failures)...))
			continue
		}
		if len(c.timeouts) >= 3 {
			out = append(out, newInsight("host_unstable", "warning", 75, "timeouts recorrentes: "+host, fmt.Sprintf("%s teve %d timeouts em 10 minutos.", host, len(c.timeouts)), "check_host_network_or_ssh", evidenceFromEvents(c.timeouts)...))
		}
	}
	return out
}

func backendFallbackInsights(events []ledger.Event) []Insight {
	var out []Insight
	for _, event := range events {
		if event.Type != ledger.EventBackendCall {
			continue
		}
		if strings.Contains(event.Payload.Message, "fallback_occurred=true") || strings.Contains(event.Payload.Message, "local timeout") || strings.Contains(event.Payload.Message, "local indisponivel") {
			out = append(out, newInsight("backend_fallback", "info", 60, "fallback de backend", "Backend local caiu para remoto.", "check_local_backend", Evidence{
				Source:  "ledger",
				Ref:     event.EventID,
				Preview: truncate(event.Payload.Message, 300),
			}))
		}
	}
	return out
}

func contextPartialInsights(events []ledger.Event) []Insight {
	var out []Insight
	for _, event := range events {
		if event.Type != ledger.EventContextCollected || event.Status != "partial" {
			continue
		}
		out = append(out, newInsight("context_partial", "warning", 65, "contexto parcial", "Uma coleta de contexto foi parcial.", "pocket context --json", Evidence{
			Source:  "ledger",
			Ref:     event.EventID,
			Preview: truncate(event.Payload.Message, 300),
		}))
	}
	return out
}

func newInsight(kind, severity string, confidence int, title, summary, action string, evidence ...Evidence) Insight {
	insight := Insight{
		Kind:              kind,
		Severity:          severity,
		Confidence:        confidence,
		Title:             truncate(title, 120),
		Summary:           truncate(summary, 500),
		Evidence:          evidence,
		RecommendedAction: truncate(action, 120),
		Status:            "active",
	}
	if len(insight.Evidence) > 10 {
		insight.Evidence = insight.Evidence[:10]
	}
	insight.ID = fingerprint(kind + "|" + title + "|" + summary)
	return insight
}

func evidenceFromEvents(events []ledger.Event) []Evidence {
	out := make([]Evidence, 0, len(events))
	for _, event := range events {
		out = append(out, Evidence{
			Source:  "ledger",
			Ref:     event.EventID,
			Preview: truncate(event.Payload.Message, 300),
		})
	}
	if len(out) > 10 {
		return out[:10]
	}
	return out
}

func dedupe(input []Insight) []Insight {
	seen := map[string]struct{}{}
	out := make([]Insight, 0, len(input))
	for _, insight := range input {
		if insight.ID == "" {
			insight.ID = fingerprint(insight.Kind + "|" + insight.Title)
		}
		if _, ok := seen[insight.ID]; ok {
			continue
		}
		seen[insight.ID] = struct{}{}
		out = append(out, insight)
	}
	return out
}

func loadState() statusState {
	dir, err := pocketpath.DataDir()
	if err != nil {
		return statusState{Statuses: map[string]string{}}
	}
	data, err := os.ReadFile(filepath.Join(dir, "insights-state.json"))
	if err != nil {
		return statusState{Statuses: map[string]string{}}
	}
	var state statusState
	if err := json.Unmarshal(data, &state); err != nil || state.Statuses == nil {
		return statusState{Statuses: map[string]string{}}
	}
	return state
}

func fingerprint(value string) string {
	sum := sha1.Sum([]byte(value))
	return hex.EncodeToString(sum[:])[:24]
}

func severityRank(severity string) int {
	switch severity {
	case "critical":
		return 3
	case "warning":
		return 2
	default:
		return 1
	}
}

func truncate(value string, max int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= max {
		return string(runes)
	}
	if max <= 0 {
		return ""
	}
	return string(runes[:max])
}
