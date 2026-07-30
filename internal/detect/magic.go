// Package detect identifies the format of a database file by inspecting
// its magic bytes and, when needed, by probing with format-specific
// open calls.
//
// The package is intentionally read-only: it never opens a file for
// writing and never modifies the file under inspection.
//
// See docs/architecture.md § 4.1 for the design and
// docs/mvp-spec.md § 5.1 (D2) for the acceptance criteria.
package detect

// Format is a database format identifier.
type Format string

// Known formats. Add new formats here as they are implemented.
const (
	FormatUnknown Format = ""
	FormatSQLite  Format = "sqlite"
	FormatBolt    Format = "bbolt"
	FormatLevelDB Format = "leveldb"
)

// magicHeader is the number of bytes read from the head of a file to
// look for format-specific signatures. 16 bytes is enough for SQLite
// (which uses a 16-byte magic) and for bbolt's page-id prefix.
const magicHeader = 16
