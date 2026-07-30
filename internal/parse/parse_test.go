package parse_test

import (
	"strings"
	"testing"

	"github.com/qiaozy1013/peekdb/internal/parse"
)

func TestDetect_JSON(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want any
	}{
		{"object", `{"a": 1, "b": "two"}`, map[string]any{"a": float64(1), "b": "two"}},
		{"array", `[1, 2, 3]`, []any{float64(1), float64(2), float64(3)}},
		{"string", `"hello"`, "hello"},
		{"number", `42`, float64(42)},
		{"bool", `true`, true},
		{"null", `null`, nil},
		{"nested", `{"x": [true, null, 1.5]}`, map[string]any{"x": []any{true, nil, 1.5}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := parse.Detect([]byte(tt.in))
			if d.Type != parse.TypeJSON {
				t.Errorf("Type = %q, want %q", d.Type, parse.TypeJSON)
			}
			if d.Raw == nil {
				t.Errorf("Raw is nil; want the original bytes")
			}
		})
	}
}

func TestDetect_String(t *testing.T) {
	tests := []string{
		"hello, world",
		"multi\nline\ntext",
		"tab\there",
		"with-dashes_and_underscores.dots",
		"",
		"中文测试 unicode",
	}
	for _, in := range tests {
		t.Run(in, func(t *testing.T) {
			d := parse.Detect([]byte(in))
			// Empty input is a special case: Detect returns
			// TypeBinary with an empty value. The user can
			// tell that from the type.
			if in == "" {
				if d.Type != parse.TypeBinary {
					t.Errorf("empty input: Type = %q, want TypeBinary", d.Type)
				}
				return
			}
			if d.Type != parse.TypeString {
				t.Errorf("Type = %q, want %q", d.Type, parse.TypeString)
			}
			s, ok := d.Value.(string)
			if !ok {
				t.Errorf("Value type = %T, want string", d.Value)
			}
			if s != in {
				t.Errorf("Value = %q, want %q", s, in)
			}
		})
	}
}

func TestDetect_Binary(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
	}{
		{"embedded_nul", []byte("hello\x00world")},
		{"control_char", []byte{0x01, 0x02, 0x03}},
		{"invalid_utf8", []byte{0xff, 0xfe, 0xfd}},
		{"too_large", make([]byte, 100*1024)}, // > 64KiB cap
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := parse.Detect(tt.in)
			if d.Type != parse.TypeBinary {
				t.Errorf("Type = %q, want %q", d.Type, parse.TypeBinary)
			}
		})
	}
}

func TestDetect_JSON_InvalidFallsBack(t *testing.T) {
	// Looks like JSON-ish (curly braces) but is not valid;
	// must fall through to the string check.
	in := []byte("{not valid json}")
	d := parse.Detect(in)
	// The string is printable text (no NUL, valid UTF-8,
	// short), so it lands in TypeString. The TUI's preview
	// will show the raw text; the user can press a key
	// to flip to binary.
	if d.Type != parse.TypeString {
		t.Errorf("Type = %q, want %q", d.Type, parse.TypeString)
	}
}

func TestDecoded_String(t *testing.T) {
	t.Run("json_pretty", func(t *testing.T) {
		d := parse.Detect([]byte(`{"a": 1, "b": "two"}`))
		got := d.String()
		if !strings.Contains(got, "\"a\": 1") {
			t.Errorf("String() = %q, want pretty JSON with 'a': 1", got)
		}
		if !strings.Contains(got, "\"b\": \"two\"") {
			t.Errorf("String() = %q, want pretty JSON with 'b': 'two'", got)
		}
	})
	t.Run("string_asis", func(t *testing.T) {
		d := parse.Detect([]byte("hello world"))
		got := d.String()
		if got != "hello world" {
			t.Errorf("String() = %q, want %q", got, "hello world")
		}
	})
	t.Run("binary_hexdump", func(t *testing.T) {
		// "Hello, World!\n" -> the canonical 14-byte sample.
		// We construct a Decoded directly because Detect
		// would route this to TypeString (printable text);
		// the binary renderer needs to be exercisable
		// on its own.
		bd := parse.Decoded{
			Type:  parse.TypeBinary,
			Value: []byte("Hello, World!\n"),
			Raw:   []byte("Hello, World!\n"),
		}
		got := bd.String()
		if !strings.Contains(got, "48 65 6c 6c 6f") {
			t.Errorf("String() = %q, want hex dump with '48 65 6c 6c 6f'", got)
		}
		if !strings.Contains(got, "Hello, World!") {
			t.Errorf("String() = %q, want ASCII column with 'Hello, World!'", got)
		}
	})
}

func TestDecoded_Head(t *testing.T) {
	d := parse.Detect([]byte("this is a long string that should be truncated at some point"))
	head := d.Head(10)
	if !strings.HasPrefix(head, "this is a ") {
		t.Errorf("Head(10) = %q, want prefix 'this is a '", head)
	}
	if !strings.Contains(head, "truncated") {
		t.Errorf("Head(10) = %q, want 'truncated' marker", head)
	}

	// n larger than the full value returns the full value.
	full := d.Head(10000)
	if !strings.Contains(full, "truncated") {
		t.Errorf("Head(10000) = %q, want full value with no truncation", full)
	}
}
