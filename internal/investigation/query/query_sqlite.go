//go:build investigate_sqlite && cgo

package query

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	sqlite3 "github.com/mattn/go-sqlite3"
	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/storage"
)

//go:embed schema.sql
var schemaFS embed.FS

type sqliteService struct {
	root   string
	mu     sync.Mutex
	driver string
}

func (*sqliteService) Available() bool { return true }

var functions = map[string]bool{"count": true, "sum": true, "avg": true, "min": true, "max": true, "coalesce": true, "ifnull": true, "nullif": true, "abs": true, "round": true, "lower": true, "upper": true, "length": true, "substr": true, "trim": true, "replace": true, "instr": true}
var allowedTable = regexp.MustCompile(`^v_[a-z_]+$`)
var allowedID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
var readPrefix = regexp.MustCompile(`(?is)^(SELECT\b|WITH\b|EXPLAIN\s+QUERY\s+PLAN\b)`)

func New(root string) (Service, error) {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return nil, err
	}
	s := &sqliteService{root: root, driver: "investigation_read_" + strconv.FormatInt(time.Now().UnixNano(), 10)}
	sql.Register(s.driver, &sqlite3.SQLiteDriver{ConnectHook: func(conn *sqlite3.SQLiteConn) error {
		conn.RegisterAuthorizer(func(op int, a, b, c string) int {
			switch op {
			case sqlite3.SQLITE_SELECT:
				return sqlite3.SQLITE_OK
			case sqlite3.SQLITE_READ:
				if allowedTable.MatchString(a) && (c == "main" || c == "") {
					return sqlite3.SQLITE_OK
				}
			case sqlite3.SQLITE_FUNCTION:
				if functions[strings.ToLower(b)] {
					return sqlite3.SQLITE_OK
				}
			}
			return sqlite3.SQLITE_DENY
		})
		return nil
	}})
	return s, nil
}

func (s *sqliteService) path(id string) (string, error) {
	if !allowedID.MatchString(id) {
		return "", denied("ERR_NOT_FOUND", "snapshot inválido")
	}
	return filepath.Join(s.root, id+".sqlite"), nil
}
func denied(code, message string) error { return &contract.ErrorResponse{Code: code, Message: message} }

