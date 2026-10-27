package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/web/mock"
)

type blockingReport struct {
	started chan context.Context
	release chan struct{}
}

func (b *blockingReport) Finalize(ctx context.Context, in contract.FinalizeRequest) (contract.ReportArtifact, error) {
	b.started <- ctx
	select {
	case <-b.release:
		return contract.ReportArtifact{ID: "report-test", SnapshotID: in.SnapshotID, Status: "completed"}, nil
	case <-ctx.Done():
		return contract.ReportArtifact{}, ctx.Err()
	}
}
func (*blockingReport) Get(string) (contract.ReportArtifact, error) {
	return contract.ReportArtifact{}, os.ErrNotExist
}
func (*blockingReport) List(string) []contract.ReportArtifact { return nil }
func (*blockingReport) Open(string) (*os.File, error)         { return nil, os.ErrNotExist }

func TestReportJobOutlivesBrowserRequestAndStopsWithServer(t *testing.T) {
	for _, tc := range []struct {
		name       string
		closeEarly bool
	}{{"browser_disconnect", false}, {"server_shutdown", true}} {
		t.Run(tc.name, func(t *testing.T) {
			jobs := NewReportJobs()
			reports := &blockingReport{started: make(chan context.Context, 1), release: make(chan struct{})}
			handler, code, err := NewHandler(mock.New(), nil, nil, nil, nil, reports, PreviewOptions{InvestigationID: "investigation-demo", BindHost: "127.0.0.1", ReportJobs: jobs})
			if err != nil {
				t.Fatal(err)
			}
			unlock := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/v1/session/unlock", strings.NewReader(`{"code":"`+code+`"}`))
			unlock.Header.Set("Origin", "http://127.0.0.1")
			unlocked := httptest.NewRecorder()
			handler.ServeHTTP(unlocked, unlock)
			if unlocked.Code != http.StatusOK {
				t.Fatalf("unlock: %d %s", unlocked.Code, unlocked.Body.String())
			}
			cookie := unlocked.Result().Cookies()[0]
			session := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/v1/session", nil)
			session.AddCookie(cookie)
			sessionResult := httptest.NewRecorder()
			handler.ServeHTTP(sessionResult, session)
			var auth struct {
				CSRF string `json:"csrf_token"`
			}
			if err := json.Unmarshal(sessionResult.Body.Bytes(), &auth); err != nil || auth.CSRF == "" {
				t.Fatalf("session: %s %v", sessionResult.Body.String(), err)
			}
			browserCtx, closeBrowser := context.WithCancel(context.Background())
			defer closeBrowser()
			request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/v1/reports", strings.NewReader(`{"snapshot_id":"snap-demo-001"}`)).WithContext(browserCtx)
			request.AddCookie(cookie)
			request.Header.Set("Origin", "http://127.0.0.1")
			request.Header.Set("X-CSRF-Token", auth.CSRF)
			response := httptest.NewRecorder()
			finished := make(chan struct{})
			go func() {
				handler.ServeHTTP(response, request)
				close(finished)
			}()
			var reportCtx context.Context
			select {
			case reportCtx = <-reports.started:
			case <-time.After(time.Second):
				t.Fatal("exportação não iniciou")
			}
			closeBrowser()
			select {
			case <-reportCtx.Done():
				t.Fatal("fechar navegador cancelou o PDF")
			default:
			}
			if tc.closeEarly {
				closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				if err := jobs.Close(closeCtx); err != nil {
					t.Fatal(err)
				}
				if !errors.Is(reportCtx.Err(), context.Canceled) {
					t.Fatalf("encerrar servidor não cancelou o PDF: %v", reportCtx.Err())
				}
			} else {
				close(reports.release)
			}
			select {
			case <-finished:
			case <-time.After(time.Second):
				t.Fatal("exportação não encerrou")
			}
			if !tc.closeEarly && response.Code != http.StatusAccepted {
				t.Fatalf("PDF não foi concluído após fechar navegador: %d %s", response.Code, response.Body.String())
			}
			if !tc.closeEarly {
				closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				if err := jobs.Close(closeCtx); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
