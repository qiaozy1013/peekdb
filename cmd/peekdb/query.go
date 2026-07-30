package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/qiaozy1013/peekdb/internal/detect"
	"github.com/qiaozy1013/peekdb/internal/inspector"
)

// queryCmd runs a read-only SQL query against a SQLite
// database. Only SQLite supports SQL; for bbolt and
// LevelDB the subcommand returns an error pointing the
// user to the TUI.
var queryCmd = &cobra.Command{
	Use:   "query <file> <sql>",
	Short: "Run a read-only SQL query against a SQLite database",
	Long: `query runs a read-only SQL query against a SQLite database and
prints the result rows in text or JSON form.

Examples:
  peekdb query ~/History 'SELECT url, title FROM urls LIMIT 5'
  peekdb query --json ~/state.db 'PRAGMA table_info(users)' | jq

Only SELECT / PRAGMA / WITH queries are guaranteed to work; the
database is opened with mode=ro&immutable=1 so any mutation
attempt fails at the SQLite engine level.`,
	Args: cobra.ExactArgs(2),
	RunE: runQuery,
}

func runQuery(cmd *cobra.Command, args []string) error {
	path, sqlText := args[0], args[1]

	// Detect the format early so we can give a precise
	// error when the user points query at a non-SQLite
	// file.
	format, err := detect.Detect(path)
	if err != nil {
		return err
	}
	if format != detect.FormatSQLite {
		return fmt.Errorf("query: only SQLite supports SQL; %s is %s. "+
			"Use the TUI to browse this file",
			path, format)
	}

	insp, err := inspector.Open(path, inspector.Options{})
	if err != nil {
		return err
	}
	defer func() { _ = insp.Close() }()

	// Type-assert to SQLiteInspector so we can call Query
	// (which is SQLite-specific, not on the Inspector
	// interface). This is the "small interface + type
	// assertion" pattern from docs/architecture.md § 3.1.
	sqlInsp, ok := insp.(*inspector.SQLiteInspector)
	if !ok {
		return errors.New("query: inspector is not SQLiteInspector (registry mismatch)")
	}
	rows, err := sqlInsp.Query(sqlText)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	out := cmd.OutOrStdout()
	first := true
	for rows.Next() {
		row := rows.Scan()
		if !first {
			_, _ = fmt.Fprintln(out)
		}
		first = false
		if rootFlags.JSON {
			if err := writeQueryRowJSON(out, row.Columns, row.Values); err != nil {
				return err
			}
		} else {
			writeQueryRowText(out, row.Columns, row.Values)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return nil
}

// writeQueryRowText prints a single row as "col=val" pairs,
// one per line. Suitable for human reading; for serious
// pipeline use, --json is recommended.
func writeQueryRowText(w io.Writer, cols []inspector.Column, values []any) {
	for i, c := range cols {
		if i >= len(values) {
			break
		}
		name := c.Name
		if name == "" {
			name = fmt.Sprintf("col%d", i)
		}
		_, _ = fmt.Fprintf(w, "%s=%v\n", name, values[i])
	}
}

// writeQueryRowJSON prints a single row as a JSON object
// with one key per column. We build a map[string]any with
// the column names as keys and the values as values, then
// hand it to encoding/json. (json.Marshal of a map has
// randomized key order, but here the JSON output is one
// object per row, so the per-object key order does not
// matter for downstream pipelines.)
func writeQueryRowJSON(w io.Writer, cols []inspector.Column, values []any) error {
	m := make(map[string]any, len(cols))
	for i, c := range cols {
		if i >= len(values) {
			break
		}
		name := c.Name
		if name == "" {
			name = fmt.Sprintf("col%d", i)
		}
		m[name] = values[i]
	}
	enc := json.NewEncoder(w)
	return enc.Encode(m)
}
