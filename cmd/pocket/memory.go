package main

import (
	"errors"
	"fmt"
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
			store, err := newMemoryStore()
			if err != nil {
				return err
			}

			cwd, err := getWorkingDir()
			if err != nil {
				cwd = ""
			}

			input, err := parseAskInput(args, cwd)
			if err != nil {
				return err
			}

			interaction, err := store.RecordAsk(input)
			if err != nil {
				return err
			}

			_, err = fmt.Fprintf(cmd.OutOrStdout(), "session_id=%s\n", interaction.SessionID)
			return err
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
	return cmd
}

func newMemorySaveCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "save [id]",
		Short: "Salva a última interação recente ou revalida uma memória existente",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 1 {
				return fmt.Errorf("accepts at most 1 arg(s), received %d", len(args))
			}

			store, err := newMemoryStore()
			if err != nil {
				return err
			}

			var entry memory.Entry
			if len(args) == 0 {
				entry, err = store.SaveFromLastInteraction()
			} else {
				entry, err = store.UpdateConfidence(args[0], 0.1)
			}
			if err != nil {
				return err
			}

			_, err = fmt.Fprintf(cmd.OutOrStdout(), "id=%s confidence=%.1f\n", entry.ID, entry.Confidence)
			return err
		},
	}
}

func newMemoryDiscardCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "discard <id>",
		Short: "Reduz a confiança de uma memória existente sem removê-la",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return fmt.Errorf("accepts 1 arg(s), received %d", len(args))
			}

			store, err := newMemoryStore()
			if err != nil {
				return err
			}

			entry, err := store.UpdateConfidence(args[0], -0.1)
			if err != nil {
				return err
			}

			_, err = fmt.Fprintf(cmd.OutOrStdout(), "id=%s confidence=%.1f\n", entry.ID, entry.Confidence)
			return err
		},
	}
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
