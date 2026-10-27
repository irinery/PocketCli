package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/web/mock"
)

type unlockedClient struct {
	t       *testing.T
	handler http.Handler
	cookie  *http.Cookie
	csrf    string
}

func newUnlockedClient(t *testing.T, core contract.Core, reports ReportService, options PreviewOptions) *unlockedClient {
	t.Helper()
	options.BindHost = "127.0.0.1"
	handler, code, err := NewHandler(core, nil, nil, nil, nil, reports, options)
	if err != nil {
		t.Fatal(err)
	}
	unlock := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/v1/session/unlock", strings.NewReader(`{"code":"`+code+`"}`))
	unlock.Header.Set("Origin", "http://127.0.0.1")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, unlock)
	if response.Code != http.StatusOK {
		t.Fatalf("unlock: %d %s", response.Code, response.Body.String())
	}
	client := &unlockedClient{t: t, handler: handler, cookie: response.Result().Cookies()[0]}
	var session struct {
		CSRF string `json:"csrf_token"`
	}
	if err := json.Unmarshal(client.do(http.MethodGet, "/session", "", "").Body.Bytes(), &session); err != nil || session.CSRF == "" {
		t.Fatalf("session token: %v", err)
	}
	client.csrf = session.CSRF
	return client
}

func (c *unlockedClient) do(method, path, body, declaredContract string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "http://127.0.0.1/api/v1"+path, strings.NewReader(body))
	req.AddCookie(c.cookie)
	if declaredContract != "" {
		req.Header.Set(ContractHeader, declaredContract)
	}
	if method != http.MethodGet {
		req.Header.Set("Origin", "http://127.0.0.1")
		req.Header.Set("X-CSRF-Token", c.csrf)
	}
	result := httptest.NewRecorder()
	c.handler.ServeHTTP(result, req)
	return result
}

// RFC 01 S01-05 / RFC 02 L01-05: uma interface com versão de contrato
// diferente é recusada antes de qualquer DTO ser interpretado.
func TestContractMismatchIsRejectedBeforeAnyHandler(t *testing.T) {
	core := &scopeCore{Core: mock.New()}
	client := newUnlockedClient(t, core, nil, PreviewOptions{InvestigationID: "investigation-demo"})

	for _, request := range []struct{ method, path, body string }{
		{http.MethodGet, "/snapshots/snap-demo-001", ""},
		{http.MethodPost, "/plans", `{}`},
		{http.MethodPost, "/snapshots/snap-demo-001/query", `{"snapshot_id":"snap-demo-001","sql":"SELECT 1"}`},
	} {
		response := client.do(request.method, request.path, request.body, "investigate.v0")
		var body contract.ErrorResponse
		if response.Code != http.StatusConflict || json.Unmarshal(response.Body.Bytes(), &body) != nil || body.Code != "ERR_CONTRACT_MISMATCH" {
			t.Fatalf("%s %s: esperado 409 ERR_CONTRACT_MISMATCH, veio %d %s", request.method, request.path, response.Code, response.Body.String())
		}
	}
	if core.plans != 0 || core.queries != 0 {
		t.Fatalf("contrato divergente alcançou o Core: plans=%d queries=%d", core.plans, core.queries)
	}
	if ok := client.do(http.MethodGet, "/snapshots/snap-demo-001", "", contract.Version); ok.Code != http.StatusOK {
		t.Fatalf("contrato atual recusado: %d %s", ok.Code, ok.Body.String())
	}
	if ok := client.do(http.MethodGet, "/snapshots/snap-demo-001", "", ""); ok.Code != http.StatusOK {
		t.Fatalf("cliente sem cabeçalho deveria continuar aceito: %d", ok.Code)
	}

	// Mesmo o desbloqueio, que não exige sessão, recusa versão divergente.
	handler, code, err := NewHandler(mock.New(), nil, nil, nil, nil, nil, PreviewOptions{InvestigationID: "investigation-demo", BindHost: "127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	unlock := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/v1/session/unlock", strings.NewReader(`{"code":"`+code+`"}`))
	unlock.Header.Set("Origin", "http://127.0.0.1")
	unlock.Header.Set(ContractHeader, "investigate.v2")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, unlock)
	if response.Code != http.StatusConflict || len(response.Result().Cookies()) != 0 {
		t.Fatalf("desbloqueio com contrato divergente: %d cookies=%d", response.Code, len(response.Result().Cookies()))
	}
}

