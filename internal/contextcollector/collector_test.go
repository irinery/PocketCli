package contextcollector

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestCollectBlocksEnvFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".env", "VAR=valor\n")
	writeFile(t, dir, "safe.txt", "ok\n")

	ctx, err := Collect(dir, Session{})
	if err != nil {
		t.Fatalf("Collect returned error: %v", err)
	}

	if containsMainFile(ctx.Project.MainFiles, ".env") {
		t.Fatal("expected .env to be blocked from main_files")
	}

	flat := flattenContext(ctx)
	if strings.Contains(flat, ".env") {
		t.Fatal("expected blocked filename to be absent from context")
	}
	if strings.Contains(flat, "VAR=valor") {
		t.Fatal("expected blocked file content to be absent from context")
	}
}

func TestCollectBlocksSensitiveFilenames(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "id_rsa", "private\n")
	writeFile(t, dir, "arquivo.pem", "pem-data\n")
	writeFile(t, dir, "credentials.json", "{\"token\":\"x\"}\n")
	writeFile(t, dir, "visible.txt", "hello\n")

	ctx, err := Collect(dir, Session{})
	if err != nil {
		t.Fatalf("Collect returned error: %v", err)
	}

	for _, name := range []string{"id_rsa", "arquivo.pem", "credentials.json"} {
		if containsMainFile(ctx.Project.MainFiles, name) {
			t.Fatalf("expected %q to be blocked", name)
		}
	}
	if !containsMainFile(ctx.Project.MainFiles, "visible.txt") {
		t.Fatal("expected non-blocked file to remain available")
	}
}

func TestCollectSanitizesSensitiveLinesButKeepsFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "config.txt", "hello\npassword=abc123\nbye\n")

	ctx, err := Collect(dir, Session{})
	if err != nil {
		t.Fatalf("Collect returned error: %v", err)
	}

	file := mustFindMainFile(t, ctx.Project.MainFiles, "config.txt")
	if strings.Contains(file.Excerpt, "password=abc123") {
		t.Fatal("expected sensitive line to be removed from excerpt")
	}
	if !strings.Contains(file.Excerpt, "hello") || !strings.Contains(file.Excerpt, "bye") {
		t.Fatal("expected non-sensitive lines to remain in excerpt")
	}
}

func TestCollectIgnoresBlockedFilesEnvOverride(t *testing.T) {
	t.Setenv("BLOCKED_FILES", "safe.txt")

	dir := t.TempDir()
	writeFile(t, dir, ".env", "VAR=valor\n")
	writeFile(t, dir, "safe.txt", "still-visible\n")

	ctx, err := Collect(dir, Session{})
	if err != nil {
		t.Fatalf("Collect returned error: %v", err)
	}

	if containsMainFile(ctx.Project.MainFiles, ".env") {
		t.Fatal("expected hardcoded blocked patterns to remain active")
	}
	if !containsMainFile(ctx.Project.MainFiles, "safe.txt") {
		t.Fatal("expected env override to be ignored")
	}
}

func TestCollectTruncatesMainFilesAt100Lines(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "large.txt", numberedLines("line", 500))

	ctx, err := Collect(dir, Session{})
	if err != nil {
		t.Fatalf("Collect returned error: %v", err)
	}

	file := mustFindMainFile(t, ctx.Project.MainFiles, "large.txt")
	if !strings.Contains(file.Excerpt, "[conteúdo truncado]") {
		t.Fatal("expected truncation note in main file excerpt")
	}
	if !strings.Contains(file.Excerpt, "line-100") {
		t.Fatal("expected line 100 to be preserved")
	}
	if strings.Contains(file.Excerpt, "line-101") {
		t.Fatal("expected excerpt to stop at line 100")
	}
}

func TestCollectTruncatesReadmeAt50Lines(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "README.md", numberedLines("readme", 200))

	ctx, err := Collect(dir, Session{})
	if err != nil {
		t.Fatalf("Collect returned error: %v", err)
	}

	if ctx.Project.ReadmeExcerpt == nil {
		t.Fatal("expected readme excerpt to be collected")
	}
	if !strings.Contains(*ctx.Project.ReadmeExcerpt, "readme-50") {
		t.Fatal("expected readme excerpt to keep line 50")
	}
	if strings.Contains(*ctx.Project.ReadmeExcerpt, "readme-51") {
		t.Fatal("expected readme excerpt to stop at line 50")
	}
}

func TestCollectCompressesToTokenLimitAndMarksPartial(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 18; i++ {
		name := filepath.Join("src", "file-"+strconv.Itoa(i)+".txt")
		writeFile(t, dir, name, repeatedTokenLines("token", 100, 20))
	}
	writeFile(t, dir, "README.md", repeatedTokenLines("readme", 80, 15))

	ctx, err := Collect(dir, Session{LastCommand: "pocket analyze"})
	if err != nil {
		t.Fatalf("Collect returned error: %v", err)
	}

	if totalTokenCount(ctx) > MaxContextTokens {
		t.Fatalf("expected collected context to fit within %d tokens, got %d", MaxContextTokens, totalTokenCount(ctx))
	}
	if !containsNote(ctx.Notes, "[contexto parcial — itens omitidos]") {
		t.Fatal("expected partial-context note when compression omits content")
	}
}

