package actions

import (
	"errors"
	"strings"

	"pocketcli/internal/capabilities"
)

type ResolveRequest struct {
	Surface         string `json:"surface"`
	IncludeDisabled bool   `json:"include_disabled"`
	Query           string `json:"query,omitempty"`
}

type Action struct {
	ID             string   `json:"id"`
	Title          string   `json:"title"`
	Description    string   `json:"description"`
	Hotkey         string   `json:"hotkey,omitempty"`
	Command        []string `json:"command"`
	Requires       []string `json:"requires"`
	Enabled        bool     `json:"enabled"`
	DisabledReason string   `json:"disabled_reason,omitempty"`
	SafetyLevel    string   `json:"safety_level"`
}

type List struct {
	Actions       []Action `json:"actions"`
	DisabledCount int      `json:"disabled_count"`
	Partial       bool     `json:"partial"`
}

var (
	ErrBadSurface    = errors.New("ERR_ACTION_BAD_SURFACE")
	ErrUnsafeCommand = errors.New("ERR_ACTION_UNSAFE_COMMAND")
)

func Resolve(request ResolveRequest, manifest capabilities.Manifest) (List, error) {
	if request.Surface == "" {
		request.Surface = "menu"
	}
	switch request.Surface {
	case "menu", "palette", "insight_card", "plain":
	default:
		return List{}, ErrBadSurface
	}

	query := strings.ToLower(strings.TrimSpace(request.Query))
	result := List{}
	for _, action := range registry() {
		if err := validateCommand(action.Command); err != nil {
			return result, err
		}
		if query != "" && !matches(action, query) {
			continue
		}
		action.Enabled = true
		for _, requirement := range action.Requires {
			if !capabilities.HasCapability(manifest.Capabilities, requirement) {
				action.Enabled = false
				action.DisabledReason = requirement + " indisponivel"
				result.DisabledCount++
				break
			}
		}
		if !action.Enabled && !request.IncludeDisabled {
			continue
		}
		if len(result.Actions) < 80 {
			result.Actions = append(result.Actions, action)
		}
	}
	return result, nil
}

func registry() []Action {
	return []Action{
		{ID: "connect_manual", Title: "Conectar", Description: "Abrir SSH manual para um host", Hotkey: "c", Command: []string{"pocket", "ssh"}, Requires: []string{"has_ssh"}, SafetyLevel: "confirm"},
		{ID: "radar_tailscale", Title: "Radar", Description: "Listar peers online no Tailscale", Hotkey: "r", Command: []string{"pocket-radar"}, Requires: []string{"has_tailscale"}, SafetyLevel: "safe"},
		{ID: "status_local", Title: "Status", Description: "Resumo local de terminal e runtime", Hotkey: "s", Command: []string{"pocket", "capabilities"}, Requires: nil, SafetyLevel: "safe"},
		{ID: "hosts", Title: "Hosts", Description: "Listar hosts conhecidos", Hotkey: "h", Command: []string{"pocket", "hosts"}, Requires: nil, SafetyLevel: "safe"},
		{ID: "update", Title: "Atualizar", Description: "Atualizar PocketCli preservando profile", Hotkey: "u", Command: []string{"pocket-update"}, Requires: []string{"has_git"}, SafetyLevel: "confirm"},
		{ID: "insights", Title: "Insights", Description: "Ver sinais operacionais ativos", Hotkey: "i", Command: []string{"pocket", "insights", "list"}, Requires: nil, SafetyLevel: "safe"},
		{ID: "doctor", Title: "Doctor", Description: "Rodar checks locais", Hotkey: "d", Command: []string{"pocket", "doctor"}, Requires: nil, SafetyLevel: "safe"},
		{ID: "ask", Title: "Ask", Description: "Perguntar usando contexto local", Hotkey: "a", Command: []string{"pocket", "ask"}, Requires: nil, SafetyLevel: "safe"},
		{ID: "context", Title: "Contexto", Description: "Inspecionar contexto coletado", Hotkey: "x", Command: []string{"pocket", "context"}, Requires: nil, SafetyLevel: "safe"},
	}
}

func validateCommand(command []string) error {
	if len(command) == 0 || len(command) > 12 {
		return ErrUnsafeCommand
	}
	for _, arg := range command {
		if len([]rune(arg)) > 160 || strings.ContainsAny(arg, "\n\r;&|`$<>") {
			return ErrUnsafeCommand
		}
	}
	return nil
}

func matches(action Action, query string) bool {
	text := strings.ToLower(strings.Join([]string{action.ID, action.Title, action.Description}, " "))
	return strings.Contains(text, query)
}
