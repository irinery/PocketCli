package mock

import (
	"context"
	"regexp"
	"strings"
	"time"

	"pocketcli/internal/investigation/contract"
)

type Setup struct{}

func (Setup) Parse(_ context.Context, in contract.SetupParseRequest) (contract.ParsedSetup, error) {
	if !strings.HasSuffix(strings.ToLower(in.Filename), ".yml") && !strings.HasSuffix(strings.ToLower(in.Filename), ".yaml") {
		return contract.ParsedSetup{}, &contract.ErrorResponse{Code: "ERR_VALIDATION", Message: "Importe um arquivo .yml ou .yaml.", Fields: []contract.FieldError{{Field: "filename", Message: "Extensão YAML obrigatória."}}}
	}
	content := strings.ToLower(in.Content)
	if strings.Contains(content, "token:") || strings.Contains(content, "api_key:") || strings.Contains(content, "api-key:") || strings.Contains(content, "secret:") || strings.Contains(content, "bearer ") || regexp.MustCompile(`\b(?:ghp_|github_pat_|gho_|lin_api_|sk-ant-)`).MatchString(content) {
		return contract.ParsedSetup{}, &contract.ErrorResponse{Code: "ERR_SECRET_IN_CONFIG", Message: "Remova credenciais do YAML e informe-as no campo seguro.", Fields: []contract.FieldError{{Field: "content", Message: "Segredo em texto claro não é aceito."}}}
	}
	const fixture = "schema_version: investigate.v1\nname: PocketCli Demo\nperiod: 90d\nscope: all_authorized"
	if strings.TrimSpace(in.Content) == fixture {
		return contract.ParsedSetup{Input: contract.PlanInput{SchemaVersion: contract.Version, Name: "PocketCli Demo", Scope: &contract.Scope{Mode: "all_authorized", Repositories: []string{}, Teams: []string{}}, Period: contract.Period{Preset: "90d", Timezone: "UTC"}, GitHubEnabled: true, LinearEnabled: true, Modules: contract.Modules{GitHubIssues: true, CodeHistory: true}, Limits: contract.Limits{GitHubRPH: 60, LinearRPH: 60, MaxSessionMS: 3600000, MaxTransferBytes: 100000000, MaxLocalBytes: 1000000000}}, MissingFields: []string{}, Warnings: []string{"Fixture YAML de demonstração; não inicia coleta real."}}, nil
	}
	return contract.ParsedSetup{}, &contract.ErrorResponse{Code: "ERR_DEMO_SETUP_UNSUPPORTED", Message: "O preview não interpreta YAML livre; use a fixture conhecida, a configuração manual ou o serviço de setup real.", Fields: []contract.FieldError{}}
}

func (Setup) ResolvePeriod(_ context.Context, p contract.Period) (contract.ResolvedPeriod, error) {
	end := time.Now().UTC().Truncate(24 * time.Hour)
	start := end
	switch p.Preset {
	case "30d":
		start = end.AddDate(0, 0, -30)
	case "60d":
		start = end.AddDate(0, 0, -60)
	case "90d":
		start = end.AddDate(0, 0, -90)
	case "1y":
		start = end.AddDate(-1, 0, 0)
	case "all":
		start = time.Time{}
	case "custom":
		from, e1 := time.Parse("2006-01-02", p.FromDate)
		to, e2 := time.Parse("2006-01-02", p.ToDate)
		if e1 != nil || e2 != nil || to.Before(from) {
			return contract.ResolvedPeriod{}, &contract.ErrorResponse{Code: "ERR_VALIDATION", Message: "Período personalizado inválido.", Fields: []contract.FieldError{{Field: "period", Message: "Informe datas válidas em ordem crescente."}}}
		}
		start = from
		end = to.AddDate(0, 0, 1)
	default:
		return contract.ResolvedPeriod{}, &contract.ErrorResponse{Code: "ERR_VALIDATION", Message: "Período não suportado.", Fields: []contract.FieldError{{Field: "preset", Message: "Use 30d, 60d, 90d, 1y, all ou custom."}}}
	}
	zone := p.Timezone
	if zone == "" {
		zone = "UTC"
	}
	if _, err := time.LoadLocation(zone); err != nil {
		return contract.ResolvedPeriod{}, &contract.ErrorResponse{Code: "ERR_VALIDATION", Message: "Fuso horário inválido.", Fields: []contract.FieldError{{Field: "timezone", Message: "Use um fuso horário IANA válido."}}}
	}
	out := contract.ResolvedPeriod{Preset: p.Preset, Timezone: zone, AnchorEnd: end.Format(time.RFC3339), WindowEnd: end.Format(time.RFC3339)}
	if !start.IsZero() {
		out.WindowStart = start.Format(time.RFC3339)
	}
	return out, nil
}
