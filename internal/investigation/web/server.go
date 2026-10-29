package web

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"pocketcli/internal/investigation/annotations"
	"pocketcli/internal/investigation/collect"
	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/editorial"
	"pocketcli/internal/investigation/insightcontract"
	"pocketcli/internal/investigation/queries"
)

type PreviewOptions struct {
	Demo            bool
	InvestigationID string
	SnapshotID      string
	BindHost        string
	Port            int
	Scenario        string
	DataDir         string
	QueryHistory    *queries.Store
	ReportJobs      *ReportJobs
	// PDFUnavailableReason is the sanitized dependency error from the
	// composition (for example an unwritable report directory). It is shown in
	// /capabilities before collection; it never contains credentials.
	PDFUnavailableReason string
	InsightReader        insightcontract.InsightReader
	AnalysisController   insightcontract.AnalysisController
	ExecutiveExporter    insightcontract.ExecutiveExporter
}

// ContractHeader carries the contract version the browser assets were built
// for. A request that declares a different version is rejected before any
// handler interprets its DTOs (RFC 01 S01-05, RFC 02 L01-05).
const ContractHeader = "X-Investigate-Contract"

type EditorialService interface {
	Models(context.Context) ([]editorial.Model, error)
	Preview(context.Context, editorial.Selection) (editorial.Preview, error)
	Generate(context.Context, editorial.GenerateInput) (editorial.Draft, error)
	Review(id string, expected int, accept bool) (editorial.Draft, error)
	Drafts(snapshot string) []editorial.Draft
	Draft(string) (editorial.Draft, bool)
	PreviewInfo(string) (editorial.Preview, bool)
}
type AnnotationStore interface {
	Create(annotations.CreateInput) (annotations.Annotation, error)
	Update(string, annotations.UpdateInput) (annotations.Annotation, error)
	Resolve(string, int) (annotations.Annotation, error)
	List(snapshot, cursor string, limit int) annotations.Page
	Get(string) (annotations.Annotation, bool)
}
type ReportService interface {
	contract.Finalizer
	Get(string) (contract.ReportArtifact, error)
	Open(string) (*os.File, error)
	List(snapshot string) []contract.ReportArtifact
}
type findingLister interface {
	Findings(string) ([]contract.Finding, error)
}
type runStateSetter interface {
	SetRunState(id, state string, lastErr *contract.ErrorResponse)
}
type planLoader interface {
	LoadPlan(string) (contract.Plan, error)
}

type session struct {
	csrf    string
	expires time.Time
}
type Handler struct {
	core              contract.Core
	setup             contract.SetupService
	credentials       contract.CredentialStore
	annotations       AnnotationStore
	editorial         EditorialService
	reports           ReportService
	insightReader     insightcontract.InsightReader
	analysis          insightcontract.AnalysisController
	executiveExports  insightcontract.ExecutiveExporter
	options           PreviewOptions
	bootstrap         string
	mu                sync.Mutex
	sessions          map[string]session
	unlockAttempts    int
	bootstrapExpires  time.Time
	finalizedRuns     map[string]bool
	authorizedInsight map[string]bool
	queryStore        *queries.Store
	events            *eventHub
}

// NewHandler composes the local application. BindHost may be loopback only.
// Credential, editorial, annotation, and report adapters are injected by the
// final integration; nil adapters remain explicitly unavailable.
func NewHandler(core contract.Core, setup contract.SetupService, credentials contract.CredentialStore, editorialService EditorialService, annotationStore AnnotationStore, reports ReportService, options PreviewOptions) (http.Handler, string, error) {
	if core == nil {
		return nil, "", errors.New("investigation core is required")
	}
	if !options.Demo && strings.TrimSpace(options.InvestigationID) == "" {
		return nil, "", errors.New("investigation id is required")
	}
	if options.BindHost == "" {
		options.BindHost = "127.0.0.1"
	}
	ip := net.ParseIP(options.BindHost)
	if ip == nil || !ip.IsLoopback() {
		return nil, "", errors.New("investigation server accepts loopback bind addresses only")
	}
	if options.Port < 0 || options.Port > 65535 {
		return nil, "", errors.New("invalid port")
	}
	if options.ReportJobs == nil {
		options.ReportJobs = NewReportJobs()
	}
	var raw [24]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return nil, "", err
	}
	code := base64.RawURLEncoding.EncodeToString(raw[:])
	h := &Handler{core: core, setup: setup, credentials: credentials, annotations: annotationStore, editorial: editorialService, reports: reports, insightReader: options.InsightReader, analysis: options.AnalysisController, executiveExports: options.ExecutiveExporter, options: options, bootstrap: code, sessions: map[string]session{}, finalizedRuns: map[string]bool{}, authorizedInsight: map[string]bool{}, events: newEventHub(), bootstrapExpires: time.Now().Add(5 * time.Minute)}
	if options.QueryHistory != nil {
		h.queryStore = options.QueryHistory
	} else if options.DataDir != "" {
		store, err := queries.Open(options.DataDir)
		if err != nil {
			return nil, "", err
		}
		h.queryStore = store
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", h.root)
	mux.HandleFunc("/api/v1/", h.api)
	return h.securityHeaders(mux), code, nil
}

