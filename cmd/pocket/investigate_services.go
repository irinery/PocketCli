package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"pocketcli/internal/investigation/annotations"
	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/core"
	"pocketcli/internal/investigation/editorial"
	"pocketcli/internal/investigation/queries"
	"pocketcli/internal/investigation/report"
	"pocketcli/internal/investigation/secrets"
	"pocketcli/internal/investigation/storage"
	"pocketcli/internal/investigation/web"
)

type investigationServices struct {
	core        *core.Core
	secrets     *secrets.SessionStore
	annotations *annotations.Store
	queries     *queries.Store
	editorial   *editorial.Service
	report      web.ReportService
	reportError error
}

func newInvestigationServices() (*investigationServices, error) {
	return newInvestigationServicesWithSecrets(secrets.NewSessionStore())
}

func newInvestigationCore(secretStore *secrets.SessionStore) (*core.Core, error) {
	services, err := newInvestigationServicesWithSecrets(secretStore)
	if err != nil {
		return nil, err
	}
	return services.core, nil
}

func newInvestigationServicesWithSecrets(secretStore *secrets.SessionStore) (*investigationServices, error) {
	reserve, err := investigationQuotaReservePercent()
	if err != nil {
		return nil, err
	}
	store, err := storage.Open("")
	if err != nil {
		return nil, err
	}
	c, err := core.NewCore(core.CoreConfig{Store: store, Secrets: secretStore, ResolvePrincipals: true, QuotaReservePercent: reserve})
	if err != nil {
		return nil, err
	}
	notes, err := annotations.Open(filepath.Join(store.Root, "annotations"))
	if err != nil {
		return nil, err
	}
	queryHistory, err := queries.Open(filepath.Join(store.Root, "queries"))
	if err != nil {
		return nil, err
	}
	edit, err := editorial.New(c, notes, secretStore, filepath.Join(store.Root, "editorial"))
	if err != nil {
		return nil, err
	}
	finalizer, reportErr := report.New(c, filepath.Join(store.Root, "reports"), os.Getenv("POCKETCLI_CHROMIUM"), false, notes, edit, queryHistory)
	var cached web.ReportService
	if reportErr == nil {
		cached = &reportCache{inner: finalizer, store: store, notes: notes, editorial: edit, queries: queryHistory}
		c.SetFinalizer(cached)
	}
	return &investigationServices{core: c, secrets: secretStore, annotations: notes, queries: queryHistory, editorial: edit, report: cached, reportError: reportErr}, nil
}

func investigationQuotaReservePercent() (*int, error) {
	value, configured := os.LookupEnv("POCKETCLI_INVESTIGATE_QUOTA_RESERVE_PERCENT")
	if !configured {
		return nil, nil
	}
	percent, err := strconv.Atoi(value)
	if err != nil || percent < 0 || percent > 100 {
		return nil, fmt.Errorf("POCKETCLI_INVESTIGATE_QUOTA_RESERVE_PERCENT deve ser um inteiro de 0 a 100")
	}
	return &percent, nil
}

func serveInvestigation(cmd *cobra.Command, args []string) error {
	services, err := newInvestigationServices()
	if err != nil {
		return err
	}
	if _, err := services.core.ListSnapshots(context.Background(), args[0]); err != nil {
		return err
	}
	port := 0
	selectedSnapshot := ""
	for i := 1; i < len(args); i++ {
		if args[i] == "--snapshot" {
			i++
			if i >= len(args) {
				return fmt.Errorf("--snapshot requer ID")
			}
			snapshot, err := services.core.GetSnapshot(context.Background(), args[i])
			if err != nil {
				return err
			}
			if snapshot.InvestigationID != args[0] {
				return fmt.Errorf("ERR_FORBIDDEN: snapshot de outra investigação")
			}
			selectedSnapshot = snapshot.ID
		} else if args[i] == "--port" {
			i++
			if i >= len(args) {
				return fmt.Errorf("--port requer número")
			}
			port, err = strconv.Atoi(args[i])
			if err != nil || port < 0 || port > 65535 {
				return fmt.Errorf("porta inválida")
			}
		} else {
			return fmt.Errorf("flag inválida: %s", args[i])
		}
	}
	jobs := web.NewReportJobs()
	handler, code, err := web.NewHandler(services.core, services.core.Setup(), services.secrets, services.editorial, services.annotations, services.report, web.PreviewOptions{InvestigationID: args[0], SnapshotID: selectedSnapshot, BindHost: "127.0.0.1", Port: port, DataDir: "", QueryHistory: services.queries, ReportJobs: jobs})
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return err
	}
	defer listener.Close()
	server := &http.Server{Handler: handler}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	fmt.Fprintf(cmd.OutOrStdout(), "http://%s/\n", listener.Addr().String())
	fmt.Fprintf(os.Stderr, "Código local de desbloqueio: %s\n", code)
	serveErr := serveInvestigationHTTP(ctx, server, listener, 5*time.Second)
	closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer closeCancel()
	closeErr := jobs.Close(closeCtx)
	if serveErr != nil {
		return serveErr
	}
	if closeErr != nil {
		return fmt.Errorf("ERR_TIMEOUT: renderização de PDF não encerrou após o servidor: %w", closeErr)
	}
	return nil
}

func serveInvestigationHTTP(ctx context.Context, server *http.Server, listener net.Listener, grace time.Duration) error {
	shutdownSignal, cancelSignal := context.WithCancel(ctx)
	defer cancelSignal()
	shutdownDone := make(chan error, 1)
	go func() {
		<-shutdownSignal.Done()
		deadline, cancel := context.WithTimeout(context.Background(), grace)
		defer cancel()
		err := server.Shutdown(deadline)
		if err != nil {
			_ = server.Close() // Cancels active requests, including a PDF render.
		}
		shutdownDone <- err
	}()
	serveErr := server.Serve(listener)
	cancelSignal()
	shutdownErr := <-shutdownDone
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		return serveErr
	}
	if shutdownErr != nil {
		return fmt.Errorf("ERR_TIMEOUT: encerramento do servidor local excedeu o prazo: %w", shutdownErr)
	}
	return nil
}

func reportInvestigation(cmd *cobra.Command, args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	services, err := newInvestigationServices()
	if err != nil {
		return err
	}
	if services.report == nil {
		return services.reportError
	}
	snapshots, err := services.core.ListSnapshots(ctx, args[0])
	if err != nil {
		return err
	}
	if len(snapshots) == 0 {
		return fmt.Errorf("ERR_NOT_FOUND: investigação sem snapshot")
	}
	id := snapshots[len(snapshots)-1].ID
	for i := 1; i < len(args); i++ {
		if args[i] != "--snapshot" || i+1 >= len(args) {
			return fmt.Errorf("uso: report INVESTIGATION_ID [--snapshot ID]")
		}
		i++
		id = args[i]
	}
	snapshot, err := services.core.GetSnapshot(ctx, id)
	if err != nil {
		return err
	}
	if snapshot.InvestigationID != args[0] {
		return fmt.Errorf("ERR_FORBIDDEN: snapshot de outra investigação")
	}
	artifact, err := services.report.Finalize(ctx, contract.FinalizeRequest{SnapshotID: id})
	if err != nil {
		return err
	}
	if err := services.core.CompleteReport(ctx, artifact); err != nil {
		return err
	}
	return writeJSON(cmd.OutOrStdout(), artifact)
}
