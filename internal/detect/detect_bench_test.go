package detect_test

import (
	"testing"

	"github.com/qiaozy1013/peekdb/internal/detect"
)

// BenchmarkDetect measures the latency of Detect() on a typical
// SQLite-shaped file (the chrome-history mock, ~4 KB). Most of the
// cost should be the magic-byte comparison, which is O(16).
func BenchmarkDetect_SQLite(b *testing.B) {
	path := testdataPath(&testing.T{}, "sqlite/chrome-history.db")
	b.ResetTimer()
	for range b.N {
		f, err := detect.Detect(path)
		if err != nil {
			b.Fatal(err)
		}
		if f != detect.FormatSQLite {
			b.Fatalf("got %q, want sqlite", f)
		}
	}
}

// BenchmarkDetect_LevelDB exercises the directory probe path.
func BenchmarkDetect_LevelDB(b *testing.B) {
	path := testdataPath(&testing.T{}, "leveldb/empty")
	b.ResetTimer()
	for range b.N {
		f, err := detect.Detect(path)
		if err != nil {
			b.Fatal(err)
		}
		if f != detect.FormatLevelDB {
			b.Fatalf("got %q, want leveldb", f)
		}
	}
}

// BenchmarkDetect_Unknown exercises the negative path: the file
// must be opened (no strong magic) and probed with bbolt (rejected).
func BenchmarkDetect_Unknown(b *testing.B) {
	path := testdataPath(&testing.T{}, "negative/random.bin")
	b.ResetTimer()
	for range b.N {
		f, err := detect.Detect(path)
		if err == nil {
			b.Fatalf("expected error, got format %q", f)
		}
		if f != detect.FormatUnknown {
			b.Fatalf("got %q, want unknown", f)
		}
	}
}
