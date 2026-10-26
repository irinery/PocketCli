package annotations

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type TargetType string

const (
	Snapshot   TargetType = "snapshot"
	Component  TargetType = "component"
	Finding    TargetType = "finding"
	SavedQuery TargetType = "saved_query"
	Report     TargetType = "report"
)

type Annotation struct {
	ID              string     `json:"id"`
	InvestigationID string     `json:"investigation_id"`
	SnapshotID      string     `json:"snapshot_id"`
	TargetType      TargetType `json:"target_type"`
	TargetID        string     `json:"target_id"`
	Body            string     `json:"body"`
	OperatorLabel   string     `json:"operator_label"`
	Status          string     `json:"status"`
	Revision        int        `json:"revision"`
	GlobalRevision  int64      `json:"global_revision"`
	CreatedAt       string     `json:"created_at"`
	UpdatedAt       string     `json:"updated_at"`
}
type CreateInput struct {
	InvestigationID string     `json:"investigation_id"`
	SnapshotID      string     `json:"snapshot_id"`
	TargetType      TargetType `json:"target_type"`
	TargetID        string     `json:"target_id"`
	Body            string     `json:"body"`
	OperatorLabel   string     `json:"operator_label"`
}
type UpdateInput struct {
	ExpectedRevision int    `json:"expected_revision"`
	Body             string `json:"body"`
}
type Page struct {
	Items          []Annotation `json:"items"`
	NextCursor     string       `json:"next_cursor,omitempty"`
	Total          int64        `json:"total"`
	GlobalRevision int64        `json:"global_revision"`
}
type event struct {
	Action     string     `json:"action"`
	Annotation Annotation `json:"annotation"`
	At         string     `json:"at"`
}
type Store struct {
	mu       sync.Mutex
	path     string
	rows     map[string]Annotation
	versions []Annotation
	revision int64
	warning  string
}

func Open(dir string) (*Store, error) {
	if dir == "" {
		return nil, errors.New("annotation directory required")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return nil, err
	}
	s := &Store{path: filepath.Join(dir, "annotations.jsonl"), rows: map[string]Annotation{}}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *Store) Warning() string { s.mu.Lock(); defer s.mu.Unlock(); return s.warning }
func (s *Store) load() error {
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err = os.Chmod(s.path, 0600); err != nil {
		return err
	}
	reader := bufio.NewReader(f)
	var offset int64
	for {
		line, e := reader.ReadBytes('\n')
		if len(line) > 0 {
			var ev event
			if json.Unmarshal(bytesTrimNL(line), &ev) != nil {
				if e == nil {
					return fmt.Errorf("annotation journal corrupted at byte %d", offset)
				}
				if err := s.quarantineTail(offset, line); err != nil {
					return err
				}
				s.warning = "A última linha incompleta foi colocada em quarentena."
				break
			}
			s.apply(ev)
			offset += int64(len(line))
		}
		if e != nil {
			if e == io.EOF {
				break
			}
			return e
		}
	}
	return nil
}
func bytesTrimNL(b []byte) []byte {
	if len(b) > 0 && b[len(b)-1] == '\n' {
		return b[:len(b)-1]
	}
	return b
}
func (s *Store) quarantineTail(offset int64, tail []byte) error {
	q := s.path + ".corrupt"
	if err := os.WriteFile(q, tail, 0600); err != nil {
		return err
	}
	return os.Truncate(s.path, offset)
}
func (s *Store) apply(e event) {
	if e.Annotation.GlobalRevision > s.revision {
		s.revision = e.Annotation.GlobalRevision
	}
	if e.Action == "delete" {
		delete(s.rows, e.Annotation.ID)
	} else {
		s.rows[e.Annotation.ID] = e.Annotation
		s.versions = append(s.versions, e.Annotation)
	}
}

