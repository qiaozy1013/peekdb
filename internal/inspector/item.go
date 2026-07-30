package inspector

// Item is a top-level container inside a database — a table in SQLite, a
// bucket in bbolt, a key-group in LevelDB. The TUI lists Items; the
// user picks one to drill into (which calls OpenItem).
//
// Item is intentionally generic: the format-specific details live in
// the inspector's Items() implementation. Meta is a free-form
// key/value bag for format-specific extras (e.g. SQLite's "sql" entry
// holding the CREATE TABLE statement).
type Item struct {
	// Name is the human identifier (table name, bucket name, ...).
	// Empty names are not allowed; the inspector implementation must
	// synthesize one when the underlying format does not provide it.
	Name string

	// Kind categorizes the item for the TUI ("table" / "bucket" /
	// "keygroup" / "view" / ...). Free-form string so future formats
	// can add their own without changing this package.
	Kind string

	// Size is a best-effort estimate of the item's on-disk footprint.
	// May be 0 when the format does not track per-item size cheaply
	// (e.g. LevelDB key groups).
	Size int64

	// Count is a best-effort estimate of the row/key count. May be 0
	// when the count is not available without a full scan.
	Count int64

	// Children lists nested item names (e.g. bbolt buckets within a
	// bucket). Empty for flat formats. Names are absolute paths
	// within the database; for bbolt this is a slash-separated
	// path (e.g. "users/active").
	Children []string

	// Meta carries format-specific extras. Always safe to read
	// individual keys with the comma-ok idiom against a nil map.
	Meta map[string]string
}

// Column describes one column in a row. Used by SQL inspectors
// (SQLite) where each row has a fixed schema. KV inspectors
// (bbolt, LevelDB) leave Columns nil.
type Column struct {
	// Name is the column name. Empty for positional access.
	Name string

	// Type is a textual type label: SQLite's declared type
	// ("INTEGER", "TEXT", ...), or a generic label such as "blob"
	// when the format does not have column types.
	Type string

	// Nullable reports whether the column allows NULL. Always true
	// when the underlying format cannot answer.
	Nullable bool
}

// Row is a single record returned by an ItemReader.
//
// Two layouts are supported, picked by the inspector:
//
//  1. KV layout (bbolt, LevelDB): Key + Value are populated,
//     Columns and Values are nil.
//
//  2. SQL layout (SQLite): Columns + Values are populated with
//     matching arity. Key/Value are nil.
//
// The TUI/CLI uses the inspector's concrete type (via type
// assertion) to know which layout applies.
type Row struct {
	// Key is the row's key for KV layouts. nil for SQL.
	Key []byte

	// Value is the row's value for KV layouts. nil for SQL.
	Value []byte

	// Columns describes the schema of Values for SQL layouts.
	// nil for KV.
	Columns []Column

	// Values holds the row's fields for SQL layouts. Each entry
	// is one of: nil (SQL NULL), []byte (BLOB), string, int64,
	// float64, time.Time, bool, or any other driver-native type.
	// nil for KV.
	Values []any
}

// ItemReader streams rows for a single Item. It is the read-only
// analog of a database/sql.Rows.
//
// Usage (mirroring database/sql):
//
//	it, err := insp.OpenItem(ctx, item)
//	for it.Next() {
//	    row := it.Scan()
//	    // ... use row.Key / row.Value or row.Columns / row.Values
//	}
//	if err := it.Err(); err != nil { ... }
//	it.Close()
type ItemReader interface {
	// Next advances to the next row. Returns false on end-of-stream
	// or error. Always call Err() after a false return to
	// disambiguate.
	Next() bool

	// Scan returns the current row. Only valid after Next() has
	// returned true. The returned Row's slices may be reused
	// across calls; copy them if retention is needed.
	Scan() Row

	// Err returns the first error encountered during iteration,
	// or nil.
	Err() error

	// Close releases any resources held by the reader. Safe to
	// call multiple times. Safe to call after Next has returned
	// false (the reader is then exhausted anyway).
	Close() error
}
