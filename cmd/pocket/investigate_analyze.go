package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"pocketcli/internal/investigation/insightcontract"
	"pocketcli/internal/investigation/storage"
)

func newInvestigateAnalyzeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "analyze --snapshot ID --config CAMINHO_LOCAL_JSON",
		Short: "Analisa um snapshot existente com a configuração local indicada",
		RunE: func(cmd *cobra.Command, args []string) error {
			snapshotID, configPath, err := parseInsightAnalysisArgs(args)
			if err != nil {
				return err
			}
			cfg, err := readAnalysisConfig(configPath)
			if err != nil {
				return err
			}
			return runInsightAnalysis(cmd, snapshotID, cfg)
		},
	}
	return cmd
}

func parseInsightAnalysisArgs(args []string) (snapshotID, configPath string, err error) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "--snapshot=") {
			snapshotID = strings.TrimPrefix(arg, "--snapshot=")
			continue
		}
		if strings.HasPrefix(arg, "--config=") {
			configPath = strings.TrimPrefix(arg, "--config=")
			continue
		}
		if arg != "--snapshot" && arg != "--config" {
			return "", "", fmt.Errorf("flag inválida: %s (uso: pocket investigate analyze --snapshot ID --config CAMINHO_LOCAL_JSON)", arg)
		}
		if i+1 >= len(args) {
			return "", "", fmt.Errorf("flag %s requer valor", arg)
		}
		i++
		if arg == "--snapshot" {
			snapshotID = args[i]
		} else {
			configPath = args[i]
		}
	}
	if snapshotID == "" || configPath == "" {
		return "", "", errors.New("uso: pocket investigate analyze --snapshot ID --config CAMINHO_LOCAL_JSON")
	}
	return snapshotID, configPath, nil
}

func readAnalysisConfig(path string) (insightcontract.AnalysisConfig, error) {
	file, err := os.Open(path)
	if err != nil {
		return insightcontract.AnalysisConfig{}, fmt.Errorf("ERR_CONFIG: abrir configuração local: %w", err)
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 1<<20+1))
	decoder.DisallowUnknownFields()
	var cfg insightcontract.AnalysisConfig
	if err := decoder.Decode(&cfg); err != nil {
		return cfg, fmt.Errorf("ERR_CONFIG: AnalysisConfig JSON inválido: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return cfg, errors.New("ERR_CONFIG: conteúdo extra ou maior que 1 MiB após AnalysisConfig")
	}
	return cfg, nil
}

func runInsightAnalysis(cmd *cobra.Command, snapshotID string, cfg insightcontract.AnalysisConfig) (resultErr error) {
	services, err := newInvestigationServices()
	if err != nil {
		return err
	}
	shutdown := func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return services.insights.Shutdown(ctx)
	}
	defer func() {
		if err := shutdown(); err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("encerrar serviço de insights: %w", err))
		}
	}()
	if !services.core.SQLAvailable() {
		return errors.New("ERR_UNAVAILABLE: análise exige SQLite; compile com CGO_ENABLED=1 e -tags investigate_sqlite")
	}
	idempotencyKey, err := storage.ID()
	if err != nil {
		return fmt.Errorf("gerar chave da análise: %w", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	run, err := services.insights.StartAnalysis(ctx, insightcontract.AnalysisRequest{SnapshotID: snapshotID, Config: cfg, IdempotencyKey: idempotencyKey})
	if err != nil {
		return err
	}
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()
	for run.State != "completed" && run.State != "completed_partial" && run.State != "failed" && run.State != "cancelled" {
		select {
		case <-ctx.Done():
			_, _ = services.insights.CancelAnalysis(context.Background(), run.ID)
			run, _ = services.insights.GetAnalysisRun(context.Background(), run.ID)
			_ = writeJSON(cmd.OutOrStdout(), run)
			return ctx.Err()
		case <-ticker.C:
			run, err = services.insights.GetAnalysisRun(ctx, run.ID)
			if err != nil {
				return err
			}
		}
	}
	if err := writeJSON(cmd.OutOrStdout(), run); err != nil {
		return err
	}
	if run.State == "failed" {
		if run.ErrorCode != nil {
			return fmt.Errorf("análise falhou: %s", *run.ErrorCode)
		}
		return errors.New("análise falhou")
	}
	return nil
}