// At returns the latest revision of every annotation in the requested
// snapshot as of a fixed global journal revision. A zero cutoff means now.
func (s *Store) At(snapshot string, cutoff int64) ([]Annotation, int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cutoff <= 0 || cutoff > s.revision {
		cutoff = s.revision
	}
	latest := map[string]Annotation{}
	for _, a := range s.versions {
		if a.SnapshotID != snapshot || a.GlobalRevision > cutoff {
			continue
		}
		latest[a.ID] = a
	}
	out := make([]Annotation, 0, len(latest))
	for _, a := range latest {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TargetType == out[j].TargetType {
			return out[i].TargetID < out[j].TargetID
		}
		return out[i].TargetType < out[j].TargetType
	})
	return out, cutoff
}
func (s *Store) append(e event) error {
	raw, err := json.Marshal(e)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(s.path, os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	if _, err = f.Write(append(raw, '\n')); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
func (s *Store) Create(in CreateInput) (Annotation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	body := strings.TrimSpace(in.Body)
	if in.InvestigationID == "" || in.SnapshotID == "" || in.TargetID == "" || len([]rune(body)) == 0 || len([]rune(body)) > 8000 || len([]rune(in.OperatorLabel)) == 0 || len([]rune(in.OperatorLabel)) > 80 || !validTarget(in.TargetType) {
		return Annotation{}, errors.New("ERR_VALIDATION: comentário ou alvo inválido")
	}
	id, err := newID()
	if err != nil {
		return Annotation{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	a := Annotation{ID: id, InvestigationID: in.InvestigationID, SnapshotID: in.SnapshotID, TargetType: in.TargetType, TargetID: in.TargetID, Body: body, OperatorLabel: in.OperatorLabel, Status: "open", Revision: 1, GlobalRevision: s.revision + 1, CreatedAt: now, UpdatedAt: now}
	if err = s.append(event{Action: "create", Annotation: a, At: now}); err != nil {
		return Annotation{}, err
	}
	s.apply(event{Action: "create", Annotation: a})
	return a, nil
}
func (s *Store) Update(id string, in UpdateInput) (Annotation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.rows[id]
	if !ok {
		return Annotation{}, errors.New("ERR_NOT_FOUND: comentário não encontrado")
	}
	if in.ExpectedRevision != a.Revision {
		return Annotation{}, errors.New("ERR_REVISION_CONFLICT: comentário foi atualizado em outra aba")
	}
	body := strings.TrimSpace(in.Body)
	if len([]rune(body)) == 0 || len([]rune(body)) > 8000 {
		return Annotation{}, errors.New("ERR_VALIDATION: comentário deve ter de 1 a 8000 caracteres")
	}
	a.Body = body
	a.Revision++
	a.GlobalRevision = s.revision + 1
	a.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err := s.append(event{Action: "update", Annotation: a, At: a.UpdatedAt}); err != nil {
		return Annotation{}, err
	}
	s.apply(event{Action: "update", Annotation: a})
	return a, nil
}
func (s *Store) Resolve(id string, expected int) (Annotation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.rows[id]
	if !ok {
		return Annotation{}, errors.New("ERR_NOT_FOUND: comentário não encontrado")
	}
	if a.Revision != expected {
		return Annotation{}, errors.New("ERR_REVISION_CONFLICT: comentário foi atualizado em outra aba")
	}
	a.Status = "resolved"
	a.Revision++
	a.GlobalRevision = s.revision + 1
	a.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err := s.append(event{Action: "resolve", Annotation: a, At: a.UpdatedAt}); err != nil {
		return Annotation{}, err
	}
	s.apply(event{Action: "resolve", Annotation: a})
	return a, nil
}
func (s *Store) List(snapshot, cursor string, limit int) Page {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit < 1 || limit > 100 {
		limit = 100
	}
	items := make([]Annotation, 0)
	for _, a := range s.rows {
		if snapshot == "" || a.SnapshotID == snapshot {
			items = append(items, a)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].GlobalRevision == items[j].GlobalRevision {
			return items[i].ID < items[j].ID
		}
		return items[i].GlobalRevision < items[j].GlobalRevision
	})
	start := 0
	if cursor != "" {
		for i, a := range items {
			if a.ID == cursor {
				start = i + 1
				break
			}
		}
	}
	end := start + limit
	if end > len(items) {
		end = len(items)
	}
	page := Page{Items: append([]Annotation{}, items[start:end]...), Total: int64(len(items)), GlobalRevision: s.revision}
	if end < len(items) && end > start {
		page.NextCursor = items[end-1].ID
	}
	return page
}
func validTarget(t TargetType) bool {
	return t == Snapshot || t == Component || t == Finding || t == SavedQuery || t == Report
}
func newID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "ann-" + hex.EncodeToString(b[:]), nil
}
