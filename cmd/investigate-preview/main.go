package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"pocketcli/internal/investigation/annotations"
	"pocketcli/internal/investigation/credentials"
	"pocketcli/internal/investigation/editorial"
	"pocketcli/internal/investigation/queries"
	"pocketcli/internal/investigation/report"
	"pocketcli/internal/investigation/web"
	"pocketcli/internal/investigation/web/mock"
)

func main() {
	bind := flag.String("bind", "127.0.0.1", "endereço loopback local")
	port := flag.Int("port", 0, "porta HTTP; 0 escolhe uma porta livre")
	scenario := flag.String("scenario", "complete", "complete|empty|partial|unauthorized|rate_limited|offline|pdf_unavailable|large_report")
	dataDir := flag.String("data-dir", "", "diretório local reservado para dados do preview")
	flag.Parse()
	if *dataDir == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			log.Fatal(err)
		}
		*dataDir = filepath.Join(base, "pocketcli", "investigation-preview")
	}
	options := web.PreviewOptions{Demo: true, BindHost: *bind, Port: *port, Scenario: *scenario, DataDir: *dataDir}
	queryStore, err := queries.Open(*dataDir)
	if err != nil {
		log.Fatal(err)
	}
	options.QueryHistory = queryStore
	annotationStore, err := annotations.Open(filepath.Join(*dataDir, "annotations"))
	if err != nil {
		log.Fatal(err)
	}
	core := mock.NewScenario(*scenario)
	credentialStore := credentials.NewMemoryStore()
	defer credentialStore.Clear()
	editorialService, err := editorial.New(core, annotationStore, credentialStore, filepath.Join(*dataDir, "editorial"))
	if err != nil {
		log.Fatal(err)
	}
	var reports web.ReportService
	if *scenario != "pdf_unavailable" {
		if f, e := report.New(core, filepath.Join(*dataDir, "reports"), "", true, annotationStore, editorialService, queryStore); e == nil {
			reports = f
		} else {
			log.Printf("PDF pendente: %v", e)
		}
	}
	handler, code, err := web.NewHandler(core, mock.Setup{}, credentialStore, editorialService, annotationStore, reports, options)
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	listener, server, err := web.Listen(ctx, handler, options)
	if err != nil {
		log.Fatal(err)
	}
	defer server.Close()
	address := fmt.Sprintf("http://%s", listener.Addr().String())
	log.Printf("PocketCli Investigation preview DEMO; sem conexões automáticas; Claude só após consentimento explícito")
	log.Printf("Abra %s", address)
	log.Printf("Código de desbloqueio (exibido uma vez): %s", code)
	<-ctx.Done()
}
