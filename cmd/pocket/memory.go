package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"pocketcli/internal/memory"
)

var (
	newMemoryStore = memory.NewStore
	getWorkingDir  = os.Getwd
)

func newAskCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "ask [--kind KIND] [--scope SCOPE] [--title TITLE] [--tags tag1,tag2] <prompt...>",
		Short: "Registra a última interação local para uso no memory save",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAudited(cmd, "ask", args, func(cmd *cobra.Command, args []string, sessionID string) (commandAudit, error) {
				store, err := newMemoryStore()
				if err != nil {
					return commandAudit{}, err
				}

				cwd, err := getWorkingDir()
				if err != nil {
					cwd = ""
				}

				input, err := parseAskInput(args, cwd)
				if err != nil {
					return commandAudit{}, err
				}
				input.SessionID = sessionID

				interaction, err := store.RecordAsk(input)
				if err != nil {
					return commandAudit{}, err
				}

				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "session_id=%s\n", interaction.SessionID); err != nil {
					return commandAudit{}, err
				}

				return commandAudit{SessionID: interaction.SessionID}, nil
			})
		},
	}
}

func newMemoryCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "memory",
		Short: "Gerencia memórias persistidas entre sessões",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cobra.ErrSubCommandRequired
		},
	}

	cmd.AddCommand(newMemorySaveCommand())
	cmd.AddCommand(newMemoryDiscardCommand())
	cmd.AddCommand(newMemorySearchCommand())
	cmd.AddCommand(newMemoryCleanCommand())
	return cmd
}

func newMemorySaveCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "save [id]",
		Short: "Salva a última interação recente ou revalida uma memória existente",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAudited(cmd, "memory save", args, func(cmd *cobra.Command, args []string, sessionID string) (commandAudit, error) {
				if len(args) > 1 {
					return commandAudit{}, fmt.Errorf("accepts at most 1 arg(s), received %d", len(args))
				}

				store, err := newMemoryStore()
				if err != nil {
					return commandAudit{}, err
				}

				var entry memory.Entry
				if len(args) == 0 {
					entry, err = store.SaveFromLastInteraction()
				} else {
					entry, err = store.UpdateConfidence(args[0], 0.1)
				}
				if err != nil {
					return commandAudit{}, err
				}

				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "id=%s confidence=%.1f\n", entry.ID, entry.Confidence); err != nil {
					return commandAudit{}, err
				}
				return commandAudit{SessionID: sessionID}, nil
			})
		},
	}
}

func newMemoryDiscardCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "discard <id>",
		Short: "Reduz a confiança de uma memória existente sem removê-la",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAudited(cmd, "memory discard", args, func(cmd *cobra.Command, args []string, sessionID string) (commandAudit, error) {
				if len(args) != 1 {
					return commandAudit{}, fmt.Errorf("accepts 1 arg(s), received %d", len(args))
				}

				store, err := newMemoryStore()
				if err != nil {
					return commandAudit{}, err
				}

				entry, err := store.UpdateConfidence(args[0], -0.1)
				if err != nil {
					return commandAudit{}, err
				}

				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "id=%s confidence=%.1f\n", entry.ID, entry.Confidence); err != nil {
					return commandAudit{}, err
				}
				return commandAudit{SessionID: sessionID}, nil
			})
		},
	}
}

func newMemorySearchCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "search [--project NAME] [--host HOST] <query...>",
		Short: "Busca memórias relevantes por score determinístico",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAudited(cmd, "memory search", args, func(cmd *cobra.Command, args []string, sessionID string) (commandAudit, error) {
				store, err := newMemoryStore()
				if err != nil {
					return commandAudit{}, err
				}

				cwd, err := getWorkingDir()
				if err != nil {
					cwd = ""
				}

				query, ctx, err := parseMemorySearchInput(args, cwd)
				if err != nil {
					return commandAudit{}, err
				}

				results, err := store.Retrieve(query, ctx)
				if err != nil {
					return commandAudit{}, err
				}

				if len(results) == 0 {
					if _, err := fmt.Fprintln(cmd.OutOrStdout(), "nenhum resultado encontrado"); err != nil {
						return commandAudit{}, err
					}
					return commandAudit{SessionID: sessionID}, nil
				}

				for _, entry := range results {
					if _, err := fmt.Fprintf(
						cmd.OutOrStdout(),
						"id=%s scope=%s confidence=%.1f accesses=%d title=%s\n",
						entry.ID,
						entry.Scope,
						entry.Confidence,
						entry.AccessCount,
						entry.Title,
					); err != nil {
						return commandAudit{}, err
					}
				}

				return commandAudit{
					SessionID: sessionID,
					MemoryHit: true,
				}, nil
			})
		},
	}
}

func newMemoryCleanCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "clean [--dry-run] [--force]",
		Short: "Lista ou remove entradas candidatas à limpeza manual",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAudited(cmd, "memory clean", args, func(cmd *cobra.Command, args []string, sessionID string) (commandAudit, error) {
				dryRun, force, err := parseMemoryCleanInput(args)
				if err != nil {
					return commandAudit{}, err
				}

				store, err := newMemoryStore()
				if err != nil {
					return commandAudit{}, err
				}

				candidates, err := store.CleanupCandidates("")
				if err != nil {
					return commandAudit{}, err
				}

				if len(candidates) == 0 {
					if _, err := fmt.Fprintln(cmd.OutOrStdout(), "nenhuma entrada candidata à remoção"); err != nil {
						return commandAudit{}, err
					}
					return commandAudit{SessionID: sessionID}, nil
				}

				if dryRun {
					for _, candidate := range candidates {
						if _, err := fmt.Fprintln(cmd.OutOrStdout(), formatCleanupCandidate(candidate)); err != nil {
							return commandAudit{}, err
						}
					}
					return commandAudit{SessionID: sessionID}, nil
				}

				if force {
					deleted, err := deleteCleanupCandidates(store, candidates)
					if err != nil {
						return commandAudit{}, err
					}
					if _, err := fmt.Fprintf(cmd.OutOrStdout(), "total_deleted=%d\n", deleted); err != nil {
						return commandAudit{}, err
					}
					return commandAudit{SessionID: sessionID}, nil
				}

				reader := bufio.NewReader(os.Stdin)
				deleted := 0
				for _, candidate := range candidates {
					if _, err := fmt.Fprintf(cmd.OutOrStdout(), "remover %s? [y/N]: ", formatCleanupCandidate(candidate)); err != nil {
						return commandAudit{}, err
					}

					answer, readErr := reader.ReadString('\n')
					if readErr != nil && !errors.Is(readErr, io.EOF) {
						return commandAudit{}, readErr
					}

					if shouldDeleteCleanupEntry(answer) {
						if err := store.DeleteEntry(candidate.Entry.ID); err != nil {
							return commandAudit{}, err
						}
						deleted++
						if _, err := fmt.Fprintf(cmd.OutOrStdout(), "removida id=%s\n", candidate.Entry.ID); err != nil {
							return commandAudit{}, err
						}
					} else {
						if _, err := fmt.Fprintf(cmd.OutOrStdout(), "mantida id=%s\n", candidate.Entry.ID); err != nil {
							return commandAudit{}, err
						}
					}
				}

				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "total_deleted=%d\n", deleted); err != nil {
					return commandAudit{}, err
				}

				return commandAudit{SessionID: sessionID}, nil
			})
		},
	}
}

func parseMemoryCleanInput(args []string) (bool, bool, error) {
	dryRun := false
	force := false

	for _, arg := range args {
		switch arg {
		case "--dry-run":
			dryRun = true
		case "--force":
			force = true
		default:
			return false, false, fmt.Errorf("flag inválida: %s", arg)
		}
	}

	if dryRun && force {
		return false, false, errors.New("use apenas uma das flags: --dry-run ou --force")
	}

	return dryRun, force, nil
}

