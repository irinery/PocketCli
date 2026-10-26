package core

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
	"pocketcli/internal/investigation/contract"
)

type Setup struct{ now func() time.Time }

func NewSetup(now func() time.Time) *Setup {
	if now == nil {
		now = time.Now
	}
	return &Setup{now: now}
}

type setupYAML struct {
	SchemaVersion string `yaml:"schema_version"`
	Name          string `yaml:"name"`
	Scope         *struct {
		Mode         string   `yaml:"mode"`
		Repositories []string `yaml:"repositories"`
		Teams        []string `yaml:"teams"`
	} `yaml:"scope"`
	Period struct {
		Preset   string `yaml:"preset"`
		FromDate string `yaml:"from"`
		ToDate   string `yaml:"to"`
		Timezone string `yaml:"timezone"`
	} `yaml:"period"`
	Sources struct {
		GitHub *bool `yaml:"github"`
		Linear *bool `yaml:"linear"`
	} `yaml:"sources"`
	Modules struct {
		GitHubIssues  *bool `yaml:"github_issues"`
		CodeHistory   *bool `yaml:"code_history"`
		CI            *bool `yaml:"ci"`
		Deployments   *bool `yaml:"deployments"`
		RuntimeImport *bool `yaml:"runtime_import"`
	} `yaml:"modules"`
	Limits struct {
		GitHubRPH        int   `yaml:"github_requests_per_hour"`
		LinearRPH        int   `yaml:"linear_requests_per_hour"`
		MaxSessionMS     int64 `yaml:"max_session_ms"`
		MaxTransferBytes int64 `yaml:"max_transfer_bytes"`
		MaxLocalBytes    int64 `yaml:"max_local_bytes"`
	} `yaml:"limits"`
	PDF struct {
		Automatic *bool `yaml:"automatic"`
	} `yaml:"pdf"`
}

func boolDefault(v *bool, d bool) bool {
	if v == nil {
		return d
	}
	return *v
}

func (s *Setup) Parse(_ context.Context, req contract.SetupParseRequest) (contract.ParsedSetup, error) {
	var out contract.ParsedSetup
	if !strings.HasSuffix(req.Filename, ".yml") && !strings.HasSuffix(req.Filename, ".yaml") {
		return out, fault("ERR_VALIDATION", "arquivo deve ser .yml ou .yaml")
	}
	if len(req.Content) > 256*1024 || !utf8.ValidString(req.Content) {
		return out, fault("ERR_VALIDATION", "YAML inválido ou maior que 256 KiB")
	}
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(req.Content), &node); err != nil {
		return out, fault("ERR_VALIDATION", "YAML inválido")
	}
	if err := validateYAML(&node); err != nil {
		return out, err
	}
	dec := yaml.NewDecoder(bytes.NewBufferString(req.Content))
	dec.KnownFields(true)
	var raw setupYAML
	if err := dec.Decode(&raw); err != nil {
		return out, fault("ERR_VALIDATION", "campo YAML inválido: "+err.Error())
	}
	var extra setupYAML
	if err := dec.Decode(&extra); err != io.EOF {
		return out, fault("ERR_VALIDATION", "somente um documento YAML é permitido")
	}
	if raw.SchemaVersion != contract.Version {
		return out, fault("ERR_SCHEMA_VERSION_UNSUPPORTED", "schema_version incompatível")
	}
	if raw.PDF.Automatic != nil && !*raw.PDF.Automatic {
		return out, fault("ERR_VALIDATION", "pdf.automatic deve ser true")
	}
	input := contract.PlanInput{SchemaVersion: contract.Version, Name: raw.Name,
		GitHubEnabled: boolDefault(raw.Sources.GitHub, true), LinearEnabled: boolDefault(raw.Sources.Linear, true),
		Period:  contract.Period{Preset: raw.Period.Preset, FromDate: raw.Period.FromDate, ToDate: raw.Period.ToDate, Timezone: raw.Period.Timezone},
		Modules: contract.Modules{GitHubIssues: boolDefault(raw.Modules.GitHubIssues, true), CodeHistory: boolDefault(raw.Modules.CodeHistory, true), CI: boolDefault(raw.Modules.CI, false), Deployments: boolDefault(raw.Modules.Deployments, false), RuntimeImport: boolDefault(raw.Modules.RuntimeImport, false)},
		Limits:  contract.Limits{GitHubRPH: raw.Limits.GitHubRPH, LinearRPH: raw.Limits.LinearRPH, MaxSessionMS: raw.Limits.MaxSessionMS, MaxTransferBytes: raw.Limits.MaxTransferBytes, MaxLocalBytes: raw.Limits.MaxLocalBytes},
	}
	if raw.Scope == nil {
		input.Scope = &contract.Scope{Mode: "all_authorized", Repositories: []string{}, Teams: []string{}}
	} else {
		input.Scope = &contract.Scope{Mode: raw.Scope.Mode, Repositories: raw.Scope.Repositories, Teams: raw.Scope.Teams}
	}
	if input.Scope.Mode == "" {
		input.Scope.Mode = "all_authorized"
	}
	if input.Scope.Repositories == nil {
		input.Scope.Repositories = []string{}
	}
	if input.Scope.Teams == nil {
		input.Scope.Teams = []string{}
	}
	if input.Limits.GitHubRPH == 0 {
		input.Limits.GitHubRPH = 300
	}
	if input.Limits.LinearRPH == 0 {
		input.Limits.LinearRPH = 180
	}
	if input.Limits.MaxSessionMS == 0 {
		input.Limits.MaxSessionMS = 7200000
	}
	if input.Limits.MaxTransferBytes == 0 {
		input.Limits.MaxTransferBytes = 536870912
	}
	if input.Limits.MaxLocalBytes == 0 {
		input.Limits.MaxLocalBytes = 5368709120
	}
	if input.Name == "" {
		out.MissingFields = append(out.MissingFields, "name")
	}
	if input.Period.Preset == "" {
		out.MissingFields = append(out.MissingFields, "period")
	}
	out.Input = input
	return out, nil
}