func (s *sqliteService) Execute(ctx context.Context, id string, req contract.QueryRequest) (contract.QueryResult, error) {
	var out contract.QueryResult
	if req.SnapshotID != id {
		return out, denied("ERR_FORBIDDEN", "snapshot divergente")
	}
	if len(req.SQL) == 0 || len(req.SQL) > 16*1024 || !utf8.ValidString(req.SQL) || len(req.Parameters) > 50 {
		return out, denied("ERR_SQL_LIMIT", "limite de SQL ou parâmetros excedido")
	}
	if req.MaxRows == 0 {
		req.MaxRows = 100
	}
	if req.TimeoutMS == 0 {
		req.TimeoutMS = 3000
	}
	if req.MaxRows < 1 || req.MaxRows > 1000 || req.TimeoutMS < 1 || req.TimeoutMS > 5000 {
		return out, denied("ERR_SQL_LIMIT", "limite de linhas ou tempo inválido")
	}
	if !singleReadStatement(req.SQL) {
		return out, denied("ERR_SQL_DENIED", "somente uma consulta SELECT é permitida")
	}
	path, err := s.path(id)
	if err != nil {
		return out, err
	}
	if _, err := os.Stat(path); err != nil {
		return out, denied("ERR_NOT_FOUND", "projeção não encontrada")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	started := time.Now()
	timed, cancel := context.WithTimeout(ctx, time.Duration(req.TimeoutMS)*time.Millisecond)
	defer cancel()
	u := url.URL{Scheme: "file", Path: path}
	dsn := u.String() + "?mode=ro&_query_only=1&_busy_timeout=1000"
	db, err := sql.Open(s.driver, dsn)
	if err != nil {
		return out, denied("ERR_SQL_DENIED", "não foi possível abrir projeção")
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	args := make([]interface{}, 0, len(req.Parameters))
	for _, p := range req.Parameters {
		value, err := parameterValue(p.Value)
		if err != nil {
			return out, err
		}
		if !regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`).MatchString(p.Name) {
			return out, denied("ERR_VALIDATION", "parâmetro inválido")
		}
		args = append(args, sql.Named(p.Name, value))
	}
	rows, err := db.QueryContext(timed, req.SQL, args...)
	if err != nil {
		return out, denied("ERR_SQL_DENIED", "consulta SQL recusada")
	}
	defer rows.Close()
	names, err := rows.Columns()
	if err != nil {
		return out, denied("ERR_SQL_DENIED", "colunas inválidas")
	}
	types, _ := rows.ColumnTypes()
	out.Columns = make([]contract.Column, len(names))
	for i, n := range names {
		var declared string
		if i < len(types) {
			declared = types[i].DatabaseTypeName()
		}
		out.Columns[i] = contract.Column{Name: n, DeclaredType: declared}
	}
	out.Rows = make([][]contract.Cell, 0)
	// Count the serialized response, including per-cell JSON overhead. A wide
	// query with tiny values can otherwise exceed the 2 MiB public limit.
	const maxResultBytes = 2 * 1024 * 1024
	// The web handler adds a 32-character query ID before returning the result.
	budgetProbe := contract.QueryResult{QueryID: strings.Repeat("x", 32), SnapshotID: id, QueryHash: storage.Hash([]byte(req.SQL)), Columns: out.Columns, Rows: [][]contract.Cell{}, ReturnedRows: req.MaxRows, Truncated: false, ElapsedMS: 9223372036854775807, Warnings: []string{}}
	baseJSON, err := json.Marshal(budgetProbe)
	if err != nil || len(baseJSON) > maxResultBytes {
		return contract.QueryResult{}, denied("ERR_SQL_LIMIT", "colunas excedem limite de resultado")
	}
	used := len(baseJSON)
	for rows.Next() {
		values := make([]interface{}, len(names))
		pointers := make([]interface{}, len(names))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			return contract.QueryResult{}, denied("ERR_SQL_DENIED", "linha inválida")
		}
		if len(out.Rows) >= req.MaxRows {
			out.Truncated = true
			break
		}
		line := make([]contract.Cell, len(names))
		for i, v := range values {
			line[i] = cell(v)
		}
		lineJSON, err := json.Marshal(line)
		if err != nil {
			return contract.QueryResult{}, denied("ERR_SQL_DENIED", "linha inválida")
		}
		if used+len(lineJSON)+1 > maxResultBytes {
			out.Truncated = true
			break
		}
		used += len(lineJSON) + 1
		out.Rows = append(out.Rows, line)
	}
	if err := rows.Err(); err != nil {
		if timed.Err() != nil {
			return contract.QueryResult{}, denied("ERR_QUERY_CANCELLED", "consulta cancelada ou tempo esgotado")
		}
		return contract.QueryResult{}, denied("ERR_SQL_DENIED", "consulta falhou")
	}
	out.SnapshotID = id
	out.QueryHash = storage.Hash([]byte(req.SQL))
	out.ReturnedRows = len(out.Rows)
	out.ElapsedMS = time.Since(started).Milliseconds()
	out.Warnings = []string{}
	return out, nil
}

func parameterValue(c contract.Cell) (interface{}, error) {
	switch c.Kind {
	case "null":
		return nil, nil
	case "text":
		if !utf8.ValidString(c.Value) {
			return nil, denied("ERR_VALIDATION", "texto UTF-8 inválido")
		}
		return c.Value, nil
	case "integer":
		v, e := strconv.ParseInt(c.Value, 10, 64)
		if e != nil {
			return nil, denied("ERR_VALIDATION", "inteiro inválido")
		}
		return v, nil
	case "real":
		v, e := strconv.ParseFloat(c.Value, 64)
		if e != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, denied("ERR_VALIDATION", "real inválido")
		}
		return v, nil
	case "boolean":
		if c.Value == "true" || c.Value == "1" {
			return int64(1), nil
		}
		if c.Value == "false" || c.Value == "0" {
			return int64(0), nil
		}
	}
	return nil, denied("ERR_VALIDATION", "tipo de parâmetro inválido")
}
func cell(v interface{}) contract.Cell {
	switch x := v.(type) {
	case nil:
		return contract.Cell{Kind: "null"}
	case int64:
		return contract.Cell{Kind: "integer", Value: strconv.FormatInt(x, 10)}
	case float64:
		return contract.Cell{Kind: "real", Value: strconv.FormatFloat(x, 'g', -1, 64)}
	case bool:
		if x {
			return contract.Cell{Kind: "boolean", Value: "true"}
		}
		return contract.Cell{Kind: "boolean", Value: "false"}
	case []byte:
		return contract.Cell{Kind: "text", Value: string(x)}
	default:
		return contract.Cell{Kind: "text", Value: fmt.Sprint(x)}
	}
}

// SQLite's authorizer validates the parsed statement. This scanner rejects a second
// statement without confusing semicolons inside quoted strings or comments.
func singleReadStatement(sqlText string) bool {
	s := strings.TrimSpace(sqlText)
	for {
		if strings.HasPrefix(s, "--") {
			if i := strings.IndexByte(s, '\n'); i >= 0 {
				s = strings.TrimSpace(s[i+1:])
				continue
			}
			return false
		}
		if strings.HasPrefix(s, "/*") {
			if i := strings.Index(s, "*/"); i >= 0 {
				s = strings.TrimSpace(s[i+2:])
				continue
			}
			return false
		}
		break
	}
	if startsRecursiveCTE(s) {
		return false
	}
	if !readPrefix.MatchString(s) {
		return false
	}
	quote := byte(0)
	lineComment := false
	blockComment := false
	ended := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		next := byte(0)
		if i+1 < len(s) {
			next = s[i+1]
		}
		if lineComment {
			if ch == '\n' {
				lineComment = false
			}
			continue
		}
		if blockComment {
			if ch == '*' && next == '/' {
				blockComment = false
				i++
			}
			continue
		}
		if quote != 0 {
			if ch == quote {
				if next == quote {
					i++
				} else {
					quote = 0
				}
			}
			continue
		}
		if ch == '\'' || ch == '"' || ch == '`' {
			quote = ch
			continue
		}
		if ch == '-' && next == '-' {
			lineComment = true
			i++
			continue
		}
		if ch == '/' && next == '*' {
			blockComment = true
			i++
			continue
		}
		if ch == ';' {
			ended = true
			continue
		}
		if ended && !strings.ContainsRune(" \t\r\n", rune(ch)) {
			return false
		}
	}
	return quote == 0 && !blockComment
}

func startsRecursiveCTE(s string) bool {
	if len(s) < 4 || !strings.EqualFold(s[:4], "WITH") {
		return false
	}
	i := 4
	for i < len(s) {
		for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r') {
			i++
		}
		if strings.HasPrefix(s[i:], "--") {
			end := strings.IndexByte(s[i:], '\n')
			if end < 0 {
				return false
			}
			i += end + 1
			continue
		}
		if strings.HasPrefix(s[i:], "/*") {
			end := strings.Index(s[i+2:], "*/")
			if end < 0 {
				return false
			}
			i += end + 4
			continue
		}
		break
	}
	if len(s[i:]) < 9 || !strings.EqualFold(s[i:i+9], "RECURSIVE") {
		return false
	}
	if len(s[i:]) == 9 {
		return true
	}
	next := s[i+9]
	return !((next >= 'a' && next <= 'z') || (next >= 'A' && next <= 'Z') || (next >= '0' && next <= '9') || next == '_')
}