func (h *Handler) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !h.validHost(r) {
			http.Error(w, "invalid host", http.StatusMisdirectedRequest)
			return
		}
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; object-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}
func (h *Handler) validHost(r *http.Request) bool {
	host := r.Host
	if strings.Contains(host, ":") {
		var err error
		host, _, err = net.SplitHostPort(r.Host)
		if err != nil {
			return false
		}
	}
	return host == "localhost" || host == "127.0.0.1" || host == "[::1]" || host == "::1"
}
func (h *Handler) root(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if asset := r.URL.Query().Get("asset"); asset == "app.css" {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		_, _ = io.WriteString(w, appCSS)
		return
	} else if asset == "app.js" {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		if h.options.Demo {
			_, _ = io.WriteString(w, appJS)
		} else if _, ok := h.authenticated(r); !ok {
			_, _ = io.WriteString(w, realUnlockJS)
		} else {
			_, _ = io.WriteString(w, realAppJS(h.options.InvestigationID, h.options.SnapshotID))
		}
		return
	}
	if _, ok := h.authenticated(r); !ok {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, unlockPage)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	page := appHTML
	if !h.options.Demo {
		page = strings.Replace(page, `<span class="demo-badge">DEMO · DADOS FICTÍCIOS</span>`, `<span class="tag green">INVESTIGAÇÃO LOCAL</span>`, 1)
		page = strings.Replace(page, "Carregando dados de demonstração…", "Carregando investigação…", 1)
	}
	_, _ = io.WriteString(w, page)
}
func (h *Handler) authenticated(r *http.Request) (string, bool) {
	c, err := r.Cookie("pocketcli_session")
	if err != nil {
		return "", false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	s, ok := h.sessions[c.Value]
	if !ok || time.Now().After(s.expires) {
		delete(h.sessions, c.Value)
		return "", false
	}
	return c.Value, true
}
func (h *Handler) api(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1")
	if declared := r.Header.Get(ContractHeader); declared != "" && declared != contract.Version {
		h.writeError(w, http.StatusConflict, "ERR_CONTRACT_MISMATCH", "Interface local usa contrato "+contractLabel(declared)+"; o servidor exige "+contract.Version+". Recarregue a página servida por este processo.")
		return
	}
	if path == "/session/unlock" && r.Method == http.MethodPost {
		if !h.sameOrigin(r) {
			h.writeError(w, http.StatusForbidden, "ERR_ORIGIN", "Origem local inválida.")
			return
		}
		h.unlock(w, r)
		return
	}
	sid, ok := h.authenticated(r)
	if !ok {
		h.writeError(w, http.StatusUnauthorized, "ERR_SESSION_REQUIRED", "Desbloqueie a sessão local.")
		return
	}
	if r.Method == http.MethodGet && path == "/session" {
		h.mu.Lock()
		csrf := h.sessions[sid].csrf
		h.mu.Unlock()
		h.writeJSON(w, http.StatusOK, map[string]any{"ok": true, "authenticated": true, "csrf_token": csrf})
		return
	}
	if isWrite(r.Method) && !h.validWrite(r, sid) {
		h.writeError(w, http.StatusForbidden, "ERR_CSRF", "Origin ou token CSRF inválido.")
		return
	}
	if r.Method == http.MethodDelete && path == "/session" {
		h.mu.Lock()
		delete(h.sessions, sid)
		h.mu.Unlock()
		http.SetCookie(w, &http.Cookie{Name: "pocketcli_session", Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: -1})
		w.WriteHeader(http.StatusNoContent)
		return
	}
	h.routeAPI(w, r, path, sid)
}
func (h *Handler) unlock(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Code string `json:"code"`
	}
	if !h.decode(w, r, &body, 4096) {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.unlockAttempts >= 10 || time.Now().After(h.bootstrapExpires) || h.bootstrap == "" || subtle.ConstantTimeCompare([]byte(body.Code), []byte(h.bootstrap)) != 1 {
		h.unlockAttempts++
		h.writeError(w, http.StatusUnauthorized, "ERR_UNLOCK_FAILED", "Código de sessão inválido ou expirado.")
		return
	}
	h.bootstrap = ""
	var cookieRaw, csrfRaw [32]byte
	if _, err := rand.Read(cookieRaw[:]); err != nil {
		h.writeError(w, 500, "ERR_INTERNAL", "Falha ao criar sessão.")
		return
	}
	if _, err := rand.Read(csrfRaw[:]); err != nil {
		h.writeError(w, 500, "ERR_INTERNAL", "Falha ao criar sessão.")
		return
	}
	sid := base64.RawURLEncoding.EncodeToString(cookieRaw[:])
	csrf := base64.RawURLEncoding.EncodeToString(csrfRaw[:])
	h.sessions[sid] = session{csrf: csrf, expires: time.Now().Add(8 * time.Hour)}
	http.SetCookie(w, &http.Cookie{Name: "pocketcli_session", Value: sid, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: 8 * 60 * 60})
	h.writeJSON(w, http.StatusOK, map[string]any{"ok": true, "authenticated": true})
}
func (h *Handler) validWrite(r *http.Request, sid string) bool {
	if !h.sameOrigin(r) {
		return false
	}
	csrf := r.Header.Get("X-CSRF-Token")
	h.mu.Lock()
	want := h.sessions[sid].csrf
	h.mu.Unlock()
	return want != "" && subtle.ConstantTimeCompare([]byte(csrf), []byte(want)) == 1
}

func (h *Handler) sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	u, err := http.NewRequest(http.MethodGet, origin, nil)
	if err != nil || u.URL == nil {
		return false
	}
	if u.URL.Scheme != "http" && u.URL.Scheme != "https" {
		return false
	}
	if u.Host != r.Host {
		return false
	}
	return h.validHost(u)
}
func isWrite(method string) bool {
	return method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions
}