func deleteCleanupCandidates(store *memory.Store, candidates []memory.CleanupCandidate) (int, error) {
	deleted := 0
	for _, candidate := range candidates {
		if err := store.DeleteEntry(candidate.Entry.ID); err != nil {
			return deleted, err
		}
		deleted++
	}
	return deleted, nil
}

func formatCleanupCandidate(candidate memory.CleanupCandidate) string {
	return fmt.Sprintf(
		"id=%s scope=%s confidence=%.1f last_accessed=%s reasons=%s",
		candidate.Entry.ID,
		candidate.Entry.Scope,
		candidate.Entry.Confidence,
		candidate.Entry.LastAccessed,
		strings.Join(candidate.Reasons, ","),
	)
}

func shouldDeleteCleanupEntry(answer string) bool {
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes", "s", "sim":
		return true
	default:
		return false
	}
}

func parseMemorySearchInput(args []string, cwd string) (string, memory.RetrievalContext, error) {
	ctx := memory.RetrievalContext{WorkingDir: cwd}

	var queryParts []string
	for idx := 0; idx < len(args); idx++ {
		arg := args[idx]
		if !strings.HasPrefix(arg, "--") {
			queryParts = append(queryParts, arg)
			continue
		}

		name, value, consumesNext, err := parseFlagValue(arg)
		if err != nil {
			return "", memory.RetrievalContext{}, err
		}
		if consumesNext {
			idx++
			if idx >= len(args) {
				return "", memory.RetrievalContext{}, fmt.Errorf("flag %s requer valor", name)
			}
			value = args[idx]
		}

		switch name {
		case "--project":
			ctx.Project = value
		case "--host":
			ctx.Host = value
		default:
			return "", memory.RetrievalContext{}, fmt.Errorf("flag inválida: %s", name)
		}
	}

	if len(queryParts) == 0 {
		return "", memory.RetrievalContext{}, errors.New("nenhuma query informada")
	}

	return strings.Join(queryParts, " "), ctx, nil
}

func parseAskInput(args []string, cwd string) (memory.AskInput, error) {
	input := memory.AskInput{
		Kind:  memory.KindPattern,
		Scope: memory.DefaultScopeFromCWD(cwd),
	}

	var promptParts []string
	for idx := 0; idx < len(args); idx++ {
		arg := args[idx]
		if !strings.HasPrefix(arg, "--") {
			promptParts = append(promptParts, arg)
			continue
		}

		name, value, consumesNext, err := parseFlagValue(arg)
		if err != nil {
			return memory.AskInput{}, err
		}
		if consumesNext {
			idx++
			if idx >= len(args) {
				return memory.AskInput{}, fmt.Errorf("flag %s requer valor", name)
			}
			value = args[idx]
		}

		switch name {
		case "--kind":
			input.Kind = value
		case "--scope":
			input.Scope = value
		case "--title":
			input.Title = value
		case "--tags":
			input.Tags = splitCSV(value)
		default:
			return memory.AskInput{}, fmt.Errorf("flag inválida: %s", name)
		}
	}

	if len(promptParts) == 0 {
		return memory.AskInput{}, errors.New("nenhuma pergunta informada")
	}

	input.Prompt = strings.Join(promptParts, " ")
	return input, nil
}

func parseFlagValue(arg string) (name string, value string, consumesNext bool, err error) {
	if !strings.HasPrefix(arg, "--") {
		return "", "", false, fmt.Errorf("flag inválida: %s", arg)
	}
	if strings.Contains(arg, "=") {
		parts := strings.SplitN(arg, "=", 2)
		if parts[1] == "" {
			return parts[0], "", false, fmt.Errorf("flag %s requer valor", parts[0])
		}
		return parts[0], parts[1], false, nil
	}
	return arg, "", true, nil
}

func splitCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}

	parts := strings.Split(value, ",")
	tags := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		tags = append(tags, trimmed)
	}
	return tags
}
