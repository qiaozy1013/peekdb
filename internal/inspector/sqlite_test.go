package inspector_test

import (
	"database/sql"
	"fmt"
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

// makeTestSearchDB creates a richer SQLite database for the
// Search tests. Schema: a `mixed` table with one INTEGER
// primary key, two TEXT columns, one INTEGER, one BLOB, and
// one VARCHAR(20) (different declared type, same TEXT affinity).
// Seeded with 5 rows that exercise every Search code path
// (literal match, case difference, LIKE-special chars, no match,
// matches in different columns).
func makeTestSearchDB(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "search.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	stmts := []string{
		`CREATE TABLE mixed (
			id    INTEGER PRIMARY KEY,
			name  TEXT,
			email TEXT,
			tag   VARCHAR(20),
			age   INTEGER,
			avatar BLOB
		)`,
		// Alice: literal "alice", contains '%' literal in email
		`INSERT INTO mixed (name, email, tag, age, avatar) VALUES ('alice', 'a%@example.com', 'red',  30, X'0102')`,
		// Bob: contains underscore literal "b_obby" tag
		`INSERT INTO mixed (name, email, tag, age, avatar) VALUES ('bob',   'b@example.com', 'b_obby', 25, X'0304')`,
		// Carol: mixed case name "CaRoL" (verifies case-insensitive)
		`INSERT INTO mixed (name, email, tag, age, avatar) VALUES ('CaRoL', 'c@example.com', 'blue',  40, X'0506')`,
		// Dave: name "dave", email "totally different"
		`INSERT INTO mixed (name, email, tag, age, avatar) VALUES ('dave',  'dave@other.org', 'green', 22, X'0708')`,
		// Eve: no match in any TEXT column for "zzz"
		`INSERT INTO mixed (name, email, tag, age, avatar) VALUES ('eve',   'e@example.com', 'gold',  35, X'090A')`,
		// A second table that has NO TEXT columns at all (used by the
		// no-TEXT-columns test to verify we return an empty reader,
		// not an error).
		`CREATE TABLE numeric_only (id INTEGER PRIMARY KEY, value INTEGER, raw BLOB)`,
		`INSERT INTO numeric_only (value, raw) VALUES (1, X'11'), (2, X'22')`,
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

// --- Search tests (v0.2.0 M1a) -----------------------------------------

// drainSearch runs reader to completion and returns the
// (rowValues, err). The rowValues are the string form of each
// column so tests can match on them without worrying about
// driver-native types ([]byte vs string for TEXT, etc.).
func drainSearch(t *testing.T, r inspector.ItemReader) [][]string {
	t.Helper()
	defer func() { _ = r.Close() }()
	var out [][]string
	for r.Next() {
		row := r.Scan()
		if len(row.Columns) == 0 {
			t.Fatalf("Search returned a row with no columns; row=%+v", row)
		}
		vals := make([]string, len(row.Columns))
		for i, v := range row.Values {
			if v == nil {
				vals[i] = ""
				continue
			}
			if b, ok := v.([]byte); ok {
				vals[i] = string(b)
				continue
			}
			vals[i] = fmt.Sprintf("%v", v)
		}
		out = append(out, vals)
	}
	if err := r.Err(); err != nil {
		t.Fatalf("Search reader error: %v", err)
	}
	return out
}

func TestSQLite_Search_BasicMatch(t *testing.T) {
	resetRegistry(t)
	path := makeTestSearchDB(t)
	insp, err := inspector.NewSQLite(path, inspector.Options{})
	if err != nil {
		t.Fatalf("NewSQLite: %v", err)
	}
	defer func() { _ = insp.Close() }()

	r, err := insp.Search("mixed", "alice")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	rows := drainSearch(t, r)
	if len(rows) != 1 {
		t.Fatalf("Search(alice) rows = %d, want 1; got=%+v", len(rows), rows)
	}
	// Columns: id, name, email, tag, age, avatar
	if rows[0][1] != "alice" {
		t.Errorf("matched row name = %q, want %q", rows[0][1], "alice")
	}
}

func TestSQLite_Search_NoMatch(t *testing.T) {
	resetRegistry(t)
	path := makeTestSearchDB(t)
	insp, err := inspector.NewSQLite(path, inspector.Options{})
	if err != nil {
		t.Fatalf("NewSQLite: %v", err)
	}
	defer func() { _ = insp.Close() }()

	r, err := insp.Search("mixed", "zzz_no_such_string")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	rows := drainSearch(t, r)
	if len(rows) != 0 {
		t.Errorf("Search(zzz) rows = %d, want 0; got=%+v", len(rows), rows)
	}
}

func TestSQLite_Search_CaseInsensitive(t *testing.T) {
	resetRegistry(t)
	path := makeTestSearchDB(t)
	insp, err := inspector.NewSQLite(path, inspector.Options{})
	if err != nil {
		t.Fatalf("NewSQLite: %v", err)
	}
	defer func() { _ = insp.Close() }()

	// "CaRoL" was inserted with mixed case; LIKE on ASCII is
	// case-insensitive by default in SQLite.
	r, err := insp.Search("mixed", "carol")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	rows := drainSearch(t, r)
	if len(rows) != 1 {
		t.Fatalf("Search(carol) rows = %d, want 1; got=%+v", len(rows), rows)
	}
	if rows[0][1] != "CaRoL" {
		t.Errorf("matched row name = %q, want %q", rows[0][1], "CaRoL")
	}
}

func TestSQLite_Search_EmptyPattern(t *testing.T) {
	resetRegistry(t)
	path := makeTestSearchDB(t)
	insp, err := inspector.NewSQLite(path, inspector.Options{})
	if err != nil {
		t.Fatalf("NewSQLite: %v", err)
	}
	defer func() { _ = insp.Close() }()

	if _, err := insp.Search("mixed", ""); err == nil {
		t.Errorf("Search('') err = nil, want error")
	}
}

func TestSQLite_Search_InvalidTableIdent(t *testing.T) {
	resetRegistry(t)
	path := makeTestSearchDB(t)
	insp, err := inspector.NewSQLite(path, inspector.Options{})
	if err != nil {
		t.Fatalf("NewSQLite: %v", err)
	}
	defer func() { _ = insp.Close() }()

	// "users; DROP TABLE x" has a space and semicolon; isValidSQLiteIdent
	// (A-Za-z0-9_ only) must reject it.
	if _, err := insp.Search("users; DROP TABLE x", "alice"); err == nil {
		t.Errorf("Search(unsafe ident) err = nil, want error")
	}
}

func TestSQLite_Search_TableNotFound(t *testing.T) {
	resetRegistry(t)
	path := makeTestSearchDB(t)
	insp, err := inspector.NewSQLite(path, inspector.Options{})
	if err != nil {
		t.Fatalf("NewSQLite: %v", err)
	}
	defer func() { _ = insp.Close() }()

	if _, err := insp.Search("nope", "alice"); err == nil {
		t.Errorf("Search(nope) err = nil, want error from Schema()")
	}
}

func TestSQLite_Search_NoTextColumns(t *testing.T) {
	resetRegistry(t)
	path := makeTestSearchDB(t)
	insp, err := inspector.NewSQLite(path, inspector.Options{})
	if err != nil {
		t.Fatalf("NewSQLite: %v", err)
	}
	defer func() { _ = insp.Close() }()

	// numeric_only has INTEGER + BLOB only. Search should return
	// an empty reader, not an error.
	r, err := insp.Search("numeric_only", "anything")
	if err != nil {
		t.Fatalf("Search on numeric-only table: %v", err)
	}
	rows := drainSearch(t, r)
	if len(rows) != 0 {
		t.Errorf("Search on numeric-only table rows = %d, want 0", len(rows))
	}
}

func TestSQLite_Search_LikeWildcardEscape(t *testing.T) {
	resetRegistry(t)
	path := makeTestSearchDB(t)
	insp, err := inspector.NewSQLite(path, inspector.Options{})
	if err != nil {
		t.Fatalf("NewSQLite: %v", err)
	}
	defer func() { _ = insp.Close() }()

	// Alice's email is "a%@example.com". A literal "%" search
	// must match only Alice; if we did not escape LIKE metachars,
	// it would match every row in the table.
	r, err := insp.Search("mixed", "%")
	if err != nil {
		t.Fatalf("Search(%%): %v", err)
	}
	rows := drainSearch(t, r)
	if len(rows) != 1 {
		t.Fatalf("Search(%%) rows = %d, want 1 (only alice); got=%+v", len(rows), rows)
	}
	if rows[0][1] != "alice" {
		t.Errorf("Search(%%) matched name = %q, want alice", rows[0][1])
	}

	// Same test for the underscore metachar: Bob's tag is "b_obby".
	// Literal "_" must match only Bob, not every row whose name is
	// 4 characters long.
	r2, err := insp.Search("mixed", "_")
	if err != nil {
		t.Fatalf("Search(_): %v", err)
	}
	rows2 := drainSearch(t, r2)
	if len(rows2) != 1 {
		t.Fatalf("Search(_) rows = %d, want 1 (only bob); got=%+v", len(rows2), rows2)
	}
	if rows2[0][1] != "bob" {
		t.Errorf("Search(_) matched name = %q, want bob", rows2[0][1])
	}
}

func TestSQLite_Search_MultiColumnOR(t *testing.T) {
	resetRegistry(t)
	path := makeTestSearchDB(t)
	insp, err := inspector.NewSQLite(path, inspector.Options{})
	if err != nil {
		t.Fatalf("NewSQLite: %v", err)
	}
	defer func() { _ = insp.Close() }()

	// "dave" matches one row (Dave — his name contains "dave";
	// the OR in the WHERE clause operates across rows, not across
	// columns of the same row, so Dave's name and email don't
	// produce a duplicate). "example" matches 4 rows (alice/bob/
	// carol/eve all have *@example.com). "blue" matches only
	// Carol (tag column). "green" matches only Dave (tag column).
	cases := []struct {
		pat  string
		want int
	}{
		{"dave", 1},    // one row contains "dave" (in name or email)
		{"example", 4}, // 4 emails end in @example.com
		{"blue", 1},    // only Carol's tag
		{"green", 1},   // only Dave's tag
	}
	for _, tc := range cases {
		r, err := insp.Search("mixed", tc.pat)
		if err != nil {
			t.Fatalf("Search(%q): %v", tc.pat, err)
		}
		got := len(drainSearch(t, r))
		if got != tc.want {
			t.Errorf("Search(%q) rows = %d, want %d", tc.pat, got, tc.want)
		}
	}
}

func TestSQLite_Search_NonTextColumnsSkipped(t *testing.T) {
	resetRegistry(t)
	path := makeTestSearchDB(t)
	insp, err := inspector.NewSQLite(path, inspector.Options{})
	if err != nil {
		t.Fatalf("NewSQLite: %v", err)
	}
	defer func() { _ = insp.Close() }()

	// "30" appears in alice's age (INTEGER) and would match
	// alice by accident if we let INTEGER participate in LIKE.
	// We skip non-TEXT columns, so the literal "30" should
	// match 0 rows (no TEXT column contains "30").
	r, err := insp.Search("mixed", "30")
	if err != nil {
		t.Fatalf("Search(30): %v", err)
	}
	rows := drainSearch(t, r)
	if len(rows) != 0 {
		t.Errorf("Search(30) rows = %d, want 0 (INTEGER must be skipped); got=%+v", len(rows), rows)
	}
}

func TestSQLite_Search_VarcharColumnIncluded(t *testing.T) {
	resetRegistry(t)
	path := makeTestSearchDB(t)
	insp, err := inspector.NewSQLite(path, inspector.Options{})
	if err != nil {
		t.Fatalf("NewSQLite: %v", err)
	}
	defer func() { _ = insp.Close() }()

	// `tag` is declared as VARCHAR(20), not TEXT. SQLite treats
	// it as TEXT-affinity, so it must participate in the LIKE.
	r, err := insp.Search("mixed", "b_obby")
	if err != nil {
		t.Fatalf("Search(b_obby): %v", err)
	}
	rows := drainSearch(t, r)
	if len(rows) != 1 {
		t.Fatalf("Search(b_obby) rows = %d, want 1; got=%+v", len(rows), rows)
	}
	if rows[0][1] != "bob" {
		t.Errorf("matched row name = %q, want bob", rows[0][1])
	}
}
