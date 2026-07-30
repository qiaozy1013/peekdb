package parse

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// jsonDecode parses b as JSON. The returned Decoded.Value
// is the unmarshalled Go value; Decoded.Value is wrapped so
// the TUI can pretty-print via String().
func jsonDecode(b []byte) Decoded {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		// json.Valid said this was valid; an Unmarshal
		// error here means a corner case (e.g. a number
		// bigger than float64). Fall back to a string
		// representation of the raw bytes.
		return Decoded{Type: TypeJSON, Value: string(b), Raw: b}
	}
	return Decoded{Type: TypeJSON, Value: v, Raw: b}
}

// formatJSON returns a pretty-printed JSON string with two-
// space indentation. It uses encoding/json's MarshalIndent
// after re-marshaling the already-decoded value, so it
// always succeeds (re-marshaling cannot fail for values
// produced by json.Unmarshal).
func formatJSON(v any) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		// Should not happen; bail to a simple string.
		return fmt.Sprintf("%v", v)
	}
	return buf.String()
}
