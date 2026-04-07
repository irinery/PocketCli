package contextcollector

import (
	stdctx "context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	MaxContextTokens = 4000

	mainFileLineLimit = 100
	readmeLineLimit   = 50
	gitDiffLineLimit  = 200
	gitLogCommitLimit = 10
	commandTimeout    = 3 * time.Second
)

var (
	blockedFilePatterns = []string{
		"*.env",
		".env*",
		"*secret*",
		"*credential*",
		"id_rsa*",
		"*.pem",
		"*.key",
	}
	blockedContentPattern = regexp.MustCompile(`(?i)(password|token|secret|key)\s*[:=]\s*\S+`)
	readmeNames           = map[string]struct{}{
		"readme":     {},
		"readme.md":  {},
		"readme.txt": {},
	}
	skippedDirs = map[string]struct{}{
		".git":         {},
		".hg":          {},
		".svn":         {},
		"node_modules": {},
		"vendor":       {},
		"third_party":  {},
	}
)

type Session struct {
	LastCommand string
	LastError   string
}

type TaskContext struct {
	Project     ProjectContext
	Session     SessionContext
	System      SystemContext
	MemoryHits  []string
	ToolResults []ToolResult
	Notes       []string
}

type ToolResult struct {
	ToolName   string
	OK         bool
	Summary    string
	Raw        string
	Artifacts  map[string]any
	DurationMS int
	Metadata   map[string]any
}

type ProjectContext struct {
	Path          string
	IsGit         bool
	Branch        *string
	MainFiles     []MainFile
	ReadmeExcerpt *string
	GitDiffHead   *string
	GitLog        *string
}

type MainFile struct {
	Path    string
	Excerpt string
}

type SessionContext struct {
	LastCommand *string
	LastError   *string
}

type SystemContext struct {
	OS string
}

func Collect(cwd string, session Session) (TaskContext, error) {
	absPath, err := filepath.Abs(cwd)
	if err != nil {
		return TaskContext{}, err
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return TaskContext{}, err
	}
	if !info.IsDir() {
		return TaskContext{}, errors.New("context collector requires a directory")
	}

	ctx := TaskContext{
		Project: ProjectContext{
			Path: absPath,
		},
		System: SystemContext{
			OS: runtime.GOOS,
		},
		MemoryHits:  []string{},
		ToolResults: []ToolResult{},
		Notes:       []string{},
	}

	if session.LastCommand != "" {
		ctx.Session.LastCommand = stringPtr(session.LastCommand)
	}
	if session.LastError != "" {
		ctx.Session.LastError = stringPtr(session.LastError)
	}

	partial := false

	readme, readmePartial, err := collectReadme(absPath)
	if err != nil {
		return TaskContext{}, err
	}
	if readme != nil {
		ctx.Project.ReadmeExcerpt = readme
	}
	partial = partial || readmePartial

	mainFiles, mainFilesPartial, err := collectMainFiles(absPath)
	if err != nil {
		return TaskContext{}, err
	}
	ctx.Project.MainFiles = mainFiles
	partial = partial || mainFilesPartial

	isGit, branch, gitDiff, gitLog, gitPartial := collectGitContext(absPath)
	ctx.Project.IsGit = isGit
	ctx.Project.Branch = branch
	if gitDiff != nil {
		ctx.Project.GitDiffHead = gitDiff
	}
	if gitLog != nil {
		ctx.Project.GitLog = gitLog
	}
	partial = partial || gitPartial

	if compress(&ctx) {
		partial = true
	}

	if partial {
		addNote(&ctx.Notes, "[contexto parcial — itens omitidos]")
		if compress(&ctx) {
			partial = true
		}
	}

	return ctx, nil
}

