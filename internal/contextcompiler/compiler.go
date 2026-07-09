package contextcompiler

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"pocketcli/internal/capabilities"
	"pocketcli/internal/contextcollector"
	"pocketcli/internal/insights"
	"pocketcli/internal/safety"
)

const MaxContextTokens = 4000

var attachmentSecretPattern = regexp.MustCompile(`(?i)(password|token|secret|api[_-]?key|authorization)\s*[:=]\s*\S+`)

type Request struct {
	UserInput   string
	Host        string
	Attachments []string
	TaskContext contextcollector.TaskContext
}

type CompiledContext struct {
	SchemaVersion int              `json:"schema_version"`
	RequestID     string           `json:"request_id"`
	Sections      []ContextSection `json:"sections"`
	TokenEstimate int              `json:"token_estimate"`
	Truncated     bool             `json:"truncated"`
	Partial       bool             `json:"partial"`
}

type ContextSection struct {
	Name       string   `json:"name"`
	Priority   int      `json:"priority"`
	Content    string   `json:"content"`
	Provenance []string `json:"provenance"`
}

func Compile(request Request) (CompiledContext, error) {
	if len(request.Attachments) > 8 {
		return CompiledContext{}, fmt.Errorf("ERR_CONTEXT_TOO_MANY_ATTACHMENTS")
	}

	compiled := CompiledContext{
		SchemaVersion: 1,
		RequestID:     newID(),
		Sections:      []ContextSection{},
	}

	manifest := capabilities.LoadOrDetect()
	compiled.Sections = append(compiled.Sections, ContextSection{
		Name:       "capabilities",
		Priority:   90,
		Content:    fmt.Sprintf("mode_effective=%s has_ssh=%t has_tailscale=%t tui_layout=%s", manifest.ModeEffective, manifest.Capabilities.HasSSH, manifest.Capabilities.HasTailscale, manifest.Terminal.TUILayout),
		Provenance: []string{"capabilities"},
	})

	if strings.TrimSpace(request.UserInput) != "" {
		content := strings.TrimSpace(request.UserInput)
		if len([]rune(content)) > 4000 {
			content = string([]rune(content)[:4000])
			compiled.Truncated = true
		}
		compiled.Sections = append(compiled.Sections, ContextSection{Name: "request", Priority: 100, Content: content, Provenance: []string{"user_input"}})
	}

	project := request.TaskContext.Project
	var projectLines []string
	if project.Path != "" {
		projectLines = append(projectLines, "path="+project.Path)
	}
	projectLines = append(projectLines, fmt.Sprintf("is_git=%t", project.IsGit))
	if project.Branch != nil {
		projectLines = append(projectLines, "branch="+strings.TrimSpace(*project.Branch))
	}
	if len(project.MainFiles) > 0 {
		projectLines = append(projectLines, fmt.Sprintf("main_files=%d", len(project.MainFiles)))
	}
	if len(projectLines) > 0 {
		compiled.Sections = append(compiled.Sections, ContextSection{Name: "project", Priority: 80, Content: strings.Join(projectLines, "\n"), Provenance: []string{"contextcollector.project"}})
	}

	var gitLines []string
	if project.GitDiffHead != nil && strings.TrimSpace(*project.GitDiffHead) != "" {
		gitLines = append(gitLines, "diff:\n"+strings.TrimSpace(*project.GitDiffHead))
	}
	if project.GitLog != nil && strings.TrimSpace(*project.GitLog) != "" {
		gitLines = append(gitLines, "log:\n"+strings.TrimSpace(*project.GitLog))
	}
	if len(gitLines) > 0 {
		compiled.Sections = append(compiled.Sections, ContextSection{Name: "git", Priority: 75, Content: strings.Join(gitLines, "\n\n"), Provenance: []string{"contextcollector.git"}})
	}

	if len(request.TaskContext.MemoryHits) > 0 {
		compiled.Sections = append(compiled.Sections, ContextSection{Name: "memory", Priority: 65, Content: strings.Join(request.TaskContext.MemoryHits, "\n"), Provenance: []string{"memory.retrieve"}})
	}

	if insightList, err := insights.Compute(insights.Request{Scope: "active", HostID: request.Host, TimeWindowMinutes: 1440}); err == nil && len(insightList.Insights) > 0 {
		lines := make([]string, 0, len(insightList.Insights))
		for _, item := range insightList.Insights {
			lines = append(lines, fmt.Sprintf("%s severity=%s action=%s", item.Title, item.Severity, item.RecommendedAction))
			if len(lines) >= 5 {
				break
			}
		}
		compiled.Sections = append(compiled.Sections, ContextSection{Name: "insights", Priority: 70, Content: strings.Join(lines, "\n"), Provenance: []string{"insights.active"}})
	}

	if strings.TrimSpace(request.Host) != "" {
		compiled.Sections = append(compiled.Sections, ContextSection{Name: "host", Priority: 70, Content: "host=" + strings.TrimSpace(request.Host), Provenance: []string{"request.host"}})
	}

	attachments, err := compileAttachments(request.Attachments)
	if err != nil {
		return CompiledContext{}, err
	}
	if strings.TrimSpace(attachments) != "" {
		compiled.Sections = append(compiled.Sections, ContextSection{Name: "attachments", Priority: 60, Content: attachments, Provenance: []string{"request.attachments"}})
	}

	sort.SliceStable(compiled.Sections, func(i, j int) bool {
		return compiled.Sections[i].Priority > compiled.Sections[j].Priority
	})
	compiled.TokenEstimate = tokenEstimate(compiled.Sections)
	for compiled.TokenEstimate > MaxContextTokens && len(compiled.Sections) > 0 {
		compiled.Truncated = true
		last := len(compiled.Sections) - 1
		if len([]rune(compiled.Sections[last].Content)) > 1000 {
			compiled.Sections[last].Content = string([]rune(compiled.Sections[last].Content)[:1000])
		} else {
			compiled.Sections = compiled.Sections[:last]
		}
		compiled.TokenEstimate = tokenEstimate(compiled.Sections)
	}
	return compiled, nil
}

