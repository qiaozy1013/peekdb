// Package encode handles character encoding detection and conversion
// for cell values, terminal output, and copy-on-open file names.
//
// peekdb assumes UTF-8 internally, but stored data may be UTF-16 (Windows
// API), GBK (legacy Chinese apps), Big5 (legacy Traditional Chinese apps),
// or Latin1 (legacy Western apps). The package provides per-column
// decoding for TUI display and encoding overrides for terminal output
// (notably Windows cmd, which defaults to GBK).
//
// See docs/architecture.md § 4.5.
package encode

import (
	"fmt"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/encoding/unicode"
)

// Encoding is a character encoding name.
type Encoding string

// Encoding names used to identify which character encoding a byte slice
// (or terminal output stream) should be decoded/encoded with. The default
// is UTF-8; the others cover legacy Windows APIs, legacy CJK apps, and
// the most common Latin encodings.
const (
	UTF8    Encoding = "utf-8"
	UTF16LE Encoding = "utf-16le"
	UTF16BE Encoding = "utf-16be"
	GBK     Encoding = "gbk"
	Big5    Encoding = "big5"
	Latin1  Encoding = "latin1"
)

// decoderFor returns the x/text encoding.Encoding for the
// given named Encoding, or nil if the name is unknown.
func decoderFor(e Encoding) encoding.Encoding {
	switch e {
	case UTF8:
		return unicode.UTF8
	case UTF16LE:
		return unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM)
	case UTF16BE:
		return unicode.UTF16(unicode.BigEndian, unicode.IgnoreBOM)
	case GBK:
		return simplifiedchinese.GBK
	case Big5:
		return traditionalchinese.Big5
	case Latin1:
		return charmap.ISO8859_1
	}
	return nil
}

// Decode converts bytes from the given encoding to a Go
// string (UTF-8). The result string is the canonical Go
// form, suitable for the TUI's preview pane and for any
// downstream JSON serialization.
//
// For UTF-8 the conversion goes through x/text's UTF-8
// decoder, which replaces invalid sequences with U+FFFD
// rather than aborting. The TUI does not break on a
// stray byte.
//
// For UTF-16 variants, the BOM is ignored (a real
// distinction matters only at file format boundaries, not
// at the cell level: by the time a cell reaches peekdb
// the byte order has been pinned by the source driver).
func Decode(b []byte, from Encoding) (string, error) {
	enc := decoderFor(from)
	if enc == nil {
		// Unknown encoding: best-effort treat as UTF-8.
		// x/text's UTF-8 decoder replaces invalid bytes
		// with U+FFFD so the rest of the string stays
		// readable.
		enc = unicode.UTF8
	}
	return enc.NewDecoder().String(string(b))
}

// Encode converts a Go string (UTF-8) to bytes in the
// given encoding. The inverse of Decode.
//
// Encode is intentionally NOT used for TUI rendering:
// the TUI expects UTF-8. It exists for terminal-output
// overrides (Windows cmd, which defaults to GBK) and for
// round-trip tests.
func Encode(s string, to Encoding) ([]byte, error) {
	enc := decoderFor(to)
	if enc == nil {
		// Unknown: best-effort treat as UTF-8.
		enc = unicode.UTF8
	}
	return enc.NewEncoder().Bytes([]byte(s))
}

// ParseEncoding normalises a free-form string ("utf-8",
// "UTF8", "utf_8", etc.) into one of the Encoding
// constants. Returns an error when the string does not
// match any known encoding.
func ParseEncoding(s string) (Encoding, error) {
	switch s {
	case "utf-8", "utf8", "UTF-8", "UTF8":
		return UTF8, nil
	case "utf-16le", "utf16le", "UTF-16LE", "UTF16LE", "utf-16-le":
		return UTF16LE, nil
	case "utf-16be", "utf16be", "UTF-16BE", "UTF16BE", "utf-16-be":
		return UTF16BE, nil
	case "gbk", "GBK", "gb2312", "GB2312", "cp936", "CP936":
		return GBK, nil
	case "big5", "Big5", "BIG5":
		return Big5, nil
	case "latin1", "Latin1", "LATIN1", "iso-8859-1", "ISO-8859-1":
		return Latin1, nil
	}
	return "", fmt.Errorf("encode: unknown encoding %q", s)
}

// (isValidUTF8 was removed: the x/text UTF-8 decoder
// already handles invalid sequences correctly, and
// reproducing that logic by hand is a bug magnet.)
