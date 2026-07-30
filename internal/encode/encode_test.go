package encode_test

import (
	"strings"
	"testing"

	"github.com/qiaozy1013/peekdb/internal/encode"
)

func TestDecode_UTF8(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{"ascii", "hello, world"},
		{"with_newlines", "line1\nline2\nline3"},
		{"chinese", "你好，世界"},
		{"emoji", "👋 🌍"},
		{"empty", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := encode.Decode([]byte(tt.in), encode.UTF8)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if got != tt.in {
				t.Errorf("Decode = %q, want %q", got, tt.in)
			}
		})
	}
}

func TestDecode_UTF16LE(t *testing.T) {
	// "hello" in UTF-16LE is the byte sequence:
	//   68 00 65 00 6c 00 6c 00 6f 00
	in := []byte{0x68, 0x00, 0x65, 0x00, 0x6c, 0x00, 0x6c, 0x00, 0x6f, 0x00}
	got, err := encode.Decode(in, encode.UTF16LE)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got != "hello" {
		t.Errorf("Decode = %q, want %q", got, "hello")
	}
}

func TestDecode_UTF16BE(t *testing.T) {
	// "hello" in UTF-16BE is the byte sequence:
	//   00 68 00 65 00 6c 00 6c 00 6f
	in := []byte{0x00, 0x68, 0x00, 0x65, 0x00, 0x6c, 0x00, 0x6c, 0x00, 0x6f}
	got, err := encode.Decode(in, encode.UTF16BE)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got != "hello" {
		t.Errorf("Decode = %q, want %q", got, "hello")
	}
}

func TestDecode_GBK(t *testing.T) {
	// "你好" in GBK is 0xC4 0xE3 0xBA 0xC3.
	in := []byte{0xC4, 0xE3, 0xBA, 0xC3}
	got, err := encode.Decode(in, encode.GBK)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got != "你好" {
		t.Errorf("Decode = %q, want %q", got, "你好")
	}
}

func TestDecode_Latin1(t *testing.T) {
	// "café" in Latin1: c=0x63, a=0x61, f=0x66, é=0xE9
	in := []byte{0x63, 0x61, 0x66, 0xE9}
	got, err := encode.Decode(in, encode.Latin1)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got != "café" {
		t.Errorf("Decode = %q, want %q", got, "café")
	}
}

func TestDecode_RoundTrip(t *testing.T) {
	// Encoding then decoding should be identity for the
	// encodings that round-trip cleanly.
	cases := []struct {
		enc  encode.Encoding
		text string
	}{
		{encode.UTF8, "hello"},
		{encode.UTF8, "中文 mixed english"},
		{encode.Latin1, "café"},
	}
	for _, tt := range cases {
		t.Run(string(tt.enc), func(t *testing.T) {
			b, err := encode.Encode(tt.text, tt.enc)
			if err != nil {
				t.Fatalf("Encode: %v", err)
			}
			got, err := encode.Decode(b, tt.enc)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if got != tt.text {
				t.Errorf("round trip = %q, want %q", got, tt.text)
			}
		})
	}
}

func TestParseEncoding(t *testing.T) {
	tests := []struct {
		in   string
		want encode.Encoding
		err  bool
	}{
		{"utf-8", encode.UTF8, false},
		{"UTF8", encode.UTF8, false},
		{"utf16le", encode.UTF16LE, false},
		{"UTF-16LE", encode.UTF16LE, false},
		{"gbk", encode.GBK, false},
		{"GB2312", encode.GBK, false},
		{"CP936", encode.GBK, false},
		{"big5", encode.Big5, false},
		{"latin1", encode.Latin1, false},
		{"ISO-8859-1", encode.Latin1, false},
		{"unknown-encoding", "", true},
		{"", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := encode.ParseEncoding(tt.in)
			if (err != nil) != tt.err {
				t.Errorf("err = %v, want error=%v", err, tt.err)
			}
			if !tt.err && got != tt.want {
				t.Errorf("ParseEncoding(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestEncodingConstantsAreDistinct(t *testing.T) {
	// Guard against accidentally giving two encodings
	// the same string value (which would break the
	// type-assertion pattern in the TUI).
	all := []encode.Encoding{
		encode.UTF8, encode.UTF16LE, encode.UTF16BE,
		encode.GBK, encode.Big5, encode.Latin1,
	}
	seen := make(map[encode.Encoding]bool, len(all))
	for _, e := range all {
		if e == "" {
			t.Errorf("encoding constant has empty string value")
		}
		if seen[e] {
			t.Errorf("encoding %q is duplicated", e)
		}
		seen[e] = true
	}
}

func TestDecode_InvalidUTF8_ReplacesNotErrors(t *testing.T) {
	// A stray 0xFF in a UTF-8 cell should not abort
	// the TUI rendering. The decoder replaces it with
	// U+FFFD; the resulting string contains that rune.
	in := []byte{'h', 'i', 0xFF, '!'}
	got, err := encode.Decode(in, encode.UTF8)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !strings.Contains(got, "\uFFFD") {
		t.Errorf("Decode = %q, want it to contain U+FFFD replacement", got)
	}
	if !strings.HasPrefix(got, "hi") || !strings.HasSuffix(got, "!") {
		t.Errorf("Decode = %q, want 'hi' prefix and '!' suffix", got)
	}
}