func insightLimit(w http.ResponseWriter, raw string, fallback int) (int, bool) {
	limit := fallback
	if raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 200 {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"code":"ERR_VALIDATION","message":"Limite deve estar entre 1 e 200."}`)
			return 0, false
		}
		limit = n
	}
	return limit, true
}

func validInsightAnalysisStatus(status string) bool {
	return status == "complete" || status == "partial" || status == "not_run" || status == "failed"
}
func validRunState(state string) bool {
	return state == "queued" || state == "running" || state == "completed" || state == "completed_partial" || state == "failed" || state == "cancelled"
}
func validRunPhase(phase string) bool {
	return phase == "source" || phase == "facts" || phase == "reasoning" || phase == "presentation" || phase == "persisting" || phase == "idle"
}
func validExecutiveReport(report insightcontract.ExecutiveReport) bool {
	if report.Version != insightcontract.Version || report.TotalCases < 0 || int64(len(report.Cases)) > report.TotalCases || len(report.Cases) != len(report.CaseIDs) {
		return false
	}
	if !(report.PublicationStatus == "ready" || report.PublicationStatus == "exporting" || report.PublicationStatus == "exported" || report.PublicationStatus == "export_failed") || !validInsightAnalysisStatus(report.AnalysisStatus) {
		return false
	}
	if !(report.SufficiencyStatus == "sufficient" || report.SufficiencyStatus == "partial" || report.SufficiencyStatus == "insufficient" || report.SufficiencyStatus == "not_applicable") {
		return false
	}
	seen := map[string]bool{}
	for i, c := range report.Cases {
		if c.ID == "" || report.CaseIDs[i] != c.ID || seen[c.ID] {
			return false
		}
		seen[c.ID] = true
	}
	return true
}

func validExecutiveArtifact(artifact insightcontract.ExecutiveArtifact) bool {
	if artifact.ID == "" || artifact.SnapshotID == "" || artifact.AnalysisID == "" ||
		(artifact.Mode != "executive" && artifact.Mode != "analysis_detail") ||
		artifact.Revision < 1 || artifact.PageCount < 1 || artifact.SelectedCaseCount < 0 ||
		artifact.TotalCaseCount < 0 || artifact.SelectedCaseCount > artifact.TotalCaseCount || artifact.RendererVersion == "" {
		return false
	}
	for _, r := range artifact.ID {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	for _, hash := range []string{artifact.ContentHash, artifact.AnalysisHash} {
		decoded, err := hex.DecodeString(hash)
		if err != nil || len(decoded) != 32 {
			return false
		}
	}
	if _, err := time.Parse(time.RFC3339, artifact.CreatedAt); err != nil {
		return false
	}
	return artifact.DownloadPath == "/api/v1/insights/exports/"+artifact.ID+"/download"
}

func (h *Handler) decode(w http.ResponseWriter, r *http.Request, dst any, max int64) bool {
	r.Body = http.MaxBytesReader(w, r.Body, max)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		h.writeError(w, http.StatusBadRequest, "ERR_VALIDATION", "Corpo JSON inválido.")
		return false
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		h.writeError(w, http.StatusBadRequest, "ERR_VALIDATION", "Corpo JSON inválido.")
		return false
	}
	return true
}

func (h *Handler) notInScope(w http.ResponseWriter) {
	h.writeError(w, http.StatusNotFound, "ERR_NOT_FOUND", "Recurso fora desta investigação.")
}

func (h *Handler) ownsSnapshot(w http.ResponseWriter, ctx context.Context, id string) bool {
	if h.options.Demo {
		return true
	}
	snapshot, err := h.core.GetSnapshot(ctx, id)
	if err != nil {
		h.fail(w, err)
		return false
	}
	if snapshot.InvestigationID != h.options.InvestigationID {
		h.notInScope(w)
		return false
	}
	return true
}

func insightIdentity(snapshotID, analysisID string) string { return snapshotID + "\x00" + analysisID }

func (h *Handler) authorizeInsight(snapshotID, analysisID string) {
	if snapshotID == "" || analysisID == "" {
		return
	}
	h.mu.Lock()
	h.authorizedInsight[insightIdentity(snapshotID, analysisID)] = true
	h.mu.Unlock()
}

func (h *Handler) requireAuthorizedInsight(w http.ResponseWriter, snapshotID, analysisID string) bool {
	h.mu.Lock()
	ok := h.authorizedInsight[insightIdentity(snapshotID, analysisID)]
	h.mu.Unlock()
	if !ok {
		h.notInScope(w)
		return false
	}
	return true
}

func (h *Handler) ownsPlan(w http.ResponseWriter, id string) bool {
	if h.options.Demo {
		return true
	}
	loader, ok := h.core.(planLoader)
	if !ok {
		h.unavailable(w, "plano")
		return false
	}
	plan, err := loader.LoadPlan(id)
	if err != nil {
		h.fail(w, err)
		return false
	}
	if plan.InvestigationID != h.options.InvestigationID {
		h.notInScope(w)
		return false
	}
	return true
}

func (h *Handler) ownsRun(w http.ResponseWriter, ctx context.Context, id string) bool {
	if h.options.Demo {
		return true
	}
	run, err := h.core.GetRun(ctx, id)
	if err != nil {
		h.fail(w, err)
		return false
	}
	return h.ownsPlan(w, run.PlanID)
}

func (h *Handler) routeAPI(w http.ResponseWriter, r *http.Request, path string, sid string) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if r.Method == http.MethodGet && path == "/capabilities" {
		capabilities := map[string]any{"contract": contract.Version, "insight_contract": insightcontract.Version, "insights_available": h.insightReader != nil, "analysis_available": h.analysis != nil, "executive_export_available": h.executiveExports != nil, "demo": h.options.Demo, "modules": []string{"github_issues", "code_history", "ci", "deployments", "runtime_import"}, "pdf_available": h.reports != nil, "claude_available": h.editorial != nil}
		if h.reports == nil {
			reason := strings.TrimSpace(h.options.PDFUnavailableReason)
			if reason == "" {
				reason = "ERR_PDF_DEPENDENCY: renderizador PDF não configurado"
			}
			capabilities["pdf_unavailable_reason"] = reason
		}
		h.writeJSON(w, 200, capabilities)
		return
	}
	if r.Method == http.MethodPost && path == "/setup/parse" {
		if h.setup == nil {
			h.unavailable(w, "setup")
			return
		}
		var in contract.SetupParseRequest
		if !h.decode(w, r, &in, 256*1024) {
			return
		}
		out, err := h.setup.Parse(r.Context(), in)
		h.respond(w, out, err)
		return
	}
	if r.Method == http.MethodPost && path == "/setup/resolve-period" {
		if h.setup == nil {
			h.unavailable(w, "setup")
			return
		}
		var in contract.Period
		if !h.decode(w, r, &in, 16*1024) {
			return
		}
		out, err := h.setup.ResolvePeriod(r.Context(), in)
		h.respond(w, out, err)
		return
	}
	if r.Method == http.MethodPost && path == "/plans" {
		var in contract.PlanInput
		if !h.decode(w, r, &in, 64*1024) {
			return
		}
		if !h.options.Demo {
			if in.InvestigationID != "" && in.InvestigationID != h.options.InvestigationID {
				h.notInScope(w)
				return
			}
			in.InvestigationID = h.options.InvestigationID
		}
		ctx := r.Context()
		if values, ok := r.URL.Query()["discovery_budget"]; ok {
			if len(values) != 1 {
				h.writeError(w, http.StatusBadRequest, "ERR_VALIDATION", "Informe um orçamento de descoberta por fonte.")
				return
			}
			budget, err := strconv.Atoi(values[0])
			if err != nil || budget < 1 {
				h.writeError(w, http.StatusBadRequest, "ERR_VALIDATION", "Orçamento de descoberta fora do intervalo permitido.")
				return
			}
			ctx = collect.WithDiscoveryBudget(ctx, budget)
		}
		out, err := h.core.PreparePlan(ctx, in)
		h.respond(w, out, err)
		return
	}
	if len(parts) == 2 && parts[0] == "credentials" {
		if h.credentials == nil {
			h.unavailable(w, "credential store")
			return
		}
		source := parts[1]
		switch r.Method {
		case http.MethodGet:
			out, err := h.credentials.Status(r.Context(), source)
			h.respond(w, out, err)
			return
		case http.MethodDelete:
			if err := h.credentials.Delete(r.Context(), source); err != nil {
				h.fail(w, err)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		case http.MethodPost:
			var in contract.CredentialInput
			if !h.decode(w, r, &in, 16*1024) {
				return
			}
			if in.Source != source {
				h.writeError(w, 400, "ERR_VALIDATION", "A fonte do corpo diverge da rota.")
				return
			}
			out, err := h.credentials.Put(r.Context(), in)
			h.respond(w, out, err)
			return
		}
	}
	if r.Method == http.MethodPost && path == "/runs" {
		var in contract.RunRequest
		if !h.decode(w, r, &in, 16*1024) {
			return
		}
		if !h.ownsPlan(w, in.PlanID) {
			return
		}
		out, err := h.core.StartRun(r.Context(), in)
		if err != nil {
			h.fail(w, err)
			return
		}
		h.finalizePublishedRun(&out)
		h.events.publish("run.state", out.ID, out.SnapshotID, out.State, "Run iniciado no serviço local.")
		h.writeJSON(w, http.StatusAccepted, out)
		return
	}
	if len(parts) == 2 && parts[0] == "runs" && r.Method == http.MethodGet {
		if !h.ownsRun(w, r.Context(), parts[1]) {
			return
		}
		out, err := h.core.GetRun(r.Context(), parts[1])
		if err == nil {
			h.finalizePublishedRun(&out)
			h.events.publish("run.state", out.ID, out.SnapshotID, out.State, "Progresso do run atualizado.")
		}
		h.respond(w, out, err)
		return
	}
	if len(parts) == 3 && parts[0] == "runs" && r.Method == http.MethodPost {
		if !h.ownsRun(w, r.Context(), parts[1]) {
			return
		}
		var empty map[string]any
		if !h.decode(w, r, &empty, 1024) {
			return
		}
		var out contract.Run
		var err error
		switch parts[2] {
		case "pause":
			out, err = h.core.PauseRun(r.Context(), parts[1])
		case "resume":
			out, err = h.core.ResumeRun(r.Context(), parts[1])
		case "cancel":
			out, err = h.core.CancelRun(r.Context(), parts[1])
		default:
			h.writeError(w, 404, "ERR_NOT_FOUND", "Rota não encontrada.")
			return
		}
		h.respond(w, out, err)
		if err == nil {
			h.events.publish("run.state", out.ID, out.SnapshotID, out.State, "Estado do run alterado localmente.")
		}
		return
	}
	if len(parts) == 3 && parts[0] == "investigations" && parts[2] == "snapshots" && r.Method == http.MethodGet {
		if !h.options.Demo && parts[1] != h.options.InvestigationID {
			h.notInScope(w)
			return
		}
		out, err := h.core.ListSnapshots(r.Context(), parts[1])
		h.respond(w, out, err)
		return
	}
	if len(parts) == 2 && parts[0] == "snapshots" && r.Method == http.MethodGet {
		if !h.ownsSnapshot(w, r.Context(), parts[1]) {
			return
		}
		out, err := h.core.GetSnapshot(r.Context(), parts[1])
		h.respond(w, out, err)
		return
	}
	if len(parts) == 3 && parts[0] == "snapshots" && parts[2] == "insights" && r.Method == http.MethodGet {
		if h.insightReader == nil {
			h.unavailable(w, "análise executiva")
			return
		}
		if h.analysis == nil {
			h.unavailable(w, "controlador de análise")
			return
		}
		if !h.ownsSnapshot(w, r.Context(), parts[1]) {
			return
		}
		limit, ok := insightLimit(w, r.URL.Query().Get("limit"), 50)
		if !ok {
			return
		}
		page, err := h.analysis.ListAnalyses(r.Context(), parts[1], r.URL.Query().Get("cursor"), limit)
		if err != nil {
			h.fail(w, err)
			return
		}
		if page.SnapshotID != parts[1] {
			h.writeError(w, 502, "ERR_INSIGHT_SCOPE", "A lista de análises retornou outro snapshot.")
			return
		}
		if page.Total < 0 || int64(len(page.Items)) > page.Total || len(page.Items) > limit || (len(page.Items) == 0 && page.NextCursor != "") || (page.NextCursor != "" && page.NextCursor == r.URL.Query().Get("cursor")) {
			h.writeError(w, 502, "ERR_INSIGHT_PAGE", "A lista de análises excedeu os limites ou não avançou.")
			return
		}
		seenAnalyses := map[string]bool{}
		for _, item := range page.Items {
			if item.ID == "" || item.SnapshotID != parts[1] || seenAnalyses[item.ID] || !validInsightAnalysisStatus(item.Status) {
				h.writeError(w, 502, "ERR_INSIGHT_PAGE", "A lista contém identificadores, status ou escopos inválidos.")
				return
			}
			seenAnalyses[item.ID] = true
			h.authorizeInsight(parts[1], item.ID)
		}
		h.writeJSON(w, http.StatusOK, page)
		return
	}
	if len(parts) == 4 && parts[0] == "snapshots" && parts[2] == "insights" && r.Method == http.MethodGet {
		if h.insightReader == nil {
			h.unavailable(w, "análise executiva")
			return
		}
		if !h.ownsSnapshot(w, r.Context(), parts[1]) {
			return
		}
		if !h.requireAuthorizedInsight(w, parts[1], parts[3]) {
			return
		}
		out, err := h.insightReader.GetExecutiveReport(r.Context(), parts[1], parts[3])
		if err == nil && (out.SnapshotID != parts[1] || out.AnalysisID != parts[3] || !validExecutiveReport(out)) {
			h.writeError(w, 502, "ERR_INSIGHT_SCOPE", "O relatório retornou outro snapshot ou análise.")
			return
		}
		h.respond(w, out, err)
		return
	}
	if len(parts) == 5 && parts[0] == "snapshots" && parts[2] == "insights" && parts[4] == "cases" && r.Method == http.MethodGet {
		if h.insightReader == nil {
			h.unavailable(w, "análise executiva")
			return
		}
		if !h.ownsSnapshot(w, r.Context(), parts[1]) {
			return
		}
		if !h.requireAuthorizedInsight(w, parts[1], parts[3]) {
			return
		}
		limit, ok := insightLimit(w, r.URL.Query().Get("limit"), 50)
		if !ok {
			return
		}
		page, err := h.insightReader.ListInsightCases(r.Context(), parts[1], parts[3], r.URL.Query().Get("cursor"), limit)
		if err != nil {
			h.fail(w, err)
			return
		}
		if page.SnapshotID != parts[1] || page.AnalysisID != parts[3] {
			h.writeError(w, 502, "ERR_INSIGHT_SCOPE", "A página retornou outro snapshot ou análise.")
			return
		}
		if page.Total < 0 || int64(len(page.Cases)) > page.Total || len(page.Cases) > limit || (len(page.Cases) == 0 && page.NextCursor != "") || (page.NextCursor != "" && page.NextCursor == r.URL.Query().Get("cursor")) {
			h.writeError(w, 502, "ERR_INSIGHT_PAGE", "A página de casos excedeu os limites ou não avançou.")
			return
		}
		seenCases := map[string]bool{}
		lastCaseID := ""
		for _, c := range page.Cases {
			if c.ID == "" || seenCases[c.ID] || (lastCaseID != "" && c.ID < lastCaseID) {
				h.writeError(w, 502, "ERR_INSIGHT_PAGE", "A página contém IDs vazios, repetidos ou fora de ordem.")
				return
			}
			seenCases[c.ID] = true
			lastCaseID = c.ID
		}
		h.writeJSON(w, http.StatusOK, page)
		return
	}
	if len(parts) == 6 && parts[0] == "snapshots" && parts[2] == "insights" && parts[4] == "cases" && r.Method == http.MethodGet {
		if h.insightReader == nil {
			h.unavailable(w, "análise executiva")
			return
		}
		if !h.ownsSnapshot(w, r.Context(), parts[1]) {
			return
		}
		if !h.requireAuthorizedInsight(w, parts[1], parts[3]) {
			return
		}
		out, err := h.insightReader.GetInsightCase(r.Context(), parts[1], parts[3], parts[5])
		if err == nil && (out.SnapshotID != parts[1] || out.AnalysisID != parts[3] || out.Case.ID != parts[5]) {
			h.writeError(w, 502, "ERR_INSIGHT_SCOPE", "O detalhe retornou outro snapshot, análise ou caso.")
			return
		}
		h.respond(w, out, err)
		return
	}
	if path == "/insights/runs" && r.Method == http.MethodPost {
		if h.analysis == nil {
			h.unavailable(w, "controlador de análise")
			return
		}
		var in insightcontract.AnalysisRequest
		if !h.decode(w, r, &in, 512*1024) {
			return
		}
		if in.SnapshotID == "" || in.Config.Version != insightcontract.Version || strings.TrimSpace(in.IdempotencyKey) == "" {
			h.writeError(w, 400, "ERR_VALIDATION", "Snapshot, configuração v1 e chave de idempotência são obrigatórios.")
			return
		}
		if !h.ownsSnapshot(w, r.Context(), in.SnapshotID) {
			return
		}
		out, err := h.analysis.StartAnalysis(r.Context(), in)
		if err != nil {
			h.fail(w, err)
			return
		}
		if out.ID == "" || out.SnapshotID != in.SnapshotID || !validRunState(out.State) || !validRunPhase(out.Phase) {
			h.writeError(w, 502, "ERR_INSIGHT_SCOPE", "O run retornou outro snapshot.")
			return
		}
		if out.AnalysisID != nil {
			h.authorizeInsight(in.SnapshotID, *out.AnalysisID)
		}
		h.writeJSON(w, http.StatusAccepted, out)
		return
	}
	if len(parts) == 3 && parts[0] == "insights" && parts[1] == "runs" && r.Method == http.MethodGet {
		if h.analysis == nil {
			h.unavailable(w, "controlador de análise")
			return
		}
		out, err := h.analysis.GetAnalysisRun(r.Context(), parts[2])
		if err != nil {
			h.fail(w, err)
			return
		}
		if out.ID != parts[2] || out.SnapshotID == "" || !validRunState(out.State) || !validRunPhase(out.Phase) {
			h.writeError(w, 502, "ERR_INSIGHT_SCOPE", "O controlador retornou um run inválido.")
			return
		}
		if !h.ownsSnapshot(w, r.Context(), out.SnapshotID) {
			return
		}
		if out.AnalysisID != nil {
			h.authorizeInsight(out.SnapshotID, *out.AnalysisID)
		}
		h.writeJSON(w, http.StatusOK, out)
		return
	}
	if len(parts) == 4 && parts[0] == "insights" && parts[1] == "runs" && parts[3] == "cancel" && r.Method == http.MethodPost {
		if h.analysis == nil {
			h.unavailable(w, "controlador de análise")
			return
		}
		var empty map[string]any
		if !h.decode(w, r, &empty, 1024) {
			return
		}
		current, err := h.analysis.GetAnalysisRun(r.Context(), parts[2])
		if err != nil {
			h.fail(w, err)
			return
		}
		if current.ID != parts[2] || current.SnapshotID == "" || !validRunState(current.State) || !validRunPhase(current.Phase) {
			h.writeError(w, 502, "ERR_INSIGHT_SCOPE", "O controlador retornou um run inválido.")
			return
		}
		if !h.ownsSnapshot(w, r.Context(), current.SnapshotID) {
			return
		}
		out, err := h.analysis.CancelAnalysis(r.Context(), parts[2])
		if err == nil && (out.ID != parts[2] || out.SnapshotID != current.SnapshotID || !validRunState(out.State) || !validRunPhase(out.Phase)) {
			h.writeError(w, 502, "ERR_INSIGHT_SCOPE", "O cancelamento retornou outro snapshot.")
			return
		}
		if err == nil && out.AnalysisID != nil {
			h.authorizeInsight(out.SnapshotID, *out.AnalysisID)
		}
		h.respond(w, out, err)
		return
	}
	if path == "/insights/exports" && r.Method == http.MethodPost {
		if h.executiveExports == nil {
			h.unavailable(w, "exportação executiva")
			return
		}
		var in insightcontract.ExecutiveExportRequest
		if !h.decode(w, r, &in, 16*1024) {
			return
		}
		if !h.ownsSnapshot(w, r.Context(), in.SnapshotID) {
			return
		}
		if !h.requireAuthorizedInsight(w, in.SnapshotID, in.AnalysisID) {
			return
		}
		jobCtx, finish, err := h.beginReport()
		if err != nil {
			h.fail(w, err)
			return
		}
		defer finish()
		out, err := h.executiveExports.ExportExecutive(jobCtx, in)
		if err != nil {
			h.fail(w, err)
			return
		}
		if out.SnapshotID != in.SnapshotID || out.AnalysisID != in.AnalysisID || out.Mode != in.Mode || !validExecutiveArtifact(out) {
			h.writeError(w, 502, "ERR_INSIGHT_SCOPE", "O artefato retornou outro snapshot ou análise.")
			return
		}
		if !h.requireAuthorizedInsight(w, out.SnapshotID, out.AnalysisID) {
			return
		}
		h.writeJSON(w, http.StatusAccepted, out)
		return
	}
	if len(parts) == 3 && parts[0] == "insights" && parts[1] == "exports" && r.Method == http.MethodGet {
		if h.executiveExports == nil {
			h.unavailable(w, "exportação executiva")
			return
		}
		out, err := h.executiveExports.GetExecutiveArtifact(r.Context(), parts[2])
		if err != nil {
			h.fail(w, err)
			return
		}
		if out.ID != parts[2] || !validExecutiveArtifact(out) {
			h.writeError(w, 502, "ERR_INSIGHT_ARTIFACT", "O artefato retornou uma identidade ou metadados inválidos.")
			return
		}
		if !h.ownsSnapshot(w, r.Context(), out.SnapshotID) {
			return
		}
		if !h.requireAuthorizedInsight(w, out.SnapshotID, out.AnalysisID) {
			return
		}
		h.writeJSON(w, http.StatusOK, out)
		return
	}
	if len(parts) == 4 && parts[0] == "insights" && parts[1] == "exports" && parts[3] == "download" && r.Method == http.MethodGet {
		if h.executiveExports == nil {
			h.unavailable(w, "exportação executiva")
			return
		}
		artifact, err := h.executiveExports.GetExecutiveArtifact(r.Context(), parts[2])
		if err != nil {
			h.fail(w, err)
			return
		}
		if artifact.ID != parts[2] || !validExecutiveArtifact(artifact) {
			h.writeError(w, 502, "ERR_INSIGHT_ARTIFACT", "O artefato retornou uma identidade ou metadados inválidos.")
			return
		}
		if !h.ownsSnapshot(w, r.Context(), artifact.SnapshotID) {
			return
		}
		if !h.requireAuthorizedInsight(w, artifact.SnapshotID, artifact.AnalysisID) {
			return
		}
		opener, ok := h.executiveExports.(interface {
			OpenExecutiveArtifact(string) (*os.File, error)
		})
		if !ok {
			h.unavailable(w, "download de exportação executiva")
			return
		}
		file, err := opener.OpenExecutiveArtifact(parts[2])
		if err != nil {
			h.fail(w, err)
			return
		}
		defer file.Close()
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", `attachment; filename="executive-report.pdf"`)
		_, _ = io.Copy(w, file)
		return
	}
	if len(parts) == 3 && parts[0] == "snapshots" && parts[2] == "queries" && r.Method == http.MethodGet {
		if !h.ownsSnapshot(w, r.Context(), parts[1]) {
			return
		}
		out, err := h.core.SuggestedQueries(r.Context(), parts[1])
		h.respond(w, out, err)
		return
	}
	if len(parts) == 3 && parts[0] == "snapshots" && parts[2] == "findings" && r.Method == http.MethodGet {
		if !h.ownsSnapshot(w, r.Context(), parts[1]) {
			return
		}
		if l, ok := h.core.(findingLister); ok {
			out, err := l.Findings(parts[1])
			h.respond(w, out, err)
			return
		}
		h.unavailable(w, "finding list")
		return
	}
	if len(parts) == 3 && parts[0] == "snapshots" && parts[2] == "query" && r.Method == http.MethodPost {
		var in contract.QueryRequest
		if !h.decode(w, r, &in, 64*1024) {
			return
		}
		if in.SnapshotID != parts[1] {
			h.writeError(w, 400, "ERR_VALIDATION", "Snapshot do caminho e do corpo divergem.")
			return
		}
		if !h.ownsSnapshot(w, r.Context(), in.SnapshotID) {
			return
		}
		if len(in.SQL) > 16*1024 || len(in.Parameters) > 50 || in.MaxRows > 1000 || in.TimeoutMS < 0 || in.TimeoutMS > 30000 {
			h.writeError(w, 400, "ERR_VALIDATION", "Consulta excede os limites locais.")
			return
		}
		out, queryErr := h.core.Query(r.Context(), in)
		if h.queryStore == nil {
			h.unavailable(w, "query history")
			return
		}
		execution, err := h.queryStore.Record(in, out, queryErr)
		if err != nil {
			h.fail(w, err)
			return
		}
		if queryErr == nil {
			out.QueryID = execution.QueryID
		}
		h.events.publish("query.completed", "", in.SnapshotID, map[bool]string{true: "error", false: "complete"}[queryErr != nil], "Execução SQL local registrada.")
		h.respond(w, out, queryErr)
		return
	}
	if path == "/queries/history" && r.Method == http.MethodGet {
		if h.queryStore == nil {
			h.unavailable(w, "query history")
			return
		}
		if !h.ownsSnapshot(w, r.Context(), r.URL.Query().Get("snapshot_id")) {
			return
		}
		limit := 100
		if n, e := strconv.Atoi(r.URL.Query().Get("limit")); e == nil && n > 0 {
			limit = n
		}
		h.writeJSON(w, 200, h.queryStore.History(r.URL.Query().Get("snapshot_id"), r.URL.Query().Get("cursor"), limit))
		return
	}
	if path == "/queries/saved" && r.Method == http.MethodGet {
		if h.queryStore == nil {
			h.unavailable(w, "saved queries")
			return
		}
		if !h.ownsSnapshot(w, r.Context(), r.URL.Query().Get("snapshot_id")) {
			return
		}
		h.writeJSON(w, 200, h.queryStore.List(r.URL.Query().Get("snapshot_id")))
		return
	}
	if path == "/queries/saved" && r.Method == http.MethodPost {
		if h.queryStore == nil {
			h.unavailable(w, "saved queries")
			return
		}
		var in queries.CreateInput
		if !h.decode(w, r, &in, 16*1024) {
			return
		}
		if !h.ownsSnapshot(w, r.Context(), r.URL.Query().Get("snapshot_id")) {
			return
		}
		snapshot, err := h.core.GetSnapshot(r.Context(), r.URL.Query().Get("snapshot_id"))
		if err != nil {
			h.fail(w, err)
			return
		}
		out, err := h.queryStore.Create(in, snapshot.InvestigationID, snapshot.ID)
		h.respond(w, out, err)
		return
	}
	if len(parts) == 3 && parts[0] == "queries" && parts[1] == "saved" && r.Method == http.MethodPatch {
		if h.queryStore == nil {
			h.unavailable(w, "saved queries")
			return
		}
		if !h.options.Demo {
			q, ok := h.queryStore.GetSaved(parts[2])
			if !ok {
				h.notInScope(w)
				return
			}
			if !h.ownsSnapshot(w, r.Context(), q.SnapshotID) {
				return
			}
		}
		var in queries.PatchInput
		if !h.decode(w, r, &in, 16*1024) {
			return
		}
		out, err := h.queryStore.Patch(parts[2], in)
		h.respond(w, out, err)
		return
	}
	if len(parts) == 4 && parts[0] == "snapshots" && parts[2] == "findings" && r.Method == http.MethodGet {
		if !h.ownsSnapshot(w, r.Context(), parts[1]) {
			return
		}
		out, err := h.core.GetFinding(r.Context(), parts[1], parts[3])
		h.respond(w, out, err)
		return
	}
	if len(parts) == 4 && parts[0] == "snapshots" && parts[2] == "evidence" && r.Method == http.MethodGet {
		if !h.ownsSnapshot(w, r.Context(), parts[1]) {
			return
		}
		out, err := h.core.GetEvidence(r.Context(), parts[1], parts[3])
		h.respond(w, out, err)
		return
	}
	if len(parts) == 3 && parts[0] == "snapshots" && parts[2] == "report" && r.Method == http.MethodGet {
		if !h.ownsSnapshot(w, r.Context(), parts[1]) {
			return
		}
		limit := 200
		if n, e := strconv.Atoi(r.URL.Query().Get("limit")); e == nil && n > 0 {
			limit = n
		}
		out, err := h.core.ReadReportPage(r.Context(), parts[1], r.URL.Query().Get("cursor"), limit)
		h.respond(w, out, err)
		return
	}
	if path == "/events" && r.Method == http.MethodGet {
		h.serveEvents(w, r, sid)
		return
	}
	if path == "/reports" && r.Method == http.MethodPost {
		if h.reports == nil {
			h.unavailable(w, "PDF")
			return
		}
		var in contract.FinalizeRequest
		if !h.decode(w, r, &in, 16*1024) {
			return
		}
		if !h.ownsSnapshot(w, r.Context(), in.SnapshotID) {
			return
		}
		jobCtx, finish, err := h.beginReport()
		if err != nil {
			h.fail(w, err)
			return
		}
		defer finish()
		out, err := h.reports.Finalize(jobCtx, in)
		if err != nil {
			h.fail(w, err)
			return
		}
		if reconciler, ok := h.core.(interface {
			CompleteReport(context.Context, contract.ReportArtifact) error
		}); ok {
			if err := reconciler.CompleteReport(jobCtx, out); err != nil {
				h.fail(w, err)
				return
			}
		}
		h.writeJSON(w, http.StatusAccepted, out)
		h.events.publish("report.state", "", in.SnapshotID, out.Status, "Nova revisão de relatório local gerada.")
		return
	}
	if path == "/reports" && r.Method == http.MethodGet {
		if h.reports == nil {
			h.unavailable(w, "PDF")
			return
		}
		if !h.ownsSnapshot(w, r.Context(), r.URL.Query().Get("snapshot_id")) {
			return
		}
		h.writeJSON(w, 200, h.reports.List(r.URL.Query().Get("snapshot_id")))
		return
	}
	if len(parts) == 2 && parts[0] == "reports" && r.Method == http.MethodGet {
		if h.reports == nil {
			h.unavailable(w, "PDF")
			return
		}
		out, err := h.reports.Get(parts[1])
		if err == nil && !h.ownsSnapshot(w, r.Context(), out.SnapshotID) {
			return
		}
		h.respond(w, out, err)
		return
	}
	if len(parts) == 3 && parts[0] == "reports" && parts[2] == "download" && r.Method == http.MethodGet {
		if h.reports == nil {
			h.unavailable(w, "PDF")
			return
		}
		artifact, err := h.reports.Get(parts[1])
		if err != nil {
			h.fail(w, err)
			return
		}
		if !h.ownsSnapshot(w, r.Context(), artifact.SnapshotID) {
			return
		}
		file, err := h.reports.Open(parts[1])
		if err != nil {
			h.fail(w, err)
			return
		}
		defer file.Close()
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", `attachment; filename="investigation-report.pdf"`)
		_, _ = io.Copy(w, file)
		return
	}
	if len(parts) == 2 && parts[0] == "editorial" && parts[1] == "models" && r.Method == http.MethodGet {
		if h.editorial == nil {
			h.llmDisabled(w)
			return
		}
		out, err := h.editorial.Models(r.Context())
		h.respond(w, out, err)
		return
	}
	if len(parts) == 2 && parts[0] == "editorial" && parts[1] == "drafts" && r.Method == http.MethodGet {
		if h.editorial == nil {
			h.llmDisabled(w)
			return
		}
		if !h.ownsSnapshot(w, r.Context(), r.URL.Query().Get("snapshot_id")) {
			return
		}
		h.writeJSON(w, 200, h.editorial.Drafts(r.URL.Query().Get("snapshot_id")))
		return
	}
	if path == "/editorial/preview" && r.Method == http.MethodPost {
		if h.editorial == nil {
			h.llmDisabled(w)
			return
		}
		var in editorial.Selection
		if !h.decode(w, r, &in, 64*1024) {
			return
		}
		if !h.ownsSnapshot(w, r.Context(), in.SnapshotID) {
			return
		}
		out, err := h.editorial.Preview(r.Context(), in)
		h.respond(w, out, err)
		if err == nil {
			h.events.publish("editorial.state", "", in.SnapshotID, "preview", "Preview editorial local pronto.")
		}
		return
	}
	if path == "/editorial/generate" && r.Method == http.MethodPost {
		if h.editorial == nil {
			h.llmDisabled(w)
			return
		}
		var in editorial.GenerateInput
		if !h.decode(w, r, &in, 16*1024) {
			return
		}
		if !h.options.Demo {
			preview, ok := h.editorial.PreviewInfo(in.PreviewID)
			if !ok {
				h.notInScope(w)
				return
			}
			if !h.ownsSnapshot(w, r.Context(), preview.SnapshotID) {
				return
			}
		}
		out, err := h.editorial.Generate(r.Context(), in)
		h.respond(w, out, err)
		if err == nil {
			h.events.publish("editorial.state", "", out.SnapshotID, out.Status, "Rascunho editorial criado para revisão.")
		}
		return
	}
	if len(parts) == 3 && parts[0] == "editorial" && (parts[2] == "accept" || parts[2] == "reject") && r.Method == http.MethodPost {
		if h.editorial == nil {
			h.llmDisabled(w)
			return
		}
		if !h.options.Demo {
			draft, ok := h.editorial.Draft(parts[1])
			if !ok {
				h.notInScope(w)
				return
			}
			if !h.ownsSnapshot(w, r.Context(), draft.SnapshotID) {
				return
			}
		}
		var in struct {
			ExpectedRevision int `json:"expected_revision"`
		}
		if !h.decode(w, r, &in, 1024) {
			return
		}
		out, err := h.editorial.Review(parts[1], in.ExpectedRevision, parts[2] == "accept")
		h.respond(w, out, err)
		if err == nil {
			h.events.publish("editorial.state", "", out.SnapshotID, out.Status, "Rascunho editorial revisado.")
		}
		return
	}
	if path == "/annotations" && r.Method == http.MethodGet {
		if h.annotations == nil {
			h.unavailable(w, "annotations")
			return
		}
		if !h.ownsSnapshot(w, r.Context(), r.URL.Query().Get("snapshot_id")) {
			return
		}
		limit := 100
		if n, e := strconv.Atoi(r.URL.Query().Get("limit")); e == nil && n > 0 {
			limit = n
		}
		h.writeJSON(w, 200, h.annotations.List(r.URL.Query().Get("snapshot_id"), r.URL.Query().Get("cursor"), limit))
		return
	}
	if path == "/annotations" && r.Method == http.MethodPost {
		if h.annotations == nil {
			h.unavailable(w, "annotations")
			return
		}
		var in annotations.CreateInput
		if !h.decode(w, r, &in, 16*1024) {
			return
		}
		if !h.options.Demo && in.InvestigationID != h.options.InvestigationID {
			h.notInScope(w)
			return
		}
		if !h.ownsSnapshot(w, r.Context(), in.SnapshotID) {
			return
		}
		if _, err := h.core.GetSnapshot(r.Context(), in.SnapshotID); err != nil {
			h.fail(w, err)
			return
		}
		if err := h.validateAnnotationTarget(r.Context(), in); err != nil {
			h.fail(w, err)
			return
		}
		out, err := h.annotations.Create(in)
		h.respond(w, out, err)
		if err == nil {
			h.events.publish("annotation.changed", "", in.SnapshotID, "created", "Comentário local criado.")
		}
		return
	}
	if len(parts) == 2 && parts[0] == "annotations" && r.Method == http.MethodPatch {
		if h.annotations == nil {
			h.unavailable(w, "annotations")
			return
		}
		if !h.options.Demo {
			a, ok := h.annotations.Get(parts[1])
			if !ok {
				h.notInScope(w)
				return
			}
			if !h.ownsSnapshot(w, r.Context(), a.SnapshotID) {
				return
			}
		}
		var in annotations.UpdateInput
		if !h.decode(w, r, &in, 16*1024) {
			return
		}
		out, err := h.annotations.Update(parts[1], in)
		h.respond(w, out, err)
		if err == nil {
			h.events.publish("annotation.changed", "", out.SnapshotID, "updated", "Comentário local atualizado.")
		}
		return
	}
	if len(parts) == 3 && parts[0] == "annotations" && parts[2] == "resolve" && r.Method == http.MethodPost {
		if h.annotations == nil {
			h.unavailable(w, "annotations")
			return
		}
		if !h.options.Demo {
			a, ok := h.annotations.Get(parts[1])
			if !ok {
				h.notInScope(w)
				return
			}
			if !h.ownsSnapshot(w, r.Context(), a.SnapshotID) {
				return
			}
		}
		var in struct {
			ExpectedRevision int `json:"expected_revision"`
		}
		if !h.decode(w, r, &in, 1024) {
			return
		}
		out, err := h.annotations.Resolve(parts[1], in.ExpectedRevision)
		h.respond(w, out, err)
		if err == nil {
			h.events.publish("annotation.changed", "", out.SnapshotID, "resolved", "Comentário local resolvido.")
		}
		return
	}
	h.writeError(w, http.StatusNotFound, "ERR_NOT_FOUND", "Rota não encontrada.")
}

func (h *Handler) beginReport() (context.Context, func(), error) {
	return h.options.ReportJobs.Begin()
}

func (h *Handler) finalizePublishedRun(run *contract.Run) {
	if run.State != "completed" || run.SnapshotID == "" {
		return
	}
	h.mu.Lock()
	already := h.finalizedRuns[run.ID]
	if !already {
		h.finalizedRuns[run.ID] = true
	}
	h.mu.Unlock()
	if already {
		return
	}
	var err error
	if h.reports == nil {
		err = errors.New("ERR_PDF_DEPENDENCY: renderer local indisponível")
	} else {
		jobCtx, finish, beginErr := h.beginReport()
		if beginErr != nil {
			err = beginErr
		} else {
			defer finish()
			_, err = h.reports.Finalize(jobCtx, contract.FinalizeRequest{SnapshotID: run.SnapshotID})
		}
	}
	if err != nil {
		run.State = "report_failed"
		run.LastError = &contract.ErrorResponse{OK: false, Code: "ERR_PDF_FAILED", Message: "Snapshot publicado; geração automática do PDF falhou. O relatório pode ser tentado novamente.", Fields: []contract.FieldError{}}
		if setter, ok := h.core.(runStateSetter); ok {
			setter.SetRunState(run.ID, run.State, run.LastError)
		}
	}
}

func (h *Handler) validateAnnotationTarget(ctx context.Context, in annotations.CreateInput) error {
	snapshot, err := h.core.GetSnapshot(ctx, in.SnapshotID)
	if err != nil {
		return err
	}
	if snapshot.InvestigationID != in.InvestigationID {
		return &contract.ErrorResponse{Code: "ERR_VALIDATION", Message: "Investigação e snapshot não correspondem.", Fields: []contract.FieldError{}}
	}
	switch in.TargetType {
	case annotations.Snapshot:
		if in.TargetID != in.SnapshotID {
			return &contract.ErrorResponse{Code: "ERR_NOT_FOUND", Message: "Snapshot alvo não corresponde ao contexto.", Fields: []contract.FieldError{}}
		}
	case annotations.Finding:
		if _, err := h.core.GetFinding(ctx, in.SnapshotID, in.TargetID); err != nil {
			return err
		}
	case annotations.Component:
		cursor := ""
		for {
			page, e := h.core.ReadReportPage(ctx, in.SnapshotID, cursor, 200)
			if e != nil {
				return e
			}
			for _, r := range page.Records {
				if r.Kind == "component" && r.Component != nil && r.Component.ID == in.TargetID {
					return nil
				}
			}
			if page.NextCursor == "" {
				break
			}
			cursor = page.NextCursor
		}
		return &contract.ErrorResponse{Code: "ERR_NOT_FOUND", Message: "Componente não encontrado no snapshot.", Fields: []contract.FieldError{}}
	case annotations.SavedQuery:
		found := false
		if h.queryStore != nil {
			for _, q := range h.queryStore.List(in.SnapshotID) {
				if q.ID == in.TargetID {
					found = true
				}
			}
		}
		if !found {
			return &contract.ErrorResponse{Code: "ERR_NOT_FOUND", Message: "Consulta salva não encontrada no snapshot.", Fields: []contract.FieldError{}}
		}
	case annotations.Report:
		if h.reports == nil {
			return &contract.ErrorResponse{Code: "ERR_NOT_FOUND", Message: "Relatório não encontrado.", Fields: []contract.FieldError{}}
		}
		if _, err := h.reports.Get(in.TargetID); err != nil {
			return err
		}
	default:
		return &contract.ErrorResponse{Code: "ERR_VALIDATION", Message: "Tipo de alvo inválido.", Fields: []contract.FieldError{}}
	}
	return nil
}

// llmDisabled answers editorial routes when no editorial service is composed.
// Analysis, SQL, comments and PDF stay available (RFC C.1 R12).
func (h *Handler) llmDisabled(w http.ResponseWriter) {
	h.writeError(w, http.StatusServiceUnavailable, "ERR_LLM_DISABLED", "Claude editorial desativado nesta sessão; análise, SQL, comentários e PDF continuam disponíveis.")
}
func (h *Handler) unavailable(w http.ResponseWriter, name string) {
	h.writeError(w, 501, "ERR_UNAVAILABLE", name+" indisponível nesta sessão.")
}
func (h *Handler) respond(w http.ResponseWriter, v any, err error) {
	if err != nil {
		h.fail(w, err)
		return
	}
	h.writeJSON(w, 200, v)
}
func (h *Handler) fail(w http.ResponseWriter, err error) {
	var e *contract.ErrorResponse
	if errors.As(err, &e) {
		status := http.StatusUnprocessableEntity
		switch e.Code {
		case "ERR_NOT_FOUND":
			status = http.StatusNotFound
		case "ERR_SOURCE_FORBIDDEN":
			status = http.StatusForbidden
		case "ERR_REVISION_CONFLICT", "ERR_PLAN_CHANGED", "ERR_CONFLICT":
			status = http.StatusConflict
		case "ERR_RATE_LIMIT":
			status = http.StatusTooManyRequests
		case "ERR_VALIDATION":
			status = http.StatusBadRequest
		case "ERR_CREDENTIAL_REQUIRED":
			status = http.StatusPreconditionRequired
		}
		h.writeJSON(w, status, e)
		return
	}
	message := err.Error()
	if i := strings.Index(message, ": "); i > 0 && strings.HasPrefix(message, "ERR_") {
		code := message[:i]
		status := http.StatusUnprocessableEntity
		if code == "ERR_NOT_FOUND" {
			status = http.StatusNotFound
		} else if code == "ERR_REVISION_CONFLICT" || code == "ERR_CONFLICT" {
			status = http.StatusConflict
		} else if code == "ERR_VALIDATION" {
			status = http.StatusBadRequest
		}
		h.writeError(w, status, code, message[i+2:])
		return
	}
	if errors.Is(err, os.ErrNotExist) {
		h.writeError(w, http.StatusNotFound, "ERR_NOT_FOUND", "Registro local não encontrado.")
		return
	}
	h.writeError(w, 500, "ERR_INTERNAL", "A operação local falhou.")
}
func (h *Handler) writeError(w http.ResponseWriter, status int, code, message string) {
	h.writeJSON(w, status, contract.ErrorResponse{OK: false, Code: code, Message: message, Fields: []contract.FieldError{}})
}
func (h *Handler) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// Listen starts an HTTP server with timeouts appropriate for a local interface.
func Listen(ctx context.Context, h http.Handler, options PreviewOptions) (net.Listener, *http.Server, error) {
	host := options.BindHost
	if host == "" {
		host = "127.0.0.1"
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(options.Port)))
	if err != nil {
		return nil, nil, err
	}
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	go func() { <-ctx.Done(); _ = srv.Close() }()
	go func() { _ = srv.Serve(listener) }()
	return listener, srv, nil
}
func CodeMessage(address, code string) string {
	return fmt.Sprintf("Abra %s e digite o código de desbloqueio: %s", address, code)
}

// contractLabel bounds the echoed header so an unexpected client value cannot
// inflate or inject content into the sanitized error message.
func contractLabel(value string) string {
	const max = 40
	clean := strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, value)
	if len(clean) > max {
		clean = clean[:max] + "…"
	}
	return strconv.Quote(clean)
}