func collectReadme(cwd string) (*string, bool, error) {
	entries, err := os.ReadDir(cwd)
	if err != nil {
		return nil, false, err
	}

	var candidates []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if _, ok := readmeNames[strings.ToLower(name)]; ok {
			candidates = append(candidates, filepath.Join(cwd, name))
		}
	}

	sort.Strings(candidates)
	if len(candidates) == 0 {
		return nil, false, nil
	}

	content, err := readTextFile(candidates[0])
	if err != nil {
		return nil, false, nil
	}

	content = sanitizeContent(content)
	content, truncated := truncateLines(content, readmeLineLimit)
	if content == "" {
		return nil, truncated, nil
	}

	return stringPtr(content), truncated, nil
}

func collectMainFiles(cwd string) ([]MainFile, bool, error) {
	var files []MainFile
	partial := false

	err := filepath.WalkDir(cwd, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == cwd {
			return nil
		}

		name := d.Name()
		if d.IsDir() {
			if shouldSkipDir(name) {
				return filepath.SkipDir
			}
			return nil
		}

		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}

		if isReadmeName(name) || shouldBlockFile(name) {
			return nil
		}
		if strings.HasPrefix(name, ".") {
			return nil
		}

		content, err := readTextFile(path)
		if err != nil {
			return nil
		}
		if content == "" {
			return nil
		}

		content = sanitizeContent(content)
		content, truncated := truncateLines(content, mainFileLineLimit)
		partial = partial || truncated

		relPath, err := filepath.Rel(cwd, path)
		if err != nil {
			return nil
		}

		files = append(files, MainFile{
			Path:    filepath.ToSlash(relPath),
			Excerpt: content,
		})
		return nil
	})
	if err != nil {
		return nil, false, err
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})

	return files, partial, nil
}

func collectGitContext(cwd string) (bool, *string, *string, *string, bool) {
	output, err := runCommand(cwd, "git", "rev-parse", "--is-inside-work-tree")
	if err != nil || strings.TrimSpace(output) != "true" {
		return false, nil, nil, nil, false
	}

	var branch *string
	if out, err := runCommand(cwd, "git", "branch", "--show-current"); err == nil {
		trimmed := strings.TrimSpace(out)
		if trimmed != "" {
			branch = stringPtr(trimmed)
		}
	}

	var gitDiff *string
	partial := false
	if out, err := runCommand(cwd, "git", "diff", "HEAD", "--"); err == nil {
		sanitized := sanitizeGitDiff(out)
		if sanitized != "" {
			var truncated bool
			sanitized, truncated = truncateLines(sanitized, gitDiffLineLimit)
			partial = partial || truncated
			gitDiff = stringPtr(sanitized)
		}
	}

	var gitLog *string
	if out, err := runCommand(cwd, "git", "log", "--oneline", "-n", strconv.Itoa(gitLogCommitLimit)); err == nil {
		trimmed := strings.TrimSpace(out)
		if trimmed != "" {
			gitLog = stringPtr(trimmed)
		}
	}

	return true, branch, gitDiff, gitLog, partial
}