func TestCollectTruncatesOversizedLastErrorByTokens(t *testing.T) {
	dir := t.TempDir()
	lastError := indexedWords("frame", 6000)

	ctx, err := Collect(dir, Session{LastError: lastError})
	if err != nil {
		t.Fatalf("Collect returned error: %v", err)
	}

	if ctx.Session.LastError == nil || *ctx.Session.LastError == "" {
		t.Fatal("expected oversized last_error to remain present")
	}
	if !strings.HasPrefix(*ctx.Session.LastError, "frame-1 frame-2") {
		t.Fatal("expected oversized last_error to preserve the beginning of the stack trace")
	}
	if totalTokenCount(ctx) > MaxContextTokens {
		t.Fatalf("expected compressed context to fit within limit, got %d", totalTokenCount(ctx))
	}
	if !containsNotePrefix(ctx.Notes, "[last_error truncado") {
		t.Fatal("expected truncation note for last_error")
	}
}

func TestCollectCapturesGitBranch(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	writeFile(t, dir, "staged-a.txt", "alpha\n")
	writeFile(t, dir, "staged-b.txt", "beta\n")
	runGit(t, dir, "add", "staged-a.txt", "staged-b.txt")

	ctx, err := Collect(dir, Session{})
	if err != nil {
		t.Fatalf("Collect returned error: %v", err)
	}

	if !ctx.Project.IsGit {
		t.Fatal("expected git repository to be detected")
	}
	if ctx.Project.Branch == nil || *ctx.Project.Branch != "main" {
		t.Fatalf("expected branch main, got %v", ctx.Project.Branch)
	}
}

func TestCollectGracefullyHandlesNonGitDir(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "notes.txt", "hello\n")

	ctx, err := Collect(dir, Session{})
	if err != nil {
		t.Fatalf("Collect returned error: %v", err)
	}

	if ctx.Project.IsGit {
		t.Fatal("expected non-git directory to report is_git=false")
	}
	if ctx.Project.Branch != nil {
		t.Fatal("expected non-git directory to report nil branch")
	}
}

func TestCollectKeepsSmallLastErrorUntouched(t *testing.T) {
	dir := t.TempDir()
	lastError := "small stack trace"

	ctx, err := Collect(dir, Session{LastError: lastError})
	if err != nil {
		t.Fatalf("Collect returned error: %v", err)
	}

	if ctx.Session.LastError == nil {
		t.Fatal("expected last_error to be present")
	}
	if *ctx.Session.LastError != lastError {
		t.Fatalf("expected last_error to remain untouched, got %q", *ctx.Session.LastError)
	}
}

func writeFile(t *testing.T, dir, relPath, content string) {
	t.Helper()

	fullPath := filepath.Join(dir, relPath)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
}

func containsMainFile(files []MainFile, name string) bool {
	for _, file := range files {
		if file.Path == name {
			return true
		}
	}
	return false
}

func mustFindMainFile(t *testing.T, files []MainFile, name string) MainFile {
	t.Helper()
	for _, file := range files {
		if file.Path == name {
			return file
		}
	}
	t.Fatalf("main file %q not found", name)
	return MainFile{}
}

func containsNote(notes []string, want string) bool {
	for _, note := range notes {
		if note == want {
			return true
		}
	}
	return false
}

func containsNotePrefix(notes []string, prefix string) bool {
	for _, note := range notes {
		if strings.HasPrefix(note, prefix) {
			return true
		}
	}
	return false
}

func flattenContext(ctx TaskContext) string {
	var parts []string
	parts = append(parts, ctx.Project.Path)
	if ctx.Project.Branch != nil {
		parts = append(parts, *ctx.Project.Branch)
	}
	if ctx.Project.ReadmeExcerpt != nil {
		parts = append(parts, *ctx.Project.ReadmeExcerpt)
	}
	if ctx.Project.GitDiffHead != nil {
		parts = append(parts, *ctx.Project.GitDiffHead)
	}
	if ctx.Project.GitLog != nil {
		parts = append(parts, *ctx.Project.GitLog)
	}
	if ctx.Session.LastCommand != nil {
		parts = append(parts, *ctx.Session.LastCommand)
	}
	if ctx.Session.LastError != nil {
		parts = append(parts, *ctx.Session.LastError)
	}
	parts = append(parts, ctx.Notes...)
	for _, file := range ctx.Project.MainFiles {
		parts = append(parts, file.Path, file.Excerpt)
	}
	return strings.Join(parts, "\n")
}

func numberedLines(prefix string, count int) string {
	lines := make([]string, 0, count)
	for i := 1; i <= count; i++ {
		lines = append(lines, prefix+"-"+strconv.Itoa(i))
	}
	return strings.Join(lines, "\n") + "\n"
}

func indexedWords(prefix string, count int) string {
	words := make([]string, 0, count)
	for i := 1; i <= count; i++ {
		words = append(words, prefix+"-"+strconv.Itoa(i))
	}
	return strings.Join(words, " ")
}

func repeatedTokenLines(prefix string, lines int, tokensPerLine int) string {
	rows := make([]string, 0, lines)
	for i := 0; i < lines; i++ {
		row := make([]string, 0, tokensPerLine)
		for j := 0; j < tokensPerLine; j++ {
			row = append(row, prefix+"-"+strconv.Itoa(i)+"-"+strconv.Itoa(j))
		}
		rows = append(rows, strings.Join(row, " "))
	}
	return strings.Join(rows, "\n") + "\n"
}

func initGitRepo(t *testing.T, dir string) {
	t.Helper()

	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "PocketCli Tests")
	writeFile(t, dir, "tracked.txt", "base\n")
	runGit(t, dir, "add", "tracked.txt")
	runGit(t, dir, "commit", "-m", "init")
	runGit(t, dir, "branch", "-M", "main")
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, output)
	}
}
