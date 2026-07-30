package inspector_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/qiaozy1013/peekdb/internal/detect"
	"github.com/qiaozy1013/peekdb/internal/inspector"
)

// makeTestSQLiteDB creates a small valid SQLite database in
// the test's TempDir and returns its path. The schema is a
// single `users` table with id/name/email columns, populated
// with three rows.
//
// We do NOT use a committed testdata fixture here because
// (a) committing a real SQLite file invites accidental
// commits of real user data later and (b) tests should
// rebuild their own data from the test's setup.
func makeTestSQLiteDB(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	stmts := []string{
		`CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, email TEXT)`,
		`INSERT INTO users (name, email) VALUES ('alice', 'a@example.com')`,
		`INSERT INTO users (name, email) VALUES ('bob',   'b@example.com')`,
		`INSERT INTO users (name, email) VALUES ('carol', 'c@example.com')`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("exec %q: %v", s, err)
		}
	}
	return path
}

func TestSQLite_NewSQLite(t *testing.T) {
	resetRegistry(t)
	path := makeTestSQLiteDB(t)
	insp, err := inspector.NewSQLite(path, inspector.Options{})
	if err != nil {
		t.Fatalf("NewSQLite: %v", err)
	}
	defer func() { _ = insp.Close() }()

	if got := insp.Format(); got != detect.FormatSQLite {
		t.Errorf("Format = %q, want %q", got, detect.FormatSQLite)
	}
	if got := insp.Path(); got != path {
		t.Errorf("Path = %q, want %q", got, path)
	}
}

func TestSQLite_NewSQLite_NotAFile(t *testing.T) {
	resetRegistry(t)
	_, err := inspector.NewSQLite(t.TempDir(), inspector.Options{})
	if err == nil {
		t.Errorf("NewSQLite on a directory: expected error, got nil")
	}
}

func TestSQLite_NewSQLite_MissingFile(t *testing.T) {
	resetRegistry(t)
	_, err := inspector.NewSQLite(filepath.Join(t.TempDir(), "missing.db"), inspector.Options{})
	if err == nil {
		t.Errorf("NewSQLite on missing file: expected error, got nil")
	}
}

