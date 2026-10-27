package queries

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"pocketcli/internal/investigation/contract"
)

type Execution struct {
	ID         string                `json:"id"`
	QueryID    string                `json:"query_id"`
	SnapshotID string                `json:"snapshot_id"`
	SQL        string                `json:"sql"`
	Parameters []contract.Parameter  `json:"parameters"`
	QueryHash  string                `json:"query_hash"`
	Status     string                `json:"status"`
	CreatedAt  string                `json:"created_at"`
	Truncated  bool                  `json:"truncated"`
	Result     *contract.QueryResult `json:"result,omitempty"`
	Error      string                `json:"error,omitempty"`
}
type HistoryPage struct {
	Items      []Execution `json:"items"`
	NextCursor string      `json:"next_cursor,omitempty"`
	Total      int64       `json:"total"`
}
type SavedQuery struct {
	ID              string               `json:"id"`
	InvestigationID string               `json:"investigation_id"`
	SnapshotID      string               `json:"snapshot_id"`
	Name            string               `json:"name"`
	SQL             string               `json:"sql"`
	Parameters      []contract.Parameter `json:"parameters"`
	QueryHash       string               `json:"query_hash"`
	Result          contract.QueryResult `json:"result"`
	CreatedAt       string               `json:"created_at"`
	OperatorLabel   string               `json:"operator_label"`
	IncludeInReport bool                 `json:"include_in_report"`
	Revision        int                  `json:"revision"`
}
type CreateInput struct {
	Name            string `json:"name"`
	QueryID         string `json:"query_id"`
	IncludeInReport bool   `json:"include_in_report"`
}
type PatchInput struct {
	ExpectedRevision int     `json:"expected_revision"`
	Name             *string `json:"name,omitempty"`
	IncludeInReport  *bool   `json:"include_in_report,omitempty"`
}
type Store struct {
	mu         sync.Mutex
	path       string
	executions []Execution
	saved      map[string]SavedQuery
}

