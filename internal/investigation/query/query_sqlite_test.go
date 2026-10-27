//go:build investigate_sqlite && cgo

package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"pocketcli/internal/investigation/contract"
)

func openFixture(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, err
	}
	schema, err := schemaFS.ReadFile("schema.sql")
	if err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(string(schema)); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func TestReadOnlyAndFixture(t *testing.T) {
	root := t.TempDir()
	q, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	fixture, err := os.ReadFile(filepath.Join("..", "..", "..", "tests", "investigation", "core", "demo.sql"))
	if err != nil {
		t.Fatal(err)
	}
	// The complete SQL fixture is executed only in a temporary projection.
	path := filepath.Join(root, "snap-demo-001.sqlite")
	db, err := openFixture(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(fixture)); err != nil {
		t.Fatal(err)
	}
	db.Close()
	check := func(sqlText string, rows int, wantErr bool) {
		t.Helper()
		got, err := q.Execute(context.Background(), "snap-demo-001", contract.QueryRequest{SnapshotID: "snap-demo-001", SQL: sqlText})
		if (err != nil) != wantErr {
			t.Fatalf("sql=%q err=%v", sqlText, err)
		}
		if !wantErr && got.ReturnedRows != rows {
			t.Fatalf("sql=%q rows=%d", sqlText, got.ReturnedRows)
		}
	}
	check("SELECT issue_id FROM v_issues WHERE reopen_count > 0", 1, false)
	check("SELECT 'a;b' AS x FROM v_snapshot", 1, false)
	check("SELECT 'recursive' AS x FROM v_snapshot", 1, false)
	check("WITH chosen AS (SELECT issue_id FROM v_issues WHERE reopen_count > 0) SELECT COUNT(*) FROM chosen", 1, false)
	check("EXPLAIN QUERY PLAN SELECT issue_id FROM v_issues", 1, false)
	check("-- local note\nSELECT\nCOUNT(*) FROM v_issues", 1, false)
	check("SELECT * FROM v_snapshot; DELETE FROM v_issues", 0, true)
	check("WITH x AS (SELECT 1) DELETE FROM v_issues", 0, true)
	check("PRAGMA table_info(v_issues)", 0, true)
	check("SELECT * FROM sqlite_master", 0, true)
	check("SELECT random() FROM v_snapshot", 0, true)
	check("WITH /*x*/ RECURSIVE x(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM x) SELECT n FROM x", 0, true)
	check("WITH RECURSIVE/*x*/ x(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM x) SELECT n FROM x", 0, true)
	check("WITH x(n) AS (VALUES(1) UNION ALL SELECT n+1 FROM x WHERE n<3) SELECT n FROM x", 0, true)
	large, err := q.Execute(context.Background(), "snap-demo-001", contract.QueryRequest{SnapshotID: "snap-demo-001", SQL: "SELECT 9007199254740993 AS large_integer FROM v_snapshot"})
	if err != nil || large.Rows[0][0].Value != "9007199254740993" {
		t.Fatalf("large integer=%+v err=%v", large, err)
	}
	want := map[string]int{"Q01": 2, "Q02": 2, "Q03": 1, "Q04": 1, "Q05": 1, "Q06": 1, "Q07": 7, "Q08": 0, "Q09": 2, "Q10": 1}
	for _, suggestion := range Catalog() {
		result, err := q.Execute(context.Background(), "snap-demo-001", contract.QueryRequest{SnapshotID: "snap-demo-001", SQL: suggestion.SQL, Parameters: suggestion.Parameters})
		if err != nil {
			t.Fatalf("%s: %v", suggestion.ID, err)
		}
		if result.ReturnedRows != want[suggestion.ID] {
			t.Fatalf("%s: got %d rows, want %d", suggestion.ID, result.ReturnedRows, want[suggestion.ID])
		}
		if suggestion.ID == "Q03" && result.Rows[0][3].Value != "48" {
			t.Fatalf("Q03 hours=%s", result.Rows[0][3].Value)
		}
		if suggestion.ID == "Q08" {
			suggestion.Parameters[0].Value.Value = "iss-1"
			timeline, err := q.Execute(context.Background(), "snap-demo-001", contract.QueryRequest{
				SnapshotID: "snap-demo-001", SQL: suggestion.SQL, Parameters: suggestion.Parameters,
			})
			if err != nil || timeline.ReturnedRows != 5 {
				t.Fatalf("Q08 with selected subject: rows=%d err=%v", timeline.ReturnedRows, err)
			}
		}
	}
	db, err = sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for i := 0; i < 1001; i++ {
		if _, err := db.Exec(`INSERT INTO v_components VALUES (?,?,?,?,?,?,?,?)`, "bulk-"+strconv.Itoa(i), "Bulk", "demo/bulk", "p", "application", "unknown", "unknown", "partial"); err != nil {
			t.Fatal(err)
		}
	}
	limited, err := q.Execute(context.Background(), "snap-demo-001", contract.QueryRequest{SnapshotID: "snap-demo-001", SQL: "SELECT component_id FROM v_components", MaxRows: 1000})
	if err != nil || limited.ReturnedRows != 1000 || !limited.Truncated {
		t.Fatalf("limit=%+v err=%v", limited, err)
	}
	wideSQL := "SELECT " + strings.TrimSuffix(strings.Repeat("'x' AS c,", 400), ",") + " FROM v_components"
	wide, err := q.Execute(context.Background(), "snap-demo-001", contract.QueryRequest{SnapshotID: "snap-demo-001", SQL: wideSQL, MaxRows: 1000, TimeoutMS: 5000})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(wide)
	if err != nil || len(encoded) > 2*1024*1024 || !wide.Truncated || wide.ReturnedRows >= 1000 {
		t.Fatalf("limite de bytes não aplicado ao JSON completo: bytes=%d rows=%d truncated=%v err=%v", len(encoded), wide.ReturnedRows, wide.Truncated, err)
	}
}