func TestSQLite_Open_Dispatches(t *testing.T) {
	resetRegistry(t)
	inspector.MustRegister(detect.FormatSQLite,
		func(path string, opts inspector.Options) (inspector.Inspector, error) {
			return inspector.NewSQLite(path, opts)
		})
	path := makeTestSQLiteDB(t)
	insp, err := inspector.Open(path, inspector.Options{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = insp.Close() }()
	if got := insp.Format(); got != detect.FormatSQLite {
		t.Errorf("Format = %q, want %q", got, detect.FormatSQLite)
	}
}

func TestSQLite_Items(t *testing.T) {
	resetRegistry(t)
	path := makeTestSQLiteDB(t)
	insp, err := inspector.NewSQLite(path, inspector.Options{})
	if err != nil {
		t.Fatalf("NewSQLite: %v", err)
	}
	defer func() { _ = insp.Close() }()

	items, err := insp.Items()
	if err != nil {
		t.Fatalf("Items: %v", err)
	}
	if len(items) == 0 {
		t.Fatalf("Items: empty; expected at least 'users'")
	}
	var found bool
	for _, it := range items {
		if it.Name == "users" {
			found = true
			if it.Kind != "table" {
				t.Errorf("users.Kind = %q, want %q", it.Kind, "table")
			}
			if it.Meta["sql"] == "" {
				t.Errorf("users.Meta[sql] empty; want CREATE statement")
			}
		}
	}
	if !found {
		t.Errorf("Items: missing 'users' table; got %+v", items)
	}
}

func TestSQLite_OpenItem(t *testing.T) {
	resetRegistry(t)
	path := makeTestSQLiteDB(t)
	insp, err := inspector.NewSQLite(path, inspector.Options{})
	if err != nil {
		t.Fatalf("NewSQLite: %v", err)
	}
	defer func() { _ = insp.Close() }()

	r, err := insp.OpenItem("users")
	if err != nil {
		t.Fatalf("OpenItem(users): %v", err)
	}
	defer func() { _ = r.Close() }()
	var count int
	for r.Next() {
		row := r.Scan()
		if len(row.Columns) != 3 {
			t.Errorf("row has %d columns, want 3 (id, name, email)", len(row.Columns))
		}
		if len(row.Values) != 3 {
			t.Errorf("row has %d values, want 3", len(row.Values))
		}
		count++
	}
	if err := r.Err(); err != nil {
		t.Errorf("Err() = %v, want nil", err)
	}
	if count != 3 {
		t.Errorf("got %d rows, want 3", count)
	}
}

func TestSQLite_OpenItem_NotFound(t *testing.T) {
	resetRegistry(t)
	path := makeTestSQLiteDB(t)
	insp, err := inspector.NewSQLite(path, inspector.Options{})
	if err != nil {
		t.Fatalf("NewSQLite: %v", err)
	}
	defer func() { _ = insp.Close() }()

	if _, err := insp.OpenItem("does-not-exist"); err == nil {
		t.Errorf("OpenItem(missing) returned nil error")
	}
}

func TestSQLite_OpenItem_InvalidIdent(t *testing.T) {
	resetRegistry(t)
	path := makeTestSQLiteDB(t)
	insp, err := inspector.NewSQLite(path, inspector.Options{})
	if err != nil {
		t.Fatalf("NewSQLite: %v", err)
	}
	defer func() { _ = insp.Close() }()

	// SQL-injection-style names must be rejected by the
	// identifier validator before they ever reach the driver.
	for _, bad := range []string{"users; DROP TABLE users", "users' OR 1=1", "1users", "users x"} {
		if _, err := insp.OpenItem(bad); err == nil {
			t.Errorf("OpenItem(%q) returned nil error; expected invalid-identifier error", bad)
		}
	}
}

func TestSQLite_Schema(t *testing.T) {
	resetRegistry(t)
	path := makeTestSQLiteDB(t)
	insp, err := inspector.NewSQLite(path, inspector.Options{})
	if err != nil {
		t.Fatalf("NewSQLite: %v", err)
	}
	defer func() { _ = insp.Close() }()

	cols, err := insp.Schema("users")
	if err != nil {
		t.Fatalf("Schema(users): %v", err)
	}
	if len(cols) != 3 {
		t.Fatalf("len(cols) = %d, want 3", len(cols))
	}
	want := []string{"id", "name", "email"}
	for i, c := range cols {
		if c.Name != want[i] {
			t.Errorf("col[%d].Name = %q, want %q", i, c.Name, want[i])
		}
	}
}

func TestSQLite_Stats(t *testing.T) {
	resetRegistry(t)
	path := makeTestSQLiteDB(t)
	insp, err := inspector.NewSQLite(path, inspector.Options{})
	if err != nil {
		t.Fatalf("NewSQLite: %v", err)
	}
	defer func() { _ = insp.Close() }()

	stats := insp.Stats()
	if stats.Size == 0 {
		t.Errorf("Stats.Size = 0, want > 0")
	}
	if stats.MTime == 0 {
		t.Errorf("Stats.MTime = 0, want > 0")
	}
	if stats.ReadMode != "readonly" {
		t.Errorf("Stats.ReadMode = %q, want %q", stats.ReadMode, "readonly")
	}
	if stats.FormatVer == "" {
		t.Errorf("Stats.FormatVer empty, want 'SQLite <ver>'")
	}
	// No -wal file is created by makeTestSQLiteDB, so
	// LockState should be empty (no concurrent-writer
	// signal).
	if stats.LockState != "" {
		t.Errorf("Stats.LockState = %q, want empty (no WAL sidecar)", stats.LockState)
	}
}

func TestSQLite_Stats_LockStateWhenWALPresent(t *testing.T) {
	// When a -wal sidecar exists (mimicking another
	// process having the db open in WAL mode), Stats
	// should report LockState="shared" so the TUI can
	// surface the "concurrent write" warning.
	resetRegistry(t)
	path := makeTestSQLiteDB(t)
	// Drop a fake -wal file alongside the db.
	if err := os.WriteFile(path+"-wal", make([]byte, 4096), 0o600); err != nil {
		t.Fatalf("write -wal: %v", err)
	}
	insp, err := inspector.NewSQLite(path, inspector.Options{})
	if err != nil {
		t.Fatalf("NewSQLite: %v", err)
	}
	defer func() { _ = insp.Close() }()

	stats := insp.Stats()
	if stats.LockState != "shared" {
		t.Errorf("Stats.LockState = %q, want %q (WAL sidecar present)",
			stats.LockState, "shared")
	}
	if stats.WALSize != 4096 {
		t.Errorf("Stats.WALSize = %d, want 4096", stats.WALSize)
	}
}

func TestSQLite_Close_Idempotent(t *testing.T) {
	resetRegistry(t)
	path := makeTestSQLiteDB(t)
	insp, err := inspector.NewSQLite(path, inspector.Options{})
	if err != nil {
		t.Fatalf("NewSQLite: %v", err)
	}
	if err := insp.Close(); err != nil {
		t.Errorf("first Close: %v", err)
	}
	_ = insp.Close()
}

// TestSQLite_AfterClose is the regression test from
// . After Close, the inspector
// must not panic on Stats / Items / OpenItem / Query. See
// the bbolt counterpart for the full rationale.
func TestSQLite_AfterClose(t *testing.T) {
	resetRegistry(t)
	path := makeTestSQLiteDB(t)
	insp, err := inspector.NewSQLite(path, inspector.Options{})
	if err != nil {
		t.Fatalf("NewSQLite: %v", err)
	}
	if err := insp.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("post-Close call panicked: %v", r)
		}
	}()
	_ = insp.Stats()
	_, _ = insp.Items()
	_, _ = insp.OpenItem("t")
	_, _ = insp.Query("SELECT 1")
}
