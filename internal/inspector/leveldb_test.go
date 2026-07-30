package inspector_test

import (
	"path/filepath"
	"testing"

	"github.com/syndtr/goleveldb/leveldb"

	"github.com/qiaozy1013/peekdb/internal/detect"
	"github.com/qiaozy1013/peekdb/internal/inspector"
)

func TestLevelDB_NewLevelDB(t *testing.T) {
	resetRegistry(t)
	path := findTestdata(t, filepath.Join("leveldb", "empty"))
	insp, err := inspector.NewLevelDB(path, inspector.Options{})
	if err != nil {
		t.Fatalf("NewLevelDB: %v", err)
	}
	defer func() { _ = insp.Close() }()

	if got := insp.Format(); got != detect.FormatLevelDB {
		t.Errorf("Format = %q, want %q", got, detect.FormatLevelDB)
	}
	if got := insp.Path(); got != path {
		t.Errorf("Path = %q, want %q", got, path)
	}
}

func TestLevelDB_NewLevelDB_NotADir(t *testing.T) {
	resetRegistry(t)
	// Pass a file path; should error.
	path := findTestdata(t, filepath.Join("bbolt", "empty.db"))
	_, err := inspector.NewLevelDB(path, inspector.Options{})
	if err == nil {
		t.Errorf("NewLevelDB on a file: expected error, got nil")
	}
}

func TestLevelDB_NewLevelDB_MissingDir(t *testing.T) {
	resetRegistry(t)
	_, err := inspector.NewLevelDB(filepath.Join(t.TempDir(), "nope"), inspector.Options{})
	if err == nil {
		t.Errorf("NewLevelDB on missing dir: expected error, got nil")
	}
}

