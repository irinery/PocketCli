package memory

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"pocketcli/internal/pocketpath"
)

const (
	KindIncident = "incident"
	KindDecision = "decision"
	KindPattern  = "pattern"

	initialSaveConfidence = 0.9
	defaultTag            = "ask"
)

var (
	ErrEntryNotFound       = errors.New("entrada não encontrada")
	ErrInvalidKind         = errors.New("kind inválido")
	ErrInvalidScope        = errors.New("scope inválido")
	ErrNoRecentInteraction = errors.New("nenhuma interação recente para salvar")
)

type Entry struct {
	ID           string   `json:"id"`
	Kind         string   `json:"kind"`
	Scope        string   `json:"scope"`
	Title        string   `json:"title"`
	Summary      string   `json:"summary"`
	Body         string   `json:"body"`
	Tags         []string `json:"tags"`
	Confidence   float64  `json:"confidence"`
	CreatedAt    string   `json:"created_at"`
	LastAccessed string   `json:"last_accessed"`
	AccessCount  int      `json:"access_count"`
	ExpiresAt    *string  `json:"expires_at"`
}

type LastInteraction struct {
	SessionID  string   `json:"session_id"`
	Kind       string   `json:"kind"`
	Scope      string   `json:"scope"`
	Title      string   `json:"title"`
	Summary    string   `json:"summary"`
	Body       string   `json:"body"`
	Tags       []string `json:"tags"`
	RecordedAt string   `json:"recorded_at"`
}

type AskInput struct {
	Prompt    string
	SessionID string
	Kind      string
	Scope     string
	Title     string
	Tags      []string
}

type sessionState struct {
	LastSessionID string          `json:"last_session_id"`
	Interaction   LastInteraction `json:"interaction"`
}

type Store struct {
	baseDir string
	now     func() time.Time
	newID   func() (string, error)
}

func NewStore() (*Store, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return NewStoreAt(filepath.Join(home, ".pocket")), nil
}

func NewStoreAt(baseDir string) *Store {
	return &Store{
		baseDir: baseDir,
		now:     func() time.Time { return time.Now().UTC() },
		newID:   newUUIDv4,
	}
}

func (s *Store) RecordAsk(input AskInput) (LastInteraction, error) {
	body := strings.TrimSpace(input.Prompt)
	if body == "" {
		return LastInteraction{}, errors.New("nenhuma pergunta informada")
	}

	kind := input.Kind
	if strings.TrimSpace(kind) == "" {
		kind = KindPattern
	}
	if err := ValidateKind(kind); err != nil {
		return LastInteraction{}, err
	}

	scope := input.Scope
	if strings.TrimSpace(scope) == "" {
		scope = "global"
	}
	scope, err := normalizeScope(scope)
	if err != nil {
		return LastInteraction{}, err
	}

	summary := compactWhitespace(body)
	title := strings.TrimSpace(input.Title)
	if title == "" {
		title = truncateRunes(summary, 80)
	}

	tags := normalizeTags(input.Tags)
	if len(tags) == 0 {
		tags = []string{defaultTag}
	}

	sessionID := strings.TrimSpace(input.SessionID)
	if sessionID == "" {
		sessionID, err = s.newID()
		if err != nil {
			return LastInteraction{}, err
		}
	}

	interaction := LastInteraction{
		SessionID:  sessionID,
		Kind:       normalizeKind(kind),
		Scope:      scope,
		Title:      title,
		Summary:    summary,
		Body:       body,
		Tags:       tags,
		RecordedAt: s.timestamp(),
	}

	if err := s.RememberInteraction(interaction); err != nil {
		return LastInteraction{}, err
	}

	return interaction, nil
}

func (s *Store) RememberInteraction(interaction LastInteraction) error {
	normalized, err := s.validateInteraction(interaction)
	if err != nil {
		return err
	}

	state := sessionState{
		LastSessionID: normalized.SessionID,
		Interaction:   normalized,
	}

	data, err := json.Marshal(state)
	if err != nil {
		return err
	}

	return pocketpath.AtomicWrite(s.lastInteractionPath(), append(data, '\n'), 0o600)
}

func (s *Store) LoadLastInteraction() (LastInteraction, error) {
	data, err := os.ReadFile(s.lastInteractionPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return LastInteraction{}, ErrNoRecentInteraction
		}
		return LastInteraction{}, err
	}

	var state sessionState
	if err := json.Unmarshal(data, &state); err != nil {
		return LastInteraction{}, err
	}

	if strings.TrimSpace(state.LastSessionID) == "" {
		return LastInteraction{}, ErrNoRecentInteraction
	}

	interaction, err := s.validateInteraction(state.Interaction)
	if err != nil {
		return LastInteraction{}, err
	}

	return interaction, nil
}

