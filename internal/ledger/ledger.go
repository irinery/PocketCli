package ledger

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"pocketcli/internal/pocketpath"
)

const (
	SchemaVersion = 1
	MaxPayloadKB  = 32
	DefaultLimit  = 50
	MaxLimit      = 500

	EventCommandStarted   = "command.started"
	EventCommandCompleted = "command.completed"
	EventCommandFailed    = "command.failed"
	EventTUIAction        = "tui.action"
	EventSSHProbe         = "ssh.probe"
	EventSSHExec          = "ssh.exec"
	EventUpdateStarted    = "update.started"
	EventUpdateCompleted  = "update.completed"
	EventContextCollected = "context.collected"
	EventBackendCall      = "backend.call"
	EventMemoryHit        = "memory.hit"
	EventSafetyEnvelope   = "safety.envelope_created"
	EventSafetyDenied     = "safety.denied"
)

var (
	ErrInvalidEvent = errors.New("ERR_LEDGER_INVALID_EVENT")
	ErrBadFilter    = errors.New("ERR_LEDGER_BAD_FILTER")

	authorizationPattern = regexp.MustCompile(`(?i)Authorization:\s*Bearer\s+\S+`)
	secretPattern        = regexp.MustCompile(`(?i)\b(password|token|secret|key)\s*[:=]\s*\S+`)
)

type Event struct {
	SchemaVersion int     `json:"schema_version"`
	EventID       string  `json:"event_id"`
	Timestamp     string  `json:"timestamp"`
	SessionID     string  `json:"session_id"`
	Type          string  `json:"type"`
	Command       string  `json:"command,omitempty"`
	HostID        string  `json:"host_id,omitempty"`
	Status        string  `json:"status"`
	DurationMS    int     `json:"duration_ms,omitempty"`
	Payload       Payload `json:"payload"`
}

type Payload struct {
	Message        string `json:"message,omitempty"`
	ErrorCode      string `json:"error_code,omitempty"`
	Backend        string `json:"backend,omitempty"`
	Model          string `json:"model,omitempty"`
	TokenUsage     int    `json:"token_usage,omitempty"`
	RedactionCount int    `json:"redaction_count"`
}

type AppendResult struct {
	Path     string `json:"path"`
	Offset   int64  `json:"offset"`
	Redacted bool   `json:"redacted"`
}

type SearchFilter struct {
	SessionID string
	HostID    string
	Since     string
	Limit     int
}

type SearchResult struct {
	Events      []Event `json:"events"`
	Truncated   bool    `json:"truncated"`
	IndexStatus string  `json:"index_status"`
	Partial     bool    `json:"partial,omitempty"`
}

type RebuildResult struct {
	EventsIndexed int `json:"events_indexed"`
	SkippedLines  int `json:"skipped_lines"`
}

type Store struct {
	baseDir string
	now     func() time.Time
}

type sessionIndex struct {
	SchemaVersion int                         `json:"schema_version"`
	GeneratedAt   string                      `json:"generated_at"`
	Sessions      map[string]sessionIndexItem `json:"sessions"`
}

type sessionIndexItem struct {
	EventCount int    `json:"event_count"`
	FirstSeen  string `json:"first_seen"`
	LastSeen   string `json:"last_seen"`
}

func NewStore() (*Store, error) {
	dataDir, err := pocketpath.EnsureDataDir()
	if err != nil {
		return nil, err
	}
	return NewStoreAt(dataDir), nil
}

func NewStoreAt(baseDir string) *Store {
	return &Store{
		baseDir: baseDir,
		now:     func() time.Time { return time.Now().UTC() },
	}
}