func TestLevelDB_Open_Dispatches(t *testing.T) {
	resetRegistry(t)
	inspector.MustRegister(detect.FormatLevelDB,
		func(path string, opts inspector.Options) (inspector.Inspector, error) {
			return inspector.NewLevelDB(path, opts)
		})
	path := findTestdata(t, filepath.Join("leveldb", "empty"))
	insp, err := inspector.Open(path, inspector.Options{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = insp.Close() }()
	if got := insp.Format(); got != detect.FormatLevelDB {
		t.Errorf("Format = %q, want %q", got, detect.FormatLevelDB)
	}
}

func TestLevelDB_Items(t *testing.T) {
	resetRegistry(t)
	path := findTestdata(t, filepath.Join("leveldb", "empty"))
	insp, err := inspector.NewLevelDB(path, inspector.Options{})
	if err != nil {
		t.Fatalf("NewLevelDB: %v", err)
	}
	defer func() { _ = insp.Close() }()

	items, _ := insp.Items()
	// The empty mock has one key: "hello" -> "world". The key has
	// no "/" so it falls into the byte-prefix group "0x68"
	// (0x68 = 'h' = first byte of "hello").
	if len(items) == 0 {
		t.Fatalf("Items() returned empty; expected at least one keygroup")
	}
	for _, it := range items {
		if it.Kind != "keygroup" {
			t.Errorf("Item %q: Kind = %q, want %q", it.Name, it.Kind, "keygroup")
		}
	}
}

func TestLevelDB_OpenItem(t *testing.T) {
	resetRegistry(t)
	path := findTestdata(t, filepath.Join("leveldb", "empty"))
	insp, err := inspector.NewLevelDB(path, inspector.Options{})
	if err != nil {
		t.Fatalf("NewLevelDB: %v", err)
	}
	defer func() { _ = insp.Close() }()

	items, _ := insp.Items()
	if len(items) == 0 {
		t.Skip("no items to test")
	}
	r, err := insp.OpenItem(items[0].Name)
	if err != nil {
		t.Fatalf("OpenItem(%q): %v", items[0].Name, err)
	}
	defer func() { _ = r.Close() }()
	// The empty mock has at least one key. Iterate; we don't
	// assert a specific count (the test generator may add
	// more keys in the future).
	got := 0
	for r.Next() {
		_ = r.Scan()
		got++
	}
	if err := r.Err(); err != nil {
		t.Errorf("Err() = %v, want nil", err)
	}
	if got == 0 {
		t.Errorf("OpenItem returned 0 rows; expected at least 1")
	}
}

func TestLevelDB_OpenItem_NotFound(t *testing.T) {
	resetRegistry(t)
	path := findTestdata(t, filepath.Join("leveldb", "empty"))
	insp, err := inspector.NewLevelDB(path, inspector.Options{})
	if err != nil {
		t.Fatalf("NewLevelDB: %v", err)
	}
	defer func() { _ = insp.Close() }()

	if _, err := insp.OpenItem("does-not-exist"); err == nil {
		t.Errorf("OpenItem(missing) returned nil error")
	}
}

func TestLevelDB_Stats(t *testing.T) {
	resetRegistry(t)
	path := findTestdata(t, filepath.Join("leveldb", "empty"))
	insp, err := inspector.NewLevelDB(path, inspector.Options{})
	if err != nil {
		t.Fatalf("NewLevelDB: %v", err)
	}
	defer func() { _ = insp.Close() }()

	stats := insp.Stats()
	if stats.MTime == 0 {
		t.Errorf("Stats.MTime = 0, want > 0")
	}
	if stats.ReadMode != "readonly" {
		t.Errorf("Stats.ReadMode = %q, want %q", stats.ReadMode, "readonly")
	}
	if stats.FormatVer == "" {
		t.Errorf("Stats.FormatVer empty, want 'leveldb'")
	}
}

func TestLevelDB_Close_Idempotent(t *testing.T) {
	resetRegistry(t)
	path := findTestdata(t, filepath.Join("leveldb", "empty"))
	insp, err := inspector.NewLevelDB(path, inspector.Options{})
	if err != nil {
		t.Fatalf("NewLevelDB: %v", err)
	}
	if err := insp.Close(); err != nil {
		t.Errorf("first Close: %v", err)
	}
	_ = insp.Close()
}

// TestLevelDB_OpenItem_EmptyKey is the regression test for C2 in
// . It builds a temp LevelDB with
// both an empty key and a non-empty key, then asserts that
// OpenItem("<empty>") returns exactly one row (the empty key),
// not the entire database.
//
// Before the C2 fix, levelDBGroup.prefix was nil for the "<empty>"
// group, which made OpenItem fall through to NewIterator(nil) and
// stream the whole DB. The fix stores an explicit *util.Range
// {[empty, 0x00)} that matches only the empty key.
func TestLevelDB_OpenItem_EmptyKey(t *testing.T) {
	resetRegistry(t)
	dir := t.TempDir()

	// Write two keys via the real library (write mode), then close.
	db, openErr := leveldb.OpenFile(dir, nil)
	if openErr != nil {
		t.Fatalf("leveldb.OpenFile: %v", openErr)
	}
	if putErr := db.Put([]byte(""), []byte("v-empty"), nil); putErr != nil {
		t.Fatalf("put empty key: %v", putErr)
	}
	if putErr := db.Put([]byte("alpha"), []byte("v-alpha"), nil); putErr != nil {
		t.Fatalf("put alpha key: %v", putErr)
	}
	if closeErr := db.Close(); closeErr != nil {
		t.Fatalf("close: %v", closeErr)
	}

	// Re-open via the inspector (read-only).
	insp, err := inspector.NewLevelDB(dir, inspector.Options{})
	if err != nil {
		t.Fatalf("NewLevelDB: %v", err)
	}
	defer func() { _ = insp.Close() }()

	items, _ := insp.Items()
	var emptyItem *inspector.Item
	for i := range items {
		if items[i].Name == "<empty>" {
			emptyItem = &items[i]
			break
		}
	}
	if emptyItem == nil {
		t.Fatalf("<empty> group not found; Items() = %+v", items)
	}
	if emptyItem.Count != 1 {
		t.Fatalf("<empty> group count = %d, want 1 (one empty key)", emptyItem.Count)
	}

	r, err := insp.OpenItem("<empty>")
	if err != nil {
		t.Fatalf("OpenItem(<empty>): %v", err)
	}
	defer func() { _ = r.Close() }()

	n := 0
	for r.Next() {
		n++
	}
	if err := r.Err(); err != nil {
		t.Errorf("Err() = %v, want nil", err)
	}
	if n != 1 {
		t.Errorf("OpenItem(<empty>) yielded %d rows; want exactly 1 (C2 fix)", n)
	}
}

// TestLevelDB_AfterClose is the regression test from
// . After Close, the inspector
// must not panic on Stats / Items / OpenItem. See the
// bbolt counterpart for the full rationale.
func TestLevelDB_AfterClose(t *testing.T) {
	resetRegistry(t)
	path := findTestdata(t, filepath.Join("leveldb", "empty"))
	insp, err := inspector.NewLevelDB(path, inspector.Options{})
	if err != nil {
		t.Fatalf("NewLevelDB: %v", err)
	}
	if err := insp.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("post-Close call panicked: %v", r)
		}
	}()
	_ = insp.Stats()
	_, _ = insp.Items()
	_, _ = insp.OpenItem("0x68")
}