func (s *Store) SaveFromLastInteraction() (Entry, error) {
	interaction, err := s.LoadLastInteraction()
	if err != nil {
		return Entry{}, err
	}

	return s.Write(Entry{
		Kind:       interaction.Kind,
		Scope:      interaction.Scope,
		Title:      interaction.Title,
		Summary:    interaction.Summary,
		Body:       interaction.Body,
		Tags:       append([]string(nil), interaction.Tags...),
		Confidence: initialSaveConfidence,
	})
}

func (s *Store) Write(entry Entry) (Entry, error) {
	normalized, err := s.prepareNewEntry(entry)
	if err != nil {
		return Entry{}, err
	}

	path, err := s.scopeFilePath(normalized.Scope)
	if err != nil {
		return Entry{}, err
	}

	entries, err := s.loadEntries(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Entry{}, err
	}

	for _, existing := range entries {
		if existing.ID == normalized.ID {
			return Entry{}, fmt.Errorf("id já existente: %s", normalized.ID)
		}
	}

	entries = append(entries, normalized)
	if err := s.writeEntries(path, entries); err != nil {
		return Entry{}, err
	}

	return normalized, nil
}

func (s *Store) UpdateConfidence(id string, delta float64) (Entry, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Entry{}, errors.New("id obrigatório")
	}

	dirEntries, err := os.ReadDir(s.memoryDir())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Entry{}, fmt.Errorf("%w: %s", ErrEntryNotFound, id)
		}
		return Entry{}, err
	}

	updatedAt := s.timestamp()
	for _, dirEntry := range dirEntries {
		if dirEntry.IsDir() || !strings.HasSuffix(dirEntry.Name(), ".jsonl") {
			continue
		}

		path := filepath.Join(s.memoryDir(), dirEntry.Name())
		entries, err := s.loadEntries(path)
		if err != nil {
			return Entry{}, err
		}

		for idx := range entries {
			if entries[idx].ID != id {
				continue
			}

			entries[idx].Confidence = roundConfidence(entries[idx].Confidence + delta)
			entries[idx].LastAccessed = updatedAt

			if err := s.writeEntries(path, entries); err != nil {
				return Entry{}, err
			}

			return entries[idx], nil
		}
	}

	return Entry{}, fmt.Errorf("%w: %s", ErrEntryNotFound, id)
}

func ValidateKind(kind string) error {
	switch normalizeKind(kind) {
	case KindIncident, KindDecision, KindPattern:
		return nil
	default:
		return fmt.Errorf("%w: %s", ErrInvalidKind, strings.TrimSpace(kind))
	}
}

func DefaultScopeFromCWD(cwd string) string {
	base := filepath.Base(strings.TrimSpace(cwd))
	slug := slugify(base)
	if slug == "" || slug == "." {
		return "global"
	}
	return "project:" + slug
}

func (s *Store) prepareNewEntry(entry Entry) (Entry, error) {
	normalized := Entry{
		ID:           strings.TrimSpace(entry.ID),
		Kind:         normalizeKind(entry.Kind),
		Scope:        strings.TrimSpace(entry.Scope),
		Title:        strings.TrimSpace(entry.Title),
		Summary:      strings.TrimSpace(entry.Summary),
		Body:         strings.TrimSpace(entry.Body),
		Tags:         normalizeTags(entry.Tags),
		Confidence:   roundConfidence(entry.Confidence),
		CreatedAt:    strings.TrimSpace(entry.CreatedAt),
		LastAccessed: strings.TrimSpace(entry.LastAccessed),
		AccessCount:  entry.AccessCount,
		ExpiresAt:    entry.ExpiresAt,
	}

	if normalized.ID == "" {
		id, err := s.newID()
		if err != nil {
			return Entry{}, err
		}
		normalized.ID = id
	}

	if err := ValidateKind(normalized.Kind); err != nil {
		return Entry{}, err
	}

	scope, err := normalizeScope(normalized.Scope)
	if err != nil {
		return Entry{}, err
	}
	normalized.Scope = scope

	if normalized.Title == "" || normalized.Summary == "" || normalized.Body == "" {
		return Entry{}, errors.New("title, summary e body são obrigatórios")
	}
	if len(normalized.Tags) == 0 {
		return Entry{}, errors.New("tags são obrigatórias")
	}
	if normalized.Confidence < 0 || normalized.Confidence > 1 {
		return Entry{}, errors.New("confidence deve ficar entre 0.0 e 1.0")
	}

	now := s.timestamp()
	if normalized.CreatedAt == "" {
		normalized.CreatedAt = now
	}
	if normalized.LastAccessed == "" {
		normalized.LastAccessed = normalized.CreatedAt
	}

	return normalized, nil
}

