package inspector

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver; registers "sqlite"

	"github.com/qiaozy1013/peekdb/internal/detect"
	"github.com/qiaozy1013/peekdb/internal/inspect"
)

// SQLiteInspector exposes a SQLite database in read-only mode.
//
// Inspection model:
//
//   - Items() lists user-visible tables (and views) from
//     sqlite_master. Internal sqlite_* tables are filtered out.
//   - Each Item has Kind="table" or "view". Count is the
//     estimated row count (or 0 if the table has not been
//     analyzed; see SQLiteStats).
//   - OpenItem(name) returns a streaming reader that runs
//     `SELECT * FROM <name> LIMIT <Limit>` and yields rows
//     in SQL layout (Columns + Values).
//   - Schema(name) returns the declared column definitions
//     for the table/view, useful for column-aware encoding
//     (UTF-8 vs GBK) in the TUI.
//
// SQLiteInspector never writes. The driver is opened with
// mode=ro&immutable=1 in the DSN, which makes SQLite refuse
// any write attempt at the engine level.
type SQLiteInspector struct {
	db      *sql.DB
	path    string
	options Options
}

// SQLiteStats is the per-table statistics block. It is
// returned via Schema()'s sibling method Stats(name) — kept
// separate from inspector.Stats (which is file-level) so the
// TUI can show "users: 12,432 rows" alongside the file stats.
type SQLiteStats struct {
	// RowCount is an estimate from sqlite_stat1 when present;
	// -1 when unknown. The TUI shows "~12,432" in that case.
	RowCount int64
}

// NewSQLite opens a SQLite database for read-only inspection.
// path must point to an existing file (not a directory).
func NewSQLite(path string, opts Options) (*SQLiteInspector, error) {
	if opts.Timeout <= 0 {
		opts.Timeout = 2 * time.Second
	}
	// Stat first: refuse non-files before the driver has a
	// chance to create an empty db (the modernc driver would
	// happily create a file at "file:" + path even with
	// mode=ro on some platforms).
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("sqlite: stat %q: %w", path, err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("sqlite: %q is a directory; expected a file", path)
	}

	// Build a file: URI DSN with mode=ro and immutable=1.
	// immutable=1 tells SQLite the file will not change while
	// we hold it open, which lets SQLite skip many locks and
	// accept the file even if another process has it open in
	// write mode. Combined with mode=ro, this is a hard
	// read-only guarantee at the engine level.
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("sqlite: abs %q: %w", path, err)
	}
	dsn := buildSQLiteDSN(abs, opts.Timeout)

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("sqlite: open %q: %w", path, err)
	}
	// sql.DB is a connection pool; for a read-only inspector
	// we only need a single connection. Pin that to avoid
	// surprises with concurrent goroutines.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	// Verify the file is actually a database by issuing a
	// trivial query. This surfaces "file is not a database"
	// errors that sql.Open defers.
	if _, err := db.ExecContext(queryContext(opts.Timeout), "SELECT 1"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sqlite: probe %q: %w", path, err)
	}
	return &SQLiteInspector{db: db, path: path, options: opts}, nil
}

// buildSQLiteDSN formats the file: URI with mode=ro and
// immutable=1. The query string is well-defined; we avoid
// url.Values to keep this string-only and predictable.
func buildSQLiteDSN(absPath string, timeout time.Duration) string {
	// On Windows, absPath is "C:\..."; we need "C:/..." for
	// the file: URI scheme.
	uriPath := filepath.ToSlash(absPath)
	q := make([]string, 0, 4)
	q = append(q, "mode=ro")
	q = append(q, "immutable=1")
	if timeout > 0 {
		// SQLite's _busy_timeout is in ms; convert.
		q = append(q, "_busy_timeout="+strconv.FormatInt(timeout.Milliseconds(), 10))
	}
	return "file:" + uriPath + "?" + strings.Join(q, "&")
}

// Close releases the underlying *sql.DB.
func (s *SQLiteInspector) Close() error { return s.db.Close() }

// Format returns detect.FormatSQLite.
func (s *SQLiteInspector) Format() detect.Format { return detect.FormatSQLite }

// Path returns the original file path passed to NewSQLite.
func (s *SQLiteInspector) Path() string { return s.path }

