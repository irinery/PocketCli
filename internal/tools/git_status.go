package tools

import (
	stdctx "context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

func GitStatusTool() Tool {
	return Tool{
		Definition: Definition{
			Name: "git_status",
			Input: Schema{
				"path": "string",
			},
			Output: Schema{
				"branch":         "string",
				"dirty":          "boolean",
				"staged_files":   "[string]",
				"unstaged_files": "[string]",
			},
			TimeoutMS:   3000,
			FailureMode: FailureModeSkip,
			Version:     "1.0.0",
		},
		Run: runGitStatus,
	}
}

func runGitStatus(ctx stdctx.Context, input map[string]any) (ExecutionOutput, error) {
	path, _ := input["path"].(string)
	path = strings.TrimSpace(path)
	if path == "" {
		return ExecutionOutput{}, errors.New("path não informado")
	}

	output, err := runGit(ctx, path, "rev-parse", "--is-inside-work-tree")
	if err != nil || strings.TrimSpace(output) != "true" {
		raw := strings.TrimSpace(output)
		if raw == "" && err != nil {
			raw = err.Error()
		}
		return ExecutionOutput{Raw: raw}, errors.New("diretório não é um repositório git")
	}

	branchOutput, err := runGit(ctx, path, "branch", "--show-current")
	if err != nil {
		return ExecutionOutput{Raw: strings.TrimSpace(branchOutput)}, fmt.Errorf("falha ao identificar branch: %w", err)
	}

	statusOutput, err := runGit(ctx, path, "status", "--porcelain")
	if err != nil {
		return ExecutionOutput{Raw: strings.TrimSpace(statusOutput)}, fmt.Errorf("falha ao ler git status: %w", err)
	}

	branch := strings.TrimSpace(branchOutput)
	stagedFiles, unstagedFiles := parsePorcelainStatus(statusOutput)
	dirty := len(stagedFiles) > 0 || len(unstagedFiles) > 0

	return ExecutionOutput{
		Summary: fmt.Sprintf(
			"branch=%s dirty=%t staged=%d unstaged=%d",
			branch,
			dirty,
			len(stagedFiles),
			len(unstagedFiles),
		),
		Raw: strings.TrimSpace(statusOutput),
		Artifacts: map[string]any{
			"branch":         branch,
			"dirty":          dirty,
			"staged_files":   stagedFiles,
			"unstaged_files": unstagedFiles,
		},
	}, nil
}

func runGit(ctx stdctx.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func parsePorcelainStatus(output string) ([]string, []string) {
	lines := strings.Split(strings.ReplaceAll(strings.TrimSpace(output), "\r\n", "\n"), "\n")
	stagedSet := map[string]struct{}{}
	unstagedSet := map[string]struct{}{}

	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" || len(line) < 3 {
			continue
		}

		status := line[:2]
		path := normalizeStatusPath(line[3:])
		if path == "" {
			continue
		}

		if status[0] != ' ' && status[0] != '?' {
			stagedSet[path] = struct{}{}
		}
		if status[1] != ' ' {
			unstagedSet[path] = struct{}{}
		}
	}

	return sortedKeys(stagedSet), sortedKeys(unstagedSet)
}

func normalizeStatusPath(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return ""
	}
	if strings.Contains(trimmed, " -> ") {
		parts := strings.Split(trimmed, " -> ")
		trimmed = parts[len(parts)-1]
	}
	return filepath.ToSlash(trimmed)
}

func sortedKeys(set map[string]struct{}) []string {
	if len(set) == 0 {
		return []string{}
	}

	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