func compileAttachments(paths []string) (string, error) {
	if len(paths) == 0 {
		return "", nil
	}
	var blocks []string
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		resolvedPath, err := filepath.EvalSymlinks(path)
		if err != nil {
			return "", err
		}
		if safety.SensitivePath(resolvedPath) {
			return "", fmt.Errorf("ERR_CONTEXT_ATTACHMENT_BLOCKED: %s", path)
		}
		info, err := os.Stat(resolvedPath)
		if err != nil {
			return "", err
		}
		if info.IsDir() {
			return "", fmt.Errorf("ERR_CONTEXT_ATTACHMENT_BLOCKED: directory %s", path)
		}
		if info.Size() > 128*1024 {
			return "", fmt.Errorf("ERR_CONTEXT_ATTACHMENT_TOO_LARGE: %s", path)
		}
		data, err := os.ReadFile(resolvedPath)
		if err != nil {
			return "", err
		}
		if attachmentSecretPattern.Match(data) {
			return "", fmt.Errorf("ERR_CONTEXT_ATTACHMENT_BLOCKED: conteúdo sensível em %s", path)
		}
		content := strings.TrimSpace(string(data))
		blocks = append(blocks, "# "+filepath.Base(path)+"\n"+content)
	}
	return strings.Join(blocks, "\n\n---\n\n"), nil
}

func tokenEstimate(sections []ContextSection) int {
	total := 0
	for _, section := range sections {
		total += len(strings.Fields(section.Content))
		total += 2
	}
	return total
}

func newID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	dst := make([]byte, 36)
	hex.Encode(dst[0:8], raw[0:4])
	dst[8] = '-'
	hex.Encode(dst[9:13], raw[4:6])
	dst[13] = '-'
	hex.Encode(dst[14:18], raw[6:8])
	dst[18] = '-'
	hex.Encode(dst[19:23], raw[8:10])
	dst[23] = '-'
	hex.Encode(dst[24:36], raw[10:16])
	return string(dst)
}