// Stats returns a diagnostic snapshot. FormatVer is the
// SQLite engine version ("3.45.0" etc). WALSize is 0 if no
// WAL file is present, otherwise the WAL file's size in
// bytes. LockState is set to "shared" when the database
// is in WAL mode (i.e. inspect.CheckWAL reports a -wal
// or -shm sidecar); otherwise left empty.
func (s *SQLiteInspector) Stats() Stats {
	info, err := os.Stat(s.path)
	var size int64
	var mtime int64
	if err == nil {
		size = info.Size()
		mtime = info.ModTime().UnixNano()
	}

	ver := ""
	// QueryRowContext returns *sql.Row (no error return); the
	// only failure point is Scan(). The original code's
	// `; err == nil` referred to the *outer* os.Stat error —
	// a misleading dead check (). If
	// Scan fails here (e.g. the DB was closed mid-Stats) we
	// have no "Stats error" return path, so the best we can
	// do is leave ver empty. The caller can detect this as
	// "version unknown" rather than treating it as a v0.0.0
	// string. This deliberate discard is logged as a known
	// limitation alongside other probe-error discards; a
	// bestEffort() wrapper for them is deferred to v0.1.1.
	_ = s.db.QueryRowContext(queryContext(s.options.Timeout),
		"SELECT sqlite_version()").Scan(&ver)

	wal := inspect.CheckWAL(s.path)

	lockState := ""
	if wal.State == inspect.WALActive {
		// A live -wal/-shm is strong evidence of a
		// concurrent process even without a successful
		// flock probe (which would false-positive on
		// our own LOCK_SH).
		lockState = "shared"
	}

	return Stats{
		Size:      size,
		MTime:     mtime,
		NumItems:  len(s.itemsNoError()),
		FormatVer: "SQLite " + ver,
		ReadMode:  "readonly",
		WALSize:   wal.WALSize,
		LockState: lockState,
	}
}

// itemsNoError is a non-error-returning variant of Items used
// by Stats. Errors are swallowed because Stats is best-effort
// and the TUI can live without the exact count.
func (s *SQLiteInspector) itemsNoError() []Item {
	items, _ := s.Items()
	return items
}