func (s *Store) validateInteraction(interaction LastInteraction) (LastInteraction, error) {
	normalized := LastInteraction{
		SessionID:  strings.TrimSpace(interaction.SessionID),
		Kind:       normalizeKind(interaction.Kind),
		Scope:      strings.TrimSpace(interaction.Scope),
		Title:      strings.TrimSpace(interaction.Title),
		Summary:    strings.TrimSpace(interaction.Summary),
		Body:       strings.TrimSpace(interaction.Body),
		Tags:       normalizeTags(interaction.Tags),
		RecordedAt: strings.TrimSpace(interaction.RecordedAt),
	}

	if normalized.SessionID == "" {
		return LastInteraction{}, errors.New("session_id obrigatório")
	}
	if err := ValidateKind(normalized.Kind); err != nil {
		return LastInteraction{}, err
	}
	scope, err := normalizeScope(normalized.Scope)
	if err != nil {
		return LastInteraction{}, err
	}
	normalized.Scope = scope
	if normalized.Title == "" || normalized.Summary == "" || normalized.Body == "" {
		return LastInteraction{}, errors.New("interação recente incompleta")
	}
	if len(normalized.Tags) == 0 {
		return LastInteraction{}, errors.New("tags são obrigatórias")
	}
	if normalized.RecordedAt == "" {
		normalized.RecordedAt = s.timestamp()
	}

	return normalized, nil
}

func (s *Store) scopeFilePath(scope string) (string, error) {
	scope, err := normalizeScope(scope)
	if err != nil {
		return "", err
	}

	switch {
	case scope == "global":
		return filepath.Join(s.memoryDir(), "global.jsonl"), nil
	case strings.HasPrefix(scope, "project:"):
		return filepath.Join(s.memoryDir(), "project_"+strings.TrimPrefix(scope, "project:")+".jsonl"), nil
	case strings.HasPrefix(scope, "host:"):
		return filepath.Join(s.memoryDir(), "host_"+strings.TrimPrefix(scope, "host:")+".jsonl"), nil
	default:
		return "", fmt.Errorf("%w: %s", ErrInvalidScope, scope)
	}
}

func (s *Store) loadEntries(path string) ([]Entry, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var entries []Entry
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var entry Entry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return entries, nil
}

func (s *Store) writeEntries(path string, entries []Entry) error {
	var data bytes.Buffer
	encoder := json.NewEncoder(&data)
	for _, entry := range entries {
		if err := encoder.Encode(entry); err != nil {
			return err
		}
	}
	return pocketpath.AtomicWrite(path, data.Bytes(), 0o600)
}

func (s *Store) memoryDir() string {
	return filepath.Join(s.baseDir, "memory")
}

func (s *Store) stateDir() string {
	return filepath.Join(s.baseDir, "state")
}

func (s *Store) lastInteractionPath() string {
	return filepath.Join(s.stateDir(), "last_interaction.json")
}

func (s *Store) timestamp() string {
	return s.now().UTC().Format(time.RFC3339)
}

func normalizeKind(kind string) string {
	return strings.ToLower(strings.TrimSpace(kind))
}

func normalizeScope(scope string) (string, error) {
	scope = strings.TrimSpace(scope)
	switch {
	case scope == "":
		return "", fmt.Errorf("%w: %s", ErrInvalidScope, scope)
	case strings.EqualFold(scope, "global"):
		return "global", nil
	case strings.HasPrefix(strings.ToLower(scope), "project:"):
		raw := strings.TrimSpace(scope[len("project:"):])
		slug := slugify(raw)
		if slug == "" {
			return "", fmt.Errorf("%w: %s", ErrInvalidScope, scope)
		}
		return "project:" + slug, nil
	case strings.HasPrefix(strings.ToLower(scope), "host:"):
		raw := strings.TrimSpace(scope[len("host:"):])
		slug := slugify(raw)
		if slug == "" {
			return "", fmt.Errorf("%w: %s", ErrInvalidScope, scope)
		}
		return "host:" + slug, nil
	default:
		return "", fmt.Errorf("%w: %s", ErrInvalidScope, scope)
	}
}

func normalizeTags(tags []string) []string {
	normalized := make([]string, 0, len(tags))
	for _, tag := range tags {
		trimmed := strings.ToLower(strings.TrimSpace(tag))
		if trimmed == "" {
			continue
		}
		normalized = append(normalized, trimmed)
	}
	return normalized
}

func compactWhitespace(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func truncateRunes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}

	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return strings.TrimSpace(string(runes[:limit]))
}

func slugify(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}

	var b strings.Builder
	underscore := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
			underscore = false
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			underscore = false
		default:
			if b.Len() > 0 && !underscore {
				b.WriteByte('_')
				underscore = true
			}
		}
	}

	return strings.Trim(b.String(), "_")
}

func roundConfidence(value float64) float64 {
	switch {
	case value < 0:
		value = 0
	case value > 1:
		value = 1
	}
	return math.Round(value*10) / 10
}

func newUUIDv4() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}

	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80

	return fmt.Sprintf(
		"%08x-%04x-%04x-%04x-%012x",
		raw[0:4],
		raw[4:6],
		raw[6:8],
		raw[8:10],
		raw[10:16],
	), nil
}
