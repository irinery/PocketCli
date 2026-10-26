package web

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
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
	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/editorial"
	"pocketcli/internal/investigation/queries"
)

type PreviewOptions struct {
	Demo         bool
	BindHost     string
	Port         int
	Scenario     string
	DataDir      string
	QueryHistory *queries.Store
}
type EditorialService interface {
	Models(context.Context) ([]editorial.Model, error)
	Preview(context.Context, editorial.Selection) (editorial.Preview, error)
	Generate(context.Context, editorial.GenerateInput) (editorial.Draft, error)
	Review(id string, expected int, accept bool) (editorial.Draft, error)
	Drafts(snapshot string) []editorial.Draft
}
type AnnotationStore interface {
	Create(annotations.CreateInput) (annotations.Annotation, error)
	Update(string, annotations.UpdateInput) (annotations.Annotation, error)
	Resolve(string, int) (annotations.Annotation, error)
	List(snapshot, cursor string, limit int) annotations.Page
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

type session struct {
	csrf    string
	expires time.Time
}
type Handler struct {
	core             contract.Core
	setup            contract.SetupService
	credentials      contract.CredentialStore
	annotations      AnnotationStore
	editorial        EditorialService
	reports          ReportService
	options          PreviewOptions
	bootstrap        string
	mu               sync.Mutex
	sessions         map[string]session
	unlockAttempts   int
	bootstrapExpires time.Time
	finalizedRuns    map[string]bool
	queryStore       *queries.Store
	events           *eventHub
}

// NewHandler composes the local application. BindHost may be loopback only.
// Credential, editorial, annotation, and report adapters are injected by the
// final integration; nil adapters remain explicitly unavailable.
func NewHandler(core contract.Core, setup contract.SetupService, credentials contract.CredentialStore, editorialService EditorialService, annotationStore AnnotationStore, reports ReportService, options PreviewOptions) (http.Handler, string, error) {
	if core == nil {
		return nil, "", errors.New("investigation core is required")
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
	var raw [24]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return nil, "", err
	}
	code := base64.RawURLEncoding.EncodeToString(raw[:])
	h := &Handler{core: core, setup: setup, credentials: credentials, annotations: annotationStore, editorial: editorialService, reports: reports, options: options, bootstrap: code, sessions: map[string]session{}, finalizedRuns: map[string]bool{}, events: newEventHub(), bootstrapExpires: time.Now().Add(5 * time.Minute)}
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
		_, _ = io.WriteString(w, appJS)
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
func (h *Handler) routeAPI(w http.ResponseWriter, r *http.Request, path string, sid string) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if r.Method == http.MethodGet && path == "/capabilities" {
		h.writeJSON(w, 200, map[string]any{"contract": contract.Version, "demo": h.options.Demo, "modules": []string{"github_issues", "code_history"}, "pdf_available": h.reports != nil, "claude_available": h.editorial != nil})
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
		out, err := h.core.PreparePlan(r.Context(), in)
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
		out, err := h.core.StartRun(r.Context(), in)
		if err != nil {
			h.fail(w, err)
			return
		}
		h.finalizePublishedRun(r.Context(), &out)
		h.events.publish("run.state", out.ID, out.SnapshotID, out.State, "Run iniciado no serviço local.")
		h.writeJSON(w, http.StatusAccepted, out)
		return
	}
	if len(parts) == 2 && parts[0] == "runs" && r.Method == http.MethodGet {
		out, err := h.core.GetRun(r.Context(), parts[1])
		if err == nil {
			h.finalizePublishedRun(r.Context(), &out)
			h.events.publish("run.state", out.ID, out.SnapshotID, out.State, "Progresso do run atualizado.")
		}
		h.respond(w, out, err)
		return
	}
	if len(parts) == 3 && parts[0] == "runs" && r.Method == http.MethodPost {
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
		out, err := h.core.ListSnapshots(r.Context(), parts[1])
		h.respond(w, out, err)
		return
	}
	if len(parts) == 2 && parts[0] == "snapshots" && r.Method == http.MethodGet {
		out, err := h.core.GetSnapshot(r.Context(), parts[1])
		h.respond(w, out, err)
		return
	}
	if len(parts) == 3 && parts[0] == "snapshots" && parts[2] == "queries" && r.Method == http.MethodGet {
		out, err := h.core.SuggestedQueries(r.Context(), parts[1])
		h.respond(w, out, err)
		return
	}
	if len(parts) == 3 && parts[0] == "snapshots" && parts[2] == "findings" && r.Method == http.MethodGet {
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
		if len(in.SQL) > 16*1024 || len(in.Parameters) > 50 || in.MaxRows > 1000 || in.TimeoutMS < 0 || in.TimeoutMS > 30000 {
			h.writeError(w, 400, "ERR_VALIDATION", "Consulta excede os limites locais.")
			return
		}
		out, queryErr := h.core.Query(r.Context(), in)
		if h.queryStore == nil {
			h.unavailable(w, "query history")
			return
		}
		if err := h.queryStore.Record(in, out, queryErr); err != nil {
			h.fail(w, err)
			return
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
		snapshot, err := h.core.GetSnapshot(r.Context(), r.URL.Query().Get("snapshot_id"))
		if err != nil {
			h.fail(w, err)
			return
		}
		out, err := h.queryStore.Create(in, snapshot.InvestigationID)
		h.respond(w, out, err)
		return
	}
	if len(parts) == 3 && parts[0] == "queries" && parts[1] == "saved" && r.Method == http.MethodPatch {
		if h.queryStore == nil {
			h.unavailable(w, "saved queries")
			return
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
		out, err := h.core.GetFinding(r.Context(), parts[1], parts[3])
		h.respond(w, out, err)
		return
	}
	if len(parts) == 4 && parts[0] == "snapshots" && parts[2] == "evidence" && r.Method == http.MethodGet {
		out, err := h.core.GetEvidence(r.Context(), parts[1], parts[3])
		h.respond(w, out, err)
		return
	}
	if len(parts) == 3 && parts[0] == "snapshots" && parts[2] == "report" && r.Method == http.MethodGet {
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
			h.unavailable(w, "PDF (Chromium/pdfinfo)")
			return
		}
		var in contract.FinalizeRequest
		if !h.decode(w, r, &in, 16*1024) {
			return
		}
		out, err := h.reports.Finalize(r.Context(), in)
		if err != nil {
			h.fail(w, err)
			return
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
		h.writeJSON(w, 200, h.reports.List(r.URL.Query().Get("snapshot_id")))
		return
	}
	if len(parts) == 2 && parts[0] == "reports" && r.Method == http.MethodGet {
		if h.reports == nil {
			h.unavailable(w, "PDF")
			return
		}
		out, err := h.reports.Get(parts[1])
		h.respond(w, out, err)
		return
	}
	if len(parts) == 3 && parts[0] == "reports" && parts[2] == "download" && r.Method == http.MethodGet {
		if h.reports == nil {
			h.unavailable(w, "PDF")
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
			h.unavailable(w, "Claude editorial")
			return
		}
		out, err := h.editorial.Models(r.Context())
		h.respond(w, out, err)
		return
	}
	if len(parts) == 2 && parts[0] == "editorial" && parts[1] == "drafts" && r.Method == http.MethodGet {
		if h.editorial == nil {
			h.unavailable(w, "Claude editorial")
			return
		}
		h.writeJSON(w, 200, h.editorial.Drafts(r.URL.Query().Get("snapshot_id")))
		return
	}
	if path == "/editorial/preview" && r.Method == http.MethodPost {
		if h.editorial == nil {
			h.unavailable(w, "Claude editorial")
			return
		}
		var in editorial.Selection
		if !h.decode(w, r, &in, 64*1024) {
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
			h.unavailable(w, "Claude editorial")
			return
		}
		var in editorial.GenerateInput
		if !h.decode(w, r, &in, 16*1024) {
			return
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
			h.unavailable(w, "Claude editorial")
			return
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

func (h *Handler) finalizePublishedRun(ctx context.Context, run *contract.Run) {
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
		_, err = h.reports.Finalize(ctx, contract.FinalizeRequest{SnapshotID: run.SnapshotID})
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
func (h *Handler) unavailable(w http.ResponseWriter, name string) {
	h.writeError(w, 501, "ERR_UNAVAILABLE", name+" indisponível no modo preview.")
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