func (s *sqliteService) Publish(id string, d Dataset) error {
	path, err := s.path(id)
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		return denied("ERR_VALIDATION", "snapshot já publicado")
	} else if !os.IsNotExist(err) {
		return err
	}
	f, err := os.CreateTemp(s.root, ".projection-*.sqlite")
	if err != nil {
		return err
	}
	tmp := f.Name()
	f.Close()
	defer os.Remove(tmp)
	db, err := sql.Open("sqlite3", tmp)
	if err != nil {
		return err
	}
	defer db.Close()
	schema, _ := schemaFS.ReadFile("schema.sql")
	if _, err := db.Exec(string(schema)); err != nil {
		return err
	}
	if _, err := db.Exec(`CREATE TABLE projection_meta (snapshot_id TEXT PRIMARY KEY, dataset_hash TEXT NOT NULL)`); err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := insertDataset(tx, d); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO projection_meta (snapshot_id, dataset_hash) VALUES (?, ?)`, id, d.Snapshot.DatasetHash); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if err := db.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		return err
	}
	f, err = os.Open(tmp)
	if err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	dir, err := os.Open(s.root)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

// VerifyPublished is internal crash recovery. The public SQL connection uses
// the restricted authorizer and cannot read projection_meta.
func (s *sqliteService) VerifyPublished(id, datasetHash string) error {
	path, err := s.path(id)
	if err != nil || datasetHash == "" {
		return denied("ERR_AUDIT_FAILED", "projeção inválida")
	}
	u := url.URL{Scheme: "file", Path: path}
	db, err := sql.Open("sqlite3", u.String()+"?mode=ro&immutable=1&_query_only=1")
	if err != nil {
		return denied("ERR_AUDIT_FAILED", "projeção indisponível")
	}
	defer db.Close()
	var check string
	if err := db.QueryRow(`PRAGMA quick_check`).Scan(&check); err != nil || check != "ok" {
		return denied("ERR_AUDIT_FAILED", "integridade da projeção falhou")
	}
	var storedID, storedHash string
	if err := db.QueryRow(`SELECT snapshot_id, dataset_hash FROM projection_meta`).Scan(&storedID, &storedHash); err != nil || storedID != id || storedHash != datasetHash {
		return denied("ERR_AUDIT_FAILED", "projeção não corresponde ao build")
	}
	return nil
}

func insertDataset(tx *sql.Tx, d Dataset) error {
	coverage := "complete"
	for _, c := range d.Coverage {
		if c.Status != "complete" && c.Status != "not_selected" {
			coverage = "partial"
			break
		}
	}
	if _, err := tx.Exec(`INSERT INTO v_snapshot VALUES (?,?,?,?,?,?)`, d.Snapshot.ID, nullable(d.Snapshot.WindowStart), d.Snapshot.WindowEnd, d.Snapshot.AnalysisReferenceAt, d.Snapshot.RulesetVersion, coverage); err != nil {
		return err
	}
	for _, c := range d.Components {
		if _, err := tx.Exec(`INSERT INTO v_components VALUES (?,?,?,?,?,?,?,?)`, c.ID, c.Name, c.Repository, c.Path, c.Kind, c.ImplementationStatus, c.DeploymentStatus, c.Coverage); err != nil {
			return err
		}
	}
	for _, i := range d.Issues {
		if _, err := tx.Exec(`INSERT INTO v_issues VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, i.ID, i.Identifier, i.Source, i.Title, i.State, nullable(i.AssigneeID), nullable(i.TeamID), nullable(i.ComponentID), i.CreatedAt, i.UpdatedAt, nullable(i.StartedAt), nullable(i.CompletedAt), i.EstimateBeforeStart, i.EstimateCurrent, i.ReopenCount, boolInt(i.IsContext), boolInt(i.DurationValid), i.CycleSeconds, i.EvidenceID); err != nil {
			return err
		}
	}
	for _, p := range d.PullRequests {
		if _, err := tx.Exec(`INSERT INTO v_pull_requests VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, p.ID, p.Repository, p.Number, p.Title, nullable(p.AuthorID), p.State, p.CreatedAt, nullable(p.ReadyAt), nullable(p.FirstHumanReviewAt), nullable(p.MergedAt), nullable(p.HeadSHA), nullable(p.MergeSHA), p.ReviewWaitSeconds, p.EvidenceID); err != nil {
			return err
		}
	}
	for _, l := range d.Links {
		if _, err := tx.Exec(`INSERT INTO v_links VALUES (?,?,?,?,?,?,?,?)`, l.ID, l.SourceID, l.Relation, l.TargetID, l.Status, l.EvidenceID, nullable(l.ValidFrom), nullable(l.ValidTo)); err != nil {
			return err
		}
	}
	for _, e := range d.Events {
		if _, err := tx.Exec(`INSERT INTO v_events VALUES (?,?,?,?,?,?,?,?,?)`, e.ID, e.EntityID, e.Type, nullable(e.EventAt), e.ObservedAt, nullable(e.ActorID), nullable(e.OldValue), nullable(e.NewValue), e.EvidenceID); err != nil {
			return err
		}
	}
	for _, f := range d.Findings {
		if _, err := tx.Exec(`INSERT INTO v_findings VALUES (?,?,?,?,?,?,?,?,?)`, f.ID, f.RuleID, f.RuleVersion, f.Severity, f.SubjectID, f.Title, f.DeterministicText, f.EvidenceStatus, coverage); err != nil {
			return err
		}
		for _, eid := range f.EvidenceIDs {
			if _, err := tx.Exec(`INSERT INTO v_finding_evidence VALUES (?,?)`, f.ID, eid); err != nil {
				return err
			}
		}
	}
	for _, c := range d.Coverage {
		if _, err := tx.Exec(`INSERT INTO v_coverage VALUES (?,?,?,?,?,?,?,?)`, c.Source, c.ResourceID, c.Collection, c.Status, c.Observed, c.Expected, c.Reason, c.CheckedAt); err != nil {
			return err
		}
	}
	for _, e := range d.Evidence {
		if _, err := tx.Exec(`INSERT INTO v_evidence VALUES (?,?,?,?,?,?,?,?,?)`, e.ID, e.Source, e.Locator, e.ObservedAt, nullable(e.SourceUpdatedAt), e.SHA256, e.Preview, boolInt(e.Redacted), boolInt(e.Truncated)); err != nil {
			return err
		}
	}
	for _, m := range d.Metrics {
		var value interface{}
		if m.Value.Kind == "integer" || m.Value.Kind == "real" {
			value = m.Value.Value
		}
		if _, err := tx.Exec(`INSERT INTO v_metrics VALUES (?,?,?,?,?,?,?,?)`, m.ID, m.Name, value, m.Unit, m.SampleSize, m.EligibleSize, m.Definition, strings.Join(m.Limitations, "; ")); err != nil {
			return err
		}
	}
	for _, v := range d.Stages {
		if _, err := tx.Exec(`INSERT INTO v_stage_times VALUES (?,?,?,?,?,?,?,?)`, v.SubjectID, nullable(v.ComponentID), v.Stage, nullable(v.StartedAt), nullable(v.EndedAt), v.ElapsedSeconds, boolInt(v.DurationValid), v.EvidenceID); err != nil {
			return err
		}
	}
	for _, v := range d.Estimates {
		if _, err := tx.Exec(`INSERT INTO v_estimate_changes VALUES (?,?,?,?,?,?,?,?)`, v.ID, v.IssueID, nullable(v.ActorID), nullable(v.EventAt), v.Before, v.After, v.Timing, v.EvidenceID); err != nil {
			return err
		}
	}
	for _, v := range d.Rework {
		if _, err := tx.Exec(`INSERT INTO v_rework VALUES (?,?,?,?,?,?,?,?)`, v.ID, v.SubjectID, nullable(v.ComponentID), v.Kind, v.EvidenceStatus, nullable(v.EventAt), v.CauseStatus, v.EvidenceID); err != nil {
			return err
		}
	}
	for _, v := range d.Deliveries {
		if _, err := tx.Exec(`INSERT INTO v_delivery_artifacts VALUES (?,?,?,?,?,?,?,?)`, v.ID, v.ComponentID, nullable(v.Digest), nullable(v.RevisionSHA), nullable(v.Environment), nullable(v.ObservedAt), v.ProvenanceStatus, v.EvidenceID); err != nil {
			return err
		}
	}
	return nil
}
func nullable(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

var _ Service = (*sqliteService)(nil)
var _ = json.Valid
