package parse

import "unicode/utf8"

// stringDecode returns a string representation of b. The
// caller (Detect) only calls this after isPrintable so we
// know the bytes are valid UTF-8 text without embedded
// control characters.
func stringDecode(b []byte) Decoded {
	return Decoded{Type: TypeString, Value: string(b), Raw: b}
}

// isPrintable reports whether b is a "string-like" payload:
// valid UTF-8, contains no embedded NUL or other ASCII
// control characters (other than \t, \n, \r), and is
// reasonably short. The size limit keeps the heuristic
// cheap for large blobs; users who want to inspect a
// 10 MB text dump can switch encoding to UTF-8 manually.
func isPrintable(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	// Hard cap at 64 KiB: beyond that, JSON validation
	// is cheaper and more reliable than the printable
	// heuristic anyway, and the user almost always
	// wanted binary.
	if len(b) > 64*1024 {
		return false
	}
	if !utf8.Valid(b) {
		return false
	}
	for _, r := range string(b) {
		if r == 0 {
			// Embedded NUL: almost certainly binary.
			return false
		}
		if r < 0x20 && r != '\t' && r != '\n' && r != '\r' {
			return false
		}
		if r == 0x7f {
			// DEL: not normally in text.
			return false
		}
	}
	return true
}