func (s *Store) Append(event Event) (AppendResult, error) {
	normalized, redacted, err := s.normalizeEvent(event)
	if err != nil {
		return AppendResult{}, err
	}
	path, err := s.activeEventPath(normalized.Timestamp)
	if err != nil {
		return AppendResult{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return AppendResult{}, err
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return AppendResult{}, err
	}
	defer file.Close()

	offset, err := file.Seek(0, 2)
	if err != nil {
		return AppendResult{}, err
	}

	data, err := json.Marshal(normalized)
	if err != nil {
		return AppendResult{}, err
	}
	if len(data) > MaxPayloadKB*1024 {
		normalized.Payload.Message = truncateRunes(normalized.Payload.Message, 4096)
		normalized.Payload.RedactionCount++
		redacted = true
		data, err = json.Marshal(normalized)
		if err != nil {
			return AppendResult{}, err
		}
	}

	if _, err := file.Write(append(data, '\n')); err != nil {
		return AppendResult{}, err
	}

	return AppendResult{Path: path, Offset: offset, Redacted: redacted}, nil
}

func (s *Store) Search(filter SearchFilter) (SearchResult, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	if limit < 1 || limit > MaxLimit {
		return SearchResult{}, fmt.Errorf("%w: limit", ErrBadFilter)
	}

	since, err := parseSince(filter.Since, s.now().AddDate(0, 0, -7))
	if err != nil {
		return SearchResult{}, err
	}

	indexStatus := "ok"
	if _, err := os.Stat(s.indexPath()); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			indexStatus = "missing"
		} else {
			indexStatus = "corrupt"
		}
	}

	events, skipped, err := s.readEventsSince(since)
	if err != nil {
		return SearchResult{}, err
	}

	matched := make([]Event, 0, minInt(limit, len(events)))
	for _, event := range events {
		if strings.TrimSpace(filter.SessionID) != "" && event.SessionID != strings.TrimSpace(filter.SessionID) {
			continue
		}
		if strings.TrimSpace(filter.HostID) != "" && event.HostID != strings.TrimSpace(filter.HostID) {
			continue
		}
		matched = append(matched, event)
		if len(matched) >= limit {
			break
		}
	}

	truncated := len(matched) >= limit
	return SearchResult{
		Events:      matched,
		Truncated:   truncated,
		IndexStatus: indexStatus,
		Partial:     skipped > 0,
	}, nil
}

func (s *Store) RebuildIndex() (RebuildResult, error) {
	events, skipped, err := s.readAllEvents()
	if err != nil {
		return RebuildResult{}, err
	}

	idx := sessionIndex{
		SchemaVersion: SchemaVersion,
		GeneratedAt:   s.now().Format(time.RFC3339),
		Sessions:      map[string]sessionIndexItem{},
	}
	for _, event := range events {
		if strings.TrimSpace(event.SessionID) == "" {
			continue
		}
		item := idx.Sessions[event.SessionID]
		item.EventCount++
		if item.FirstSeen == "" || event.Timestamp < item.FirstSeen {
			item.FirstSeen = event.Timestamp
		}
		if item.LastSeen == "" || event.Timestamp > item.LastSeen {
			item.LastSeen = event.Timestamp
		}
		idx.Sessions[event.SessionID] = item
	}

	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return RebuildResult{}, err
	}
	if err := pocketpath.AtomicWrite(s.indexPath(), append(data, '\n'), 0o644); err != nil {
		return RebuildResult{}, err
	}
	return RebuildResult{EventsIndexed: len(events), SkippedLines: skipped}, nil
}

func (s *Store) normalizeEvent(event Event) (Event, bool, error) {
	if event.SchemaVersion == 0 {
		event.SchemaVersion = SchemaVersion
	}
	if event.SchemaVersion != SchemaVersion {
		return Event{}, false, ErrInvalidEvent
	}
	if strings.TrimSpace(event.EventID) == "" {
		event.EventID = newID()
	}
	if strings.TrimSpace(event.Timestamp) == "" {
		event.Timestamp = s.now().Format(time.RFC3339)
	}
	if _, err := time.Parse(time.RFC3339, event.Timestamp); err != nil {
		return Event{}, false, fmt.Errorf("%w: timestamp", ErrInvalidEvent)
	}
	if strings.TrimSpace(event.SessionID) == "" {
		event.SessionID = newID()
	}
	event.Type = strings.TrimSpace(event.Type)
	if event.Type == "" {
		return Event{}, false, fmt.Errorf("%w: type", ErrInvalidEvent)
	}
	event.Command = truncateRunes(strings.TrimSpace(event.Command), 128)
	event.HostID = truncateRunes(strings.TrimSpace(event.HostID), 128)
	event.Status = strings.TrimSpace(event.Status)
	if event.Status == "" {
		event.Status = "ok"
	}
	if event.DurationMS < 0 {
		event.DurationMS = 0
	}
	if event.Payload.TokenUsage < 0 {
		event.Payload.TokenUsage = 0
	}

	redactionCount := 0
	event.Payload.Message, redactionCount = RedactText(event.Payload.Message)
	event.Payload.ErrorCode = truncateRunes(strings.TrimSpace(event.Payload.ErrorCode), 80)
	event.Payload.Backend = truncateRunes(strings.TrimSpace(event.Payload.Backend), 80)
	event.Payload.Model = truncateRunes(strings.TrimSpace(event.Payload.Model), 120)
	event.Payload.RedactionCount += redactionCount

	if containsSensitivePath(event.Payload.Message) {
		event.Payload.Message = "[REDACTED]"
		event.Payload.RedactionCount++
	}

	return event, event.Payload.RedactionCount > 0, nil
}