func compress(ctx *TaskContext) bool {
	partial := false

	for totalTokenCount(*ctx) > MaxContextTokens {
		switch {
		case ctx.Project.GitLog != nil:
			ctx.Project.GitLog = nil
			partial = true
		case ctx.Project.ReadmeExcerpt != nil:
			ctx.Project.ReadmeExcerpt = nil
			partial = true
		case len(ctx.Project.MainFiles) > 1:
			ctx.Project.MainFiles = ctx.Project.MainFiles[:len(ctx.Project.MainFiles)-1]
			partial = true
		case len(ctx.Project.MainFiles) == 1 && ctx.Project.MainFiles[0].Excerpt != "":
			if shrinkMainFile(ctx) {
				partial = true
			} else {
				ctx.Project.MainFiles = nil
				partial = true
			}
		case ctx.Project.GitDiffHead != nil:
			if shrinkStringField(&ctx.Project.GitDiffHead, MaxContextTokens-totalTokenCountWithoutField(*ctx, ctx.Project.GitDiffHead)) {
				partial = true
			} else {
				ctx.Project.GitDiffHead = nil
				partial = true
			}
		case ctx.Session.LastCommand != nil && approxTokens(*ctx.Session.LastCommand) > 1:
			allowed := MaxContextTokens - totalTokenCountWithoutField(*ctx, ctx.Session.LastCommand)
			if allowed < 1 {
				allowed = 1
			}
			if shrinkStringField(&ctx.Session.LastCommand, allowed) {
				partial = true
				upsertFieldNote(&ctx.Notes, "last_command", allowed)
			}
		case ctx.Session.LastError != nil:
			allowed := MaxContextTokens - totalTokenCountWithoutField(*ctx, ctx.Session.LastError)
			if allowed < 1 {
				allowed = 1
			}
			if shrinkStringField(&ctx.Session.LastError, allowed) {
				partial = true
				upsertFieldNote(&ctx.Notes, "last_error", allowed)
			} else {
				return partial
			}
		default:
			return partial
		}
	}

	return partial
}

func shrinkMainFile(ctx *TaskContext) bool {
	file := &ctx.Project.MainFiles[0]
	current := approxTokens(file.Excerpt)
	if current <= 1 {
		return false
	}

	allowed := MaxContextTokens - totalTokenCountWithoutMainFileExcerpt(*ctx, file.Excerpt)
	if allowed < 1 {
		allowed = 1
	}

	truncated, changed := truncateToTokens(file.Excerpt, allowed)
	if !changed {
		return false
	}

	file.Excerpt = truncated
	return true
}

func shrinkStringField(field **string, allowedTokens int) bool {
	if field == nil || *field == nil {
		return false
	}
	truncated, changed := truncateToTokens(**field, allowedTokens)
	if !changed {
		return false
	}
	*field = stringPtr(truncated)
	return true
}

func lastFieldNote(label string, tokens int) string {
	return "[" + label + " truncado — exibindo primeiros " + strconv.Itoa(tokens) + " tokens]"
}

func sanitizeContent(content string) string {
	lines := splitLines(content)
	sanitized := make([]string, 0, len(lines))
	for _, line := range lines {
		if blockedContentPattern.MatchString(line) {
			continue
		}
		sanitized = append(sanitized, line)
	}
	return strings.Join(sanitized, "\n")
}

func sanitizeGitDiff(diff string) string {
	lines := splitLines(diff)
	sanitized := make([]string, 0, len(lines))
	skipFile := false

	for _, line := range lines {
		if strings.HasPrefix(line, "diff --git ") {
			skipFile = false
			fields := strings.Fields(line)
			if len(fields) >= 4 {
				target := strings.TrimPrefix(fields[3], "b/")
				if shouldBlockFile(filepath.Base(target)) {
					skipFile = true
				}
			}
		}
		if skipFile {
			continue
		}
		if blockedContentPattern.MatchString(line) {
			continue
		}
		sanitized = append(sanitized, line)
	}

	return strings.TrimSpace(strings.Join(sanitized, "\n"))
}

func truncateLines(content string, maxLines int) (string, bool) {
	lines := splitLines(content)
	if len(lines) <= maxLines {
		return strings.Join(lines, "\n"), false
	}

	truncated := append([]string{}, lines[:maxLines]...)
	truncated = append(truncated, "[conteúdo truncado]")
	return strings.Join(truncated, "\n"), true
}

func readTextFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if len(data) == 0 {
		return "", nil
	}
	if !utf8.Valid(data) || bytesContainsZero(data) {
		return "", nil
	}
	return strings.TrimRight(string(data), "\n"), nil
}

func bytesContainsZero(data []byte) bool {
	for _, b := range data {
		if b == 0 {
			return true
		}
	}
	return false
}

func shouldSkipDir(name string) bool {
	if _, ok := skippedDirs[name]; ok {
		return true
	}
	return strings.HasPrefix(name, ".")
}

