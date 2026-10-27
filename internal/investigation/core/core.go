package core

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"pocketcli/internal/investigation/collect"
	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/query"
	"pocketcli/internal/investigation/storage"
)

type CoreConfig struct {
	Clock               func() time.Time
	HTTPClient          *http.Client
	Secrets             contract.SecretProvider
	Store               *storage.Store
	Root                string
	Finalizer           contract.Finalizer
	ResolvePrincipals   bool
	QuotaReservePercent *int
}

type Core struct {
	setup     *Setup
	collector *collect.Client
	store     *storage.Store
	query     query.Service
	finalizer contract.Finalizer
	gitMirror gitMirrorFactory
	mu        sync.Mutex
	runs      map[string]context.CancelFunc
	locks     map[string]func()
}

func NewCore(cfg CoreConfig) (*Core, error) {
	s := cfg.Store
	var err error
	if s == nil {
		s, err = storage.Open(cfg.Root)
		if err != nil {
			return nil, fault("ERR_AUDIT_FAILED", "não foi possível criar armazenamento privado")
		}
	}
	q, err := query.New(filepath.Join(s.Root, "projection"))
	if err != nil {
		return nil, fault("ERR_AUDIT_FAILED", "não foi possível criar armazenamento SQL")
	}
	collector := collect.NewClient(cfg.HTTPClient, cfg.Secrets)
	if cfg.QuotaReservePercent != nil {
		if err := collector.SetQuotaReservePercent(*cfg.QuotaReservePercent); err != nil {
			return nil, err
		}
	}
	collector.SetCacheStore(s)
	if cfg.ResolvePrincipals {
		collector.EnablePrincipalResolution()
	}
	c := &Core{setup: NewSetup(cfg.Clock), collector: collector, store: s, query: q, finalizer: cfg.Finalizer, gitMirror: newGitMirror, runs: make(map[string]context.CancelFunc), locks: make(map[string]func())}
	if err := c.recoverInterrupted(); err != nil {
		return nil, fault("ERR_AUDIT_FAILED", "falha ao recuperar runs interrompidos")
	}
	return c, nil
}