func TestContractMismatchMessageIsBoundedAndSanitized(t *testing.T) {
	client := newUnlockedClient(t, mock.New(), nil, PreviewOptions{InvestigationID: "investigation-demo"})
	hostile := "investigate.v9" + strings.Repeat("A", 500) + "\x01<script>"
	response := client.do(http.MethodGet, "/capabilities", "", hostile)
	var body contract.ErrorResponse
	if response.Code != http.StatusConflict || json.Unmarshal(response.Body.Bytes(), &body) != nil {
		t.Fatalf("resposta inesperada: %d %s", response.Code, response.Body.String())
	}
	if len(body.Message) > 300 || strings.Contains(body.Message, "<script>") {
		t.Fatalf("mensagem não limitada: %d bytes %q", len(body.Message), body.Message)
	}
	for _, r := range body.Message {
		if unicode.IsControl(r) {
			t.Fatalf("mensagem contém controle: %q", body.Message)
		}
	}
}

// RFC 02 L03-10: /capabilities informa, sem segredos, que o PDF automático
// está indisponível e por quê, além de todos os módulos do contrato.
func TestCapabilitiesReportPDFReasonAndAllModules(t *testing.T) {
	read := func(client *unlockedClient) map[string]any {
		response := client.do(http.MethodGet, "/capabilities", "", contract.Version)
		var body map[string]any
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &body) != nil {
			t.Fatalf("capabilities: %d %s", response.Code, response.Body.String())
		}
		return body
	}
	missing := read(newUnlockedClient(t, mock.New(), nil, PreviewOptions{InvestigationID: "investigation-demo", PDFUnavailableReason: "ERR_PDF_DEPENDENCY: diretório de relatórios indisponível (permission denied)"}))
	if missing["contract"] != contract.Version || missing["pdf_available"] != false || !strings.Contains(missing["pdf_unavailable_reason"].(string), "diretório de relatórios") {
		t.Fatalf("PDF indisponível não informado: %+v", missing)
	}
	modules := missing["modules"].([]any)
	for _, want := range []string{"github_issues", "code_history", "ci", "deployments", "runtime_import"} {
		found := false
		for _, module := range modules {
			found = found || module == want
		}
		if !found {
			t.Fatalf("módulo %s ausente em %v", want, modules)
		}
	}
	defaulted := read(newUnlockedClient(t, mock.New(), nil, PreviewOptions{InvestigationID: "investigation-demo"}))
	if reason, _ := defaulted["pdf_unavailable_reason"].(string); !strings.HasPrefix(reason, "ERR_PDF_DEPENDENCY") {
		t.Fatalf("motivo padrão ausente: %+v", defaulted)
	}
	available := read(newUnlockedClient(t, mock.New(), &scopeReports{}, PreviewOptions{InvestigationID: "investigation-demo", PDFUnavailableReason: "ignorado"}))
	if available["pdf_available"] != true {
		t.Fatalf("PDF disponível não informado: %+v", available)
	}
	if _, present := available["pdf_unavailable_reason"]; present {
		t.Fatalf("motivo não deveria aparecer com PDF disponível: %+v", available)
	}
}

func TestBrowserAssetsDeclareContractAndShowPDFDependency(t *testing.T) {
	real := realAppJS("investigation-demo", "")
	for _, want := range []string{"'X-Investigate-Contract', contractVersion", "capabilities.contract !== contractVersion", "ERR_CONTRACT_MISMATCH", "capabilities.pdf_unavailable_reason", "report_failed"} {
		if !strings.Contains(real, want) {
			t.Fatalf("asset real sem %q", want)
		}
	}
	for _, want := range []string{"headers.set('X-Investigate-Contract','investigate.v1')", "caps.contract!=='investigate.v1'", "ERR_CONTRACT_MISMATCH", "caps.pdf_unavailable_reason"} {
		if !strings.Contains(appJS, want) {
			t.Fatalf("asset de preview sem %q", want)
		}
	}
	if !strings.Contains(real, "'"+contract.Version+"'") || !strings.Contains(appJS, "'"+contract.Version+"'") {
		t.Fatal("assets não usam a versão congelada do contrato")
	}
}

// RFC 02 L02-02: o código de desbloqueio nunca vai para a URL. O script do
// preview não pode falhar na página de desbloqueio (sem #nav) antes de
// registrar o submit; se o script faltar, o formulário usa POST, não GET.
func TestUnlockCodeNeverTravelsInURL(t *testing.T) {
	if !strings.Contains(unlockPage, `<form id="unlock-form" method="post">`) {
		t.Fatal("formulário de desbloqueio sem method=post")
	}
	navGuard := strings.Index(appJS, "if($('#nav'))$('#nav').addEventListener")
	unlockWiring := strings.Index(appJS, "uf.addEventListener('submit'")
	if navGuard < 0 || !strings.Contains(appJS, "if($('#logout'))") || unlockWiring < 0 {
		t.Fatal("preview acessa #nav/#logout sem guarda na página de desbloqueio")
	}
}