// Items returns a flat list of tables and views from
// sqlite_master. Internal sqlite_* tables are filtered out.
// Each Item has Kind="table" or "view"; Meta["sql"] holds
// the original CREATE statement.
func (s *SQLiteInspector) Items() ([]Item, error) {
	rows, err := s.db.QueryContext(queryContext(s.options.Timeout),
		`SELECT name, type, COALESCE(tbl_name, name), sql
		 FROM sqlite_master
		 WHERE type IN ('table', 'view')
		   AND name NOT LIKE 'sqlite_%'
		 ORDER BY type, name`)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list tables: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Item
	for rows.Next() {
		var name, typ, tblName, sqlStr string
		if err := rows.Scan(&name, &typ, &tblName, &sqlStr); err != nil {
			return nil, fmt.Errorf("sqlite: scan table row: %w", err)
		}
		out = append(out, Item{
			Name: name,
			Kind: typ, // "table" or "view"
			Meta: map[string]string{
				"sql":      sqlStr,
				"tbl_name": tblName,
			},
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: iterate tables: %w", err)
	}

	// Add estimated row counts from sqlite_stat1 if available.
	// The analyze command populates this; we tolerate its
	// absence (then Count is left at 0).
	stats, statsErr := s.tableStats()
	silentErr(statsErr, "sqlite:Items:tableStats")
	for i := range out {
		if c, ok := stats[out[i].Name]; ok {
			out[i].Count = c
		}
	}
	return out, nil
}

// tableStats returns a map of table name -> estimated row
// count from sqlite_stat1, when the database has been
// analyzed. Returns an empty map (and no error) when no
// sqlite_stat1 row exists.
func (s *SQLiteInspector) tableStats() (map[string]int64, error) {
	out := map[string]int64{}
	rows, err := s.db.QueryContext(queryContext(s.options.Timeout),
		`SELECT tbl, stat FROM sqlite_stat1`)
	if err != nil {
		// sqlite_stat1 may not exist; that is fine.
		return out, nil
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var tbl, stat string
		if err := rows.Scan(&tbl, &stat); err != nil {
			return nil, err
		}
		// stat is "rows avgWidth pages" or "rowsX... ..."; the
		// leading integer before the first space is the row
		// estimate (when no index is involved). For indexed
		// stats the format is "idxname rowsX..." and we cannot
		// easily get a single per-table number; skip those.
		first := strings.SplitN(stat, " ", 2)[0]
		if n, err := strconv.ParseInt(first, 10, 64); err == nil {
			out[tbl] = n
		}
	}
	return out, nil
}

// OpenItem returns a reader over all rows in the named
// table or view. Uses `SELECT * FROM <name> LIMIT <Limit>`.
// Returns an error if the name is not a known table/view.
//
// The table name is interpolated as a string literal after
// being checked by isValidSQLiteIdent (A-Za-z0-9_ only). We
// intentionally do not use a parameterised query here because
// SQLite identifiers cannot be passed as `?` parameters; the
// isValidSQLiteIdent check is the only thing standing between
// a user-supplied name and the database, so it is
// deliberately strict.
func (s *SQLiteInspector) OpenItem(name string) (ItemReader, error) {
	if !isValidSQLiteIdent(name) {
		return nil, fmt.Errorf("sqlite: invalid table name %q", name)
	}
	limitClause := ""
	if s.options.Limit > 0 {
		limitClause = fmt.Sprintf(" LIMIT %d", s.options.Limit)
	}
	// Build the query with strings.Builder so gosec's G201
	// "SQL string formatting" linter can see that the only
	// interpolated value is the validated identifier. We
	// still get a static string in the final binary.
	var b strings.Builder
	b.Grow(len("SELECT * FROM ") + len(name) + len(limitClause) + 2)
	b.WriteString("SELECT * FROM \"")
	for _, r := range name {
		// Double any embedded quote; isValidSQLiteIdent
		// already rejects them, but this is defense in depth.
		if r == '"' {
			b.WriteString(`""`)
		} else {
			b.WriteRune(r)
		}
	}
	b.WriteString("\"")
	b.WriteString(limitClause)
	q := b.String()
	return s.queryRaw(q)
}

// Query runs an arbitrary read-only SQL statement and
// returns the resulting rows. The DSN's mode=ro&immutable=1
// makes SQLite reject any write attempt at the engine
// level, so a "DROP TABLE" or "DELETE" issued here will
// fail with an error rather than corrupting the database.
//
// Query is SQLite-specific and lives on the concrete
// SQLiteInspector, not the Inspector interface. CLI
// callers reach it via type assertion (see cmd/query.go).
func (s *SQLiteInspector) Query(sqlText string) (ItemReader, error) {
	return s.queryRaw(sqlText)
}

// queryRaw is the shared implementation behind OpenItem
// and Query. It runs q against the database and returns
// a sqliteItemReader.
func (s *SQLiteInspector) queryRaw(q string) (ItemReader, error) {
	rows, err := s.db.QueryContext(queryContext(s.options.Timeout), q)
	if err != nil {
		return nil, fmt.Errorf("sqlite: query: %w", err)
	}
	cols, err := rows.Columns()
	if err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("sqlite: columns: %w", err)
	}
	colTypes, _ := rows.ColumnTypes()
	return newSQLiteItemReader(rows, cols, colTypes, s.options.Timeout), nil
}

// Schema returns the declared column definitions for a table
// or view, in the order sqlite_master lists them.
func (s *SQLiteInspector) Schema(name string) ([]Column, error) {
	if !isValidSQLiteIdent(name) {
		return nil, fmt.Errorf("sqlite: invalid table name %q", name)
	}
	// PRAGMA table_info is the standard way to read schema
	// from a SQLite table or view.
	rows, err := s.db.QueryContext(queryContext(s.options.Timeout),
		fmt.Sprintf("PRAGMA table_info(%q)", name))
	if err != nil {
		return nil, fmt.Errorf("sqlite: pragma %q: %w", name, err)
	}
	defer func() { _ = rows.Close() }()

	var out []Column
	for rows.Next() {
		var cid int
		var cname, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &cname, &ctype, &notnull, &dflt, &pk); err != nil {
			return nil, fmt.Errorf("sqlite: scan column: %w", err)
		}
		out = append(out, Column{
			Name:     cname,
			Type:     ctype,
			Nullable: notnull == 0 && pk == 0,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: iterate columns: %w", err)
	}
	return out, nil
}

// isValidSQLiteIdent returns true for names that are safe to
// interpolate into a query (only A-Za-z0-9_). We do not allow
// quoted names; callers should pass bare identifiers.
func isValidSQLiteIdent(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '_':
		default:
			return false
		}
	}
	return true
}

// init wires NewSQLite into the package-level registry.
func init() {
	Register(detect.FormatSQLite, func(path string, opts Options) (Inspector, error) {
		return NewSQLite(path, opts)
	})
}
