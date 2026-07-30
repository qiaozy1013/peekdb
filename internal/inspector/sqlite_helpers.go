package inspector

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"time"
)

// queryContext returns a context. The inspector uses the
// timeout via the DSN's _busy_timeout (set by buildSQLiteDSN)
// and via SQLite's own connection-level locking, not via the
// per-query context. Keeping it as a function lets us change
// that policy in one place if D5 (use-detection) ever wants
// per-query deadlines.
//
// The timeout argument is accepted for symmetry with the
// other helpers and ignored intentionally; the SQLite driver
// does its own timing.
func queryContext(_ time.Duration) context.Context {
	return context.Background()
}

// sqliteItemReader wraps a *sql.Rows and adapts it to
// inspector.ItemReader. Columns and ColumnTypes are captured
// at Open time (sql.Rows.Columns() must be called before
// the first Next, per the database/sql contract).
type sqliteItemReader struct {
	rows     *sql.Rows
	cols     []string
	colTypes []*sql.ColumnType
	values   []any
	scanDest []any
	row      Row
	err      error
	started  bool
	closed   bool
}

func newSQLiteItemReader(rows *sql.Rows, cols []string, colTypes []*sql.ColumnType, _ time.Duration) *sqliteItemReader {
	r := &sqliteItemReader{
		rows:     rows,
		cols:     cols,
		colTypes: colTypes,
	}
	r.scanDest = make([]any, len(cols))
	return r
}

// Next advances the cursor. Returns false at end-of-stream or
// on error. After Next() == true, Scan() returns the current
// row's values; the columns are fixed at Open time.
func (r *sqliteItemReader) Next() bool {
	if r.closed || r.err != nil {
		return false
	}
	if !r.rows.Next() {
		// Capture any error from the rows iteration. End of
		// stream yields a nil error, which is fine.
		if err := r.rows.Err(); err != nil {
			r.err = err
		}
		return false
	}
	r.started = true

	// Allocate a fresh values slice each row. The values
	// are driver-specific Go types (string, int64, float64,
	// []byte for BLOB, time.Time, nil for NULL). sql.Rows
	// copies into the provided destinations, so reusing
	// destinations across rows is safe.
	r.values = make([]any, len(r.cols))
	r.scanDest = r.scanDest[:0]
	for i := range r.cols {
		// Allocate a typed pointer matching the driver's
		// expected type when we know it; otherwise use *any
		// (interface{}) which sql.Scan supports.
		var v any
		if r.colTypes != nil && i < len(r.colTypes) {
			scanType := r.colTypes[i].ScanType()
			v = reflect.New(scanType).Interface()
		} else {
			var x any
			v = &x
		}
		r.scanDest = append(r.scanDest, v)
	}
	if err := r.rows.Scan(r.scanDest...); err != nil {
		r.err = fmt.Errorf("sqlite: scan row: %w", err)
		return false
	}
	// Convert the typed pointers back to interface values.
	for i, d := range r.scanDest {
		r.values[i] = reflect.ValueOf(d).Elem().Interface()
	}

	cols := make([]Column, len(r.cols))
	for i, name := range r.cols {
		typ := ""
		nullable := true
		if r.colTypes != nil && i < len(r.colTypes) {
			typ = r.colTypes[i].DatabaseTypeName()
			nullable, _ = r.colTypes[i].Nullable()
		}
		cols[i] = Column{
			Name:     name,
			Type:     typ,
			Nullable: nullable,
		}
	}
	r.row = Row{Columns: cols, Values: r.values}
	return true
}

// Scan returns the current row.
func (r *sqliteItemReader) Scan() Row { return r.row }

// Err returns the first error encountered during iteration.
func (r *sqliteItemReader) Err() error { return r.err }

// Close releases the underlying *sql.Rows. Idempotent.
func (r *sqliteItemReader) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true
	return r.rows.Close()
}