func Open(dir string) (*Store, error) {
	if dir == "" {
		return nil, errors.New("query journal directory required")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return nil, err
	}
	p := filepath.Join(dir, "queries.jsonl")
	s := &Store{path: p, saved: map[string]SavedQuery{}}
	f, e := os.OpenFile(p, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	_ = os.Chmod(p, 0600)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 2*1024*1024)
	for sc.Scan() {
		var row struct {
			Kind      string     `json:"kind"`
			Execution Execution  `json:"execution"`
			Saved     SavedQuery `json:"saved"`
		}
		if e = json.Unmarshal(sc.Bytes(), &row); e != nil {
			_ = f.Close()
			return nil, e
		}
		if row.Kind == "execution" {
			s.executions = append(s.executions, row.Execution)
		} else if row.Kind == "saved" {
			s.saved[row.Saved.ID] = row.Saved
		}
	}
	if e = sc.Err(); e != nil {
		_ = f.Close()
		return nil, e
	}
	return s, f.Close()
}
func (s *Store) append(value any) error {
	b, e := json.Marshal(value)
	if e != nil {
		return e
	}
	f, e := os.OpenFile(s.path, os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	if _, e = f.Write(append(b, '\n')); e != nil {
		_ = f.Close()
		return e
	}
	if e = f.Sync(); e != nil {
		_ = f.Close()
		return e
	}
	return f.Close()
}
func (s *Store) Record(req contract.QueryRequest, res contract.QueryResult, runErr error) (Execution, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, e := newID()
	if e != nil {
		return Execution{}, e
	}
	queryID := res.QueryID
	if queryID == "" {
		queryID = id
	}
	res.QueryID = queryID
	x := Execution{ID: id, QueryID: queryID, SnapshotID: req.SnapshotID, SQL: req.SQL, Parameters: append([]contract.Parameter{}, req.Parameters...), QueryHash: res.QueryHash, Status: "complete", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), Truncated: res.Truncated}
	if runErr != nil {
		x.Status = "error"
		x.Error = runErr.Error()
	}
	if runErr == nil {
		x.Result = &res
	}
	if e = s.append(map[string]any{"kind": "execution", "execution": x}); e != nil {
		return Execution{}, e
	}
	s.executions = append(s.executions, x)
	return x, nil
}
func (s *Store) History(snapshot, cursor string, limit int) HistoryPage {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit < 1 || limit > 100 {
		limit = 100
	}
	rows := []Execution{}
	for _, x := range s.executions {
		if snapshot == "" || x.SnapshotID == snapshot {
			rows = append(rows, x)
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].CreatedAt > rows[j].CreatedAt })
	start := 0
	if cursor != "" {
		for i, x := range rows {
			if x.ID == cursor {
				start = i + 1
				break
			}
		}
	}
	end := start + limit
	if end > len(rows) {
		end = len(rows)
	}
	p := HistoryPage{Items: append([]Execution{}, rows[start:end]...), Total: int64(len(rows))}
	if end < len(rows) && end > start {
		p.NextCursor = rows[end-1].ID
	}
	return p
}
func (s *Store) Create(in CreateInput, investigation, snapshot string) (SavedQuery, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	name := in.Name
	if len([]rune(name)) < 1 || len([]rune(name)) > 120 {
		return SavedQuery{}, errors.New("ERR_VALIDATION: nome da query deve ter de 1 a 120 caracteres")
	}
	if in.QueryID == "" || snapshot == "" {
		return SavedQuery{}, errors.New("ERR_VALIDATION: execução e snapshot são obrigatórios")
	}
	var x *Execution
	for i := len(s.executions) - 1; i >= 0; i-- {
		if s.executions[i].QueryID == in.QueryID && s.executions[i].SnapshotID == snapshot && s.executions[i].Status == "complete" {
			x = &s.executions[i]
			break
		}
	}
	if x == nil || x.Result == nil {
		return SavedQuery{}, errors.New("ERR_NOT_FOUND: execução válida não encontrada")
	}
	id, e := newID()
	if e != nil {
		return SavedQuery{}, e
	}
	q := SavedQuery{ID: id, InvestigationID: investigation, SnapshotID: x.SnapshotID, Name: name, SQL: x.SQL, Parameters: x.Parameters, QueryHash: x.QueryHash, Result: *x.Result, CreatedAt: time.Now().UTC().Format(time.RFC3339), OperatorLabel: "Operador local", IncludeInReport: in.IncludeInReport, Revision: 1}
	if e = s.append(map[string]any{"kind": "saved", "saved": q}); e != nil {
		return SavedQuery{}, e
	}
	s.saved[id] = q
	return q, nil
}
func (s *Store) GetSaved(id string) (SavedQuery, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	q, ok := s.saved[id]
	return q, ok
}
func (s *Store) Patch(id string, in PatchInput) (SavedQuery, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	q, ok := s.saved[id]
	if !ok {
		return SavedQuery{}, errors.New("ERR_NOT_FOUND: query salva não encontrada")
	}
	if q.Revision != in.ExpectedRevision {
		return SavedQuery{}, errors.New("ERR_REVISION_CONFLICT: query foi atualizada em outra aba")
	}
	if in.Name != nil {
		if len([]rune(*in.Name)) < 1 || len([]rune(*in.Name)) > 120 {
			return SavedQuery{}, errors.New("ERR_VALIDATION: nome inválido")
		}
		q.Name = *in.Name
	}
	if in.IncludeInReport != nil {
		q.IncludeInReport = *in.IncludeInReport
	}
	q.Revision++
	if e := s.append(map[string]any{"kind": "saved", "saved": q}); e != nil {
		return SavedQuery{}, e
	}
	s.saved[id] = q
	return q, nil
}
func (s *Store) List(snapshot string) []SavedQuery {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []SavedQuery{}
	for _, q := range s.saved {
		if snapshot == "" || q.SnapshotID == snapshot {
			out = append(out, q)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out
}
func newID() (string, error) {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		return "", e
	}
	return hex.EncodeToString(b[:]), nil
}
func (s *Store) AllHistory(snapshot string) []Execution {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []Execution{}
	for _, x := range s.executions {
		if snapshot == "" || x.SnapshotID == snapshot {
			out = append(out, x)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt < out[j].CreatedAt })
	return out
}
