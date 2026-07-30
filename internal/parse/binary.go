package parse

import (
	"fmt"
	"strings"
)

// binaryDecode represents b as a hex dump. The Decoded.Value
// is the original []byte (so callers can copy it or run
// further parsers); the String() method renders the dump
// for the TUI/CLI.
func binaryDecode(b []byte) Decoded {
	return Decoded{Type: TypeBinary, Value: b, Raw: b}
}

// formatBinary produces a classic hex dump:
//
//	0000  48 65 6c 6c 6f 2c 20 57  6f 72 6c 64 21 0a        |Hello, World!..|
//
// The output is multi-line; each line is 16 bytes. The
// right-hand column is a printable-text rendering, with
// non-printable bytes shown as dots. This is the same shape
// `xxd` and `hexdump -C` produce, which is what most
// developers expect.
func formatBinary(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	const lineWidth = 16
	var sb strings.Builder
	for i := 0; i < len(b); i += lineWidth {
		end := i + lineWidth
		if end > len(b) {
			end = len(b)
		}
		chunk := b[i:end]

		// Offset column.
		fmt.Fprintf(&sb, "%04x  ", i)

		// Hex columns: 8 bytes, gap, 8 bytes.
		for j := range lineWidth {
			if j < len(chunk) {
				fmt.Fprintf(&sb, "%02x ", chunk[j])
			} else {
				sb.WriteString("   ")
			}
			if j == 7 {
				sb.WriteString(" ")
			}
		}
		sb.WriteString(" |")
		for _, c := range chunk {
			if c >= 0x20 && c < 0x7f {
				sb.WriteByte(c)
			} else {
				sb.WriteByte('.')
			}
		}
		sb.WriteString("|\n")
	}
	return sb.String()
}