func validateYAML(n *yaml.Node) error {
	if n.Anchor != "" || n.Alias != nil || n.Kind == yaml.AliasNode {
		return fault("ERR_VALIDATION", "aliases/anchors YAML não são permitidos")
	}
	if strings.HasPrefix(n.Tag, "!") && !strings.HasPrefix(n.Tag, "!!") {
		return fault("ERR_VALIDATION", "tag YAML customizada não permitida")
	}
	if n.Kind == yaml.MappingNode {
		seen := make(map[string]bool)
		for i := 0; i < len(n.Content); i += 2 {
			k := n.Content[i].Value
			if seen[k] {
				return fault("ERR_VALIDATION", "chave YAML duplicada: "+k)
			}
			seen[k] = true
			if secretKey.MatchString(k) {
				return fault("ERR_VALIDATION", "segredo não pode estar no YAML")
			}
		}
	}
	for _, child := range n.Content {
		if err := validateYAML(child); err != nil {
			return err
		}
	}
	return nil
}

var secretKey = regexp.MustCompile(`(?i)(^|_)(token|secret|api_key|password|credential)(_|$)`)

func (s *Setup) ResolvePeriod(_ context.Context, p contract.Period) (contract.ResolvedPeriod, error) {
	var out contract.ResolvedPeriod
	if p.Preset == "" {
		return out, fault("ERR_PERIOD_REQUIRED", "escolha um período")
	}
	if p.Timezone == "" {
		p.Timezone = "UTC"
	}
	loc, err := time.LoadLocation(p.Timezone)
	if err != nil {
		return out, fault("ERR_VALIDATION", "timezone inválida")
	}
	anchor := s.now().In(loc)
	out = contract.ResolvedPeriod{Preset: p.Preset, Timezone: p.Timezone, AnchorEnd: anchor.UTC().Format(time.RFC3339), WindowEnd: anchor.UTC().Format(time.RFC3339)}
	var start time.Time
	switch p.Preset {
	case "30d", "60d", "90d":
		var days int
		fmt.Sscanf(p.Preset, "%dd", &days)
		start = anchor.AddDate(0, 0, -days)
	case "1y":
		start = anchor.AddDate(-1, 0, 0)
		if anchor.Month() == time.February && anchor.Day() == 29 {
			start = time.Date(anchor.Year()-1, time.February, 28, anchor.Hour(), anchor.Minute(), anchor.Second(), anchor.Nanosecond(), loc)
		}
	case "all":
		return out, nil
	case "custom":
		if p.FromDate == "" || p.ToDate == "" {
			return contract.ResolvedPeriod{}, fault("ERR_PERIOD_REQUIRED", "custom exige from e to")
		}
		from, e1 := time.ParseInLocation("2006-01-02", p.FromDate, loc)
		to, e2 := time.ParseInLocation("2006-01-02", p.ToDate, loc)
		if e1 != nil || e2 != nil || to.Before(from) {
			return contract.ResolvedPeriod{}, fault("ERR_VALIDATION", "intervalo custom inválido")
		}
		start = from
		out.WindowEnd = to.AddDate(0, 0, 1).UTC().Format(time.RFC3339)
	default:
		return contract.ResolvedPeriod{}, fault("ERR_VALIDATION", "preset inválido")
	}
	out.WindowStart = start.UTC().Format(time.RFC3339)
	return out, nil
}

func fault(code, message string) error { return &contract.ErrorResponse{Code: code, Message: message} }
