// Package parse decomposes opaque byte slices (the values stored in KV
// databases, or the raw cell bytes in SQL databases) into a human-readable
// form. The package is format-agnostic: it does not know or care whether
// the bytes came from SQLite, bbolt, or LevelDB.
//
// See docs/architecture.md § 4.4 and docs/design-decisions.md .
package parse

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Type is the result of probing a byte slice.
type Type string

const (
	// TypeJSON means the bytes parse as valid JSON.
	TypeJSON Type = "json"
	// TypeString means the bytes are valid printable text in the chosen encoding.
	TypeString Type = "string"
	// TypeBinary means the bytes are not recognized as any other type.
	TypeBinary Type = "binary"
)

// Decoded is the result of probing a byte slice.
type Decoded struct {
	// Type identifies which parser succeeded.
	Type Type

	// Value is the parsed representation. For TypeJSON this is a Go value
	// (map[string]any, []any, string, float64, bool, nil).
	// For TypeString this is the decoded string.
	// For TypeBinary this is the original []byte.
	Value any

	// Raw is the original byte slice, preserved so the TUI can re-render
	// the raw bytes on demand.
	Raw []byte
}

// String returns a human-readable rendering of the parsed
// value, suitable for the TUI's preview pane or `peekdb
// inspect` output. The exact form depends on Type:
//
//   - json:   pretty-printed with 2-space indent
//   - string: the decoded string itself
//   - binary: a hex dump (xxd-style)
//
// For very large values, callers should consider truncating
// or paging the output; the renderers do not impose a cap.
func (d Decoded) String() string {
	switch d.Type {
	case TypeJSON:
		return formatJSON(d.Value)
	case TypeString:
		s, _ := d.Value.(string)
		return s
	case TypeBinary:
		b, _ := d.Value.([]byte)
		return formatBinary(b)
	}
	return fmt.Sprintf("parse: unknown type %q", d.Type)
}

// Head returns the first n bytes of the rendered output,
// followed by a marker if the full output is longer. This
// is what the TUI's preview pane should call when the user
// is looking at a large value.
func (d Decoded) Head(n int) string {
	full := d.String()
	if n <= 0 || len(full) <= n {
		return full
	}
	if !strings.HasSuffix(full, "\n") {
		// Avoid splitting mid-line where possible.
		if idx := strings.LastIndexByte(full[:n], '\n'); idx > 0 {
			return full[:idx] + "\n[truncated]"
		}
	}
	return full[:n] + "…[truncated]"
}

// Detect tries each parser in order and returns the first
// successful result. The order is:
//
//  1. JSON: cheap json.Valid check, then full unmarshal
//  2. String: UTF-8 valid + no embedded NUL/control chars
//     + bounded size (64 KiB)
//  3. Binary: fallback (always succeeds)
//
// An empty input returns TypeBinary with no value (caller
// can decide how to render an empty cell).
func Detect(b []byte) Decoded {
	if len(b) == 0 {
		return Decoded{Type: TypeBinary, Value: b, Raw: b}
	}
	if json.Valid(b) {
		return jsonDecode(b)
	}
	if isPrintable(b) {
		return stringDecode(b)
	}
	return binaryDecode(b)
}