func (c *Core) recoverInterrupted() error {
	ids, err := c.store.List("runs")
	if err != nil {
		return err
	}
	for _, id := range ids {
		var run contract.Run
		if err := c.store.Get("runs", id, &run); err != nil {
			return err
		}
		switch run.State {
		case "queued", "collecting", "building", "analysis_ready", "reporting":
		default:
			continue
		}
		var plan contract.Plan
		if err := c.store.Get("plans", run.PlanID, &plan); err != nil {
			return err
		}
		release, err := c.store.Lock(plan.InvestigationID)
		if err != nil { // Another process still owns this investigation.
			continue
		}
		if (run.State == "analysis_ready" || run.State == "reporting") && run.SnapshotID != "" {
			run.State = "report_failed"
			run.LastError = &contract.ErrorResponse{Code: "ERR_PDF_FAILED", Message: "finalização interrompida; gere o relatório novamente"}
		} else {
			run.State = "paused"
			run.LastError = &contract.ErrorResponse{Code: "ERR_UNAVAILABLE", Message: "execução interrompida; retome a coleta"}
		}
		err = c.store.Put("runs", id, run)
		release()
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *Core) Setup() contract.SetupService { return c.setup }
func (c *Core) SQLAvailable() bool           { return c.query.Available() }

func (c *Core) SetFinalizer(f contract.Finalizer) { c.mu.Lock(); c.finalizer = f; c.mu.Unlock() }

func (c *Core) LoadPlan(id string) (contract.Plan, error) {
	var p contract.Plan
	if c.store.Get("plans", id, &p) != nil {
		return p, fault("ERR_NOT_FOUND", "plano não encontrado")
	}
	return p, nil
}

func (c *Core) PreparePlan(ctx context.Context, input contract.PlanInput) (contract.Plan, error) {
	var out contract.Plan
	if input.SchemaVersion != contract.Version {
		return out, fault("ERR_SCHEMA_VERSION_UNSUPPORTED", "schema_version incompatível")
	}
	if len(input.Name) > 120 {
		return out, fault("ERR_VALIDATION", "name excede 120 caracteres")
	}
	if input.Scope == nil {
		input.Scope = &contract.Scope{Mode: "all_authorized"}
	}
	if input.Scope.Mode != "all_authorized" && input.Scope.Mode != "selected" {
		return out, fault("ERR_VALIDATION", "scope.mode inválido")
	}
	if input.Scope.Mode == "selected" && len(input.Scope.Repositories) == 0 && len(input.Scope.Teams) == 0 {
		return out, fault("ERR_VALIDATION", "selected exige recursos")
	}
	input.Scope.Repositories = dedupe(input.Scope.Repositories)
	input.Scope.Teams = dedupe(input.Scope.Teams)
	discoveryLimit := collect.DiscoveryBudget(ctx)
	if discoveryLimit == 0 {
		discoveryLimit = collect.DefaultDiscoveryBudget
	}
	if discoveryLimit < 1 {
		return out, fault("ERR_VALIDATION", "orçamento de descoberta deve ser positivo")
	}
	if input.GitHubEnabled && len(input.Scope.Repositories) > discoveryLimit {
		return out, fault("ERR_RESOURCE_EXHAUSTED", "repositórios selecionados excedem orçamento de descoberta; aprove mais chamadas")
	}
	ctx = collect.WithDiscoveryBudget(ctx, discoveryLimit)
	if !input.GitHubEnabled && !input.LinearEnabled && !input.Modules.RuntimeImport {
		return out, fault("ERR_VALIDATION", "selecione uma fonte")
	}
	period, err := c.setup.ResolvePeriod(ctx, input.Period)
	if err != nil {
		return out, err
	}
	input.Period.Timezone = period.Timezone
	if input.InvestigationID == "" {
		input.InvestigationID, err = storage.ID()
		if err != nil {
			return out, err
		}
	}
	planID, err := storage.ID()
	if err != nil {
		return out, err
	}
	ctx = collect.WithAuditID(ctx, planID)
	if input.Limits.GitHubRPH < 0 || input.Limits.LinearRPH < 0 || input.Limits.MaxSessionMS < 0 || input.Limits.MaxTransferBytes < 0 || input.Limits.MaxLocalBytes < 0 {
		return out, fault("ERR_VALIDATION", "limite negativo")
	}
	if input.Limits.GitHubRPH <= 0 {
		input.Limits.GitHubRPH = 300
	}
	if input.Limits.LinearRPH <= 0 {
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
	c.collector.SetRPH("github", input.Limits.GitHubRPH)
	c.collector.SetRPH("linear", input.Limits.LinearRPH)
	auth, err := c.currentAuth(ctx, input)
	if err != nil {
		return out, err
	}
	verifiedGitHubVersion := ""
	if input.GitHubEnabled {
		lookupNeeded, err := c.collector.PrincipalLookupNeeded(ctx, "github")
		if err != nil {
			return out, err
		}
		versionNeeded, err := c.collector.GitHubVersionLookupNeeded(ctx)
		if err != nil {
			return out, err
		}
		minimum := len(input.Scope.Repositories)
		if minimum == 0 {
			minimum = 1 // Primeira página de repositórios autorizados.
		}
		if lookupNeeded {
			minimum++
		}
		if versionNeeded {
			minimum++
		}
		if minimum > discoveryLimit {
			return out, fault("ERR_RESOURCE_EXHAUSTED", "identidade, versão GitHub e repositórios excedem orçamento de descoberta; aprove mais chamadas")
		}
		verifiedGitHubVersion, err = c.collector.VerifyGitHubVersion(ctx)
		if err != nil {
			return out, err
		}
	}
	resources := make([]contract.Resource, 0)
	if input.GitHubEnabled {
		if len(input.Scope.Repositories) > 0 {
			for _, name := range input.Scope.Repositories {
				if !validRepo(name) {
					return out, fault("ERR_VALIDATION", "repositório deve ser owner/name")
				}
				r, err := c.discoverRepository(ctx, name)
				if err != nil {
					return out, err
				}
				resources = append(resources, r)
			}
		} else {
			items, _, err := c.collector.GitHubList(ctx, "/user/repos?per_page=100&affiliation=owner,collaborator,organization_member", discoveryLimit)
			if err != nil {
				return out, err
			}
			for _, item := range items {
				var r struct {
					ID       int64  `json:"id"`
					FullName string `json:"full_name"`
				}
				if json.Unmarshal(item, &r) == nil && r.ID > 0 {
					if !validRepo(r.FullName) {
						return out, fault("ERR_UNAVAILABLE", "nome de repositório inválido na descoberta")
					}
					resources = append(resources, contract.Resource{ID: fmt.Sprintf("%d", r.ID), Source: "github", Name: r.FullName, Access: "read"})
				}
			}
		}
	}
	if input.LinearEnabled {
		const q = `query Teams($after: String) { teams(first: 100, after: $after) { nodes { id key name } pageInfo { hasNextPage endCursor } } }`
		var result struct {
			Teams struct {
				Nodes []struct {
					ID   string `json:"id"`
					Key  string `json:"key"`
					Name string `json:"name"`
				} `json:"nodes"`
				PageInfo struct {
					HasNextPage bool   `json:"hasNextPage"`
					EndCursor   string `json:"endCursor"`
				} `json:"pageInfo"`
			} `json:"teams"`
		}
		foundTeams := make(map[string]string)
		foundIDs := make(map[string]string)
		selectedMatches := make(map[string]int)
		cursor := ""
		complete := false
		for page := 0; page < discoveryLimit; page++ {
			vars := map[string]string{}
			if cursor != "" {
				vars["after"] = cursor
			}
			data, err := c.collector.LinearQuery(ctx, q, vars)
			if err != nil {
				return out, err
			}
			result.Teams.Nodes = nil
			result.Teams.PageInfo.HasNextPage = false
			result.Teams.PageInfo.EndCursor = ""
			if err := json.Unmarshal(data, &result); err != nil || result.Teams.Nodes == nil {
				return out, fault("ERR_UNAVAILABLE", "teams Linear inválidos")
			}
			for _, team := range result.Teams.Nodes {
				matches := make(map[string]bool)
				for _, selected := range input.Scope.Teams {
					if strings.EqualFold(selected, team.Key) || strings.EqualFold(selected, team.ID) {
						matches[strings.ToLower(selected)] = true
					}
				}
				if len(input.Scope.Teams) > 0 && len(matches) == 0 {
					continue
				}
				if team.ID == "" || team.Key == "" {
					return out, fault("ERR_UNAVAILABLE", "team Linear sem identidade")
				}
				key := strings.ToLower(team.Key)
				id := strings.ToLower(team.ID)
				if oldID, seen := foundTeams[key]; seen {
					if oldID != team.ID {
						return out, fault("ERR_UNAVAILABLE", "team Linear mudou durante descoberta")
					}
					continue
				}
				if oldKey, seen := foundIDs[id]; seen && oldKey != key {
					return out, fault("ERR_UNAVAILABLE", "ID de team Linear duplicado na descoberta")
				}
				foundTeams[key] = team.ID
				foundIDs[id] = key
				for selected := range matches {
					selectedMatches[selected]++
				}
				resources = append(resources, contract.Resource{ID: team.ID, Source: "linear", Name: team.Key, Access: "read"})
			}
			if !result.Teams.PageInfo.HasNextPage {
				complete = true
				break
			}
			if result.Teams.PageInfo.EndCursor == "" || result.Teams.PageInfo.EndCursor == cursor {
				return out, fault("ERR_UNAVAILABLE", "cursor Linear inválido")
			}
			cursor = result.Teams.PageInfo.EndCursor
		}
		if !complete {
			return out, fault("ERR_RESOURCE_EXHAUSTED", "descoberta Linear incompleta")
		}
		for _, selected := range input.Scope.Teams {
			switch selectedMatches[strings.ToLower(selected)] {
			case 0:
				return out, fault("ERR_FORBIDDEN", "team Linear selecionado não foi descoberto: "+selected)
			case 1:
			default:
				return out, fault("ERR_VALIDATION", "identificador de team Linear ambíguo: "+selected)
			}
		}
	}
	if input.Modules.RuntimeImport {
		runtimeResource, _, err := c.runtimeResource(input.InvestigationID)
		if err != nil {
			return out, err
		}
		resources = append(resources, runtimeResource)
	}
	if len(resources) == 0 {
		return out, fault("ERR_FORBIDDEN", "nenhum recurso autorizado encontrado")
	}
	sort.Slice(resources, func(i, j int) bool {
		if resources[i].Source == resources[j].Source {
			return resources[i].ID < resources[j].ID
		}
		return resources[i].Source < resources[j].Source
	})
	canonicalInput := input
	canonicalInput.InvestigationID = ""
	canonical, _ := json.Marshal(struct {
		Input      contract.PlanInput
		Auth       planAuth
		Resources  []contract.Resource
		Start, End string
	}{canonicalInput, auth, resources, period.WindowStart, period.WindowEnd})
	id := planID
	warnings := []string{}
	if verifiedGitHubVersion != "" {
		warnings = append(warnings, "GitHub REST API "+verifiedGitHubVersion+" confirmada pelo servidor")
	}
	for _, source := range []struct {
		name    string
		enabled bool
	}{{"github", input.GitHubEnabled}, {"linear", input.LinearEnabled}} {
		if !source.enabled {
			continue
		}
		principalID, err := c.collector.PrincipalID(ctx, source.name)
		if err != nil {
			return out, err
		}
		if principalID != "" {
			warnings = append(warnings, fmt.Sprintf("conta %s verificada: ID %s", source.name, principalID))
		}
	}
	if discoveryLimit > collect.DefaultDiscoveryBudget {
		warnings = append(warnings, fmt.Sprintf("orçamento de descoberta aprovado: até %d chamadas por fonte", discoveryLimit))
	}
	if input.Modules.CodeHistory {
		warnings = append(warnings, "coleta Git de refs e manifestos ativa; histórico de arquivos ainda tem cobertura parcial")
	}
	if input.Modules.CI {
		warnings = append(warnings, "CI observa workflow runs e checks dos PRs; sucesso não comprova implantação")
	}
	if input.Modules.Deployments {
		warnings = append(warnings, "status de deployment é declaração da API; histórico antigo pode ser parcial")
	}
	if input.Modules.RuntimeImport {
		for _, resource := range resources {
			if resource.Source == "runtime" && resource.ID == "runtime-pending" {
				warnings = append(warnings, "snapshot operacional pendente; importe o JSON e prepare novo plano para incluí-lo")
			}
		}
	}
	out = contract.Plan{InvestigationID: input.InvestigationID, ID: id, Hash: storage.Hash(canonical), Input: input, Resources: resources, WindowStart: period.WindowStart, WindowEnd: period.WindowEnd, RequiresConfirmation: true, Warnings: warnings}
	if err := c.store.Put("planauth", id, auth); err != nil {
		return contract.Plan{}, fault("ERR_AUDIT_FAILED", "falha ao persistir identidade do plano")
	}
	if err := c.store.Put("plans", id, out); err != nil {
		return contract.Plan{}, fault("ERR_AUDIT_FAILED", "falha ao persistir plano")
	}
	return out, nil
}

func validRepo(name string) bool {
	if !repoName.MatchString(name) {
		return false
	}
	repo := strings.SplitN(name, "/", 2)[1]
	return repo != "." && repo != ".."
}

var repoName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,38}/[A-Za-z0-9_.-]{1,100}$`)

func containsFold(values []string, target string) bool {
	for _, v := range values {
		if strings.EqualFold(v, target) {
			return true
		}
	}
	return false
}
func dedupe(values []string) []string {
	set := make(map[string]bool)
	for _, v := range values {
		if v != "" {
			set[v] = true
		}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (c *Core) discoverRepository(ctx context.Context, name string) (contract.Resource, error) {
	var empty contract.Resource
	base, err := url.Parse(c.collector.GitHubURL)
	if err != nil {
		return empty, err
	}
	endpoint := base.ResolveReference(&url.URL{Path: "/repos/" + name}).String()
	data, _, _, err := c.collector.GetGitHub(ctx, endpoint)
	if err != nil {
		return empty, err
	}
	var repo struct {
		ID       int64  `json:"id"`
		FullName string `json:"full_name"`
	}
	if json.Unmarshal(data, &repo) != nil || repo.ID == 0 || !validRepo(repo.FullName) {
		return empty, fault("ERR_UNAVAILABLE", "repositório inválido")
	}
	return contract.Resource{ID: fmt.Sprintf("%d", repo.ID), Source: "github", Name: repo.FullName, Access: "read"}, nil
}