func RedactText(value string) (string, int) {
	count := 0
	value = authorizationPattern.ReplaceAllStringFunc(value, func(match string) string {
		count++
		return "Authorization: Bearer [REDACTED]"
	})
	value = secretPattern.ReplaceAllStringFunc(value, func(match string) string {
		count++
		key := strings.SplitN(strings.NewReplacer(":", "=", " ", "").Replace(match), "=", 2)[0]
		key = strings.TrimSpace(key)
		if key == "" {
			key = "secret"
		}
		return key + "=[REDACTED]"
	})
	return value, count
}

func (s *Store) readAllEvents() ([]Event, int, error) {
	return s.readEventsSince(time.Time{})
}

func (s *Store) readEventsSince(since time.Time) ([]Event, int, error) {
	paths, err := filepath.Glob(filepath.Join(s.eventsDir(), "*.jsonl"))
	if err != nil {
		return nil, 0, err
	}
	sort.Sort(sort.Reverse(sort.StringSlice(paths)))

	var events []Event
	skipped := 0
	for _, path := range paths {
		if !since.IsZero() {
			day := strings.TrimSuffix(filepath.Base(path), ".jsonl")
			if len(day) >= len("2006-01-02") && day[:10] < since.Format("2006-01-02") {
				continue
			}
		}
		file, err := os.Open(path)
		if err != nil {
			return nil, skipped, err
		}
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			var event Event
			if err := json.Unmarshal([]byte(line), &event); err != nil {
				skipped++
				continue
			}
			if !since.IsZero() {
				timestamp, err := time.Parse(time.RFC3339, event.Timestamp)
				if err != nil {
					skipped++
					continue
				}
				if timestamp.Before(since) {
					continue
				}
			}
			events = append(events, event)
		}
		if err := scanner.Err(); err != nil {
			_ = file.Close()
			return nil, skipped, err
		}
		_ = file.Close()
	}
	sort.SliceStable(events, func(i, j int) bool {
		return events[i].Timestamp > events[j].Timestamp
	})
	return events, skipped, nil
}

func (s *Store) activeEventPath(timestamp string) (string, error) {
	parsed, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		return "", err
	}
	return filepath.Join(s.eventsDir(), parsed.UTC().Format("2006-01-02")+".jsonl"), nil
}

func (s *Store) eventsDir() string {
	return filepath.Join(s.baseDir, "ledger")
}

func (s *Store) indexPath() string {
	return filepath.Join(s.baseDir, "session-index.json")
}

func parseSince(value string, fallback time.Time) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback.UTC(), nil
	}
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse("2006-01-02", value); err == nil {
		return t.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("%w: since", ErrBadFilter)
}

func containsSensitivePath(value string) bool {
	lower := strings.ToLower(value)
	blocked := []string{"/.ssh/", "\\.ssh\\", "/.aws/", "/.kube/", ".env", ".pem", ".key", ".p12", ".pfx"}
	for _, token := range blocked {
		if strings.Contains(lower, token) {
			return true
		}
	}
	return false
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

func truncateRunes(value string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= max {
		return string(runes)
	}
	return string(runes[:max])
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
