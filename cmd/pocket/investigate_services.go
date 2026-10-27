package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"

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
	store, err := storage.Open("")
	if err != nil {
		return nil, err
	}
	c, err := core.NewCore(core.CoreConfig{Store: store, Secrets: secretStore, ResolvePrincipals: true})
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
	handler, code, err := web.NewHandler(services.core, services.core.Setup(), services.secrets, services.editorial, services.annotations, services.report, web.PreviewOptions{InvestigationID: args[0], SnapshotID: selectedSnapshot, BindHost: "127.0.0.1", Port: port, DataDir: "", QueryHistory: services.queries})
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
	go func() { <-ctx.Done(); _ = server.Shutdown(context.Background()) }()
	fmt.Fprintf(cmd.OutOrStdout(), "http://%s/\n", listener.Addr().String())
	fmt.Fprintf(os.Stderr, "Código local de desbloqueio: %s\n", code)
	err = server.Serve(listener)
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

func reportInvestigation(cmd *cobra.Command, args []string) error {
	services, err := newInvestigationServices()
	if err != nil {
		return err
	}
	if services.report == nil {
		return services.reportError
	}
	snapshots, err := services.core.ListSnapshots(context.Background(), args[0])
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
	snapshot, err := services.core.GetSnapshot(context.Background(), id)
	if err != nil {
		return err
	}
	if snapshot.InvestigationID != args[0] {
		return fmt.Errorf("ERR_FORBIDDEN: snapshot de outra investigação")
	}
	artifact, err := services.report.Finalize(context.Background(), contract.FinalizeRequest{SnapshotID: id})
	if err != nil {
		return err
	}
	if err := services.core.CompleteReport(context.Background(), artifact); err != nil {
		return err
	}
	return writeJSON(cmd.OutOrStdout(), artifact)
}