func shouldBlockFile(name string) bool {
	lower := strings.ToLower(name)
	for _, pattern := range blockedFilePatterns {
		if matched, _ := filepath.Match(pattern, lower); matched {
			return true
		}
	}
	return false
}

func isReadmeName(name string) bool {
	_, ok := readmeNames[strings.ToLower(name)]
	return ok
}

func splitLines(content string) []string {
	if content == "" {
		return nil
	}
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	trimmed := strings.TrimRight(normalized, "\n")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}

func runCommand(cwd string, name string, args ...string) (string, error) {
	commandCtx, cancel := stdctx.WithTimeout(stdctx.Background(), commandTimeout)
	defer cancel()

	cmd := exec.CommandContext(commandCtx, name, args...)
	cmd.Dir = cwd
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(output), nil
}

func totalTokenCount(ctx TaskContext) int {
	total := approxTokens(ctx.Project.Path)
	if ctx.Project.Branch != nil {
		total += approxTokens(*ctx.Project.Branch)
	}
	if ctx.Project.ReadmeExcerpt != nil {
		total += approxTokens(*ctx.Project.ReadmeExcerpt)
	}
	if ctx.Project.GitDiffHead != nil {
		total += approxTokens(*ctx.Project.GitDiffHead)
	}
	if ctx.Project.GitLog != nil {
		total += approxTokens(*ctx.Project.GitLog)
	}
	if ctx.Session.LastCommand != nil {
		total += approxTokens(*ctx.Session.LastCommand)
	}
	if ctx.Session.LastError != nil {
		total += approxTokens(*ctx.Session.LastError)
	}
	total += approxTokens(ctx.System.OS)
	for _, file := range ctx.Project.MainFiles {
		total += approxTokens(file.Path)
		total += approxTokens(file.Excerpt)
	}
	for _, note := range ctx.Notes {
		total += approxTokens(note)
	}
	return total
}

func totalTokenCountWithoutField(ctx TaskContext, field *string) int {
	total := totalTokenCount(ctx)
	if field != nil {
		total -= approxTokens(*field)
	}
	return total
}

func totalTokenCountWithoutMainFileExcerpt(ctx TaskContext, excerpt string) int {
	return totalTokenCount(ctx) - approxTokens(excerpt)
}

func approxTokens(input string) int {
	count := 0
	inToken := false
	for _, r := range input {
		if unicode.IsSpace(r) {
			inToken = false
			continue
		}
		if !inToken {
			count++
			inToken = true
		}
	}
	return count
}

func truncateToTokens(input string, allowedTokens int) (string, bool) {
	if allowedTokens < 1 {
		allowedTokens = 1
	}
	if approxTokens(input) <= allowedTokens {
		return input, false
	}

	count := 0
	inToken := false
	keepEnd := 0

	for idx, r := range input {
		if unicode.IsSpace(r) {
			if count <= allowedTokens {
				keepEnd = idx + utf8.RuneLen(r)
			}
			inToken = false
			continue
		}
		if !inToken {
			count++
			inToken = true
			if count > allowedTokens {
				return strings.TrimRightFunc(input[:keepEnd], unicode.IsSpace), true
			}
		}
		if count <= allowedTokens {
			keepEnd = idx + utf8.RuneLen(r)
		}
	}

	return input, false
}

func addNote(notes *[]string, note string) {
	for _, existing := range *notes {
		if existing == note {
			return
		}
	}
	*notes = append(*notes, note)
}

func upsertFieldNote(notes *[]string, label string, tokens int) {
	filtered := (*notes)[:0]
	for _, note := range *notes {
		if strings.HasPrefix(note, "["+label+" truncado") {
			continue
		}
		filtered = append(filtered, note)
	}
	*notes = filtered
	*notes = append(*notes, lastFieldNote(label, tokens))
}

func stringPtr(value string) *string {
	return &value
}
