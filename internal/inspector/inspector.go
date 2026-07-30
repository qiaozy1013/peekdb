// Package inspector provides read-only database accessors ("inspectors")
// for the formats peekdb supports.
//
// Design notes (see docs/architecture.md § 3.1 and docs/design-decisions.md
// ):
//
//   - The Inspector interface is intentionally small (lifecycle + status).
//   - Format-specific methods (e.g. SQLite.Query, Bolt.ForEachBucket) are
//     exposed on the concrete inspector type and reached via type assertion
//     in TUI/CLI code.
//   - All inspectors are read-only. The default build has no write APIs.
package inspector

import (
	"github.com/qiaozy1013/peekdb/internal/detect"
)

// Inspector is the minimum interface every format-specific inspector must
// implement. Format-specific methods live on the concrete type and are
// reached via type assertion.
type Inspector interface {
	// Close releases any underlying resources (DB handle, file descriptors).
	Close() error

	// Format returns the database format this inspector wraps.
	Format() detect.Format

	// Path returns the original file path passed to Open.
	Path() string

	// Stats returns diagnostic information for the TUI status bar and the
	// `inspect` CLI subcommand.
	Stats() Stats

	// Items returns the top-level items in the database (tables,
	// buckets, key groups). Format-specific kind tags (table,
	// bucket, view) are carried in each Item's Kind field.
	Items() ([]Item, error)

	// OpenItem opens one top-level item by its name (the same
	// name Items() reported) and returns a streaming reader
	// over its rows. All three v1 formats implement this;
	// the TUI/CLI reach it through this interface rather than
	// type assertion because the "open a named item" call
	// is genuinely common across all formats.
	OpenItem(name string) (ItemReader, error)
}

// Stats is the diagnostic snapshot shown in the TUI status bar and printed
// by `peekdb inspect`. All fields are best-effort; missing data is zero.
type Stats struct {
	// Size is the file size in bytes.
	Size int64

	// MTime is the file's last-modified time.
	MTime int64 // unix nanos; use time.Unix(0, s.MTime) to convert

	// NumItems is the count of top-level containers (tables, buckets, etc.).
	NumItems int

	// FormatVer is the format-specific version string (e.g. "3.39.4" for SQLite).
	FormatVer string

	// ReadMode is the session mode: "readonly" (direct open) or "copy" (Copy-on-Open).
	ReadMode string

	// LockState summarizes the detected lock: "none", "shared", "exclusive", "unknown".
	LockState string

	// CopyPath is the path to the temporary copy if ReadMode is "copy"; empty otherwise.
	CopyPath string

	// WALSize is the SQLite WAL file size in bytes; 0 for other formats.
	WALSize int64
}
